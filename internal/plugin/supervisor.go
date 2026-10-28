package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/buildinfo"
	"clubmate/internal/paths"
	"clubmate/internal/rpc"
	"clubmate/internal/sessionhost"
)

const (
	StateStarting = "starting"
	StateRunning  = "running"
	StateBackoff  = "backoff"
	StateFailed   = "failed"
	StateStopped  = "stopped"

	defaultBackoffBase = time.Second
	defaultBackoffMax  = 30 * time.Second
	defaultCrashLimit  = 5
	defaultCrashWindow = time.Minute

	initializeTimeout      = 5 * time.Second
	shutdownRequestTimeout = 2 * time.Second
	termAfter              = 2 * time.Second
	killAfter              = 5 * time.Second
	crashKillGrace         = 500 * time.Millisecond

	pluginIDEnv   = "CLUBMATE_PLUGIN_ID"
	runtimeDirEnv = "CLUBMATE_PLUGIN_RUNTIME_DIR"
)

var (
	errExited     = errors.New("plugin exited")
	errConnClosed = errors.New("plugin connection closed")
)

type Status struct {
	ID        string   `json:"id"`
	State     string   `json:"state"`
	PID       int      `json:"pid"`
	Restarts  int      `json:"restarts"`
	LastError string   `json:"lastError"`
	Manifest  Manifest `json:"manifest"`
}

type SupervisorOption func(*Supervisor)

type Supervisor struct {
	srv         *rpc.Server
	dirs        paths.Dirs
	logger      *slog.Logger
	backoffBase time.Duration
	backoffMax  time.Duration
	crashLimit  int
	crashWindow time.Duration
	onStop      func(id string)

	hurryCtx context.Context
	hurry    context.CancelFunc
	wg       sync.WaitGroup

	mu      sync.Mutex
	cancel  context.CancelFunc
	plugins map[string]*instance
}

type instance struct {
	status  Status
	conn    *jsonrpc2.Conn
	crashes []time.Time
}

type process struct {
	cmd        *exec.Cmd
	conn       *jsonrpc2.Conn
	cancelConn context.CancelFunc
	exited     chan struct{}
	waitErr    error
}

type pipeStream struct {
	r *os.File
	w *os.File
}

type initializeParams struct {
	Protocol    int    `json:"protocol"`
	PluginID    string `json:"pluginId"`
	CoreVersion string `json:"coreVersion"`
}

type initializeResult struct {
	Protocol int `json:"protocol"`
}

func WithBackoff(base, maxDelay time.Duration) SupervisorOption {
	return func(s *Supervisor) { s.backoffBase, s.backoffMax = base, maxDelay }
}

func WithCrashLimit(n int, window time.Duration) SupervisorOption {
	return func(s *Supervisor) { s.crashLimit, s.crashWindow = n, window }
}

func WithOnStop(fn func(id string)) SupervisorOption {
	return func(s *Supervisor) { s.onStop = fn }
}

func NewSupervisor(srv *rpc.Server, dirs paths.Dirs, logger *slog.Logger, opts ...SupervisorOption) *Supervisor {
	s := &Supervisor{
		srv:         srv,
		dirs:        dirs,
		logger:      logger,
		backoffBase: defaultBackoffBase,
		backoffMax:  defaultBackoffMax,
		crashLimit:  defaultCrashLimit,
		crashWindow: defaultCrashWindow,
		plugins:     map[string]*instance{},
	}
	s.hurryCtx, s.hurry = context.WithCancel(context.Background())
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Supervisor) Start(ctx context.Context, manifests []Manifest) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		s.logger.Warn("plugin supervisor already started, ignoring Start")
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	for _, m := range manifests {
		s.plugins[m.ID] = &instance{status: Status{ID: m.ID, State: StateStarting, Manifest: m}}
	}
	s.mu.Unlock()
	for _, m := range manifests {
		s.wg.Go(func() { s.supervise(ctx, m) })
	}
}

