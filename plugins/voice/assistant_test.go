package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

var testCommands = []commandDef{
	{ID: "git.worktree-remove", Plugin: "git", Agent: true, Destructive: true},
	{ID: "git.branch-switch", Plugin: "git", Destructive: true},
	{ID: "session.send", Plugin: "core", Agent: true, Destructive: true},
	{ID: "agents.answer", Plugin: "agents", Agent: true, Destructive: true},
	{ID: "backlog.list", Plugin: "backlog", Agent: true},
}

func TestRunRefusesNonAgentCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    error
	}{
		{"not agent-visible", "git.branch-switch", errNotAgentCommand},
		{"not dangerous", "backlog.list", errNotDangerous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, h, _ := newTestVoice(t)
			uid := routed(t, v, h)

			_, err := v.command(t.Context(), nil, voiceCall("voice.run", map[string]any{"utterance": uid, "command": tt.command, "args": "{}", "summary": "run"}))

			assert.ErrorIs(t, err, tt.want)
			assert.Empty(t, h.calls(tt.command))
		})
	}
}

func TestEndPromptsVoiceSession(t *testing.T) {
	v, h, _ := newTestVoice(t)

	routed(t, v, h)
	routed(t, v, h)

	created := h.calls("agents.new")
	require.Len(t, created, 1)
	var args newSessionArgs
	require.NoError(t, json.Unmarshal(created[0], &args))
	assert.Contains(t, args.DisallowedTools, "mcp__clubmate__git_worktree_remove")
	assert.Contains(t, args.DisallowedTools, "mcp__clubmate__session_send")
	assert.NotContains(t, args.DisallowedTools, "mcp__clubmate__agents_answer")
	assert.Equal(t, "bypassPermissions", args.Mode)
	for _, p := range h.calls("agents.prompt") {
		var prompt promptArgs
		require.NoError(t, json.Unmarshal(p, &prompt))
		assert.Equal(t, voiceSession, prompt.Session)
	}
}

func TestPromptFailureRecreatesVoiceSession(t *testing.T) {
	v, h, _ := newTestVoice(t)
	require.NoError(t, h.StorageSet(t.Context(), sessionKeyPrefix+testWorkspace, sessionA))
	h.commands["agents.sessions"] = func(json.RawMessage) (any, error) { return []map[string]string{{"id": sessionA}}, nil }
	h.commands["agents.prompt"] = func(args json.RawMessage) (any, error) {
		var p promptArgs
		_ = json.Unmarshal(args, &p)
		if p.Session == sessionA {
			return nil, errors.New("cannot resume")
		}
		return nil, nil
	}

	uid := start(t, v)
	_, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(h.calls("agents.prompt")) == 2 }, 5*time.Second, time.Millisecond)
	assert.Len(t, h.calls("agents.new"), 1)
	require.Eventually(t, func() bool {
		st, err := v.state(testWorkspace, uid)
		return err == nil && st.Session == voiceSession && st.Phase == phaseThinking
	}, 5*time.Second, time.Millisecond)
}

func TestAnswerRejectsOtherCaller(t *testing.T) {
	v, h, _ := newTestVoice(t)
	uid := routed(t, v, h)
	cmd := voiceCall("voice.answer", map[string]any{"utterance": uid, "session": sessionA, "text": "да"})
	cmd.Caller = sessionB

	_, err := v.command(t.Context(), nil, cmd)

	assert.ErrorIs(t, err, errNotVoiceSession)
	assert.Empty(t, h.calls("agents.answer"))
}

func TestAnswerQuestionRunsElicitationAsPlugin(t *testing.T) {
	v, h, _ := newTestVoice(t)
	h.commands["agents.elicitation"] = func(json.RawMessage) (any, error) { return nil, nil }
	uid := routed(t, v, h)

	_, err := v.command(t.Context(), nil, voiceCall("voice.answer", map[string]any{"utterance": uid, "session": sessionA, "request": "q1", "answers": []map[string]string{{"key": "store", "value": "sql"}}}))

	require.NoError(t, err)
	calls := h.calls("agents.elicitation")
	require.Len(t, calls, 1)
	assert.JSONEq(t, `{"session":"`+sessionA+`","request":"q1","action":"accept","answers":[{"key":"store","value":"sql"}]}`, string(calls[0]))
}

func TestAnswerPermissionOutsideContextWaitsForConfirm(t *testing.T) {
	v, h, _ := newTestVoice(t)
	h.commands["agents.transcript"] = func(json.RawMessage) (any, error) {
		return map[string]any{"entries": []map[string]any{{
			"kind": "permission", "id": "p1", "toolCall": map[string]string{"title": "Run ls"},
			"options": []map[string]string{{"optionId": "once", "name": "Allow", "kind": "allow_once"}},
		}}}, nil
	}
	h.commands["agents.permission"] = func(json.RawMessage) (any, error) { return nil, nil }
	uid := routed(t, v, h)
	errs := make(chan error, 1)
	go func() {
		_, err := v.command(t.Context(), nil, voiceCall("voice.answer", map[string]any{"utterance": uid, "session": sessionA, "request": "p1", "option": "once"}))
		errs <- err
	}()

	require.Eventually(t, func() bool { return phaseOf(t, v, uid) == phaseConfirm }, 5*time.Second, time.Millisecond)
	assert.Empty(t, h.calls("agents.permission"))
	require.NoError(t, v.cancel(testWorkspace, uid))
	assert.ErrorIs(t, <-errs, errUtteranceOver)
}

