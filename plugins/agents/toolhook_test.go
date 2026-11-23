package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const toolAsk = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"Clubmate gate"}}`

type hookMeta struct {
	dir      string
	settings map[string]json.RawMessage
}

func hookMetaOf(t *testing.T, c fakeCall) hookMeta {
	t.Helper()
	var meta struct {
		Meta struct {
			ClaudeCode struct {
				Options struct {
					Plugins []struct {
						Path string `json:"path"`
					} `json:"plugins"`
					Settings map[string]json.RawMessage `json:"settings"`
				} `json:"options"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(c.Params, &meta))
	got := hookMeta{settings: meta.Meta.ClaudeCode.Options.Settings}
	for _, p := range meta.Meta.ClaudeCode.Options.Plugins {
		if strings.Contains(p.Path, string(filepath.Separator)+"toolhook"+string(filepath.Separator)) {
			got.dir = p.Path
		}
	}
	return got
}

func (f *fixture) sessionHook(t *testing.T, method, session string) hookMeta {
	t.Helper()
	return hookMetaOf(t, f.waitCall(t, method, func(c fakeCall) bool { return strings.Contains(string(c.Params), session) }))
}

func (f *fixture) addPushGate(t *testing.T, workspace, repo string) gateListed {
	t.Helper()
	return f.addCommandGate(t, workspace, repo, "git push")
}

func (f *fixture) addCommandGate(t *testing.T, workspace, repo, text string) gateListed {
	t.Helper()
	var g gateListed
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "tool", "how": "command", "text": text}, &g))
	return g
}

func (f *fixture) hookedTurn(t *testing.T, workspace, session, text string) {
	t.Helper()
	f.prompt(t, workspace, session, text)
	f.hookedPrompt(t, workspace, session, text)
}

func (f *fixture) hookedPrompt(t *testing.T, workspace, session, text string) {
	t.Helper()
	var calls []fakeCall
	at := -1
	testutil.Eventually(t, func() bool {
		calls = f.allCalls(t)
		at = slices.IndexFunc(calls, func(c fakeCall) bool { return c.Method == "session/prompt" && strings.Contains(string(c.Params), text) })
		return at >= 0
	}, wait)
	attach := -1
	for i := at - 1; i >= 0 && attach < 0; i-- {
		if c := calls[i]; slices.Contains([]string{"session/new", "session/resume", "session/fork"}, c.Method) && strings.Contains(string(c.Params), session) {
			attach = i
		}
	}
	require.GreaterOrEqual(t, attach, 0)
	assert.NotEmpty(t, hookMetaOf(t, calls[attach]).dir, "the turn %q ran on an attach without the hook", text)
	f.waitRecord(t, workspace, session, "hooked", func(r record) bool { return r.Hooked && r.Status == "done" })
}

func TestGatedMetaCarriesHookOnlyInAToolGatedRepo(t *testing.T) {
	t.Parallel()
	f := start(t, asideScript())
	tool, paused, plain := newRepo(t), newRepo(t), newRepo(t)
	toolWS, pausedWS, plainWS := f.workspace(t, tool), f.workspace(t, paused), f.workspace(t, plain)
	f.addPushGate(t, toolWS, tool)
	g := f.addPushGate(t, pausedWS, paused)
	require.NoError(t, f.run(t, pausedWS, "agents.gate-update", map[string]any{"id": g.ID, "paused": true}, nil))
	require.NoError(t, f.run(t, plainWS, "agents.gate-add", map[string]any{"repo": plain}, nil))
	require.NoError(t, f.run(t, plainWS, "agents.gate-add", map[string]any{"repo": plain, "point": "turn"}, nil))
	carries := func(got hookMeta) {
		t.Helper()
		assert.NotEmpty(t, got.dir)
		assert.JSONEq(t, `false`, string(got.settings["disableAllHooks"]))
		assert.JSONEq(t, `{"clubmate-gates@inline":true}`, string(got.settings["enabledPlugins"]))
	}

	carries(f.sessionHook(t, "session/new", f.newSession(t, pausedWS)))
	parent := f.promptedParent(t, toolWS)
	carries(f.sessionHook(t, "session/new", parent.ID))
	carries(f.sessionHook(t, "session/fork", f.aside(t, toolWS, parent.ID, "quote", "x").Session))
	got := f.sessionHook(t, "session/new", f.newSession(t, plainWS))
	assert.Empty(t, got.dir)
	assert.NotContains(t, got.settings, "disableAllHooks")
	assert.NotContains(t, got.settings, "enabledPlugins")
}

