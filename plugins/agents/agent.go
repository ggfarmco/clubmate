package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"clubmate/plugins/agents/acp"
)

const (
	stdinGrace    = 3 * time.Second
	termGrace     = 2 * time.Second
	drainWait     = time.Second
	stderrCap     = 64 << 10
	stderrTailCap = 4 << 10
	clientName    = "clubmate"
	clientVersion = "0.1.0"
	maxOrphans    = 1000
)

var (
	errAgentExited = errors.New("agent exited")
)

type launch struct {
	argv []string
	env  []string
	mcp  []acp.McpServer
}

type agentProc struct {
	agent    string
	root     string
	cmd      *exec.Cmd
	pgid     int
	stdin    *os.File
	conn     *acp.Conn
	stderr   ring
	caps     acp.AgentCapabilities
	steering bool
	auth     []acp.AuthMethod
	launch   launch
	mcp      []acp.McpServer
	onExit   func(p *agentProc, reason string)

	mu       sync.Mutex
	sessions map[string]*session
	orphans  map[string][]json.RawMessage

	quiet    atomic.Bool
	stopOnce sync.Once
	exited   chan struct{}
}

type ring struct {
	mu  sync.Mutex
	buf []byte
}

func startProc(l launch, agent, root string, onExit func(p *agentProc, reason string)) (*agentProc, error) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		closeAll(stdinR, stdinW)
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeAll(stdinR, stdinW, stdoutR, stdoutW)
		return nil, err
	}
	cmd := exec.Command(l.argv[0], l.argv[1:]...)
	cmd.Dir = root
	cmd.Env = l.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		closeAll(stdinR, stdinW, stdoutR, stdoutW, stderrR, stderrW)
		return nil, fmt.Errorf("start %s: %w", l.argv[0], err)
	}
	closeAll(stdinR, stdoutW, stderrW)
	p := &agentProc{agent: agent, root: root, cmd: cmd, pgid: cmd.Process.Pid, stdin: stdinW, launch: l, mcp: l.mcp, onExit: onExit, sessions: map[string]*session{}, orphans: map[string][]json.RawMessage{}, exited: make(chan struct{})}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		buf := make([]byte, 32<<10)
		for {
			n, err := stderrR.Read(buf)
			_, _ = p.stderr.Write(buf[:n])
			if err != nil {
				return
			}
		}
	}()
	p.conn = acp.NewConn(stdoutR, stdinW, p, func(line []byte) {
		fmt.Fprintf(os.Stderr, "agents: %s: skipped non-JSON line: %.200s\n", agent, line)
	})
	p.conn.Start()
	go p.wait(stdoutR, stderrR, stderrDone)
	return p, nil
}

func (p *agentProc) wait(stdout, stderr *os.File, stderrDone chan struct{}) {
	err := p.cmd.Wait()
	_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
	select {
	case <-stderrDone:
	case <-time.After(drainWait):
	}
	select {
	case <-p.conn.Done():
	case <-time.After(drainWait):
	}
	closeAll(stdout, stderr, p.stdin)
	close(p.exited)
	reason := errAgentExited.Error()
	if err != nil {
		reason += ": " + err.Error()
	}
	if tail := p.stderr.tail(); tail != "" {
		reason += "\n" + tail
	}
	if p.onExit != nil {
		p.onExit(p, reason)
	}
}

func (p *agentProc) initialize(ctx context.Context) error {
	req := acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersion,
		ClientCapabilities: acp.ClientCapabilities{Session: acp.ClientSessionCapabilities{Notices: &acp.Capability{}}, Auth: &acp.AuthCapabilities{Terminal: true}},
		ClientInfo:         &acp.Implementation{Name: clientName, Version: clientVersion},
	}
	var res acp.InitializeResponse
	if err := p.conn.Call(ctx, acp.MethodInitialize, req, &res); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if res.ProtocolVersion != acp.ProtocolVersion {
		return fmt.Errorf("initialize: unsupported ACP protocol version %d", res.ProtocolVersion)
	}
	p.caps = res.AgentCapabilities
	p.steering = res.Meta != nil && res.Meta.Steering.Supported
	for _, raw := range res.AuthMethods {
		var m acp.AuthMethod
		if json.Unmarshal(raw, &m) == nil && m.ID != "" {
			p.auth = append(p.auth, m)
		}
	}
	return nil
}