func TestConfirmForAnotherActionIsRefused(t *testing.T) {
	v, h, _ := newTestVoice(t)
	h.commands["git.worktree-remove"] = func(json.RawMessage) (any, error) { return map[string]bool{"removed": true}, nil }
	uid := routed(t, v, h)
	errs := make(chan error, 1)
	go func() {
		_, err := v.command(t.Context(), nil, voiceCall("voice.run", map[string]any{"utterance": uid, "command": "git.worktree-remove", "args": `{"path":"/tmp/wt"}`, "summary": "удалить worktree"}))
		errs <- err
	}()
	require.Eventually(t, func() bool { return phaseOf(t, v, uid) == phaseConfirm }, 5*time.Second, time.Millisecond)

	_, err := v.command(t.Context(), nil, pluginsdk.CommandRun{ID: "voice.confirm", WorkspaceID: testWorkspace, Args: raw(t, map[string]any{"utterance": uid, "approve": true, "confirm": 2})})
	require.ErrorIs(t, err, errConfirmChanged)
	assert.Empty(t, h.calls("git.worktree-remove"))
	assert.Equal(t, phaseConfirm, phaseOf(t, v, uid))

	_, err = v.command(t.Context(), nil, pluginsdk.CommandRun{ID: "voice.confirm", WorkspaceID: testWorkspace, Args: raw(t, map[string]any{"utterance": uid, "approve": true, "confirm": 1})})
	require.NoError(t, err)
	require.NoError(t, <-errs)
	assert.Len(t, h.calls("git.worktree-remove"), 1)
}

func TestRunDangerousWaitsForConfirm(t *testing.T) {
	tests := []struct {
		name    string
		approve bool
		runs    int
		phase   string
		result  string
	}{
		{"approved", true, 1, phaseThinking, ""},
		{"declined", false, 0, phaseCancelled, resultCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, h, _ := newTestVoice(t)
			h.commands["git.worktree-remove"] = func(json.RawMessage) (any, error) { return map[string]bool{"removed": true}, nil }
			uid := routed(t, v, h)
			errs := make(chan error, 1)
			go func() {
				_, err := v.command(t.Context(), nil, voiceCall("voice.run", map[string]any{"utterance": uid, "command": "git.worktree-remove", "args": `{"path":"/tmp/wt"}`, "summary": "удалить worktree"}))
				errs <- err
			}()
			require.Eventually(t, func() bool { return phaseOf(t, v, uid) == phaseConfirm }, 5*time.Second, time.Millisecond)
			st, err := v.state(testWorkspace, uid)
			require.NoError(t, err)
			assert.Equal(t, &Confirm{ID: 1, Title: "удалить worktree", Command: "git.worktree-remove", Args: json.RawMessage(`{"path":"/tmp/wt"}`)}, st.Confirm)
			assert.Empty(t, h.calls("git.worktree-remove"))

			_, err = v.command(t.Context(), nil, pluginsdk.CommandRun{ID: "voice.confirm", WorkspaceID: testWorkspace, Args: raw(t, map[string]any{"utterance": uid, "approve": tt.approve})})
			require.NoError(t, err)

			runErr := <-errs
			assert.Len(t, h.calls("git.worktree-remove"), tt.runs)
			st, err = v.state(testWorkspace, uid)
			require.NoError(t, err)
			assert.Equal(t, tt.phase, st.Phase)
			assert.Equal(t, tt.result, st.Result)
			if tt.approve {
				assert.NoError(t, runErr)
			} else {
				assert.ErrorIs(t, runErr, errDeclined)
			}
		})
	}
}

func TestTurnTimeoutEndsWithError(t *testing.T) {
	v, h, _ := newTestVoice(t)
	clk := &fakeClock{}
	v.after = clk.after
	uid := routed(t, v, h)

	clk.fire(turnTimeout)

	st, err := v.state(testWorkspace, uid)
	require.NoError(t, err)
	assert.Equal(t, phaseError, st.Phase)
	assert.Equal(t, msgNoAnswer, st.Error)
}

func routed(t *testing.T, v *voice, h *fakeHost) string {
	t.Helper()
	prompts := len(h.calls("agents.prompt"))
	uid := start(t, v)
	_, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(h.calls("agents.prompt")) > prompts }, 5*time.Second, time.Millisecond)
	return uid
}

func voiceCall(id string, args map[string]any) pluginsdk.CommandRun {
	b, _ := json.Marshal(args)
	return pluginsdk.CommandRun{ID: id, WorkspaceID: testWorkspace, Args: b, Agent: true, Caller: voiceSession}
}

func phaseOf(t *testing.T, v *voice, uid string) string {
	st, err := v.state(testWorkspace, uid)
	require.NoError(t, err)
	return st.Phase
}

func (c *fakeClock) fire(d time.Duration) {
	c.mu.Lock()
	var due []func()
	for _, t := range c.timers {
		if t.d == d && !t.stopped {
			t.stopped = true
			due = append(due, t.f)
		}
	}
	c.mu.Unlock()
	for _, f := range due {
		f()
	}
}