func TestSyncToolHooksWritesPluginAndRules(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	g := f.addPushGate(t, workspace, repo)
	dir := f.sessionHook(t, "session/new", f.newSession(t, workspace)).dir
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		return string(raw)
	}

	assert.JSONEq(t, `{"name":"clubmate-gates","version":"1"}`, read(".claude-plugin/plugin.json"))
	type hookEntry struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command   string  `json:"command"`
			Timeout   int     `json:"timeout"`
			OnFailure *string `json:"onFailure"`
		} `json:"hooks"`
	}
	var hooks struct {
		Hooks struct {
			PreToolUse    []hookEntry `json:"PreToolUse"`
			PostToolBatch []hookEntry `json:"PostToolBatch"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(read("hooks/hooks.json")), &hooks))
	require.Len(t, hooks.Hooks.PreToolUse, 1)
	require.Len(t, hooks.Hooks.PostToolBatch, 1)
	entry, batch := hooks.Hooks.PreToolUse[0], hooks.Hooks.PostToolBatch[0]
	assert.Equal(t, ".*", entry.Matcher)
	require.Len(t, entry.Hooks, 1)
	assert.Equal(t, 10, entry.Hooks[0].Timeout)
	require.NotNil(t, entry.Hooks[0].OnFailure)
	assert.Equal(t, "block", *entry.Hooks[0].OnFailure)
	assert.Equal(t, ".*", batch.Matcher)
	require.Len(t, batch.Hooks, 1)
	assert.Equal(t, 10, batch.Hooks[0].Timeout)
	assert.Nil(t, batch.Hooks[0].OnFailure)
	command := entry.Hooks[0].Command
	bin, rest, ok := strings.Cut(strings.TrimPrefix(command, "'"), "' ")
	require.True(t, ok)
	assert.Equal(t, "tool-hook", filepath.Base(bin))
	assert.True(t, strings.HasPrefix(rest, "'"+dir+"' || printf"))
	assert.Equal(t, "'"+bin+"' batch '"+dir+"' || true", batch.Hooks[0].Command)
	raw, err := os.ReadFile(testutil.BuildBinary(t, "clubmate/plugins/agents/toolhook"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(bin, raw, 0o755))
	run := func(input string) string {
		cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", command)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.Output()
		require.NoError(t, err)
		return string(out)
	}
	assert.Equal(t, toolAsk, run(`{"tool_name":"Bash","tool_input":{"command":"cd x && git push"},"tool_use_id":"toolu_1"}`))
	assert.Empty(t, run(`{"tool_name":"Bash","tool_input":{"command":"ls"},"tool_use_id":"toolu_2"}`))
	assert.FileExists(t, filepath.Join(dir, "marks", "toolu_1"))
	assert.NoFileExists(t, filepath.Join(dir, "marks", "toolu_2"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "findings", "toolu_1"), []byte("fix it"), 0o600))
	batchRun := func(input string) string {
		cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", batch.Hooks[0].Command)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.Output()
		require.NoError(t, err)
		return string(out)
	}
	assert.JSONEq(t, `{"hookSpecificOutput":{"hookEventName":"PostToolBatch","additionalContext":"fix it"}}`, batchRun(`{"tool_calls":[{"tool_use_id":"toolu_1"}]}`))
	assert.FileExists(t, filepath.Join(dir, "told", "toolu_1"))
	pushRule := `[{"id":"` + g.ID + `","tool":"Bash","how":"command","text":"git push"}]`
	assert.JSONEq(t, pushRule, read("rules.json"))

	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": g.ID, "paused": true}, nil))
	assert.JSONEq(t, `[]`, read("rules.json"))
	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": g.ID, "paused": false}, nil))
	assert.JSONEq(t, pushRule, read("rules.json"))
	require.NoError(t, f.run(t, workspace, "agents.gate-delete", map[string]any{"id": g.ID}, nil))
	assert.JSONEq(t, `[]`, read("rules.json"))
}

func TestToolHooksAreWrittenAtPluginStart(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"resume": true})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	f.addPushGate(t, workspace, repo)
	dir := f.sessionHook(t, "session/new", f.newSession(t, workspace)).dir
	require.NoError(t, os.RemoveAll(dir))

	f.restartPlugin(t, workspace)

	assert.FileExists(t, filepath.Join(dir, "rules.json"))
	assert.FileExists(t, filepath.Join(dir, "hooks", "hooks.json"))
	assert.DirExists(t, filepath.Join(dir, "marks"))
}

func TestHookedFollowsTheLastRealAttach(t *testing.T) {
	t.Parallel()
	f := startSuspending(t, map[string]any{"resume": true})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	g := f.addPushGate(t, workspace, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitRecord(t, workspace, session, "hooked", func(r record) bool { return r.Hooked && r.Suspended && r.Status == "done" })

	require.NoError(t, f.run(t, workspace, "agents.gate-delete", map[string]any{"id": g.ID}, nil))
	f.prompt(t, workspace, session, "two")

	assert.Empty(t, f.sessionHook(t, "session/resume", session).dir)
	f.waitRecord(t, workspace, session, "unhooked", func(r record) bool { return !r.Hooked && r.Status == "done" })
}

func TestFirstToolGateReattachesIdleSessions(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "long", "wait": hold}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	idle := f.newSession(t, workspace)
	f.waitRecord(t, workspace, idle, "attached", func(r record) bool { return r.AgentSessionID != "" })
	busy := f.newChild(t, workspace, idle, map[string]any{"prompt": "long"}).Session
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "long") })

	f.addPushGate(t, workspace, repo)

	f.waitRecord(t, workspace, idle, "suspended", func(r record) bool { return r.Suspended })
	testutil.Never(t, func() bool { return f.session(t, workspace, busy).Suspended }, wait/10)
	f.hookedTurn(t, workspace, idle, "two")

	write(t, hold, "")
	f.waitRecord(t, workspace, busy, "suspended", func(r record) bool { return r.Suspended })
	f.hookedTurn(t, workspace, busy, "three")

	f.addCommandGate(t, workspace, repo, "gh pr create")
	testutil.Never(t, func() bool { return f.session(t, workspace, idle).Suspended || f.session(t, workspace, busy).Suspended }, wait/10)
}

func TestReleasedDoneHoldIsHookedBeforeItsNextTurn(t *testing.T) {
	t.Parallel()
	d := startDone(t, map[string]any{}, map[string]any{"resume": true}, nil)
	d.f.addPushGate(t, d.workspace, d.repo)
	require.NoError(t, d.verdict(t, d.reviewer(t), "approved"))
	d.f.waitStatus(t, d.workspace, d.session, "done")

	d.f.hookedTurn(t, d.workspace, d.session, "next after release")
}

func TestFailedTurnIsHookedBeforeItsNextTurn(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "long", "wait": hold}, {"match": "long", "fail": "long boom"}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "long")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "long") })
	f.addPushGate(t, workspace, repo)
	write(t, hold, "")
	f.waitStatus(t, workspace, session, "error")

	f.hookedTurn(t, workspace, session, "next after error")
}

func TestAttachInFlightPicksUpTheGate(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"resume": true, "newSleep": 1500})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.waitCall(t, "session/new", func(c fakeCall) bool { return strings.Contains(string(c.Params), session) })
	f.addPushGate(t, workspace, repo)
	f.waitRecord(t, workspace, session, "attached", func(r record) bool { return r.AgentSessionID != "" })

	f.hookedTurn(t, workspace, session, "first turn")
}

func TestBackgroundCommandLeavesSessionsVisiblyUngatedUntilItEnds(t *testing.T) {
	t.Parallel()
	release := filepath.Join(t.TempDir(), "release")
	f := startEnv(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, map[string]any{
		"resume": true,
		"prompt": []map[string]any{{"match": "watch", "update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "bg", "_meta": map[string]any{"claudeCode": map[string]any{"toolResponse": map[string]any{"backgroundTaskId": "b1"}}}}}},
		"after": []map[string]any{
			{"match": "watch", "run": "while [ ! -e " + release + " ]; do sleep 0.05; done"},
			{"match": "watch", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "tests green"}}},
			{"match": "watch", "sleep": 1500},
			{"match": "watch", "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
		},
	})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	owner := f.newSession(t, workspace)
	f.prompt(t, workspace, owner, "watch")
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return true })
	sibling := f.newSession(t, workspace)
	f.waitRecord(t, workspace, sibling, "attached", func(r record) bool { return r.AgentSessionID != "" })
	ungated := func(when string) func(record) bool {
		return func(r record) bool {
			return r.Ungated != nil && r.Ungated.Badge == "not gated yet · "+when && r.Landing != nil && r.Landing.Words == "not gated yet" && r.Landing.Tone == "plain" && r.Landing.Line == "Not gated yet · "+when && !r.Hooked
		}
	}

	f.addPushGate(t, workspace, repo)

	f.waitRecord(t, workspace, owner, "owner visibly ungated", ungated("after its background command"))
	f.waitRecord(t, workspace, sibling, "sibling visibly ungated", ungated("after the worktree's other work"))
	f.prompt(t, workspace, sibling, "while the command runs")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "while the command runs") })
	f.waitRecord(t, workspace, sibling, "ran ungated", func(r record) bool { return ungated("after the worktree's other work")(r) && r.Status == "done" })

	write(t, release, "")

	f.hookedPrompt(t, workspace, owner, "[Clubmate] Your background command finished")
	f.waitRecord(t, workspace, owner, "no longer ungated", func(r record) bool { return r.Landing == nil && r.Ungated == nil })
	f.waitRecord(t, workspace, sibling, "re-attached", func(r record) bool { return r.Suspended && r.Landing == nil && r.Ungated == nil && r.Status == "done" })
	testutil.Never(t, func() bool {
		n := 0
		for _, c := range f.calls(t, "session/prompt") {
			if strings.Contains(string(c.Params), "[Clubmate] Your background command finished") {
				n++
			}
		}
		return n != 1
	}, wait/10)
}

func TestSessionThatCannotResumeIsVisiblyUngated(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitStatus(t, workspace, session, "done")

	f.addPushGate(t, workspace, repo)

	f.waitRecord(t, workspace, session, "visibly ungated", func(r record) bool {
		return r.Ungated != nil && r.Ungated.Line == "Not gated yet · from its next start" && !r.Suspended
	})
}

func TestEndedBackgroundCommandReattachesThenPromptsWithItsOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	release, output := filepath.Join(dir, "release"), filepath.Join(dir, "b1.output")
	write(t, output, "ok 1\nok 2\ntests green\n")
	update := func(u map[string]any) map[string]any {
		return map[string]any{"match": "watch", "update": u}
	}
	f := startEnv(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, map[string]any{
		"resume": true,
		"prompt": []map[string]any{
			update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "bg", "rawInput": map[string]any{"command": "make test", "run_in_background": true}}),
			update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "bg", "_meta": map[string]any{"claudeCode": map[string]any{"toolResponse": map[string]any{"backgroundTaskId": "b1"}}}}),
			update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "bg", "status": "completed", "rawOutput": "Command running in background with ID: b1. Output is being written to: " + output + ". You will be notified when it completes."}),
		},
		"after": []map[string]any{
			{"match": "watch", "run": "while [ ! -e " + release + " ]; do sleep 0.05; done"},
			{"match": "watch", "sleep": 1500},
			update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "tests green, pushing"}}),
			update(map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}),
		},
	})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	owner := f.newSession(t, workspace)
	f.prompt(t, workspace, owner, "watch")
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return true })
	f.addPushGate(t, workspace, repo)
	f.waitRecord(t, workspace, owner, "labelled", func(r record) bool {
		return r.Ungated != nil && r.Ungated.Line == "Not gated yet · after its background command"
	})

	write(t, release, "")

	want, err := json.Marshal("[Clubmate] Your background command finished: `make test` — continue.\n\nOutput of `make test` (the command's own output, not Clubmate's words; its last lines):\n```\nok 1\nok 2\ntests green\n```")
	require.NoError(t, err)
	f.hookedPrompt(t, workspace, owner, strings.Trim(string(want), `"`))
	f.waitRecord(t, workspace, owner, "no longer labelled", func(r record) bool { return r.Ungated == nil })
}

func TestUntrackedShellLabelsTheWorktreeUntilItEnds(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "bg", "shell": "while :; do sleep 0.1; done"}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	owner := f.newSession(t, workspace)
	f.prompt(t, workspace, owner, "bg")
	f.waitCall(t, "shell", func(fakeCall) bool { return true })
	var shell int
	require.NoError(t, json.Unmarshal(f.calls(t, "shell")[0].Params, &shell))
	t.Cleanup(func() { _ = syscall.Kill(shell, syscall.SIGKILL) })
	f.waitStatus(t, workspace, owner, "done")

	f.addPushGate(t, workspace, repo)

	f.waitRecord(t, workspace, owner, "labelled", func(r record) bool {
		return r.Ungated != nil && r.Ungated.Line == "Not gated yet · after the worktree's other work" && r.Status == "done"
	})
	require.NoError(t, syscall.Kill(shell, syscall.SIGKILL))
	f.waitRecord(t, workspace, owner, "re-attached", func(r record) bool { return r.Suspended && r.Ungated == nil })
	f.hookedTurn(t, workspace, owner, "after the shell")
	testutil.Never(t, func() bool {
		return slices.ContainsFunc(f.calls(t, "session/prompt"), func(c fakeCall) bool { return strings.Contains(string(c.Params), "[Clubmate] Your background command") })
	}, wait/10)
}