func (s *Supervisor) Stop(ctx context.Context) {
	defer s.hurry()
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		s.hurry()
		<-done
	}
}

func (s *Supervisor) Conn(id string) (*jsonrpc2.Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.plugins[id]
	if !ok || inst.status.State != StateRunning || inst.conn == nil {
		return nil, false
	}
	return inst.conn, true
}

func (s *Supervisor) Statuses() []Status {
	s.mu.Lock()
	out := make([]Status, 0, len(s.plugins))
	for _, inst := range s.plugins {
		st := inst.status
		st.Manifest.Contributes.Sections = nonNil(st.Manifest.Contributes.Sections)
		st.Manifest.Contributes.Commands = nonNil(st.Manifest.Contributes.Commands)
		out = append(out, st)
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b Status) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (s *Supervisor) PIDs() []int {
	s.mu.Lock()
	var pids []int
	for _, inst := range s.plugins {
		if inst.status.PID > 0 {
			pids = append(pids, inst.status.PID)
		}
	}
	s.mu.Unlock()
	slices.Sort(pids)
	return pids
}

func (s *Supervisor) supervise(ctx context.Context, m Manifest) {
	for {
		delay, restart := s.attempt(ctx, m)
		if !restart {
			return
		}
		if !sleep(ctx, delay) {
			s.update(m.ID, func(inst *instance) { inst.status.State = StateStopped })
			return
		}
	}
}

func (s *Supervisor) attempt(ctx context.Context, m Manifest) (time.Duration, bool) {
	s.update(m.ID, func(inst *instance) { inst.status.State = StateStarting })
	p, err := s.spawn(m)
	if err != nil {
		return s.settle(ctx, m.ID, err)
	}
	s.update(m.ID, func(inst *instance) { inst.status.PID = p.cmd.Process.Pid })
	err = s.serve(ctx, m, p)
	p.cancelConn()
	delay, restart := s.settle(ctx, m.ID, err)
	if s.onStop != nil {
		s.onStop(m.ID)
	}
	return delay, restart
}

func (s *Supervisor) spawn(m Manifest) (*process, error) {
	logFile, err := os.OpenFile(s.dirs.PluginLog(m.ID), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open plugin log: %w", err)
	}
	defer logFile.Close()
	runtimeDir := s.dirs.PluginRuntime(m.ID)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return nil, fmt.Errorf("create plugin runtime dir: %w", err)
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	defer stdinR.Close()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		stdinW.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	defer stdoutW.Close()
	cmd := exec.Command(m.Executable(), m.Command[1:]...)
	cmd.Dir = m.Dir
	cmd.Env = append(sessionhost.StripMarkers(os.Environ()), pluginIDEnv+"="+m.ID, runtimeDirEnv+"="+runtimeDir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		stdinW.Close()
		stdoutR.Close()
		return nil, fmt.Errorf("start %s: %w", m.Executable(), err)
	}
	p := &process{cmd: cmd, exited: make(chan struct{})}
	go p.wait()
	connCtx, cancel := context.WithCancel(context.Background())
	p.cancelConn = cancel
	p.conn = s.srv.ServeConn(connCtx, jsonrpc2.NewPlainObjectStream(pipeStream{r: stdoutR, w: stdinW}), rpc.Caller{PluginID: m.ID})
	return p, nil
}

func (s *Supervisor) serve(ctx context.Context, m Manifest, p *process) error {
	if err := initialize(ctx, m, p.conn); err != nil {
		if ctx.Err() != nil {
			s.shutdown(m.ID, p)
			return nil
		}
		return p.abort(err)
	}
	s.update(m.ID, func(inst *instance) {
		inst.status.State = StateRunning
		inst.conn = p.conn
	})
	s.logger.Info("plugin running", "id", m.ID, "pid", p.cmd.Process.Pid)
	select {
	case <-ctx.Done():
		s.shutdown(m.ID, p)
		return nil
	case <-p.exited:
		return p.abort(errExited)
	case <-p.conn.DisconnectNotify():
		return p.abort(errConnClosed)
	}
}

func (s *Supervisor) shutdown(id string, p *process) {
	ctx, cancel := context.WithTimeout(s.hurryCtx, shutdownRequestTimeout)
	_, err := callConn(ctx, p.conn, id, "plugin.shutdown", json.RawMessage("{}"))
	cancel()
	if err != nil {
		s.logger.Debug("plugin shutdown request", "pid", p.cmd.Process.Pid, "err", err)
	}
	p.conn.Close()
	if p.waitExit(termAfter, s.hurryCtx.Done()) {
		return
	}
	p.signal(syscall.SIGTERM)
	if p.waitExit(killAfter, s.hurryCtx.Done()) {
		return
	}
	p.signal(syscall.SIGKILL)
	<-p.exited
}

func (s *Supervisor) settle(ctx context.Context, id string, cause error) (time.Duration, bool) {
	s.mu.Lock()
	inst := s.plugins[id]
	inst.conn = nil
	inst.status.PID = 0
	if ctx.Err() != nil {
		inst.status.State = StateStopped
		s.mu.Unlock()
		return 0, false
	}
	now := time.Now()
	inst.crashes = append(slices.DeleteFunc(inst.crashes, func(t time.Time) bool { return now.Sub(t) >= s.crashWindow }), now)
	inst.status.Restarts++
	inst.status.LastError = cause.Error()
	recent, restarts := len(inst.crashes), inst.status.Restarts
	if recent >= s.crashLimit {
		inst.status.State = StateFailed
		s.mu.Unlock()
		s.logger.Error("plugin failed, giving up", "id", id, "err", cause, "restarts", restarts)
		return 0, false
	}
	inst.status.State = StateBackoff
	s.mu.Unlock()
	delay := s.backoff(recent)
	s.logger.Warn("plugin crashed", "id", id, "err", cause, "restarts", restarts, "retryIn", delay)
	return delay, true
}

func (s *Supervisor) backoff(crashes int) time.Duration {
	d := s.backoffBase
	for range crashes - 1 {
		if d >= s.backoffMax {
			break
		}
		d *= 2
	}
	return min(d, s.backoffMax)
}

func (s *Supervisor) update(id string, fn func(inst *instance)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.plugins[id])
}

