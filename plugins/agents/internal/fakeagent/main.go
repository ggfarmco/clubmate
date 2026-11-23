package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/contract"
	"clubmate/plugins/agents/acp"
)

type script struct {
	AuthFile              string                    `json:"authFile"`
	Resume                bool                      `json:"resume"`
	Fork                  bool                      `json:"fork"`
	Linger                bool                      `json:"linger"`
	Embedded              bool                      `json:"embedded"`
	Image                 bool                      `json:"image"`
	CloseSleep            int                       `json:"closeSleep"`
	NewSleep              int                       `json:"newSleep"`
	AdditionalDirectories bool                      `json:"additionalDirectories"`
	Steering              bool                      `json:"steering"`
	Idle                  bool                      `json:"idle"`
	FailSetMode           bool                      `json:"failSetMode"`
	Modes                 *acp.SessionModeState     `json:"modes"`
	Configs               []acp.SessionConfigOption `json:"configs"`
	Commands              []acp.AvailableCommand    `json:"commands"`
	Prompt                []step                    `json:"prompt"`
	After                 []step                    `json:"after"`
	AfterFail             bool                      `json:"afterFail"`
}

type step struct {
	Update     json.RawMessage `json:"update"`
	Burst      int             `json:"burst"`
	Permission json.RawMessage `json:"permission"`
	Async      bool            `json:"async"`
	Withdraw   string          `json:"withdraw"`
	Stderr     string          `json:"stderr"`
	Child      bool            `json:"child"`
	Shell      string          `json:"shell"`
	Run        string          `json:"run"`
	Wait       string          `json:"wait"`
	Await      string          `json:"await"`
	Hangup     bool            `json:"hangup"`
	Exit       *int            `json:"exit"`
	Sleep      int             `json:"sleep"`
	Echo       bool            `json:"echo"`
	Match      string          `json:"match"`
	Fail       string          `json:"fail"`
	ErrorKind  string          `json:"errorKind"`
	Session    string          `json:"session"`
	Batch      []string        `json:"batch"`

	Elicitation json.RawMessage `json:"elicitation"`
	Complete    string          `json:"complete"`
}

const loginArg = "--fake-login"

func login(path string) int {
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent:", err)
		return 1
	}
	return 0
}