func TestLabelledHeldTurnIsNotResettledEveryPoll(t *testing.T) {
	t.Parallel()
	d := startDoneIn(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, "", map[string]any{}, map[string]any{
		"prompt": []map[string]any{{"match": "MORE", "run": "date +%s%N >> note2.md"}},
	}, nil)
	d.f.addPushGate(t, d.workspace, d.repo)
	require.NoError(t, d.verdict(t, d.reviewer(t), "approved"))
	d.f.waitStatus(t, d.workspace, d.session, "done")
	d.f.prompt(t, d.workspace, d.session, "MORE")
	d.f.waitRecord(t, d.workspace, d.session, "held and labelled", func(r record) bool { return r.Ungated != nil && r.Status == "running" })
	d.f.waitHeld(t, d.workspace, d.session)
	d.reviewer(t)
	updated := func() time.Time {
		var list []struct {
			UpdatedAt time.Time `json:"updatedAt"`
		}
		require.NoError(t, d.f.run(t, d.workspace, "agents.sessions", map[string]any{"session": d.session}, &list))
		require.Len(t, list, 1)
		return list[0].UpdatedAt
	}
	seen := map[time.Time]bool{}

	testutil.Never(t, func() bool { seen[updated()] = true; return len(seen) > 3 }, wait/5)
}

