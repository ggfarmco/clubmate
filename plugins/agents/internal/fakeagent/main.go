package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/plugins/agents/acp"
)

type script struct {
	AuthFile string                    `json:"authFile"`
	Resume   bool                      `json:"resume"`
	Linger   bool                      `json:"linger"`
	Embedded bool                      `json:"embedded"`
	Steering bool                      `json:"steering"`
	Idle     bool                      `json:"idle"`
	Modes    *acp.SessionModeState     `json:"modes"`
	Configs  []acp.SessionConfigOption `json:"configs"`
	Commands []acp.AvailableCommand    `json:"commands"`
	Prompt   []step                    `json:"prompt"`
	After    []step                    `json:"after"`
}

type step struct {
	Update     json.RawMessage `json:"update"`
	Burst      int             `json:"burst"`
	Permission json.RawMessage `json:"permission"`
	Stderr     string          `json:"stderr"`
	Child      bool            `json:"child"`
	Exit       *int            `json:"exit"`
	Sleep      int             `json:"sleep"`
	Echo       bool            `json:"echo"`
	Match      string          `json:"match"`
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
	cancelled atomic.Bool
	turns     atomic.Int64
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
	a.record("start", map[string]string{"project": os.Getenv("CLUBMATE_PROJECT_ID")})
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
		a.cancelled.Store(true)
	}
}

func (a *agent) Request(ctx context.Context, _, method string, params json.RawMessage) (any, error) {
	a.record(method, params)
	var req acp.SessionRequest
	_ = json.Unmarshal(params, &req)
	switch method {
	case acp.MethodInitialize:
		caps := acp.AgentCapabilities{PromptCapabilities: acp.PromptCapabilities{EmbeddedContext: a.script.Embedded}, SessionCapabilities: acp.SessionCapabilities{Close: &acp.Capability{}, Delete: &acp.Capability{}}}
		if a.script.Resume {
			caps.SessionCapabilities.Resume = &acp.Capability{}
		}
		res := acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersion, AgentCapabilities: caps, AgentInfo: &acp.Implementation{Name: "fakeagent", Version: "0"}}
		if a.script.Steering {
			res.Meta = &acp.InitializeMeta{Steering: acp.SteeringMeta{Supported: true}}
		}
		if a.script.AuthFile != "" {
			res.AuthMethods = []json.RawMessage{json.RawMessage(`{"id":"login","name":"Fake login","type":"terminal","args":["` + loginArg + `"]}`)}
		}
		return res, nil
	case acp.MethodSessionNew:
		if a.script.AuthFile != "" {
			if _, err := os.Stat(a.script.AuthFile); err != nil {
				return nil, &acp.Error{Code: acp.CodeAuthRequired, Message: "Authentication required"}
			}
		}
		id := "fake-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(a.sessions.Add(1), 10)
		go a.announce(id)
		return acp.NewSessionResponse{SessionID: id, Modes: a.script.Modes, ConfigOptions: a.script.Configs}, nil
	case acp.MethodSessionResume:
		go a.announce(req.SessionID)
		return acp.ResumeSessionResponse{Modes: a.script.Modes, ConfigOptions: a.script.Configs}, nil
	case acp.MethodSessionPrompt:
		a.cancelled.Store(false)
		a.turns.Add(1)
		defer a.turns.Add(-1)
		var prompt acp.PromptRequest
		_ = json.Unmarshal(params, &prompt)
		a.run(ctx, prompt, a.script.Prompt)
		go a.run(context.Background(), prompt, a.script.After)
		res := acp.PromptResponse{StopReason: acp.StopEndTurn}
		if a.cancelled.Load() {
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
		var set acp.SetSessionModeRequest
		_ = json.Unmarshal(params, &set)
		a.update(set.SessionID, map[string]any{"sessionUpdate": acp.UpdateCurrentMode, "currentModeId": set.ModeID})
		return struct{}{}, nil
	case acp.MethodSessionSetConfig:
		var set acp.SetSessionConfigOptionRequest
		_ = json.Unmarshal(params, &set)
		return acp.SetSessionConfigOptionResponse{ConfigOptions: a.setConfig(set.ConfigID, set.Value)}, nil
	case acp.MethodSessionClose, acp.MethodSessionDelete:
		return struct{}{}, nil
	}
	return nil, acp.MethodNotFound(method)
}

func (a *agent) setConfig(id, value string) []acp.SessionConfigOption {
	out := slices.Clone(a.script.Configs)
	raw, _ := json.Marshal(value)
	for i, o := range out {
		if o.ID == id {
			out[i].CurrentValue = raw
		}
	}
	return out
}

func (a *agent) announce(session string) {
	if len(a.script.Commands) > 0 {
		a.update(session, map[string]any{"sessionUpdate": acp.UpdateAvailableCommands, "availableCommands": a.script.Commands})
	}
}

func (a *agent) update(session string, update any) {
	_ = a.conn.Notify(acp.MethodSessionUpdate, map[string]any{"sessionId": session, "update": update})
}

func (a *agent) run(ctx context.Context, prompt acp.PromptRequest, steps []step) {
	session := prompt.SessionID
	for _, s := range steps {
		if s.Match != "" && (len(prompt.Prompt) == 0 || !strings.Contains(prompt.Prompt[0].Text, s.Match)) {
			continue
		}
		switch {
		case s.Update != nil:
			a.update(session, s.Update)
		case s.Burst > 0:
			for i := range s.Burst {
				a.update(session, map[string]any{"sessionUpdate": acp.UpdateToolCall, "toolCallId": "b" + strconv.Itoa(i), "title": "step " + strconv.Itoa(i), "kind": "read", "status": "completed"})
			}
		case s.Permission != nil:
			var p struct {
				ToolCall json.RawMessage        `json:"toolCall"`
				Options  []acp.PermissionOption `json:"options"`
			}
			_ = json.Unmarshal(s.Permission, &p)
			var res json.RawMessage
			err := a.conn.Call(ctx, acp.MethodRequestPermission, acp.RequestPermissionRequest{SessionID: session, ToolCall: p.ToolCall, Options: p.Options}, &res)
			if err != nil {
				a.record("permission_error", err.Error())
				continue
			}
			a.record("permission_response", res)
		case s.Stderr != "":
			fmt.Fprintln(os.Stderr, s.Stderr)
		case s.Child:
			cmd := exec.Command("sleep", "60")
			if err := cmd.Start(); err == nil {
				a.record("child", cmd.Process.Pid)
			}
		case s.Exit != nil:
			os.Exit(*s.Exit)
		case s.Echo && len(prompt.Prompt) > 0:
			a.update(session, map[string]any{"sessionUpdate": acp.UpdateAgentMessageChunk, "content": prompt.Prompt[0]})
		case s.Sleep > 0:
			time.Sleep(time.Duration(s.Sleep) * time.Millisecond)
		}
	}
}