type record struct {
	PID    int             `json:"pid"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type agent struct {
	script    script
	conn      *acp.Conn
	logMu     sync.Mutex
	log       *os.File
	sessions  atomic.Int64
	cancelled sync.Map
	turns     atomic.Int64
	placesMu  sync.Mutex
	places    map[string]place
}

type place struct {
	cwd     string
	env     map[string]string
	plugins []string
}

func main() {
	a := &agent{}
	if path := os.Getenv("FAKEAGENT_SCRIPT"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeagent:", err)
			os.Exit(2)
		}
		if err := json.Unmarshal(raw, &a.script); err != nil {
			fmt.Fprintln(os.Stderr, "fakeagent:", err)
			os.Exit(2)
		}
	}
	if path := os.Getenv("FAKEAGENT_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeagent:", err)
			os.Exit(2)
		}
		a.log = f
	}
	if slices.Contains(os.Args[1:], loginArg) {
		os.Exit(login(a.script.AuthFile))
	}
	a.record("start", map[string]string{"workspace": contract.WorkspaceFromEnv()})
	fmt.Fprintln(os.Stdout, "fakeagent: not a JSON line")
	a.conn = acp.NewConn(os.Stdin, os.Stdout, a, nil)
	a.conn.Start()
	<-a.conn.Done()
	if a.script.Linger {
		time.Sleep(time.Minute)
	}
}

func (a *agent) record(method string, params any) {
	if a.log == nil {
		return
	}
	raw, _ := json.Marshal(params)
	line, _ := json.Marshal(record{PID: os.Getpid(), Method: method, Params: raw})
	a.logMu.Lock()
	defer a.logMu.Unlock()
	_, _ = a.log.Write(append(line, '\n'))
}

func (a *agent) Notify(method string, params json.RawMessage) {
	a.record(method, params)
	if method == acp.MethodSessionCancel {
		var req acp.SessionRequest
		_ = json.Unmarshal(params, &req)
		a.cancelled.Store(req.SessionID, true)
	}
}

func (a *agent) Request(ctx context.Context, _, method string, params json.RawMessage) (any, error) {
	a.record(method, params)
	var req acp.SessionRequest
	_ = json.Unmarshal(params, &req)
	switch method {
	case acp.MethodInitialize:
		caps := acp.AgentCapabilities{PromptCapabilities: acp.PromptCapabilities{EmbeddedContext: a.script.Embedded, Image: a.script.Image}, SessionCapabilities: acp.SessionCapabilities{Close: &acp.Capability{}, Delete: &acp.Capability{}}}
		if a.script.Resume {
			caps.SessionCapabilities.Resume = &acp.Capability{}
		}
		if a.script.Fork {
			caps.SessionCapabilities.Fork = &acp.Capability{}
		}
		if a.script.AdditionalDirectories {
			caps.SessionCapabilities.AdditionalDirectories = &acp.Capability{}
		}
		res := acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion, AgentCapabilities: caps, AgentInfo: &acp.Implementation{Name: "fakeagent", Version: "0"}}
		if a.script.Steering {
			res.Meta = &acp.InitializeMeta{Steering: acp.SteeringMeta{Supported: true}}
		}
		if a.script.AuthFile != "" {
			res.AuthMethods = []json.RawMessage{json.RawMessage(`{"id":"login","name":"Fake login","type":"terminal","args":["` + loginArg + `"],"env":{"FAKEAGENT_SCRIPT":` + strconv.Quote(os.Getenv("FAKEAGENT_SCRIPT")) + `}}`)}
		}
		return res, nil
	case acp.MethodSessionNew:
		if a.script.AuthFile != "" {
			if _, err := os.Stat(a.script.AuthFile); err != nil {
				return nil, &acp.Error{Code: acp.CodeAuthRequired, Message: "Authentication required"}
			}
		}
		time.Sleep(time.Duration(a.script.NewSleep) * time.Millisecond)
		id := cmp.Or(os.Getenv("FAKEAGENT_SESSION_ID"), "fake-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(a.sessions.Add(1), 10))
		var start acp.NewSessionRequest
		_ = json.Unmarshal(params, &start)
		a.remember(id, start.Cwd, start.Meta)
		go a.announce(id)
		return acp.NewSessionResponse{SessionID: id, Modes: a.script.Modes, ConfigOptions: a.script.Configs}, nil
	case acp.MethodSessionFork:
		if !a.script.Fork {
			return nil, acp.MethodNotFound(method)
		}
		var fork acp.ForkSessionRequest
		_ = json.Unmarshal(params, &fork)
		id := "fork-" + fork.SessionID + "-" + strconv.FormatInt(a.sessions.Add(1), 10)
		a.remember(id, fork.Cwd, fork.Meta)
		go a.announce(id)
		return acp.ForkSessionResponse{SessionID: id, Modes: a.script.Modes, ConfigOptions: a.script.Configs}, nil
	case acp.MethodSessionResume:
		var resume acp.ResumeSessionRequest
		_ = json.Unmarshal(params, &resume)
		a.remember(req.SessionID, resume.Cwd, resume.Meta)
		go a.announce(req.SessionID)
		return acp.ResumeSessionResponse{Modes: a.script.Modes, ConfigOptions: a.script.Configs}, nil
	case acp.MethodSessionPrompt:
		var prompt acp.PromptRequest
		_ = json.Unmarshal(params, &prompt)
		a.cancelled.Delete(prompt.SessionID)
		a.turns.Add(1)
		defer a.turns.Add(-1)
		if err := a.run(ctx, prompt, a.script.Prompt); err != nil {
			a.record("prompt_error", err.Error())
			if a.script.AfterFail {
				go a.run(context.Background(), prompt, a.script.After)
			}
			return nil, err
		}
		go a.run(context.Background(), prompt, a.script.After)
		res := acp.PromptResponse{StopReason: acp.StopEndTurn}
		if a.isCancelled(prompt.SessionID) {
			res.StopReason = acp.StopCancelled
		}
		a.record("prompt_end", res)
		return res, nil
	case acp.MethodSessionSteering:
		var steer acp.SteerRequest
		_ = json.Unmarshal(params, &steer)
		if steer.Meta.Steering.IdleBehavior != acp.SteerPromptRequired {
			return nil, &acp.Error{Code: -32602, Message: "steering without promptRequired"}
		}
		if a.script.Idle || a.turns.Load() == 0 {
			return acp.SteerResponse{Outcome: acp.SteerPromptRequired}, nil
		}
		return acp.SteerResponse{Outcome: acp.SteerInjected}, nil
	case acp.MethodSessionSetMode:
		if a.script.FailSetMode {
			return nil, &acp.Error{Code: acp.CodeInternal, Message: "set_mode failed"}
		}
		var set acp.SetSessionModeRequest
		_ = json.Unmarshal(params, &set)
		a.update(set.SessionID, map[string]any{"sessionUpdate": acp.UpdateCurrentMode, "currentModeId": set.ModeID})
		return struct{}{}, nil
	case acp.MethodSessionSetConfig:
		var set acp.SetSessionConfigOptionRequest
		_ = json.Unmarshal(params, &set)
		return acp.SetSessionConfigOptionResponse{ConfigOptions: a.setConfig(set.ConfigID, set.Value)}, nil
	case acp.MethodSessionClose, acp.MethodSessionDelete:
		time.Sleep(time.Duration(a.script.CloseSleep) * time.Millisecond)
		return struct{}{}, nil
	}
	return nil, acp.MethodNotFound(method)
}

func (a *agent) permission(ctx context.Context, target string, s step) {
	var p struct {
		ToolCall json.RawMessage        `json:"toolCall"`
		Options  []acp.PermissionOption `json:"options"`
	}
	_ = json.Unmarshal(s.Permission, &p)
	if s.Withdraw != "" {
		ctx = acp.WithCallID(ctx, func(id json.RawMessage) {
			go func() {
				awaitFile(s.Withdraw)
				_ = a.conn.Notify(acp.MethodCancelRequest, acp.CancelRequestNotification{RequestID: id})
			}()
		})
	}
	var res json.RawMessage
	if err := a.conn.Call(ctx, acp.MethodRequestPermission, acp.RequestPermissionRequest{SessionID: target, ToolCall: p.ToolCall, Options: p.Options}, &res); err != nil {
		a.record("permission_error", err.Error())
		return
	}
	a.record("permission_response", res)
}

func awaitFile(path string) {
	for _, err := os.Stat(path); err != nil; _, err = os.Stat(path) {
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *agent) setConfig(id string, value any) []acp.SessionConfigOption {
	out := slices.Clone(a.script.Configs)
	raw, _ := json.Marshal(value)
	for i, o := range out {
		if o.ID == id {
			out[i].CurrentValue = raw
		}
	}
	return out
}

func (a *agent) remember(session, cwd string, meta *acp.SessionMeta) {
	p := place{cwd: cwd}
	if meta != nil {
		p.env = meta.ClaudeCode.Options.Env
		for _, pl := range meta.ClaudeCode.Options.Plugins {
			p.plugins = append(p.plugins, pl.Path)
		}
	}
	a.placesMu.Lock()
	defer a.placesMu.Unlock()
	if a.places == nil {
		a.places = map[string]place{}
	}
	a.places[session] = p
}

func (a *agent) runIn(session, script string) {
	a.placesMu.Lock()
	p := a.places[session]
	a.placesMu.Unlock()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = p.cwd
	cmd.Env = os.Environ()
	for k, v := range p.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	a.record("run", map[string]any{"output": string(out), "error": fmt.Sprint(err)})
}

func (a *agent) batchHook(session string, ids []string) {
	a.placesMu.Lock()
	p := a.places[session]
	a.placesMu.Unlock()
	calls := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, map[string]string{"tool_name": "Bash", "tool_use_id": id})
	}
	stdin, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolBatch", "tool_calls": calls})
	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	var out strings.Builder
	for _, dir := range p.plugins {
		raw, err := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
		if err != nil || json.Unmarshal(raw, &hooks) != nil {
			continue
		}
		for _, m := range hooks.Hooks["PostToolBatch"] {
			for _, h := range m.Hooks {
				cmd := exec.Command("/bin/sh", "-c", h.Command)
				cmd.Stdin = strings.NewReader(string(stdin))
				got, _ := cmd.Output()
				out.Write(got)
			}
		}
	}
	a.record("batch_hook", out.String())
}

func (a *agent) announce(session string) {
	if len(a.script.Commands) > 0 {
		a.update(session, map[string]any{"sessionUpdate": acp.UpdateAvailableCommands, "availableCommands": a.script.Commands})
	}
}

func (a *agent) update(session string, update any) {
	_ = a.conn.Notify(acp.MethodSessionUpdate, map[string]any{"sessionId": session, "update": update})
}

func (a *agent) isCancelled(session string) bool {
	_, ok := a.cancelled.Load(session)
	return ok
}

func (a *agent) waitFile(session, path string) {
	for !a.isCancelled(session) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *agent) run(ctx context.Context, prompt acp.PromptRequest, steps []step) error {
	session := prompt.SessionID
	for _, s := range steps {
		if s.Match != "" && (len(prompt.Prompt) == 0 || !strings.Contains(prompt.Prompt[0].Text, s.Match)) {
			continue
		}
		target := cmp.Or(s.Session, session)
		switch {
		case s.Update != nil:
			a.update(target, s.Update)
		case s.Burst > 0:
			for i := range s.Burst {
				a.update(session, map[string]any{"sessionUpdate": acp.UpdateToolCall, "toolCallId": "b" + strconv.Itoa(i), "title": "step " + strconv.Itoa(i), "kind": "read", "status": "completed"})
			}
		case s.Permission != nil && s.Async:
			go a.permission(ctx, target, s)
		case s.Permission != nil:
			a.permission(ctx, target, s)
		case s.Elicitation != nil:
			var req map[string]any
			_ = json.Unmarshal(s.Elicitation, &req)
			req["sessionId"] = session
			if s.Complete != "" {
				time.AfterFunc(200*time.Millisecond, func() {
					_ = a.conn.Notify(acp.MethodElicitationComplete, acp.CompleteElicitationNotification{ElicitationID: s.Complete})
				})
			}
			var res json.RawMessage
			if err := a.conn.Call(ctx, acp.MethodElicitationCreate, req, &res); err != nil {
				a.record("elicitation_error", err.Error())
				continue
			}
			a.record("elicitation_response", res)
		case s.Stderr != "":
			fmt.Fprintln(os.Stderr, s.Stderr)
		case s.Child:
			cmd := exec.Command("sleep", "60")
			if err := cmd.Start(); err == nil {
				a.record("child", cmd.Process.Pid)
			}
		case s.Shell != "":
			cmd := exec.Command("/bin/sh", "-c", s.Shell)
			if err := cmd.Start(); err == nil {
				a.record("shell", cmd.Process.Pid)
				go cmd.Wait()
			}
		case s.Run != "":
			a.runIn(session, s.Run)
		case s.Batch != nil:
			a.batchHook(session, s.Batch)
		case s.Wait != "":
			a.waitFile(session, s.Wait)
		case s.Await != "":
			awaitFile(s.Await)
		case s.Hangup:
			a.record("hangup", nil)
			os.Stdout.Close()
			select {}
		case s.Exit != nil:
			os.Exit(*s.Exit)
		case s.Echo && len(prompt.Prompt) > 0:
			a.update(session, map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "content": prompt.Prompt[0]})
		case s.Sleep > 0:
			time.Sleep(time.Duration(s.Sleep) * time.Millisecond)
		case s.Fail != "":
			rpcErr := &acp.Error{Code: acp.CodeInternal, Message: s.Fail}
			if s.ErrorKind != "" {
				rpcErr.Data, _ = json.Marshal(acp.ErrorData{ErrorKind: s.ErrorKind})
			}
			return rpcErr
		}
	}
	return nil
}