func (p *agentProc) terminalAuth() (authCommand, bool) {
	for _, m := range p.auth {
		if m.Type != acp.AuthTerminal {
			continue
		}
		env := make([]string, 0, len(m.Env))
		for k, v := range m.Env {
			if k != apiKeyEnv {
				env = append(env, k+"="+v)
			}
		}
		return authCommand{argv: append(slices.Clone(p.launch.argv), m.Args...), env: env, label: m.Name}, true
	}
	return authCommand{}, false
}

func (p *agentProc) Notify(method string, params json.RawMessage) {
	if method == acp.MethodCancelRequest {
		var n acp.CancelRequestNotification
		if json.Unmarshal(params, &n) == nil {
			request := acp.RequestKey(n.RequestID)
			for _, s := range p.attached() {
				if s.cancelRequest(request) {
					return
				}
			}
		}
		return
	}
	if method != acp.MethodSessionUpdate {
		return
	}
	var n acp.SessionNotification
	if json.Unmarshal(params, &n) != nil {
		return
	}
	p.mu.Lock()
	s, ok := p.sessions[n.SessionID]
	if !ok {
		queue := p.orphans[n.SessionID]
		if len(queue) >= maxOrphans {
			fmt.Fprintf(os.Stderr, "agents: %s: dropped an update for unknown session %.64s\n", p.agent, n.SessionID)
			queue = queue[1:]
		}
		p.orphans[n.SessionID] = append(queue, params)
	}
	p.mu.Unlock()
	if ok {
		s.update(params)
	}
}

func (p *agentProc) Request(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	if method != acp.MethodRequestPermission {
		return nil, acp.MethodNotFound(method)
	}
	var req acp.RequestPermissionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &acp.Error{Code: -32602, Message: err.Error()}
	}
	p.mu.Lock()
	s, ok := p.sessions[req.SessionID]
	p.mu.Unlock()
	if !ok {
		return acp.RequestPermissionResponse{Outcome: acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}}, nil
	}
	return acp.RequestPermissionResponse{Outcome: s.requestPermission(ctx, id, params)}, nil
}

func (p *agentProc) register(agentSessionID string, s *session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessions[agentSessionID] = s
	s.mu.Lock()
	s.proc = p
	s.rec.Suspended = false
	s.mu.Unlock()
	pending := p.orphans[agentSessionID]
	delete(p.orphans, agentSessionID)
	for _, params := range pending {
		s.update(params)
	}
}

func (p *agentProc) unregister(agentSessionID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, agentSessionID)
	return len(p.sessions)
}

func (p *agentProc) attached() []*session {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		out = append(out, s)
	}
	return out
}

func (p *agentProc) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

func (p *agentProc) shutdown() {
	p.quiet.Store(true)
	p.stop()
}

func (p *agentProc) stop() {
	p.stopOnce.Do(func() {
		p.stdin.Close()
		if p.waitExit(stdinGrace) {
			return
		}
		_ = syscall.Kill(-p.pgid, syscall.SIGTERM)
		if p.waitExit(termGrace) {
			return
		}
		_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
	})
	<-p.exited
}

func (p *agentProc) waitExit(d time.Duration) bool {
	select {
	case <-p.exited:
		return true
	case <-time.After(d):
		return false
	}
}

func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if over := len(r.buf) - stderrCap; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	return len(p), nil
}

func (r *ring) tail() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.buf
	if len(b) > stderrTailCap {
		b = b[len(b)-stderrTailCap:]
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(b), ""))
}

func closeAll(files ...*os.File) {
	for _, f := range files {
		f.Close()
	}
}