func initialize(ctx context.Context, m Manifest, conn *jsonrpc2.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	var res initializeResult
	params := initializeParams{Protocol: ProtocolVersion, PluginID: m.ID, CoreVersion: buildinfo.String()}
	if err := conn.Call(ctx, "plugin.initialize", params, &res); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if res.Protocol != ProtocolVersion {
		return fmt.Errorf("initialize: plugin speaks protocol %d, want %d", res.Protocol, ProtocolVersion)
	}
	return nil
}

func (p *process) wait() {
	p.waitErr = p.cmd.Wait()
	p.signal(syscall.SIGKILL)
	close(p.exited)
}

func (p *process) abort(cause error) error {
	p.conn.Close()
	if !p.waitExit(crashKillGrace, nil) {
		p.signal(syscall.SIGKILL)
		<-p.exited
		return cause
	}
	if p.waitErr != nil {
		return fmt.Errorf("%w: %v", errExited, p.waitErr)
	}
	return errExited
}

func (p *process) waitExit(d time.Duration, hurry <-chan struct{}) bool {
	select {
	case <-p.exited:
		return true
	default:
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-p.exited:
		return true
	case <-t.C:
		return false
	case <-hurry:
		return false
	}
}

func (p *process) signal(sig syscall.Signal) {
	pid := p.cmd.Process.Pid
	if pid <= 1 {
		return
	}
	_ = syscall.Kill(-pid, sig)
}

func (ps pipeStream) Read(b []byte) (int, error) {
	return ps.r.Read(b)
}

func (ps pipeStream) Write(b []byte) (int, error) {
	return ps.w.Write(b)
}

func (ps pipeStream) Close() error {
	return errors.Join(ps.w.Close(), ps.r.Close())
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