const finishedLead = "[Clubmate] Your background command finished"

func bgSteps(match, id, command, task, output string) []map[string]any {
	update := func(u map[string]any) map[string]any { return map[string]any{"match": match, "update": u} }
	return []map[string]any{
		update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "rawInput": map[string]any{"command": command, "run_in_background": true}}),
		update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "_meta": map[string]any{"claudeCode": map[string]any{"toolResponse": map[string]any{"backgroundTaskId": task}}}}),
		update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "completed", "rawOutput": "Command running in background with ID: " + task + ". Output is being written to: " + output + ". You will be notified when it completes."}),
	}
}

func followSteps(match, release, text string, sleep int) []map[string]any {
	steps := []map[string]any{
		{"match": match, "run": "while [ ! -e " + release + " ]; do sleep 0.05; done"},
		{"match": match, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}},
	}
	if sleep > 0 {
		steps = append(steps, map[string]any{"match": match, "sleep": sleep})
	}
	return append(steps, map[string]any{"match": match, "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}})
}

func (f *fixture) finishedPrompts(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, c := range f.calls(t, "session/prompt") {
		if strings.Contains(string(c.Params), finishedLead) {
			out = append(out, string(c.Params))
		}
	}
	return out
}

func (f *fixture) transcriptHas(t *testing.T, workspace, session, text string) bool {
	t.Helper()
	raw, err := json.Marshal(f.transcript(t, workspace, session))
	require.NoError(t, err)
	return strings.Contains(string(raw), text)
}

func busyWorktree(t *testing.T, stop bool) (*fixture, string, string, int) {
	t.Helper()
	dir := t.TempDir()
	release, output := filepath.Join(dir, "release"), filepath.Join(dir, "b1.output")
	write(t, output, "ok 1\ntests green\n")
	sleep := 0
	if stop {
		sleep = 4000
	}
	prompt := append(bgSteps("WATCH", "bg", "make test", "b1", output), map[string]any{"match": "SERVE", "shell": "while :; do sleep 0.1; done"})
	f := startEnv(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, map[string]any{"resume": true, "prompt": prompt, "after": followSteps("WATCH", release, "tests green, pushing", sleep)})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	server := f.newSession(t, workspace)
	f.prompt(t, workspace, server, "SERVE")
	f.waitCall(t, "shell", func(fakeCall) bool { return true })
	var shell int
	require.NoError(t, json.Unmarshal(f.calls(t, "shell")[0].Params, &shell))
	t.Cleanup(func() { _ = syscall.Kill(shell, syscall.SIGKILL) })
	f.waitStatus(t, workspace, server, "done")
	owner := f.newSession(t, workspace)
	f.prompt(t, workspace, owner, "WATCH")
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return len(f.calls(t, "prompt_end")) >= 2 })
	f.addPushGate(t, workspace, repo)
	f.waitRecord(t, workspace, owner, "labelled", func(r record) bool { return r.Ungated != nil })
	write(t, release, "")
	testutil.Eventually(t, func() bool { return f.transcriptHas(t, workspace, owner, "tests green, pushing") }, wait)
	return f, workspace, owner, shell
}

