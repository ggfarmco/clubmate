package main_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	wait           = 10 * time.Second
	restartWait    = 15 * time.Second
	gitManifest    = `{"id":"git","name":"Git","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"git.review","title":"Review","hidden":true}]},"echoResults":{"git.review":{}}}`
	editorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"editor-nvim.checktime","title":"Checktime","hidden":true}]},"echoResults":{"editor-nvim.checktime":{}}}`
	codeManifest   = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","icon":"code","order":10}],"commands":[{"id":"editor-nvim.open","provides":["editor.open"],"title":"Open File","hidden":true}]},"echoResults":{"editor-nvim.open":{}}}`
)

type record struct {
	ID             string   `json:"id"`
	Workspace      string   `json:"workspace"`
	Worktree       string   `json:"worktree"`
	Agent          string   `json:"agent"`
	AgentSessionID string   `json:"agentSessionId"`
	Status         string   `json:"status"`
	Mode           string   `json:"mode"`
	Closed         bool     `json:"closed"`
	Error          string   `json:"error"`
	Pending        []string `json:"pending"`
}

type entry struct {
	Kind    string                     `json:"kind"`
	ID      string                     `json:"id"`
	Turn    int                        `json:"turn"`
	Text    string                     `json:"text"`
	Tool    map[string]json.RawMessage `json:"tool"`
	Outcome json.RawMessage            `json:"outcome"`
}

type transcript struct {
	Session record `json:"session"`
	Modes   []struct {
		ID string `json:"id"`
	} `json:"modes"`
	Commands []struct {
		Name string `json:"name"`
	} `json:"commands"`
	Turns []struct {
		Turn          int    `json:"turn"`
		CheckpointSha string `json:"checkpointSha"`
		StopReason    string `json:"stopReason"`
	} `json:"turns"`
	Entries []entry `json:"entries"`
}