func TestFollowUpThatRanToItsEndIsNotPromptedAgain(t *testing.T) {
	t.Parallel()
	f, workspace, owner, shell := busyWorktree(t, false)
	f.waitRecord(t, workspace, owner, "its own follow-up ended", func(r record) bool { return r.Status == "done" })

	require.NoError(t, syscall.Kill(shell, syscall.SIGKILL))

	f.waitRecord(t, workspace, owner, "re-attached", func(r record) bool { return r.Suspended && r.Ungated == nil })
	testutil.Never(t, func() bool { return len(f.finishedPrompts(t)) > 0 }, wait/10)
}

func TestStoppedFollowUpIsNotWokenAgain(t *testing.T) {
	t.Parallel()
	f, workspace, owner, shell := busyWorktree(t, true)
	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": owner}, nil))
	f.waitRecord(t, workspace, owner, "stopped", func(r record) bool { return r.Status != "running" })

	require.NoError(t, syscall.Kill(shell, syscall.SIGKILL))

	f.waitRecord(t, workspace, owner, "re-attached", func(r record) bool { return r.Suspended && r.Ungated == nil })
	testutil.Never(t, func() bool { return len(f.finishedPrompts(t)) > 0 }, wait/10)
}

func TestPromptListsOnlyCommandsNotFollowedUp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relA, relB := filepath.Join(dir, "relA"), filepath.Join(dir, "relB")
	outA, outB := filepath.Join(dir, "a1.output"), filepath.Join(dir, "b1.output")
	write(t, outA, "alpha output\n")
	write(t, outB, "bravo output\n")
	f := startEnv(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, map[string]any{
		"resume": true,
		"prompt": append(bgSteps("ALPHA", "bgA", "make test-a", "a1", outA), bgSteps("BRAVO", "bgB", "make test-b", "b1", outB)...),
		"after":  append(followSteps("ALPHA", relA, "alpha handled", 0), append([]map[string]any{{"match": "BRAVO", "sleep": 1}}, followSteps("BRAVO", relB, "bravo handled", 1500)...)...),
	})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	owner := f.newSession(t, workspace)
	f.prompt(t, workspace, owner, "ALPHA")
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return true })
	write(t, relA, "")
	testutil.Eventually(t, func() bool { return f.transcriptHas(t, workspace, owner, "alpha handled") }, wait)
	f.waitStatus(t, workspace, owner, "done")
	f.prompt(t, workspace, owner, "BRAVO")
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return len(f.calls(t, "prompt_end")) >= 2 })
	f.addPushGate(t, workspace, repo)
	f.waitRecord(t, workspace, owner, "labelled", func(r record) bool { return r.Ungated != nil })

	write(t, relB, "")

	testutil.Eventually(t, func() bool { return len(f.finishedPrompts(t)) == 1 }, wait)
	got := f.finishedPrompts(t)[0]
	assert.Contains(t, got, "make test-b")
	assert.NotContains(t, got, "make test-a")
}