type fakeCall struct {
	PID    int             `json:"pid"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type fixture struct {
	h       *testutil.PluginHarness
	fakeLog string
}

func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.name", "t")
	git(t, repo, "config", "user.email", "t@t")
	write(t, filepath.Join(repo, "a.go"), "one\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	return repo
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func start(t *testing.T, script map[string]any, dirs ...string) *fixture {
	t.Helper()
	raw, err := json.Marshal(script)
	require.NoError(t, err)
	scriptPath := filepath.Join(t.TempDir(), "script.json")
	require.NoError(t, os.WriteFile(scriptPath, raw, 0o600))
	f := &fixture{fakeLog: filepath.Join(t.TempDir(), "fake.jsonl")}
	t.Setenv("CLUBMATE_AGENTS_COMMAND", testutil.BuildBinary(t, "clubmate/plugins/agents/internal/fakeagent"))
	t.Setenv("FAKEAGENT_SCRIPT", scriptPath)
	t.Setenv("FAKEAGENT_LOG", f.fakeLog)
	f.h = testutil.NewPluginHarness(t, append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/agents", "plugins/agents/plugin.json")}, dirs...)...)
	f.waitReady(t, restartWait)
	return f
}

func (f *fixture) waitReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "agents.ready", 0, 10)
		require.NoError(t, err)
		return len(events) > 0
	}, timeout)
}

func (f *fixture) project(t *testing.T, repo string) string {
	t.Helper()
	p, err := f.h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	return p.ID
}

func (f *fixture) run(t *testing.T, project, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "projectId": project, "args": args}, out)
}

func (f *fixture) newSession(t *testing.T, project string) string {
	t.Helper()
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, project, "agents.new", map[string]any{}, &out))
	require.NotEmpty(t, out.Session)
	return out.Session
}

func (f *fixture) prompt(t *testing.T, project, session, text string) {
	t.Helper()
	require.NoError(t, f.run(t, project, "agents.prompt", map[string]any{"session": session, "text": text}, nil))
}

func (f *fixture) session(t *testing.T, project, session string) record {
	t.Helper()
	var list []record
	require.NoError(t, f.run(t, project, "agents.sessions", map[string]any{}, &list))
	for _, r := range list {
		if r.ID == session {
			return r
		}
	}
	return record{}
}

func (f *fixture) waitStatus(t *testing.T, project, session, status string) record {
	t.Helper()
	var got record
	testutil.Eventually(t, func() bool {
		got = f.session(t, project, session)
		return got.Status == status
	}, wait)
	return got
}

func (f *fixture) transcript(t *testing.T, project, session string) transcript {
	t.Helper()
	var out transcript
	require.NoError(t, f.run(t, project, "agents.transcript", map[string]any{"session": session}, &out))
	return out
}

func (f *fixture) calls(t *testing.T, method string) []fakeCall {
	t.Helper()
	data, err := os.ReadFile(f.fakeLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var out []fakeCall
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var c fakeCall
		require.NoError(t, json.Unmarshal([]byte(line), &c))
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *fixture) waitCall(t *testing.T, method string, match func(fakeCall) bool) fakeCall {
	t.Helper()
	var got fakeCall
	testutil.Eventually(t, func() bool {
		i := slices.IndexFunc(f.calls(t, method), match)
		if i < 0 {
			return false
		}
		got = f.calls(t, method)[i]
		return true
	}, wait)
	return got
}

func (f *fixture) logLines(t *testing.T, session string) []map[string]json.RawMessage {
	t.Helper()
	file, err := os.Open(filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", session+".jsonl"))
	require.NoError(t, err)
	defer file.Close()
	var out []map[string]json.RawMessage
	r := bufio.NewReader(file)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var m map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(line, &m))
			out = append(out, m)
		}
		if err != nil {
			return out
		}
	}
}

func (f *fixture) logText(t *testing.T, session string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", session+".jsonl"))
	require.NoError(t, err)
	return string(data)
}

func updates(t *testing.T, lines []map[string]json.RawMessage, kind string) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, l := range lines {
		if string(l["kind"]) != `"session/update"` {
			continue
		}
		var head struct {
			SessionUpdate string `json:"sessionUpdate"`
		}
		require.NoError(t, json.Unmarshal(l["payload"], &head))
		if head.SessionUpdate == kind {
			out = append(out, l["payload"])
		}
	}
	return out
}

func ofKind(e []entry, kind string) []entry {
	var out []entry
	for _, x := range e {
		if x.Kind == kind {
			out = append(out, x)
		}
	}
	return out
}

func permissionScript() map[string]any {
	return map[string]any{
		"toolCall": map[string]any{"toolCallId": "bg1", "title": "Run tests", "kind": "execute"},
		"options": []map[string]string{
			{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
			{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
		},
	}
}

func TestPromptStreamsAndLogs(t *testing.T) {
	repo := newRepo(t)
	toolCall := map[string]any{
		"sessionUpdate": "tool_call", "toolCallId": "e1", "title": "Edit a.go", "kind": "edit", "status": "pending",
		"content":   []map[string]any{{"type": "diff", "path": filepath.Join(repo, "a.go"), "oldText": "one\n", "newText": "two\n"}},
		"locations": []map[string]any{{"path": filepath.Join(repo, "a.go"), "line": 1}},
	}
	chunk := func(text string) map[string]any {
		return map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}}}
	}
	f := start(t, map[string]any{"prompt": []map[string]any{
		chunk("Hel"), chunk("lo "), chunk("world"),
		{"update": toolCall},
		{"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "e1", "status": "completed"}},
	}}, testutil.EchoPluginDirAs(t, editorManifest))
	project := f.project(t, repo)
	session := f.newSession(t, project)

	f.prompt(t, project, session, "hi")
	f.waitStatus(t, project, session, "done")

	tr := f.transcript(t, project, session)
	agent := ofKind(tr.Entries, "agent")
	require.Len(t, agent, 1)
	assert.Equal(t, "Hello world", agent[0].Text)
	tools := ofKind(tr.Entries, "tool")
	require.Len(t, tools, 1)
	assert.Equal(t, "e1", tools[0].ID)
	assert.JSONEq(t, `"completed"`, string(tools[0].Tool["status"]))
	assert.Contains(t, string(tools[0].Tool["content"]), `"diff"`)

	lines := f.logLines(t, session)
	chunks := updates(t, lines, "agent_message_chunk")
	require.Len(t, chunks, 1)
	assert.Contains(t, string(chunks[0]), `"text":"Hello world"`)
	logged := updates(t, lines, "tool_call")
	require.Len(t, logged, 1)
	want, err := json.Marshal(toolCall)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(logged[0]))
	for i, l := range lines {
		for _, k := range []string{"workspace", "worktree", "session", "parentSession", "role", "turn", "seq", "actor", "ts", "kind", "payload"} {
			assert.Contains(t, l, k)
		}
		assert.JSONEq(t, `"`+session+`"`, string(l["session"]))
		assert.JSONEq(t, `"`+project+`"`, string(l["workspace"]))
		var seq int
		require.NoError(t, json.Unmarshal(l["seq"], &seq))
		assert.Equal(t, i+1, seq)
	}
	actors := map[string]string{}
	for _, l := range lines {
		actors[string(l["kind"])] = string(l["actor"])
	}
	assert.Equal(t, `"user"`, actors[`"session/prompt"`])
	assert.Equal(t, `"agent:claude"`, actors[`"session/update"`])

	changed, err := f.h.Bus.Read(t.Context(), "agents.changed", 0, 1000)
	require.NoError(t, err)
	require.NotEmpty(t, changed)
	for _, e := range changed {
		assert.Contains(t, string(e.Payload), `"session":"`+session+`"`)
		assert.Less(t, len(e.Payload), 1024)
	}
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "echo.command", 0, 100)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			return strings.Contains(string(e.Payload), `"editor-nvim.checktime"`) && strings.Contains(string(e.Payload), repo)
		})
	}, wait)
}

func TestReaderNeverBlocks(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"burst": 5000}}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)

	f.prompt(t, project, session, "flood")
	for range 5 {
		began := time.Now()
		f.session(t, project, session)
		assert.Less(t, time.Since(began), time.Second)
	}
	f.waitStatus(t, project, session, "done")

	assert.Len(t, updates(t, f.logLines(t, session), "tool_call"), 5000)
}

func TestPermissionOutsidePrompt(t *testing.T) {
	f := start(t, map[string]any{"after": []map[string]any{{"permission": permissionScript()}}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)

	f.prompt(t, project, session, "go")
	waiting := f.waitStatus(t, project, session, "waiting")
	perms := ofKind(f.transcript(t, project, session).Entries, "permission")
	require.Len(t, perms, 1)
	assert.Equal(t, []string{perms[0].ID}, waiting.Pending)
	require.NoError(t, f.run(t, project, "agents.permission", map[string]any{"session": session, "request": perms[0].ID, "option": "allow"}, nil))

	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"selected","optionId":"allow"}}`, string(got.Params))
	f.waitStatus(t, project, session, "done")
}

func TestCancelAnswersPendingPermission(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)

	f.prompt(t, project, session, "go")
	f.waitStatus(t, project, session, "waiting")
	require.NoError(t, f.run(t, project, "agents.cancel", map[string]any{"session": session}, nil))

	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"cancelled"}}`, string(got.Params))
	f.waitStatus(t, project, session, "done")
	tr := f.transcript(t, project, session)
	require.Len(t, tr.Turns, 1)
	assert.Equal(t, "cancelled", tr.Turns[0].StopReason)
}

func TestAgentCrash(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"child": true},
		{"stderr": "fatal: adapter blew up"},
		{"exit": 3},
	}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)

	f.prompt(t, project, session, "go")
	got := f.waitStatus(t, project, session, "error")

	assert.Contains(t, got.Error, "fatal: adapter blew up")
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	child := f.calls(t, "child")
	require.Len(t, child, 1)
	testutil.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(-starts[0].PID, 0), syscall.ESRCH)
	}, wait)
}

func TestTwoWorktrees(t *testing.T) {
	repo := newRepo(t)
	tree := filepath.Join(t.TempDir(), "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", tree)
	tree, err := filepath.EvalSymlinks(tree)
	require.NoError(t, err)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	first, second := f.project(t, repo), f.project(t, tree)
	a, b := f.newSession(t, first), f.newSession(t, second)

	f.prompt(t, first, a, "alpha")
	f.prompt(t, second, b, "beta")
	f.waitStatus(t, first, a, "done")
	f.waitStatus(t, second, b, "done")

	news := f.calls(t, "session/new")
	require.Len(t, news, 2)
	assert.NotEqual(t, news[0].PID, news[1].PID)
	assert.Equal(t, "alpha", ofKind(f.transcript(t, first, a).Entries, "agent")[0].Text)
	assert.Equal(t, "beta", ofKind(f.transcript(t, second, b).Entries, "agent")[0].Text)
	assert.NotContains(t, f.logText(t, a), "beta")
	assert.NotContains(t, f.logText(t, b), "alpha")
	assert.Equal(t, tree, f.session(t, second, b).Worktree)
}

func TestCheckpointPerTurn(t *testing.T) {
	repo := newRepo(t)
	write(t, filepath.Join(repo, "a.go"), "two\n")
	write(t, filepath.Join(repo, "b.go"), "staged\n")
	git(t, repo, "add", "b.go")
	write(t, filepath.Join(repo, "u.txt"), "untracked\n")
	head, status, index := git(t, repo, "rev-parse", "HEAD"), git(t, repo, "status", "--porcelain"), git(t, repo, "ls-files", "-s")
	f := start(t, map[string]any{})
	project := f.project(t, repo)
	session := f.newSession(t, project)

	f.prompt(t, project, session, "go")
	f.waitStatus(t, project, session, "done")

	tr := f.transcript(t, project, session)
	require.Len(t, tr.Turns, 1)
	sha := tr.Turns[0].CheckpointSha
	assert.Equal(t, sha, git(t, repo, "rev-parse", "refs/clubmate/cp/"+session+"/1"))
	assert.Equal(t, head, git(t, repo, "rev-parse", sha+"^"))
	assert.Contains(t, strings.Fields(git(t, repo, "ls-tree", "-r", "--name-only", sha)), "u.txt")
	assert.Equal(t, "two", git(t, repo, "show", sha+":a.go"))
	assert.Equal(t, head, git(t, repo, "rev-parse", "HEAD"))
	assert.Equal(t, status, git(t, repo, "status", "--porcelain"))
	assert.Equal(t, index, git(t, repo, "ls-files", "-s"))

	require.NoError(t, f.run(t, project, "agents.delete", map[string]any{"session": session}, nil))
	assert.Empty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))
}

func TestResume(t *testing.T) {
	f := start(t, map[string]any{"resume": true})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)
	var stored string
	testutil.Eventually(t, func() bool {
		stored = f.session(t, project, session).AgentSessionID
		return stored != ""
	}, wait)

	pids := f.h.Sup.PIDs()
	require.Len(t, pids, 1)
	require.NoError(t, syscall.Kill(pids[0], syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		now := f.h.Sup.PIDs()
		return len(now) == 1 && now[0] != pids[0] && f.run(t, project, "agents.sessions", map[string]any{}, nil) == nil
	}, restartWait)
	first := f.calls(t, "start")
	require.Len(t, first, 1)
	testutil.Eventually(t, func() bool { return errors.Is(syscall.Kill(first[0].PID, 0), syscall.ESRCH) }, wait)
	require.NoError(t, f.run(t, project, "agents.resume", map[string]any{"session": session}, nil))

	got := f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
	var params struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(got.Params, &params))
	assert.Equal(t, stored, params.SessionID)
}

func TestModeAndCommands(t *testing.T) {
	f := start(t, map[string]any{
		"modes":    map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}, {"id": "plan", "name": "Plan"}}},
		"commands": []map[string]string{{"name": "review", "description": "Review the diff"}},
	})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)

	testutil.Eventually(t, func() bool {
		tr := f.transcript(t, project, session)
		return len(tr.Commands) == 1 && len(tr.Modes) == 2
	}, wait)
	tr := f.transcript(t, project, session)
	assert.Equal(t, "review", tr.Commands[0].Name)
	assert.Equal(t, []string{"default", "plan"}, []string{tr.Modes[0].ID, tr.Modes[1].ID})
	assert.Equal(t, "default", tr.Session.Mode)

	require.NoError(t, f.run(t, project, "agents.mode", map[string]any{"session": session, "mode": "plan"}, nil))

	got := f.waitCall(t, "session/set_mode", func(fakeCall) bool { return true })
	assert.Contains(t, string(got.Params), `"modeId":"plan"`)
	testutil.Eventually(t, func() bool { return f.transcript(t, project, session).Session.Mode == "plan" }, wait)
}

func TestDeleteDuringTurn(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 1500}}})
	project := f.project(t, repo)
	session, keeper := f.newSession(t, project), f.newSession(t, project)
	testutil.Eventually(t, func() bool { return f.session(t, project, keeper).AgentSessionID != "" }, wait)

	f.prompt(t, project, session, "slow")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	require.NoError(t, f.run(t, project, "agents.delete", map[string]any{"session": session}, nil))
	f.waitCall(t, "prompt_end", func(fakeCall) bool { return true })

	path := filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", session+".jsonl")
	require.Never(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 500*time.Millisecond, 20*time.Millisecond)
	assert.Empty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))
}

func TestPermissionAfterAgentRestart(t *testing.T) {
	f := start(t, map[string]any{"resume": true, "prompt": []map[string]any{{"permission": permissionScript()}}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)
	f.prompt(t, project, session, "one")
	f.waitStatus(t, project, session, "waiting")
	first := ofKind(f.transcript(t, project, session).Entries, "permission")
	require.Len(t, first, 1)
	require.NoError(t, f.run(t, project, "agents.permission", map[string]any{"session": session, "request": first[0].ID, "option": "allow"}, nil))
	f.waitStatus(t, project, session, "done")
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	require.NoError(t, syscall.Kill(starts[0].PID, syscall.SIGKILL))
	f.waitStatus(t, project, session, "error")

	f.prompt(t, project, session, "two")
	f.waitStatus(t, project, session, "waiting")

	perms := ofKind(f.transcript(t, project, session).Entries, "permission")
	require.Len(t, perms, 2)
	assert.NotEqual(t, perms[0].ID, perms[1].ID)
	assert.Equal(t, 2, perms[1].Turn)
	assert.Empty(t, perms[1].Outcome)
	assert.Len(t, f.calls(t, "session/resume"), 1)
}

func TestStopRoot(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	project := f.project(t, repo)
	session := f.newSession(t, project)
	f.prompt(t, project, session, "go")
	f.waitStatus(t, project, session, "done")
	require.NotEmpty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))

	require.NoError(t, f.run(t, project, "agents.stop-root", map[string]any{"root": repo}, nil))

	assert.True(t, f.session(t, project, session).Closed)
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	assert.ErrorIs(t, syscall.Kill(starts[0].PID, 0), syscall.ESRCH)
	assert.Empty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))
}

func cardsOf(n sdui.Node, typ string) []sdui.Node {
	var out []sdui.Node
	if n.Type == typ {
		out = append(out, n)
	}
	for _, c := range n.Children {
		out = append(out, cardsOf(c, typ)...)
	}
	return out
}

func echoed(t *testing.T, f *fixture, command string) json.RawMessage {
	t.Helper()
	var args json.RawMessage
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "echo.command", 0, 100)
		require.NoError(t, err)
		for _, e := range events {
			var p struct {
				ID   string          `json:"id"`
				Args json.RawMessage `json:"args"`
			}
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			if p.ID == command {
				args = p.Args
				return true
			}
		}
		return false
	}, wait)
	return args
}

func TestPermissionCardAction(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)
	f.prompt(t, project, session, "go")

	main := f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool {
		cards := cardsOf(n, "permissionCard")
		return len(cards) == 1 && cards[0].Props["outcome"] == nil
	})
	card := cardsOf(main, "permissionCard")[0]
	assert.Equal(t, "Claude wants to run Run tests", card.Props["text"])
	f.h.Event(t, project, "agents.main", card.ID, "permission", map[string]any{"request": card.Props["request"], "option": "allow"})

	f.waitCall(t, "permission_response", func(c fakeCall) bool { return strings.Contains(string(c.Params), `"optionId":"allow"`) })
	f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool {
		cards := cardsOf(n, "permissionCard")
		return len(cards) == 1 && cards[0].Props["outcome"] == "allow"
	})
}

func TestReviewActionUsesCheckpoint(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}}, testutil.EchoPluginDirAs(t, gitManifest))
	project := f.project(t, repo)
	session := f.newSession(t, project)
	f.prompt(t, project, session, "change it")
	f.waitStatus(t, project, session, "done")
	sha := f.transcript(t, project, session).Turns[0].CheckpointSha
	require.NotEmpty(t, sha)

	f.h.Event(t, project, "agents.main", session+"/review", "review", nil)

	assert.JSONEq(t, `{"base":"commit:`+sha+`","head":"`+repo+`","baseLabel":"before turn 1"}`, string(echoed(t, f, "git.review")))
}

func TestOpenLocationShowsEditor(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, "a.go")
	read := map[string]any{"sessionUpdate": "tool_call", "toolCallId": "r1", "title": "Read a.go", "kind": "read", "status": "completed", "locations": []map[string]any{{"path": path, "line": 1}}}
	f := start(t, map[string]any{"prompt": []map[string]any{{"update": read}}}, testutil.EchoPluginDirAs(t, codeManifest))
	project := f.project(t, repo)
	session := f.newSession(t, project)
	f.prompt(t, project, session, "read it")
	main := f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "toolCard")) == 1 })
	card := cardsOf(main, "toolCard")[0]
	c := f.h.Client(t)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": project}, nil))

	f.h.Event(t, project, "agents.main", card.ID, "openLocation", map[string]any{"path": path, "line": 1})

	assert.JSONEq(t, `{"path":"`+path+`","line":1}`, string(echoed(t, f, "editor-nvim.open")))
	deadline := time.After(wait)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method != "ui.focus" {
				continue
			}
			var got struct{ Section, Slot string }
			require.NoError(t, json.Unmarshal(n.Params, &got))
			assert.Equal(t, "editor-nvim", got.Section)
			assert.Equal(t, "editor-nvim.main", got.Slot)
			return
		case <-deadline:
			t.Fatal("no ui.focus notification")
		}
	}
}

func TestSignInReattaches(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "authed")
	f := start(t, map[string]any{"authFile": authFile})
	project := f.project(t, newRepo(t))
	session := f.newSession(t, project)
	f.waitStatus(t, project, session, "error")
	require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))

	f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.Props["action"] == "signIn" })
	})
	f.h.Event(t, project, "agents.main", session+"/signin", "signIn", nil)

	testutil.Eventually(t, func() bool {
		require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))
		return f.session(t, project, session).Status == "idle"
	}, wait)
	assert.NotEmpty(t, f.session(t, project, session).AgentSessionID)
	assert.FileExists(t, authFile)
}

func (f *fixture) rev(t *testing.T, project, slot string) int64 {
	t.Helper()
	var views []struct {
		Slot string `json:"slot"`
		Rev  int64  `json:"rev"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"projectId": project}, &views))
	for _, v := range views {
		if v.Slot == slot {
			return v.Rev
		}
	}
	return 0
}