func TestSecondOfTwoOverlappingCommandsGetsItsOwnPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	relA, relB := filepath.Join(dir, "alpha-release"), filepath.Join(dir, "bravo-release")
	waitA, waitB := "while [ ! -e "+relA+" ]; do sleep 0.05; done", "while [ ! -e "+relB+" ]; do sleep 0.05; done"
	outA, outB := filepath.Join(dir, "a1.output"), filepath.Join(dir, "b1.output")
	write(t, outA, "alpha output\n")
	write(t, outB, "bravo output\n")
	prompt := append(bgSteps("WATCH", "bgA", waitA, "a1", outA), bgSteps("WATCH", "bgB", waitB, "b1", outB)...)
	prompt = append(prompt, map[string]any{"match": "WATCH", "shell": waitB})
	after := append(followSteps("WATCH", relA, "alpha handled", 0),
		map[string]any{"match": "WATCH", "run": waitB},
		map[string]any{"match": "WATCH", "sleep": 1500},
		map[string]any{"match": "WATCH", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "bravo handled"}}},
		map[string]any{"match": "WATCH", "update": map[string]any{"sessionUpdate": "usage_update", "used": 1, "size": 10, "cost": map[string]any{"amount": 1, "currency": "USD"}}},
	)
	f := startEnv(t, []string{"CLUBMATE_AGENTS_HOOK_POLL=100ms"}, map[string]any{"resume": true, "prompt": prompt, "after": after})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	owner := f.newSession(t, workspace)
	f.prompt(t, workspace, owner, "WATCH")
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return true })
	f.addPushGate(t, workspace, repo)
	f.waitRecord(t, workspace, owner, "labelled", func(r record) bool { return r.Ungated != nil })
	write(t, relA, "")
	testutil.Eventually(t, func() bool { return f.transcriptHas(t, workspace, owner, "alpha handled") }, wait)

	write(t, relB, "")

	testutil.Eventually(t, func() bool { return len(f.finishedPrompts(t)) == 1 }, wait)
	got := f.finishedPrompts(t)[0]
	assert.Contains(t, got, "bravo-release")
	assert.NotContains(t, got, "alpha-release")
}