func TestBackgroundSessionKeepsMainUnrendered(t *testing.T) {
	steps := []map[string]any{}
	for i := range 30 {
		steps = append(steps, map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": strconv.Itoa(i)}}}, map[string]any{"sleep": 10})
	}
	f := start(t, map[string]any{"prompt": steps})
	project := f.project(t, newRepo(t))
	background := f.newSession(t, project)
	open := f.newSession(t, project)
	f.waitStatus(t, project, background, "idle")
	f.waitStatus(t, project, open, "idle")
	f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool {
		return len(cardsOf(n, "composer")) == 1 && strings.HasPrefix(cardsOf(n, "composer")[0].ID, open+"/")
	})
	time.Sleep(300 * time.Millisecond)
	main, sidebar := f.rev(t, project, "agents.main"), f.rev(t, project, "agents.sidebar")

	f.prompt(t, project, background, "go")
	f.waitStatus(t, project, background, "done")
	time.Sleep(300 * time.Millisecond)

	assert.Equal(t, main, f.rev(t, project, "agents.main"))
	assert.Greater(t, f.rev(t, project, "agents.sidebar"), sidebar)
}

func TestContinueInNewSession(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	project := f.project(t, repo)
	old := f.newSession(t, project)
	f.waitStatus(t, project, old, "idle")
	require.NoError(t, f.run(t, project, "agents.close", map[string]any{"session": old}, nil))

	f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.ID == old+"/continue" })
	})
	f.h.Event(t, project, "agents.main", old+"/continue", "continue", nil)

	var list []record
	require.NoError(t, f.run(t, project, "agents.sessions", map[string]any{}, &list))
	require.Len(t, list, 2)
	fresh := list[1]
	assert.NotEqual(t, old, fresh.ID)
	assert.Equal(t, repo, fresh.Worktree)
	f.h.WaitSlot(t, project, "agents.main", func(n sdui.Node) bool {
		return len(cardsOf(n, "transcript")) == 1 && cardsOf(n, "transcript")[0].ID == fresh.ID+"/transcript"
	})
	assert.True(t, f.session(t, project, old).Closed)
}
