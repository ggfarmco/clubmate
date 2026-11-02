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
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

const (
	wait             = 10 * time.Second
	restartWait      = 15 * time.Second
	diffviewManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"editor-nvim.diffview","title":"Diffview","hidden":true}]},"echoResults":{"editor-nvim.diffview":{}}}`
	editorManifest   = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"editor-nvim.checktime","title":"Checktime","hidden":true}]},"echoResults":{"editor-nvim.checktime":{}}}`
	codeManifest     = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","icon":"code","order":10}],"commands":[{"id":"editor-nvim.open","provides":["editor.open"],"title":"Open File","hidden":true}]},"echoResults":{"editor-nvim.open":{}}}`
	docsManifest     = `{"id":"docs","name":"Docs","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"docs.open","title":"Open Doc","hidden":true}]},"echoResults":{"docs.open":{}}}`
	stopManifest     = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"editor-nvim.stop-root","title":"Stop","hidden":true,"handles":["git.worktree.removing"]},{"id":"editor-nvim.release-root","title":"Release","hidden":true,"handles":["git.worktree.released"]}]},"echoResults":{"editor-nvim.stop-root":{},"editor-nvim.release-root":{}}}`
)

type record struct {
	ID             string   `json:"id"`
	Workspace      string   `json:"workspace"`
	Worktree       string   `json:"worktree"`
	Agent          string   `json:"agent"`
	AgentSessionID string   `json:"agentSessionId"`
	Title          string   `json:"title"`
	ParentSession  string   `json:"parentSession"`
	HandoffFrom    string   `json:"handoffFrom"`
	HandoffTo      string   `json:"handoffTo"`
	ImportedFrom   string   `json:"importedFrom"`
	Notify         string   `json:"notify"`
	Advisor        string   `json:"advisor"`
	Status         string   `json:"status"`
	Mode           string   `json:"mode"`
	Finish         string   `json:"finish"`
	AFK            bool     `json:"afk"`
	CronJob        string   `json:"cronJob"`
	Error          string   `json:"error"`
	Pending        []string `json:"pending"`
	Asks           bool     `json:"asks"`
	PendingSummary *struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"pendingSummary"`
	Suspended       bool     `json:"suspended"`
	AutoClosed      bool     `json:"autoClosed"`
	Service         bool     `json:"service"`
	Review          bool     `json:"review"`
	NeedsYou        bool     `json:"needsYou"`
	AsksParent      bool     `json:"asksParent"`
	ChildStatus     string   `json:"childStatus"`
	DisallowedTools []string `json:"disallowedTools"`
	Failure         *struct {
		Badge   string `json:"badge"`
		Actions []struct {
			ID      string            `json:"id"`
			Command string            `json:"command"`
			Args    map[string]string `json:"args"`
		} `json:"actions"`
	} `json:"failure"`
	ModeLabel string `json:"modeLabel"`
}

type entry struct {
	Kind      string                     `json:"kind"`
	ID        string                     `json:"id"`
	Turn      int                        `json:"turn"`
	Text      string                     `json:"text"`
	Tool      map[string]json.RawMessage `json:"tool"`
	Outcome   json.RawMessage            `json:"outcome"`
	Notice    json.RawMessage            `json:"notice"`
	Subagent  string                     `json:"subagent"`
	Stoppable bool                       `json:"stoppable"`
	Voice     json.RawMessage            `json:"voice"`
	DocCards  []sdui.DocCard             `json:"docCards"`
}

type transcript struct {
	Session record `json:"session"`
	Advisor string `json:"advisor"`
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
	subs    map[string]string
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
	f := &fixture{fakeLog: filepath.Join(t.TempDir(), "fake.jsonl"), subs: map[string]string{}}
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

func (f *fixture) workspace(t *testing.T, repo string) string {
	t.Helper()
	p, err := f.h.Svc.CreateWorkspace(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	f.subscribe(t, p.ID)
	return p.ID
}

func (f *fixture) subscribe(t *testing.T, workspace string) {
	t.Helper()
	var out struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": workspace}, &out))
	f.subs[workspace] = out.Subscription
}

func (f *fixture) run(t *testing.T, workspace, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "workspaceId": workspace, "args": args}, out)
}

func (f *fixture) runAs(t *testing.T, workspace, caller, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "workspaceId": workspace, "args": args, "agent": true, "caller": caller}, out)
}

func (f *fixture) answerPermission(t *testing.T, workspace, session, request string) {
	t.Helper()
	f.h.Event(t, workspace, "agents.main", session+"/permission/"+request, "permission", map[string]any{"request": request, "option": "allow"})
}

func (f *fixture) newSession(t *testing.T, workspace string) string {
	t.Helper()
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{}, &out))
	require.NotEmpty(t, out.Session)
	return out.Session
}

func (f *fixture) prompt(t *testing.T, workspace, session, text string) {
	t.Helper()
	require.NoError(t, f.run(t, workspace, "agents.prompt", map[string]any{"session": session, "text": text}, nil))
}

func (f *fixture) session(t *testing.T, workspace, session string) record {
	t.Helper()
	var list []record
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	for _, r := range list {
		if r.ID == session {
			return r
		}
	}
	return record{}
}

func (f *fixture) waitStatus(t *testing.T, workspace, session, status string) record {
	t.Helper()
	var got record
	testutil.Eventually(t, func() bool {
		got = f.session(t, workspace, session)
		return got.Status == status
	}, wait)
	return got
}

func (f *fixture) transcript(t *testing.T, workspace, session string) transcript {
	t.Helper()
	var out transcript
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &out))
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
	for line := range strings.Lines(string(data)) {
		if !strings.HasSuffix(line, "\n") {
			break
		}
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

func TestNewTitleSurvivesAgentTitle(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"update": map[string]any{"sessionUpdate": "session_info_update", "title": "Agent title"}},
	}})
	workspace := f.workspace(t, newRepo(t))
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"title": "Mine", "prompt": "go"}, &out))
	assert.Equal(t, "Mine", f.waitStatus(t, workspace, out.Session, "done").Title)
}

func TestAgentTitleIgnoresSenderTitle(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"update": map[string]any{"sessionUpdate": "session_info_update", "title": "orchestrate"}},
	}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "orchestrate")
	f.waitStatus(t, workspace, parent, "done")

	out := f.newChild(t, workspace, parent, map[string]any{"prompt": "hi"})

	assert.Equal(t, "hi", f.waitStatus(t, workspace, out.Session, "done").Title)
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
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "hi")
	f.waitStatus(t, workspace, session, "done")

	tr := f.transcript(t, workspace, session)
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
		assert.JSONEq(t, `"`+workspace+`"`, string(l["workspace"]))
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

func TestStoppedTranscriptClosesOpenToolsWithoutRawOutputAndCompactDropsImages(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "w1", "title": "Wait", "kind": "other", "status": "in_progress", "rawOutput": "partial", "_meta": map[string]any{"claudeCode": map[string]any{"toolResponse": "partial"}},
			"content": []map[string]any{{"type": "content", "content": map[string]any{"type": "image", "data": "AAAA", "mimeType": "image/png"}}, {"type": "content", "content": map[string]any{"type": "text", "text": "seen"}}}}},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	full := ofKind(f.transcript(t, workspace, session).Entries, "tool")
	require.Len(t, full, 1)
	assert.JSONEq(t, `"cancelled"`, string(full[0].Tool["status"]))
	assert.NotContains(t, full[0].Tool, "rawOutput")
	assert.NotContains(t, full[0].Tool, "_meta")

	var compact transcript
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session, "compact": true}, &compact))
	tools := ofKind(compact.Entries, "tool")
	require.Len(t, tools, 1)
	assert.JSONEq(t, `[{"type":"content","content":{"type":"text","text":"seen"}}]`, string(tools[0].Tool["content"]))
}

func TestTranscriptLastKeepsOnlyTheLastTurnWithoutCatalogs(t *testing.T) {
	f := start(t, map[string]any{
		"prompt":   []map[string]any{{"echo": true}},
		"modes":    map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}}},
		"commands": []map[string]string{{"name": "review", "description": "Review the diff"}},
	})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	for _, text := range []string{"one", "two"} {
		f.prompt(t, workspace, session, text)
		f.waitStatus(t, workspace, session, "done")
	}
	require.NotEmpty(t, f.transcript(t, workspace, session).Modes)

	var last transcript
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session, "last": true}, &last))
	require.Len(t, last.Turns, 1)
	agent := ofKind(last.Entries, "agent")
	require.Len(t, agent, 1)
	assert.Equal(t, "two", agent[0].Text)
	for _, en := range last.Entries {
		assert.Equal(t, last.Turns[0].Turn, en.Turn)
	}
	assert.Empty(t, last.Modes)
	assert.Empty(t, last.Commands)
}

func TestReaderNeverBlocks(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"burst": 5000}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "flood")
	for range 5 {
		began := time.Now()
		f.session(t, workspace, session)
		assert.Less(t, time.Since(began), time.Second)
	}
	f.waitStatus(t, workspace, session, "done")

	assert.Len(t, updates(t, f.logLines(t, session), "tool_call"), 5000)
	changed, err := f.h.Bus.Read(t.Context(), "agents.changed", 0, 10000)
	require.NoError(t, err)
	assert.Less(t, len(changed), 10)
	for _, e := range changed {
		assert.NotContains(t, string(e.Payload), `"kind":"update"`)
	}
}

func TestDocsAnAgentOpensOrPresentsBecomePresentedCards(t *testing.T) {
	repo := newRepo(t)
	for _, name := range []string{"spec.md", "plan.md"} {
		write(t, filepath.Join(repo, name), "# "+name+"\n")
	}
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Wrote `spec.md`."}}},
	}})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "hi")
	f.waitStatus(t, workspace, session, "done")

	c := f.h.Client(t)
	for topic, name := range map[string]string{"docs.opened": "spec.md", "docs.presented": "plan.md"} {
		require.NoError(t, c.Call(t.Context(), "bus.publish", map[string]any{"topic": topic, "payload": map[string]string{"workspace": workspace, "session": session, "path": filepath.Join(repo, name)}}, nil))
	}

	testutil.Eventually(t, func() bool {
		agent := ofKind(f.transcript(t, workspace, session).Entries, "agent")
		return len(agent) == 1 && len(agent[0].DocCards) == 2 && agent[0].DocCards[0].Presented && agent[0].DocCards[1].Presented
	}, wait)
}

func TestPermissionOutsidePrompt(t *testing.T) {
	f := start(t, map[string]any{"after": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	waiting := f.waitStatus(t, workspace, session, "waiting")
	perms := ofKind(f.transcript(t, workspace, session).Entries, "permission")
	require.Len(t, perms, 1)
	assert.Equal(t, []string{perms[0].ID}, waiting.Pending)
	f.answerPermission(t, workspace, session, perms[0].ID)

	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"selected","optionId":"allow"}}`, string(got.Params))
	f.waitStatus(t, workspace, session, "done")
}

func TestAnsweredPermissionPublishesResolved(t *testing.T) {
	f := start(t, map[string]any{"after": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")
	perms := ofKind(f.transcript(t, workspace, session).Entries, "permission")
	require.Len(t, perms, 1)
	f.answerPermission(t, workspace, session, perms[0].ID)

	f.waitResolved(t, api.Changed{Workspace: workspace, Session: session, Kind: api.ChangeResolved, Request: perms[0].ID, RequestKind: api.RequestPermission})
}

func TestPendingRequestsPostNotifications(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}, {"elicitation": askScript(false)}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")
	perms := ofKind(f.transcript(t, workspace, session).Entries, "permission")
	require.Len(t, perms, 1)
	f.answerPermission(t, workspace, session, perms[0].ID)
	f.waitStatus(t, workspace, session, "waiting")

	title := f.session(t, workspace, session).Title
	type posted struct {
		Workspace, Session, Title, Message, Kind string
	}
	want := []posted{
		{workspace, session, title, "Wants to run a command", "permission"},
		{workspace, session, title, "Has a question", "question"},
	}
	var got []posted
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "notify.posted", 0, 100)
		require.NoError(t, err)
		got = got[:0]
		for _, e := range events {
			var p posted
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			got = append(got, p)
		}
		return len(got) == len(want)
	}, wait)
	assert.Equal(t, want, got)
}

func (f *fixture) waitResolved(t *testing.T, want api.Changed) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), api.TopicChanged, 0, 1000)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var got api.Changed
			err := json.Unmarshal(e.Payload, &got)
			got.Status = ""
			return err == nil && got == want
		})
	}, wait)
}

func TestCancelAnswersPendingPermission(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")
	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"cancelled"}}`, string(got.Params))
	f.waitStatus(t, workspace, session, "done")
	tr := f.transcript(t, workspace, session)
	require.Len(t, tr.Turns, 1)
	assert.Equal(t, "cancelled", tr.Turns[0].StopReason)
}

func TestAgentCrash(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"child": true},
		{"stderr": "fatal: adapter blew up"},
		{"exit": 3},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	got := f.waitStatus(t, workspace, session, "error")

	assert.Contains(t, got.Error, "fatal: adapter blew up")
	require.NotNil(t, got.Failure)
	assert.Contains(t, got.Failure.Badge, "Stopped · ")
	require.Len(t, got.Failure.Actions, 1)
	assert.Equal(t, "agents.handoff", got.Failure.Actions[0].Command)
	assert.Equal(t, map[string]string{"session": session}, got.Failure.Actions[0].Args)
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
	first, second := f.workspace(t, repo), f.workspace(t, tree)
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

func TestWorkspacesOnOneRootGetOwnAgents(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	first, second := f.workspace(t, repo), f.workspace(t, repo)
	a, b := f.newSession(t, first), f.newSession(t, second)

	testutil.Eventually(t, func() bool {
		return f.session(t, first, a).AgentSessionID != "" && f.session(t, second, b).AgentSessionID != ""
	}, wait)

	starts := f.calls(t, "start")
	require.Len(t, starts, 2)
	assert.NotEqual(t, starts[0].PID, starts[1].PID)
	var workspaces []string
	for _, s := range starts {
		var p struct {
			Workspace string `json:"workspace"`
		}
		require.NoError(t, json.Unmarshal(s.Params, &p))
		workspaces = append(workspaces, p.Workspace)
	}
	assert.ElementsMatch(t, []string{first, second}, workspaces)
}

func TestCheckpointPerTurn(t *testing.T) {
	repo := newRepo(t)
	write(t, filepath.Join(repo, "a.go"), "two\n")
	write(t, filepath.Join(repo, "b.go"), "staged\n")
	git(t, repo, "add", "b.go")
	write(t, filepath.Join(repo, "u.txt"), "untracked\n")
	head, status, index := git(t, repo, "rev-parse", "HEAD"), git(t, repo, "status", "--porcelain"), git(t, repo, "ls-files", "-s")
	f := start(t, map[string]any{})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")

	tr := f.transcript(t, workspace, session)
	require.Len(t, tr.Turns, 1)
	sha := tr.Turns[0].CheckpointSha
	assert.Equal(t, sha, git(t, repo, "rev-parse", "refs/clubmate/cp/"+session+"/1"))
	assert.Equal(t, head, git(t, repo, "rev-parse", sha+"^"))
	assert.Contains(t, strings.Fields(git(t, repo, "ls-tree", "-r", "--name-only", sha)), "u.txt")
	assert.Equal(t, "two", git(t, repo, "show", sha+":a.go"))
	assert.Equal(t, head, git(t, repo, "rev-parse", "HEAD"))
	assert.Equal(t, status, git(t, repo, "status", "--porcelain"))
	assert.Equal(t, index, git(t, repo, "ls-files", "-s"))

	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": session}, nil))
	assert.Empty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))
}

func (f *fixture) restartPlugin(t *testing.T, workspace string) {
	t.Helper()
	pids := f.h.Sup.PIDs()
	require.Len(t, pids, 1)
	first := f.calls(t, "start")
	require.NotEmpty(t, first)
	require.NoError(t, syscall.Kill(pids[0], syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		now := f.h.Sup.PIDs()
		return len(now) == 1 && now[0] != pids[0] && f.run(t, workspace, "agents.sessions", map[string]any{}, nil) == nil
	}, restartWait)
	testutil.Eventually(t, func() bool {
		return !slices.ContainsFunc(first, func(c fakeCall) bool { return !errors.Is(syscall.Kill(c.PID, 0), syscall.ESRCH) })
	}, wait)
}

func TestResume(t *testing.T) {
	f := start(t, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	stored := f.waitStatus(t, workspace, session, "done").AgentSessionID

	f.restartPlugin(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": session}, nil))

	got := f.waitCall(t, "session/resume", func(fakeCall) bool { return true })
	var params struct {
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(got.Params, &params))
	assert.Equal(t, stored, params.SessionID)
	for _, call := range []fakeCall{f.calls(t, "session/new")[0], got} {
		var meta struct {
			Meta struct {
				ClaudeCode struct {
					Options struct {
						Env map[string]string `json:"env"`
					} `json:"options"`
				} `json:"claudeCode"`
			} `json:"_meta"`
		}
		require.NoError(t, json.Unmarshal(call.Params, &meta))
		assert.Equal(t, map[string]string{"CLUBMATE_AGENT_SESSION": session}, meta.Meta.ClaudeCode.Options.Env, call.Method)
	}
}

func TestUnpromptedSessionStartsFreshAfterRestart(t *testing.T) {
	f := start(t, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).AgentSessionID != "" }, wait)

	f.restartPlugin(t, workspace)
	f.prompt(t, workspace, session, "hi")

	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "hi" })
	assert.Len(t, f.calls(t, "session/new"), 2)
	assert.Empty(t, f.calls(t, "session/resume"))
}

func TestFailedAttachKeepsPrompt(t *testing.T) {
	f := start(t, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitStatus(t, workspace, session, "done")
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	require.NoError(t, syscall.Kill(starts[0].PID, syscall.SIGKILL))
	f.waitStatus(t, workspace, session, "error")

	f.prompt(t, workspace, session, "two")

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(buttonsWith(n, "unqueue")) == 1 })
	assert.Equal(t, "the agent cannot resume this session", f.waitStatus(t, workspace, session, "error").Error)
}

func TestModeAndCommands(t *testing.T) {
	f := start(t, map[string]any{
		"modes":    map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}, {"id": "plan", "name": "Plan"}}},
		"commands": []map[string]string{{"name": "review", "description": "Review the diff"}},
	})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	testutil.Eventually(t, func() bool {
		tr := f.transcript(t, workspace, session)
		return len(tr.Commands) == 1 && len(tr.Modes) == 2
	}, wait)
	tr := f.transcript(t, workspace, session)
	assert.Equal(t, "review", tr.Commands[0].Name)
	assert.Equal(t, []string{"default", "plan"}, []string{tr.Modes[0].ID, tr.Modes[1].ID})
	assert.Equal(t, "default", tr.Session.Mode)

	require.NoError(t, f.run(t, workspace, "agents.mode", map[string]any{"session": session, "mode": "plan"}, nil))

	got := f.waitCall(t, "session/set_mode", func(fakeCall) bool { return true })
	assert.Contains(t, string(got.Params), `"modeId":"plan"`)
	testutil.Eventually(t, func() bool { return f.transcript(t, workspace, session).Session.Mode == "plan" }, wait)
	assert.Equal(t, "Plan", f.session(t, workspace, session).ModeLabel)
}

func TestConfigOptionsAndAdvisor(t *testing.T) {
	f := start(t, map[string]any{"configs": []map[string]any{
		{"id": "mode", "name": "Mode", "category": "mode", "type": "select", "currentValue": "default", "options": []map[string]string{{"value": "default", "name": "Manual"}}},
		{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "default", "options": []map[string]string{{"value": "default", "name": "Default"}, {"value": "opus", "name": "Opus"}}},
	}})
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	pickers := func(session string, want map[string]string) sdui.Node {
		return f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
			composers := cardsOf(n, "composer")
			if len(composers) != 1 || !strings.HasPrefix(composers[0].ID, session+"/") {
				return false
			}
			got := map[string]string{}
			list, _ := composers[0].Props["pickers"].([]any)
			for _, p := range list {
				m, _ := p.(map[string]any)
				id, _ := m["id"].(string)
				got[id], _ = m["value"].(string)
			}
			return assert.ObjectsAreEqual(want, got)
		})
	}
	composer := cardsOf(pickers(first, map[string]string{"model": "default", "advisor": "", "finish": "ask", "afk": "false"}), "composer")[0].ID

	f.h.Event(t, workspace, "agents.main", composer, "config", map[string]string{"id": "model", "value": "opus"})
	f.h.Event(t, workspace, "agents.main", composer, "config", map[string]string{"id": "advisor", "value": "opus"})
	f.h.Event(t, workspace, "agents.main", composer, "config", map[string]string{"id": "afk", "value": "true"})
	pickers(first, map[string]string{"model": "opus", "advisor": "opus", "finish": "ask", "afk": "true"})
	assert.Equal(t, "opus", f.transcript(t, workspace, first).Advisor)

	second := f.newSession(t, workspace)
	pickers(second, map[string]string{"model": "opus", "advisor": "opus", "finish": "ask", "afk": "false"})
	sets := f.calls(t, "session/set_config_option")
	require.Len(t, sets, 2)
	assert.JSONEq(t, `{"sessionId":"`+f.session(t, workspace, second).AgentSessionID+`","configId":"model","value":"opus"}`, string(sets[1].Params))
	news := f.calls(t, "session/new")
	require.Len(t, news, 2)
	assert.NotContains(t, string(news[0].Params), "advisorModel")
	assert.Contains(t, string(news[1].Params), `"settings":{"advisorModel":"opus"}`)
}

func TestFinishMode(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.finish-default", map[string]any{"value": "merge"}, nil))
	second := f.newSession(t, workspace)
	assert.Equal(t, []string{"ask", "merge"}, []string{f.session(t, workspace, first).Finish, f.session(t, workspace, second).Finish})

	f.prompt(t, workspace, first, "one")
	f.waitStatus(t, workspace, first, "done")
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": first, "id": "finish", "value": "merge"}, nil))
	f.prompt(t, workspace, first, "two")
	f.waitStatus(t, workspace, first, "done")

	prompts := f.calls(t, "session/prompt")
	require.Len(t, prompts, 2)
	assert.Contains(t, string(prompts[0].Params), "finish mode: ask")
	assert.Contains(t, string(prompts[1].Params), "finish mode: merge")
	for _, e := range ofKind(f.transcript(t, workspace, first).Entries, "user") {
		assert.NotContains(t, e.Text, "Clubmate finish mode")
	}
}

func TestSettingsViewFinishDefault(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	view := func() string {
		var views []internalsdui.SlotView
		require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"workspaceId": ""}, &views))
		i := slices.IndexFunc(views, func(v internalsdui.SlotView) bool { return v.Slot == "agents.settings" })
		if i < 0 {
			return ""
		}
		return string(views[i].Tree)
	}
	testutil.Eventually(t, func() bool { return strings.Contains(view(), `"value":"ask"`) }, 5*time.Second)
	require.NoError(t, f.run(t, workspace, "agents.finish-default", map[string]any{"value": "merge"}, nil))
	testutil.Eventually(t, func() bool { return strings.Contains(view(), `"value":"merge"`) }, 5*time.Second)
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": "", "slot": "agents.settings", "nodeId": "finish", "action": "finish", "value": "ask"}, nil))
	var got struct {
		Value string `json:"value"`
	}
	require.NoError(t, f.run(t, workspace, "agents.finish-default", map[string]any{}, &got))
	assert.Equal(t, "ask", got.Value)
	assert.Contains(t, view(), `"value":"ask"`)
}

func TestAFKMode(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	require.NoError(t, f.run(t, workspace, "agents.afk-default", map[string]any{"on": true}, nil))
	parent, other := f.newSession(t, workspace), f.newSession(t, workspace)
	assert.True(t, f.session(t, workspace, parent).AFK)
	assert.ErrorContains(t, f.runAs(t, workspace, other, "agents.afk", map[string]any{"session": parent, "on": false}, nil), "the session was not started by this agent")

	away := f.newChild(t, workspace, parent, map[string]any{"prompt": "while away"}).Session
	f.waitStatus(t, workspace, away, "done")
	require.NoError(t, f.runAs(t, workspace, parent, "agents.afk", map[string]any{"on": false}, nil))
	back := f.newChild(t, workspace, parent, map[string]any{"prompt": "after return"}).Session
	f.waitStatus(t, workspace, back, "done")

	afk := map[string]bool{}
	for _, c := range f.calls(t, "session/prompt") {
		for _, text := range []string{"while away", "after return"} {
			if strings.Contains(string(c.Params), text) {
				afk[text] = strings.Contains(string(c.Params), "[Clubmate AFK mode]")
			}
		}
	}
	assert.Equal(t, map[string]bool{"while away": true, "after return": false}, afk)
	for _, e := range ofKind(f.transcript(t, workspace, away).Entries, "user") {
		assert.NotContains(t, e.Text, "Clubmate AFK mode")
	}
}

func TestAFKOnMidTurnSteersRunningSessions(t *testing.T) {
	f := start(t, map[string]any{"steering": true, "prompt": []map[string]any{{"sleep": 3000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "one")
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "two"}).Session
	testutil.Eventually(t, func() bool { return len(f.calls(t, "session/prompt")) == 2 }, wait)

	require.NoError(t, f.run(t, workspace, "agents.afk", map[string]any{"session": parent, "on": true}, nil))

	steered := func() []string {
		var out []string
		for _, c := range f.calls(t, "_session/steering") {
			if strings.HasPrefix(promptText(t, c), "[Clubmate AFK mode]") {
				var p struct {
					SessionID string `json:"sessionId"`
				}
				require.NoError(t, json.Unmarshal(c.Params, &p))
				out = append(out, p.SessionID)
			}
		}
		return out
	}
	want := []string{f.session(t, workspace, parent).AgentSessionID, f.session(t, workspace, kid).AgentSessionID}
	testutil.Eventually(t, func() bool { return len(steered()) == 2 }, wait)
	assert.ElementsMatch(t, want, steered())
	assert.Equal(t, []string{"one"}, userTexts(t, f, workspace, parent))
}

func TestDeleteDuringTurn(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 1500}}})
	workspace := f.workspace(t, repo)
	session, keeper := f.newSession(t, workspace), f.newSession(t, workspace)
	testutil.Eventually(t, func() bool { return f.session(t, workspace, keeper).AgentSessionID != "" }, wait)

	f.prompt(t, workspace, session, "slow")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": session}, nil))
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
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitStatus(t, workspace, session, "waiting")
	first := ofKind(f.transcript(t, workspace, session).Entries, "permission")
	require.Len(t, first, 1)
	f.answerPermission(t, workspace, session, first[0].ID)
	f.waitStatus(t, workspace, session, "done")
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	require.NoError(t, syscall.Kill(starts[0].PID, syscall.SIGKILL))
	f.waitStatus(t, workspace, session, "error")

	f.prompt(t, workspace, session, "two")
	f.waitStatus(t, workspace, session, "waiting")

	perms := ofKind(f.transcript(t, workspace, session).Entries, "permission")
	require.Len(t, perms, 2)
	assert.NotEqual(t, perms[0].ID, perms[1].ID)
	assert.Equal(t, 2, perms[1].Turn)
	assert.Empty(t, perms[1].Outcome)
	assert.Len(t, f.calls(t, "session/resume"), 1)
}

func TestDeleteWorkspaceDeletesSessions(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")
	path := filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", session+".jsonl")
	require.FileExists(t, path)

	require.NoError(t, f.h.Svc.DeleteWorkspace(t.Context(), workspace))

	testutil.Eventually(t, func() bool {
		_, found, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		_, statErr := os.Stat(path)
		return !found && errors.Is(statErr, os.ErrNotExist) && git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/") == ""
	}, wait)
	assert.NotEmpty(t, f.calls(t, "session/close"))
	assert.Empty(t, f.calls(t, "session/delete"))
}

func TestOrphanAgentReapedAfterPluginCrash(t *testing.T) {
	f := start(t, map[string]any{"linger": true})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).AgentSessionID != "" }, wait)
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)

	pids := f.h.Sup.PIDs()
	require.Len(t, pids, 1)
	require.NoError(t, syscall.Kill(pids[0], syscall.SIGKILL))

	testutil.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(-starts[0].PID, 0), syscall.ESRCH)
	}, restartWait)
}

func TestStopRoot(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "done")
	require.NotEmpty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))

	require.NoError(t, f.run(t, workspace, "agents.stop-root", map[string]any{"path": repo}, nil))

	assert.Equal(t, "closed", f.session(t, workspace, session).Status)
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	assert.ErrorIs(t, syscall.Kill(starts[0].PID, 0), syscall.ESRCH)
	assert.Empty(t, git(t, repo, "for-each-ref", "refs/clubmate/cp/"+session+"/"))

	require.NoError(t, f.h.Svc.DeleteWorkspace(t.Context(), workspace))

	path := filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", session+".jsonl")
	testutil.Eventually(t, func() bool {
		_, found, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		_, statErr := os.Stat(path)
		return !found && errors.Is(statErr, os.ErrNotExist)
	}, wait)
}

func TestResumedSessionClosedWhenWorktreeVanishes(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.stop-root", map[string]any{"path": repo}, nil))
	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": session}, nil))
	require.NotEqual(t, "closed", f.session(t, workspace, session).Status)

	require.NoError(t, os.RemoveAll(repo))
	_, err := f.h.Svc.RenameWorkspace(t.Context(), workspace, "renamed")
	require.NoError(t, err)

	f.waitStatus(t, workspace, session, "closed")
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
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")

	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		cards := cardsOf(n, "permissionCard")
		return len(cards) == 1 && cards[0].Props["outcome"] == nil
	})
	card := cardsOf(main, "permissionCard")[0]
	assert.Equal(t, "Claude wants to run tests", card.Props["text"])
	f.h.Event(t, workspace, "agents.main", card.ID, "permission", map[string]any{"request": card.Props["request"], "option": "allow"})

	f.waitCall(t, "permission_response", func(c fakeCall) bool { return strings.Contains(string(c.Params), `"optionId":"allow"`) })
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		cards := cardsOf(n, "permissionCard")
		return len(cards) == 1 && cards[0].Props["outcome"] == "allow"
	})
}

func TestReviewActionUsesCheckpoint(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}}, testutil.EchoPluginDirAs(t, diffviewManifest))
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "change it")
	f.waitStatus(t, workspace, session, "done")
	sha := f.transcript(t, workspace, session).Turns[0].CheckpointSha
	require.NotEmpty(t, sha)

	f.h.Event(t, workspace, "agents.main", session+"/review", "review", nil)

	assert.JSONEq(t, `{"cwd":"`+repo+`","rev":"`+sha+`"}`, string(echoed(t, f, "editor-nvim.diffview")))
}

func TestReviewButtonFollowsWorktreeChanges(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	button := func(n sdui.Node, id string) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.ID == session+"/"+id })
	}

	f.prompt(t, workspace, session, "just answer")
	f.waitStatus(t, workspace, session, "done")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "message")) == 2 && !button(n, "stop") })
	assert.False(t, button(main, "review"))

	write(t, filepath.Join(repo, "new.txt"), "x\n")
	f.prompt(t, workspace, session, "now change it")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return button(n, "review") })

	require.NoError(t, os.Remove(filepath.Join(repo, "new.txt")))
	f.h.Event(t, workspace, "agents.sidebar", session, "select", session)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return !button(n, "review") })
}

func TestOpenLocationShowsEditor(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, "a.go")
	read := map[string]any{"sessionUpdate": "tool_call", "toolCallId": "r1", "title": "Read a.go", "kind": "read", "status": "completed", "locations": []map[string]any{{"path": path, "line": 1}}}
	f := start(t, map[string]any{"prompt": []map[string]any{{"update": read}}}, testutil.EchoPluginDirAs(t, codeManifest))
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "read it")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "toolCard")) == 1 })
	card := cardsOf(main, "toolCard")[0]

	f.h.Event(t, workspace, "agents.main", card.ID, "openLocation", map[string]any{"path": path, "line": 1})

	assert.JSONEq(t, `{"path":"`+path+`","line":1,"focus":true}`, string(echoed(t, f, "editor-nvim.open")))
}

func TestOpenDocRunsDocsOpenForTheSession(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, "a.md")
	edit := map[string]any{"sessionUpdate": "tool_call", "toolCallId": "w1", "title": "Write a.md", "kind": "edit", "status": "completed", "locations": []map[string]any{{"path": path}}}
	f := start(t, map[string]any{"prompt": []map[string]any{{"update": edit}}}, testutil.EchoPluginDirAs(t, docsManifest))
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write it")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "toolCard")) == 1 })

	f.h.Event(t, workspace, "agents.main", cardsOf(main, "toolCard")[0].ID, "openDoc", path)

	assert.JSONEq(t, `{"path":"`+path+`","session":"`+session+`"}`, string(echoed(t, f, "docs.open")))
}

func TestMessageDocLinkOpensTheDoc(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, "a.md")
	require.NoError(t, os.WriteFile(path, []byte("# A"), 0o600))
	reply := map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Wrote `a.md`."}}
	f := start(t, map[string]any{"prompt": []map[string]any{{"update": reply}}}, testutil.EchoPluginDirAs(t, docsManifest))
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "write it")
	linked := func(n sdui.Node) []sdui.Node {
		return slices.DeleteFunc(cardsOf(n, "message"), func(m sdui.Node) bool { return m.Props["docLinks"] == nil })
	}
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(linked(n)) == 1 })
	msg := linked(main)[0]
	assert.Equal(t, map[string]any{"a.md": path}, msg.Props["docLinks"])

	f.h.Event(t, workspace, "agents.main", msg.ID, "openDoc", path)

	assert.JSONEq(t, `{"path":"`+path+`","session":"`+session+`"}`, string(echoed(t, f, "docs.open")))
}

func TestOpenOutputWritesFullOutputAndOpensIt(t *testing.T) {
	output := strings.Repeat("line\n", 300)
	run := map[string]any{"sessionUpdate": "tool_call", "toolCallId": "x1", "title": "make", "kind": "execute", "status": "completed", "content": []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": output}}}}
	f := start(t, map[string]any{"prompt": []map[string]any{{"update": run}}}, testutil.EchoPluginDirAs(t, codeManifest))
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "run it")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "toolCard")) == 1 })
	card := cardsOf(main, "toolCard")[0]

	f.h.Event(t, workspace, "agents.main", card.ID, "openOutput", nil)

	var args struct {
		Path  string
		Focus bool
	}
	require.NoError(t, json.Unmarshal(echoed(t, f, "editor-nvim.open"), &args))
	assert.Equal(t, session+"-x1.log", filepath.Base(args.Path))
	assert.True(t, args.Focus)
	got, err := os.ReadFile(args.Path)
	require.NoError(t, err)
	assert.Equal(t, output, string(got))
}

func TestRevealSelectsTheSessionAndFocusesAgents(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)
	f.newSession(t, workspace)
	c := f.h.Client(t)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": workspace}, nil))

	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": first}, nil))

	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool { return cardsOf(n, "tree")[0].Props["selected"] == first })
	waitFocus(t, c, "agents", "agents.main")
}

func TestRevealOpensTheGraphRunRow(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	var out struct{ Session, Worktree string }
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"title": "Loop · Coder", "run": "gr_1", "runTitle": "Loop"}, &out))
	row := "wt:run:" + out.Worktree + "/gr_1"
	runRow := func(n sdui.Node) (sdui.TreeItem, bool) {
		trees := cardsOf(n, "tree")
		if len(trees) == 0 {
			return sdui.TreeItem{}, false
		}
		raw, err := json.Marshal(trees[0].Props["items"])
		require.NoError(t, err)
		var groups []sdui.TreeItem
		require.NoError(t, json.Unmarshal(raw, &groups))
		for _, g := range groups {
			if i := slices.IndexFunc(g.Children, func(item sdui.TreeItem) bool { return item.ID == row }); i >= 0 {
				return g.Children[i], true
			}
		}
		return sdui.TreeItem{}, false
	}

	f.h.Event(t, workspace, "agents.sidebar", "sessions", "collapse", row)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool { item, ok := runRow(n); return ok && !item.Expanded })
	require.NoError(t, f.run(t, workspace, "agents.reveal", map[string]any{"session": out.Session}, nil))

	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		item, ok := runRow(n)
		return ok && item.Expanded && len(item.Children) == 1 && item.Children[0].Label == "Coder"
	})
}

func waitFocus(t *testing.T, c *rpc.Client, section, slot string) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method != "ui.focus" {
				continue
			}
			var got struct{ Section, Slot string }
			require.NoError(t, json.Unmarshal(n.Params, &got))
			assert.Equal(t, section, got.Section)
			assert.Equal(t, slot, got.Slot)
			return
		case <-deadline:
			t.Fatal("no ui.focus notification")
		}
	}
}

func TestCodePanelFollowsWorktree(t *testing.T) {
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	first := f.newSession(t, workspace)
	shows := func(sid string) func(sdui.Node) bool {
		return func(n sdui.Node) bool {
			composers := cardsOf(n, "composer")
			return len(composers) == 1 && strings.HasPrefix(composers[0].ID, sid+"/")
		}
	}
	f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", shows(first))

	var other struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"branch": "side"}, &other))
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool { return strings.Contains(string(rawTree(t, n)), other.Session) })
	panel := f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", shows(first))
	assert.NotContains(t, string(rawTree(t, panel)), other.Session)

	c := f.h.Client(t)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": workspace}, nil))
	f.h.Event(t, workspace, "editor-nvim.sidebar", "code/new", "codeNew", nil)
	waitFocus(t, c, "editor-nvim", "editor-nvim.sidebar")
	panel = f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", func(n sdui.Node) bool { return !shows(first)(n) && len(cardsOf(n, "composer")) == 1 })
	second := strings.SplitN(cardsOf(panel, "composer")[0].ID, "/", 2)[0]
	assert.NotEqual(t, other.Session, second)

	f.h.Event(t, workspace, "editor-nvim.sidebar", "code/session", "codeSelect", first)
	f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", shows(first))
}

func TestCodePanelStarterAttachesEditorFile(t *testing.T) {
	f := start(t, map[string]any{}, testutil.EchoPluginDirAs(t, codeManifest))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	file := filepath.Join(repo, "notify.go")
	c := f.h.Client(t)
	require.NoError(t, c.Call(t.Context(), "bus.publish", map[string]any{"topic": "editor-nvim.file", "payload": map[string]string{"workspace": workspace, "root": repo, "path": file}}, nil))
	f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", func(n sdui.Node) bool {
		badges := cardsOf(n, "badge")
		return len(badges) == 1 && badges[0].Props["text"] == "notify.go"
	})

	f.h.Event(t, workspace, "editor-nvim.sidebar", session+"/starter/1", "starter", nil)
	call := f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	assert.Contains(t, string(call.Params), "Explain this file.")
	assert.Contains(t, string(call.Params), "notify.go")
	f.h.WaitSlot(t, workspace, "editor-nvim.sidebar", func(n sdui.Node) bool { return len(buttonsWith(n, "starter")) == 0 })
}

func rawTree(t *testing.T, n sdui.Node) []byte {
	t.Helper()
	raw, err := json.Marshal(n)
	require.NoError(t, err)
	return raw
}

func TestSignInReattaches(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "authed")
	f := start(t, map[string]any{"authFile": authFile})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.waitStatus(t, workspace, session, "error")
	require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.Props["action"] == "signIn" })
	})
	f.h.Event(t, workspace, "agents.main", session+"/signin", "signIn", nil)

	testutil.Eventually(t, func() bool {
		require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))
		return f.session(t, workspace, session).Status == "idle"
	}, wait)
	assert.NotEmpty(t, f.session(t, workspace, session).AgentSessionID)
	assert.FileExists(t, authFile)
}

func TestCloseCancelsSignIn(t *testing.T) {
	f := start(t, map[string]any{"authFile": filepath.Join(t.TempDir(), "authed")})
	workspace := f.workspace(t, newRepo(t))
	id := f.newSession(t, workspace)
	f.waitStatus(t, workspace, id, "error")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.Props["action"] == "signIn" })
	})
	f.h.Event(t, workspace, "agents.main", id+"/signin", "signIn", nil)

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": id}, nil))
	require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))

	terminals, err := f.h.Svc.ListSessions(t.Context(), session.ListFilter{WorkspaceID: workspace, Owner: "agents"})
	require.NoError(t, err)
	assert.Empty(t, terminals)
	for range 10 {
		time.Sleep(50 * time.Millisecond)
		require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))
		require.Equal(t, "closed", f.session(t, workspace, id).Status)
	}
	changed, err := f.h.Bus.Read(t.Context(), "agents.changed", 0, 1000)
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(changed, func(e bus.Event) bool {
		return strings.Contains(string(e.Payload), `"kind":"status"`) && strings.Contains(string(e.Payload), `"status":"closed"`)
	}))
}

func (f *fixture) rev(t *testing.T, workspace, slot string) int64 {
	t.Helper()
	var views []struct {
		Slot string `json:"slot"`
		Rev  int64  `json:"rev"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"workspaceId": workspace}, &views))
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
	workspace := f.workspace(t, newRepo(t))
	background := f.newSession(t, workspace)
	open := f.newSession(t, workspace)
	f.waitStatus(t, workspace, background, "idle")
	f.waitStatus(t, workspace, open, "idle")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return len(cardsOf(n, "composer")) == 1 && strings.HasPrefix(cardsOf(n, "composer")[0].ID, open+"/")
	})
	time.Sleep(300 * time.Millisecond)
	main, sidebar := f.rev(t, workspace, "agents.main"), f.rev(t, workspace, "agents.sidebar")

	f.prompt(t, workspace, background, "go")
	f.waitStatus(t, workspace, background, "done")
	time.Sleep(300 * time.Millisecond)

	assert.Equal(t, main, f.rev(t, workspace, "agents.main"))
	assert.Greater(t, f.rev(t, workspace, "agents.sidebar"), sidebar)
}

func TestHiddenSlotsStayUnrendered(t *testing.T) {
	steps := []map[string]any{}
	for i := range 100 {
		steps = append(steps, map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": strconv.Itoa(i) + " "}}}, map[string]any{"sleep": 20})
	}
	commands := make([]map[string]string, 0, 100)
	for i := range 100 {
		commands = append(commands, map[string]string{"name": "skill-" + strconv.Itoa(i), "description": strings.Repeat("Use when the task needs skill "+strconv.Itoa(i)+". ", 25)})
	}
	f := start(t, map[string]any{"prompt": steps, "commands": commands}, testutil.EchoPluginDirAs(t, codeManifest))
	workspace := f.workspace(t, newRepo(t))
	c := f.h.Client(t)
	session := f.newSession(t, workspace)
	f.waitStatus(t, workspace, session, "idle")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		composers := cardsOf(n, "composer")
		return len(composers) == 1 && composers[0].Props["commands"] != nil
	})

	slots := []string{"agents.main", "agents.sidebar", "editor-nvim.sidebar"}
	renders := map[string]map[string]int64{}
	for _, section := range []string{"agents", "terminals", "closed"} {
		if section == "closed" {
			require.NoError(t, c.Call(t.Context(), "ui.state.patch", map[string]any{"workspaceId": workspace, "key": "section", "value": "agents"}, nil))
			require.NoError(t, c.Call(t.Context(), "ui.unsubscribe", map[string]string{"subscription": f.subs[workspace]}, nil))
		} else {
			require.NoError(t, c.Call(t.Context(), "ui.state.patch", map[string]any{"workspaceId": workspace, "key": "section", "value": section}, nil))
		}
		time.Sleep(300 * time.Millisecond)
		before := map[string]int64{}
		for _, slot := range slots {
			before[slot] = f.rev(t, workspace, slot)
		}
		began := time.Now()
		f.prompt(t, workspace, session, "go")
		f.waitStatus(t, workspace, session, "done")
		took := time.Since(began)
		time.Sleep(300 * time.Millisecond)
		renders[section] = map[string]int64{}
		for _, slot := range slots {
			renders[section][slot] = f.rev(t, workspace, slot) - before[slot]
		}
		t.Logf("section %s, turn %s: renders %v, agents.main %d bytes", section, took.Round(time.Millisecond), renders[section], f.treeBytes(t, workspace, "agents.main"))
	}

	assert.Positive(t, renders["agents"]["agents.main"])
	assert.Zero(t, renders["agents"]["editor-nvim.sidebar"])
	assert.Zero(t, renders["terminals"]["agents.main"])
	assert.Zero(t, renders["terminals"]["editor-nvim.sidebar"])
	assert.Equal(t, map[string]int64{"agents.main": 0, "agents.sidebar": 0, "editor-nvim.sidebar": 0}, renders["closed"])

	main := f.rev(t, workspace, "agents.main")
	f.subscribe(t, workspace)
	testutil.Eventually(t, func() bool { return f.rev(t, workspace, "agents.main") > main }, wait)
}

func (f *fixture) treeBytes(t *testing.T, workspace, slot string) int {
	t.Helper()
	var views []struct {
		Slot string          `json:"slot"`
		Tree json.RawMessage `json:"tree"`
	}
	require.NoError(t, f.h.Client(t).Call(t.Context(), "ui.get", map[string]string{"workspaceId": workspace}, &views))
	for _, v := range views {
		if v.Slot == slot {
			return len(v.Tree)
		}
	}
	return 0
}

func TestContinueHandsOff(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{})
	workspace := f.workspace(t, repo)
	old := f.newSession(t, workspace)
	f.waitStatus(t, workspace, old, "idle")
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": old}, nil))

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.ID == old+"/continue" })
	})
	f.h.Event(t, workspace, "agents.main", old+"/continue", "continue", nil)

	var list []record
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	require.Len(t, list, 2)
	fresh := list[1]
	assert.NotEqual(t, old, fresh.ID)
	assert.Equal(t, repo, fresh.Worktree)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return len(cardsOf(n, "transcript")) == 1 && cardsOf(n, "transcript")[0].ID == fresh.ID+"/transcript"
	})
	assert.Equal(t, old, fresh.HandoffFrom)
	assert.Equal(t, fresh.ID, f.session(t, workspace, old).HandoffTo)
}

func TestHandoff(t *testing.T) {
	reply := map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "half done"}}}
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 800}, reply}})
	workspace := f.workspace(t, newRepo(t))
	var started child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"afk": true}, &started))
	old := started.Session
	f.prompt(t, workspace, old, "fix refunds")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	args := map[string]any{"session": old, "note": "finish it"}
	require.ErrorContains(t, f.run(t, workspace, "agents.handoff", args, nil), "stop the session's running turn before handing it off")
	f.waitStatus(t, workspace, old, "done")
	require.NoError(t, f.run(t, workspace, "agents.notify", map[string]any{"session": old, "policy": "muted"}, nil))
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.handoff", args, &out))

	next := f.session(t, workspace, out.Session)
	assert.Equal(t, old, next.HandoffFrom)
	assert.Equal(t, "muted", next.Notify)
	assert.True(t, next.AFK)
	closed := f.waitStatus(t, workspace, old, "closed")
	assert.Equal(t, out.Session, closed.HandoffTo)
	sent := f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.HasSuffix(promptText(t, c), "finish it") })
	assert.Contains(t, promptText(t, sent), "Last reply (turn 1):\nhalf done")
}

func TestExportImportContinuesInAnotherClone(t *testing.T) {
	reply := map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "half done"}}}
	f := start(t, map[string]any{"prompt": []map[string]any{reply}})
	repo := newRepo(t)
	from := f.workspace(t, repo)
	old := f.newSession(t, from)
	f.prompt(t, from, old, "fix refunds")
	f.waitStatus(t, from, old, "done")
	dir := filepath.Join(t.TempDir(), "bundle")
	require.NoError(t, f.run(t, from, "agents.export", map[string]any{"session": old, "dir": dir}, nil))

	clone := filepath.Join(t.TempDir(), "clone")
	git(t, repo, "clone", "-q", repo, clone)
	clone, err := filepath.EvalSymlinks(clone)
	require.NoError(t, err)
	to := f.workspace(t, clone)
	var out struct {
		Session  string `json:"session"`
		Imported string `json:"imported"`
		Worktree string `json:"worktree"`
	}
	require.NoError(t, f.run(t, to, "agents.import", map[string]any{"dir": dir, "note": "finish it"}, &out))

	refs := "refs/clubmate/cp/" + old + "/"
	assert.Equal(t, git(t, repo, "for-each-ref", refs), git(t, clone, "for-each-ref", refs))
	assert.Equal(t, clone, out.Worktree)
	copied := f.session(t, to, out.Imported)
	assert.Equal(t, old, copied.ImportedFrom)
	assert.Equal(t, out.Session, copied.HandoffTo)
	assert.Equal(t, "closed", copied.Status)
	assert.Equal(t, out.Imported, f.session(t, to, out.Session).HandoffFrom)
	assert.Equal(t, "fix refunds", ofKind(f.transcript(t, to, out.Imported).Entries, "user")[0].Text)
	sent := f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.HasSuffix(promptText(t, c), "finish it") })
	assert.Contains(t, promptText(t, sent), "agents.transcript session="+out.Imported)
	assert.Contains(t, promptText(t, sent), "Last reply (turn 1):\nhalf done")
}

func promptText(t *testing.T, c fakeCall) string {
	t.Helper()
	var p struct {
		Prompt []struct {
			Text string `json:"text"`
		} `json:"prompt"`
	}
	require.NoError(t, json.Unmarshal(c.Params, &p))
	require.NotEmpty(t, p.Prompt)
	return p.Prompt[0].Text
}

func buttonsWith(n sdui.Node, action string) []sdui.Node {
	return slices.DeleteFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.Props["action"] != action })
}

func startQueued(t *testing.T, queued ...string) (*fixture, string, string) {
	t.Helper()
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 1500}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	for _, text := range queued {
		f.prompt(t, workspace, session, text)
	}
	return f, workspace, session
}

func TestQueuedPromptsSentAsNextTurn(t *testing.T) {
	f, workspace, session := startQueued(t, "two", "three")

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(buttonsWith(n, "unqueue")) == 2 })
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "two\n\nthree" })
	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.calls(t, "session/prompt"), 2)
	assert.Len(t, f.transcript(t, workspace, session).Turns, 2)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(buttonsWith(n, "unqueue")) == 0 })
}

func TestInternalErrorRetriesTurnOnce(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "boom", "fail": "no results for tool calls"}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "boom")

	f.waitStatus(t, workspace, session, "done")
	prompts := f.calls(t, "session/prompt")
	require.Len(t, prompts, 2)
	assert.Contains(t, promptText(t, prompts[1]), "no results for tool calls")
	assert.Len(t, f.transcript(t, workspace, session).Turns, 1)
}

func TestInternalErrorAfterRetryFailsTurn(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"fail": "no results for tool calls"}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "boom")

	got := f.waitStatus(t, workspace, session, "error")
	assert.Len(t, f.calls(t, "session/prompt"), 2)
	assert.Contains(t, got.Error, "no results for tool calls")
}

func TestToolCallsOpenAtSteerDoNotFailTurn(t *testing.T) {
	f := start(t, map[string]any{"steering": true, "prompt": []map[string]any{
		{"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "srvtoolu_a", "title": "advisor", "kind": "other", "status": "pending"}},
		{"sleep": 1500},
		{"fail": "Internal error: Claude ended the turn without returning results for tool calls: srvtoolu_a", "errorKind": "incomplete_tool_call"},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c := cardsOf(n, "composer")
		return len(c) == 1 && c[0].Props["steers"] == true
	})

	f.prompt(t, workspace, session, "two")

	f.waitCall(t, "_session/steering", func(c fakeCall) bool { return promptText(t, c) == "two" })
	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.calls(t, "session/prompt"), 1)
}

func TestCancelKeepsQueue(t *testing.T) {
	f, workspace, session := startQueued(t, "two")

	require.NoError(t, f.run(t, workspace, "agents.cancel", map[string]any{"session": session}, nil))

	f.waitStatus(t, workspace, session, "done")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(buttonsWith(n, "sendQueued")) == 1 })
	assert.Len(t, buttonsWith(main, "unqueue"), 1)
	assert.Len(t, f.calls(t, "session/prompt"), 1)
	f.h.Event(t, workspace, "agents.main", buttonsWith(main, "sendQueued")[0].ID, "sendQueued", nil)
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "two" })
}

func TestUnqueueDropsPrompt(t *testing.T) {
	f, workspace, _ := startQueued(t, "two", "three")
	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(buttonsWith(n, "unqueue")) == 2 })

	f.h.Event(t, workspace, "agents.main", buttonsWith(main, "unqueue")[0].ID, "unqueue", nil)

	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "three" })
}

func userTexts(t *testing.T, f *fixture, workspace, session string) []string {
	t.Helper()
	var out []string
	for _, en := range ofKind(f.transcript(t, workspace, session).Entries, "user") {
		out = append(out, en.Text)
	}
	return out
}

func TestPromptDuringTurnSteers(t *testing.T) {
	f := start(t, map[string]any{"steering": true, "prompt": []map[string]any{{"sleep": 1500}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		c := cardsOf(n, "composer")
		return len(c) == 1 && c[0].Props["steers"] == true
	})
	f.prompt(t, workspace, session, "two")

	f.waitCall(t, "_session/steering", func(c fakeCall) bool { return promptText(t, c) == "two" })
	assert.Equal(t, []string{"one", "two"}, userTexts(t, f, workspace, session))
	f.waitStatus(t, workspace, session, "done")
	assert.Len(t, f.calls(t, "session/prompt"), 1)
	assert.Len(t, f.transcript(t, workspace, session).Turns, 1)
}

func TestSteerPromptRequiredQueues(t *testing.T) {
	f := start(t, map[string]any{"steering": true, "idle": true, "prompt": []map[string]any{{"sleep": 1500}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	f.prompt(t, workspace, session, "two")

	f.waitCall(t, "_session/steering", func(c fakeCall) bool { return promptText(t, c) == "two" })
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "two" })
	f.waitStatus(t, workspace, session, "done")
	assert.Equal(t, []string{"one", "two"}, userTexts(t, f, workspace, session))
}

func TestAgentProcessRegistered(t *testing.T) {
	repo := newRepo(t)
	f := start(t, map[string]any{"linger": true})
	workspace := f.workspace(t, repo)
	session := f.newSession(t, workspace)
	type root struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Owner string `json:"owner"`
		PID   int    `json:"pid"`
		Ended bool   `json:"ended"`
	}
	roots := func() []root {
		var out []root
		require.NoError(t, f.h.Client(t).Call(t.Context(), "process.roots", map[string]string{"workspaceId": workspace}, &out))
		return out
	}
	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).AgentSessionID != "" && len(roots()) == 1 }, wait)
	got := roots()[0]
	starts := f.calls(t, "start")
	require.Len(t, starts, 1)
	assert.Equal(t, root{ID: got.ID, Label: "Claude · " + filepath.Base(repo), Owner: "agents", PID: starts[0].PID}, got)

	require.NoError(t, f.h.Client(t).Call(t.Context(), "process.kill", map[string]any{"sessionId": got.ID, "pids": []int{got.PID}}, nil))
	f.waitStatus(t, workspace, session, "error")
	left := roots()
	require.Len(t, left, 1)
	assert.True(t, left[0].Ended)
}

type child struct {
	Session  string `json:"session"`
	Worktree string `json:"worktree"`
}

func (f *fixture) newChild(t *testing.T, workspace, caller string, args map[string]any) child {
	t.Helper()
	var out child
	require.NoError(t, f.runAs(t, workspace, caller, "agents.new", args, &out))
	require.NotEmpty(t, out.Session)
	return out
}

func promptEvent(t *testing.T, lines []map[string]json.RawMessage) (string, string) {
	t.Helper()
	for _, l := range lines {
		if string(l["kind"]) != `"session/prompt"` {
			continue
		}
		var actor string
		var payload struct {
			From string `json:"from"`
		}
		require.NoError(t, json.Unmarshal(l["actor"], &actor))
		require.NoError(t, json.Unmarshal(l["payload"], &payload))
		return actor, payload.From
	}
	return "", ""
}

func TestAgentNewCreatesChildInOwnWorktree(t *testing.T) {
	f := start(t, map[string]any{
		"modes": map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}, {"id": "plan", "name": "Plan"}}},
		"configs": []map[string]any{
			{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "default", "options": []map[string]string{{"value": "default", "name": "Default"}, {"value": "opus", "name": "Opus"}}},
		},
	}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "orchestrate")
	f.waitStatus(t, workspace, parent, "done")
	require.NoError(t, f.run(t, workspace, "agents.mode", map[string]any{"session": parent, "mode": "plan"}, nil))
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": parent, "id": "model", "value": "opus"}, nil))
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": parent, "id": "advisor", "value": "opus"}, nil))

	out := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "hi"})

	assert.Equal(t, filepath.Join(repo, ".worktrees", "c1"), out.Worktree)
	got := f.waitStatus(t, workspace, out.Session, "done")
	assert.Equal(t, parent, got.ParentSession)
	assert.Equal(t, out.Worktree, got.Worktree)
	assert.Equal(t, "plan", got.Mode)
	p, err := f.h.Svc.GetWorkspace(t.Context(), workspace)
	require.NoError(t, err)
	assert.Equal(t, []string{repo}, p.Roots())
	sent := f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), got.AgentSessionID) })
	assert.Equal(t, `[From Clubmate agent session "orchestrate" (`+parent+`), not from the user]`+"\n\nhi", promptText(t, sent))
	f.waitCall(t, "session/set_config_option", func(c fakeCall) bool {
		return string(c.Params) == `{"sessionId":"`+got.AgentSessionID+`","configId":"model","value":"opus"}`
	})
	news := f.calls(t, "session/new")
	require.Len(t, news, 2)
	assert.Contains(t, string(news[0].Params), `"systemPrompt":{"append":"When you need the user's answer`)
	assert.NotContains(t, string(news[1].Params), "systemPrompt")
	assert.Contains(t, string(news[1].Params), `"settings":{"advisorModel":"opus"}`)
	actor, from := promptEvent(t, f.logLines(t, out.Session))
	assert.Equal(t, "agent:"+parent, actor)
	assert.Equal(t, parent, from)
}

func TestChildLimit(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	idle := f.newChild(t, workspace, parent, map[string]any{}).Session
	for range 8 {
		f.newChild(t, workspace, parent, map[string]any{"prompt": "work"})
	}

	started := f.runAs(t, workspace, parent, "agents.new", map[string]any{"prompt": "work"}, nil)
	prompted := f.runAs(t, workspace, parent, "agents.prompt", map[string]any{"session": idle, "text": "work"}, nil)

	require.Error(t, started)
	require.Error(t, prompted)
	assert.Equal(t, "plugin agents command.run: a session may run at most 8 child sessions at once", started.Error())
	assert.Equal(t, started.Error(), prompted.Error())
}

func TestChildrenFoldedUntilOpenedAndRemembered(t *testing.T) {
	f := start(t, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{}).Session
	expanded := func(n sdui.Node) (bool, bool) {
		trees := cardsOf(n, "tree")
		if len(trees) == 0 {
			return false, false
		}
		raw, err := json.Marshal(trees[0].Props["items"])
		require.NoError(t, err)
		var groups []sdui.TreeItem
		require.NoError(t, json.Unmarshal(raw, &groups))
		for _, g := range groups {
			for _, item := range g.Children {
				if item.ID == parent && item.HasChildren {
					return item.Expanded, true
				}
			}
		}
		return false, false
	}

	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool { open, ok := expanded(n); return ok && !open })
	f.h.Event(t, workspace, "agents.sidebar", kid, "select", kid)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool { open, _ := expanded(n); return open })

	before := f.rev(t, workspace, "agents.sidebar")
	f.restartPlugin(t, workspace)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		open, _ := expanded(n)
		return open && f.rev(t, workspace, "agents.sidebar") > before
	})
}

func TestDeleteParentDeletesChildren(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 5000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "work"}).Session
	grandkid := f.newChild(t, workspace, kid, map[string]any{}).Session
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": parent}, nil))

	f.waitCall(t, "session/cancel", func(fakeCall) bool { return true })
	var list []record
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	for _, r := range list {
		assert.NotContains(t, []string{parent, kid, grandkid}, r.ID)
	}
	for _, id := range []string{parent, kid, grandkid} {
		assert.NoFileExists(t, filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", id+".jsonl"))
	}
}

func TestDoneParentShowsRunningChildren(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "slow", "sleep": 1500}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "orchestrate")
	f.waitStatus(t, workspace, parent, "done")
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "slow"}).Session
	f.waitStatus(t, workspace, kid, "running")
	childStatus := func() map[string]string {
		var list []struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			ChildStatus string `json:"childStatus"`
		}
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
		out := map[string]string{}
		for _, s := range list {
			out[s.ID] = s.Status + "/" + s.ChildStatus
		}
		return out
	}

	assert.Equal(t, map[string]string{parent: "done/running", kid: "running/"}, childStatus())
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		require.NoError(t, err)
		return strings.Contains(string(raw), "Waiting for child sessions")
	})

	f.waitStatus(t, workspace, kid, "done")
	assert.Equal(t, map[string]string{parent: "done/", kid: "done/"}, childStatus())
}

func TestSessionsFilteredToOneSessionKeepItsChildStatus(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "slow", "sleep": 1500}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "orchestrate")
	f.waitStatus(t, workspace, parent, "done")
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "slow"}).Session
	f.waitStatus(t, workspace, kid, "running")
	filtered := func(session string) []string {
		var list []struct {
			ID          string `json:"id"`
			ChildStatus string `json:"childStatus"`
		}
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": session}, &list))
		out := []string{}
		for _, s := range list {
			out = append(out, s.ID+"/"+s.ChildStatus)
		}
		return out
	}

	assert.Equal(t, []string{parent + "/running"}, filtered(parent))
	assert.Empty(t, filtered("agt_missing"))
}

func TestCloseParentClosesChildren(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{}).Session
	grandkid := f.newChild(t, workspace, kid, map[string]any{}).Session

	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": parent}, nil))

	f.waitStatus(t, workspace, kid, "closed")
	f.waitStatus(t, workspace, grandkid, "closed")
}

func TestChildWorktreeFailure(t *testing.T) {
	f := start(t, nil, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".worktrees", "c1"), 0o700))
	workspace := f.workspace(t, repo)
	parent := f.newSession(t, workspace)

	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "hi"}).Session

	assert.Contains(t, f.waitStatus(t, workspace, kid, "error").Error, ".worktrees/c1 already exists")

	require.NoError(t, os.Remove(filepath.Join(repo, ".worktrees", "c1")))
	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": kid}, nil))

	f.waitStatus(t, workspace, kid, "idle")
	assert.Contains(t, git(t, repo, "worktree", "list"), filepath.Join(repo, ".worktrees", "c1"))
}

func TestChildAdvisorOnlyInRecord(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{}).Session

	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": kid, "id": "advisor", "value": "opus"}, nil))

	assert.Equal(t, "opus", f.session(t, workspace, kid).Advisor)
	keys, err := f.h.KV.List(t.Context(), "agents", "advisor/")
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestAgentPromptWithoutCaller(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	require.NoError(t, f.runAs(t, workspace, "", "agents.prompt", map[string]any{"session": session, "text": "hi"}, nil))

	sent := f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	assert.Equal(t, "[From another agent session, not from the user]\n\nhi", promptText(t, sent))
	f.waitStatus(t, workspace, session, "done")
	actor, _ := promptEvent(t, f.logLines(t, session))
	assert.Equal(t, "agent:unknown", actor)
}

func TestPermissionScope(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "go"}).Session
	other := f.newChild(t, workspace, "", map[string]any{"prompt": "go"}).Session
	assert.Empty(t, f.session(t, workspace, other).ParentSession)
	answer := func(caller, session string) error {
		f.waitStatus(t, workspace, session, "waiting")
		request := ofKind(f.transcript(t, workspace, session).Entries, "permission")[0].ID
		return f.runAs(t, workspace, caller, "agents.permission", map[string]any{"session": session, "request": request, "option": "allow"}, nil)
	}

	assert.ErrorContains(t, f.runAs(t, workspace, parent, "agents.permission", map[string]any{"session": parent, "request": "permission-1", "option": "allow"}, nil), "the session was not started by this agent")
	assert.ErrorContains(t, f.runAs(t, workspace, parent, "agents.cancel", map[string]any{"session": other}, nil), "the session was not started by this agent")
	assert.ErrorContains(t, f.runAs(t, workspace, "", "agents.cancel", map[string]any{"session": kid}, nil), "the session was not started by this agent")
	assert.ErrorContains(t, answer(parent, other), "the session was not started by this agent")
	assert.ErrorContains(t, answer("", kid), "the session was not started by this agent")
	require.NoError(t, answer(parent, kid))
	f.waitStatus(t, workspace, kid, "done")
	request := ofKind(f.transcript(t, workspace, other).Entries, "permission")[0].ID
	params := map[string]any{"id": "agents.permission", "workspaceId": workspace, "args": map[string]any{"session": other, "request": request, "option": "allow"}}
	assert.ErrorContains(t, f.h.Client(t).Call(t.Context(), "command.run", params, nil), "the session was not started by this agent")
	params["via"] = "remote"
	assert.ErrorContains(t, f.h.Client(t).Call(t.Context(), "command.run", params, nil), "the session was not started by this agent")
	delete(params, "via")
	require.NoError(t, f.h.PluginCall(t, "remote", "command.run", params))
	f.waitStatus(t, workspace, other, "done")
}

func TestPermissionFromVoicePluginIsUser(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")
	request := ofKind(f.transcript(t, workspace, session).Entries, "permission")[0].ID

	params := map[string]any{"id": "agents.permission", "workspaceId": workspace, "args": map[string]any{"session": session, "request": request, "option": "allow"}}
	require.NoError(t, f.h.PluginCall(t, "voice", "command.run", params))

	f.waitStatus(t, workspace, session, "done")
}

func TestNewSessionOptionsOnlyFromPlugin(t *testing.T) {
	f := start(t, map[string]any{
		"modes": map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "default", "name": "Manual"}, {"id": "bypassPermissions", "name": "Bypass permissions"}}},
		"configs": []map[string]any{
			{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "default", "options": []map[string]string{{"value": "default", "name": "Default"}, {"value": "sonnet", "name": "Sonnet"}}},
		},
	})
	workspace := f.workspace(t, newRepo(t))
	args := map[string]any{"disallowedTools": []string{"Bash"}, "mode": "bypassPermissions", "model": "sonnet"}
	params := map[string]any{"id": "agents.new", "workspaceId": workspace, "args": args}

	assert.ErrorContains(t, f.runAs(t, workspace, "", "agents.new", args, nil), "only the voice plugin may set disallowedTools, systemPrompt, mode or model")
	assert.ErrorContains(t, f.h.PluginCall(t, "remote", "command.run", params), "only the voice plugin may set disallowedTools, systemPrompt, mode or model")
	require.NoError(t, f.h.PluginCall(t, "voice", "command.run", params))

	voice := listedIDs(t, f, workspace)[0]
	assert.Equal(t, []string{"Bash"}, f.session(t, workspace, voice).DisallowedTools)
	got := f.waitCall(t, "session/set_mode", func(fakeCall) bool { return true })
	assert.Contains(t, string(got.Params), `"modeId":"bypassPermissions"`)
	model := f.waitCall(t, "session/set_config_option", func(fakeCall) bool { return true })
	assert.Contains(t, string(model.Params), `"configId":"model","value":"sonnet"`)
	sent := string(f.calls(t, "session/new")[0].Params)
	assert.Contains(t, sent, `"disallowedTools":["Bash"],"settingSources":[],"strictMcpConfig":true`)
	assert.Contains(t, sent, `"CLAUDE_CODE_DISABLE_AUTO_MEMORY":"1"`)
	kid := f.newChild(t, workspace, voice, map[string]any{"prompt": "go"}).Session
	f.waitStatus(t, workspace, kid, "done")
	assert.Len(t, f.calls(t, "session/set_mode"), 1)
	assert.NotContains(t, string(f.calls(t, "session/new")[1].Params), "settingSources")
}

func TestNewSessionFromVoiceNotSelected(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	first := f.newSession(t, workspace)

	params := map[string]any{"id": "agents.new", "workspaceId": workspace, "args": map[string]any{"systemPrompt": "Route the user's words."}}
	require.NoError(t, f.h.PluginCall(t, "voice", "command.run", params))

	ids := listedIDs(t, f, workspace)
	voice := ids[slices.IndexFunc(ids, func(id string) bool { return id != first })]
	testutil.Eventually(t, func() bool { return f.session(t, workspace, voice).AgentSessionID != "" }, wait)
	news := f.calls(t, "session/new")
	sent := string(news[slices.IndexFunc(news, func(c fakeCall) bool { return strings.Contains(string(c.Params), voice) })].Params)
	assert.Contains(t, sent, `"systemPrompt":{"append":"Route the user's words."}`)
	assert.NotContains(t, sent, "When you need the user's answer")
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		trees := cardsOf(n, "tree")
		if len(trees) == 0 {
			return false
		}
		raw, err := json.Marshal(trees[0].Props["items"])
		require.NoError(t, err)
		return strings.Contains(string(raw), "Service · 1") && trees[0].Props["selected"] == first
	})
	assert.True(t, f.session(t, workspace, voice).Service)

	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.runAs(t, workspace, voice, "agents.new", map[string]any{"prompt": "hi"}, &out))
	assert.Empty(t, f.session(t, workspace, out.Session).ParentSession)
	assert.False(t, f.session(t, workspace, out.Session).Service)
}

func TestVoiceSessionNeverSelectedByFallback(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	params := map[string]any{"id": "agents.new", "workspaceId": workspace, "args": map[string]any{"systemPrompt": "Route the user's words."}}

	require.NoError(t, f.h.PluginCall(t, "voice", "command.run", params))

	require.Len(t, listedIDs(t, f, workspace), 1)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		trees := cardsOf(n, "tree")
		if len(trees) == 0 {
			return false
		}
		raw, err := json.Marshal(trees[0].Props["items"])
		require.NoError(t, err)
		_, selected := trees[0].Props["selected"]
		return strings.Contains(string(raw), "Service · 1") && !selected
	})
}

func TestVoicePromptMarkedInTranscript(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	text := `{"utterance":"vu_1","device":"mac","heard":"открой бэклог","context":{}}`
	params := map[string]any{"id": "agents.prompt", "workspaceId": workspace, "args": map[string]any{"session": session, "text": text}}

	require.NoError(t, f.h.PluginCall(t, "voice", "command.run", params))
	f.waitStatus(t, workspace, session, "done")
	f.prompt(t, workspace, session, text)
	testutil.Eventually(t, func() bool { return len(ofKind(f.transcript(t, workspace, session).Entries, "user")) == 2 }, wait)
	f.restartPlugin(t, workspace)

	users := ofKind(f.transcript(t, workspace, session).Entries, "user")
	require.Len(t, users, 2)
	assert.JSONEq(t, `{"heard":"открой бэклог","utterance":"vu_1"}`, string(users[0].Voice))
	assert.Empty(t, users[1].Voice)
}

func TestVoiceSessionGetsNoPolicyTexts(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	params := map[string]any{"id": "agents.new", "workspaceId": workspace, "args": map[string]any{"systemPrompt": "Route the user's words.", "prompt": `{"text":"hi"}`}}

	require.NoError(t, f.h.PluginCall(t, "voice", "command.run", params))

	sent := f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	assert.NotContains(t, string(sent.Params), "[Clubmate")
	assert.Equal(t, `{"text":"hi"}`, promptText(t, sent))
}

func listedIDs(t *testing.T, f *fixture, workspace string) []string {
	t.Helper()
	var list []record
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	ids := make([]string, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestAnswerIsUserTurn(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{}).Session

	require.NoError(t, f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "agents.answer", "workspaceId": workspace, "args": map[string]any{"session": kid, "text": "yes"}, "caller": parent}, nil))

	f.waitStatus(t, workspace, kid, "done")
	sent := f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	assert.Equal(t, "yes", promptText(t, sent))
	actor, from := promptEvent(t, f.logLines(t, kid))
	assert.Equal(t, "user", actor)
	assert.Empty(t, from)
}

func TestNotifyPolicy(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"notify": "needs-input"}).Session
	policies := func() map[string][2]string {
		var list []struct {
			ID              string `json:"id"`
			Notify          string `json:"notify"`
			NotifyEffective string `json:"notifyEffective"`
		}
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
		out := map[string][2]string{}
		for _, s := range list {
			out[s.ID] = [2]string{s.Notify, s.NotifyEffective}
		}
		return out
	}

	assert.Equal(t, map[string][2]string{parent: {"", "all"}, kid: {"needs-input", "needs-input"}}, policies())
	assert.ErrorContains(t, f.runAs(t, workspace, kid, "agents.notify", map[string]any{"session": parent, "policy": "muted"}, nil), "the session was not started by this agent")
	require.NoError(t, f.runAs(t, workspace, parent, "agents.notify", map[string]any{"session": parent, "policy": "muted"}, nil))
	assert.Equal(t, map[string][2]string{parent: {"muted", "muted"}, kid: {"needs-input", "muted"}}, policies())

	f.h.Event(t, workspace, "agents.sidebar", "sessions", "notifyAll", parent)
	assert.Equal(t, map[string][2]string{parent: {"", "all"}, kid: {"needs-input", "needs-input"}}, policies())
}

func startWithGit(t *testing.T, dirs ...string) (*fixture, string, string, string) {
	t.Helper()
	f := start(t, nil, append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json")}, dirs...)...)
	repo := newRepo(t)
	git(t, repo, "checkout", "-qb", "feat")
	workspace := f.workspace(t, repo)
	return f, repo, workspace, f.newSession(t, workspace)
}

func TestChildPolicyNamesParentBranch(t *testing.T) {
	f, repo, workspace, parent := startWithGit(t)

	own := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "own"}).Session
	shared := f.newChild(t, workspace, parent, map[string]any{"prompt": "shared"}).Session

	sent := func(session string) string {
		id := f.waitStatus(t, workspace, session, "done").AgentSessionID
		return string(f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), id) }).Params)
	}
	assert.Contains(t, sent(own), "merge your branch into feat inside "+repo+" (`git -C "+repo+" merge --no-edit c1`)")
	assert.NotContains(t, sent(shared), "merge your branch")
	assert.Contains(t, sent(shared), "[Clubmate finish mode: child]")
}

func TestChildOfDefaultBranchOnlyCommits(t *testing.T) {
	f := start(t, nil, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)

	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "own"}).Session

	id := f.waitStatus(t, workspace, kid, "done").AgentSessionID
	sent := string(f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), id) }).Params)
	assert.Contains(t, sent, "commit your work and stop")
	assert.NotContains(t, sent, "merge your branch")
}

func TestMergedChildCleanedUp(t *testing.T) {
	f, repo, workspace, parent := startWithGit(t, testutil.EchoPluginDirAs(t, stopManifest))
	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "plan"})
	f.waitStatus(t, workspace, kid.Session, "done")
	testutil.Never(t, func() bool { return f.session(t, workspace, kid.Session).Status == "closed" }, 500*time.Millisecond)

	write(t, filepath.Join(kid.Worktree, "b.go"), "two\n")
	git(t, kid.Worktree, "add", ".")
	git(t, kid.Worktree, "commit", "-qm", "work")
	git(t, repo, "merge", "--no-edit", "c1")
	require.NoError(t, f.runAs(t, workspace, parent, "agents.prompt", map[string]any{"session": kid.Session, "text": "finish"}, nil))

	f.waitStatus(t, workspace, kid.Session, "closed")
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(kid.Worktree)
		return errors.Is(err, os.ErrNotExist) && git(t, repo, "branch", "--list", "c1") == ""
	}, wait)
}

func TestChildWithRemovedWorktreeClosedAfterParentTurn(t *testing.T) {
	f, repo, workspace, parent := startWithGit(t)
	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "plan"})
	f.waitStatus(t, workspace, kid.Session, "done")

	git(t, repo, "worktree", "remove", "--force", kid.Worktree)
	f.prompt(t, workspace, parent, "squashed")

	f.waitStatus(t, workspace, kid.Session, "closed")
}

func mergeChild(t *testing.T, f *fixture, repo, workspace, parent string, kid child) {
	t.Helper()
	f.waitStatus(t, workspace, kid.Session, "done")
	write(t, filepath.Join(kid.Worktree, "b.go"), "two\n")
	git(t, kid.Worktree, "add", ".")
	git(t, kid.Worktree, "commit", "-qm", "work")
	git(t, repo, "merge", "--no-edit", "c1")
	require.NoError(t, f.runAs(t, workspace, parent, "agents.prompt", map[string]any{"session": kid.Session, "text": "Merged, may I stop?"}, nil))
}

func TestMergedChildAskingStaysOpen(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"), testutil.EchoPluginDirAs(t, stopManifest))
	repo := newRepo(t)
	git(t, repo, "checkout", "-qb", "feat")
	workspace := f.workspace(t, repo)
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "plan"})

	mergeChild(t, f, repo, workspace, parent, kid)

	testutil.Eventually(t, func() bool { return f.session(t, workspace, kid.Session).Asks }, wait)
	testutil.Never(t, func() bool { return f.session(t, workspace, kid.Session).Status == "closed" }, 500*time.Millisecond)
	assert.DirExists(t, kid.Worktree)
}

func TestMergedChildCleanupFailureNotice(t *testing.T) {
	failing := strings.Replace(stopManifest, `"echoResults"`, `"echoErrors":{"editor-nvim.stop-root":"unsaved buffers: b.go"},"echoResults"`, 1)
	f, repo, workspace, parent := startWithGit(t, testutil.EchoPluginDirAs(t, failing))
	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "plan"})

	mergeChild(t, f, repo, workspace, parent, kid)

	f.waitStatus(t, workspace, kid.Session, "closed")
	testutil.Eventually(t, func() bool { return len(ofKind(f.transcript(t, workspace, kid.Session).Entries, "notice")) == 1 }, wait)
	assert.Contains(t, f.logText(t, kid.Session), "unsaved buffers: b.go")
	assert.DirExists(t, kid.Worktree)
}

func TestOrchestratorPolicyAfterFirstChild(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	parent, alone := f.newSession(t, workspace), f.newSession(t, workspace)
	f.newChild(t, workspace, parent, map[string]any{})

	f.prompt(t, workspace, parent, "orchestrate")
	f.prompt(t, workspace, alone, "solo")

	sent := func(text string) string {
		return string(f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == text }).Params)
	}
	assert.Contains(t, sent("orchestrate"), "[Clubmate orchestrator]")
	assert.NotContains(t, sent("solo"), "[Clubmate orchestrator]")
}

type waited struct {
	Session    string `json:"session"`
	Status     string `json:"status"`
	Turn       int    `json:"turn"`
	StopReason string `json:"stopReason"`
	Error      string `json:"error"`
	Asks       bool   `json:"asks"`
	Permission *struct {
		Request string              `json:"request"`
		Title   string              `json:"title"`
		Kind    string              `json:"kind"`
		Options []map[string]string `json:"options"`
	} `json:"permission"`
}

type waitedAll struct {
	Sessions []waited `json:"sessions"`
	Message  *struct {
		Text string `json:"text"`
	} `json:"message"`
}

func (f *fixture) waitAll(t *testing.T, workspace, caller string, sessions ...string) waitedAll {
	t.Helper()
	var out waitedAll
	require.NoError(t, f.runAs(t, workspace, caller, "agents.wait", map[string]any{"sessions": sessions}, &out))
	require.Len(t, out.Sessions, len(sessions))
	return out
}

func (f *fixture) wait(t *testing.T, workspace, caller string, sessions ...string) []waited {
	t.Helper()
	return f.waitAll(t, workspace, caller, sessions...).Sessions
}

func TestWaitReturnsOnFirstDone(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"sleep": 500}, {"match": "slow", "sleep": 3000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	quick := f.newChild(t, workspace, parent, map[string]any{"prompt": "quick"}).Session
	slow := f.newChild(t, workspace, parent, map[string]any{"prompt": "slow"}).Session

	got := f.wait(t, workspace, parent, quick, slow)

	assert.Equal(t, waited{Session: quick, Status: "done", Turn: 1, StopReason: "end_turn"}, got[0])
	assert.Equal(t, slow, got[1].Session)
	assert.Equal(t, "running", got[1].Status)
}

func TestWaitReturnsOnNextTurn(t *testing.T) {
	f, workspace, session := startQueued(t, "two")

	got := f.wait(t, workspace, "", session)[0]

	assert.Equal(t, "running", got.Status)
	assert.Equal(t, 2, got.Turn)
}

func TestWaitReturnsQueuedUserMessage(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "slow", "sleep": 4000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "slow")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "slow"}).Session
	go func() {
		time.Sleep(300 * time.Millisecond)
		assert.NoError(t, f.run(t, workspace, "agents.prompt", map[string]any{"session": parent, "text": "stop"}, nil))
	}()

	got := f.waitAll(t, workspace, parent, kid)

	assert.Equal(t, "running", got.Sessions[0].Status)
	require.NotNil(t, got.Message)
	assert.Equal(t, "stop", got.Message.Text)
	f.waitStatus(t, workspace, parent, "done")
	assert.False(t, slices.ContainsFunc(f.calls(t, "session/prompt"), func(c fakeCall) bool { return promptText(t, c) == "stop" }))
}

func TestWaitImmediateWhenIdle(t *testing.T) {
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "hi")
	f.waitStatus(t, workspace, session, "done")

	began := time.Now()
	got := f.wait(t, workspace, "", session)

	assert.Less(t, time.Since(began), time.Second)
	assert.Equal(t, "done", got[0].Status)
}

func TestWaitReportsPermission(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "go"}).Session

	got := f.wait(t, workspace, parent, kid)[0]

	assert.Equal(t, "waiting", got.Status)
	require.NotNil(t, got.Permission)
	request := ofKind(f.transcript(t, workspace, kid).Entries, "permission")[0].ID
	assert.Equal(t, request, got.Permission.Request)
	assert.Equal(t, "Run tests", got.Permission.Title)
	assert.Equal(t, "execute", got.Permission.Kind)
	summary := f.session(t, workspace, kid).PendingSummary
	require.NotNil(t, summary)
	assert.Equal(t, "permission", summary.Kind)
	assert.Equal(t, "Run tests", summary.Text)
	assert.Equal(t, []map[string]string{{"optionId": "allow", "name": "Allow", "kind": "allow_once"}, {"optionId": "deny", "name": "Deny", "kind": "reject_once"}}, got.Permission.Options)
}

func TestAsksUser(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "Ship it? ")
	got := f.wait(t, workspace, "", session)[0]

	assert.Equal(t, "done", got.Status)
	assert.True(t, got.Asks)
	listed := f.session(t, workspace, session)
	assert.True(t, listed.Asks)
	require.NotNil(t, listed.PendingSummary)
	assert.Equal(t, "question", listed.PendingSummary.Kind)
	assert.Equal(t, "Ship it?", listed.PendingSummary.Text)
	type turnEvent struct {
		Workspace string `json:"workspace"`
		Session   string `json:"session"`
		Status    string `json:"status"`
		Asks      bool   `json:"asks"`
		Reply     string `json:"reply"`
	}
	var last turnEvent
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "agents.turn", 0, 100)
		require.NoError(t, err)
		if len(events) == 0 {
			return false
		}
		require.NoError(t, json.Unmarshal(events[len(events)-1].Payload, &last))
		return true
	}, wait)
	assert.Equal(t, turnEvent{Workspace: workspace, Session: session, Status: "done", Asks: true, Reply: "Ship it?"}, last)

	testutil.Eventually(t, func() bool {
		raw, _, err := f.h.KV.Get(t.Context(), "agents", "session/"+workspace+"/"+session)
		require.NoError(t, err)
		return strings.Contains(string(raw), `"asks":true`)
	}, wait)
	require.NoError(t, os.Remove(filepath.Join(f.h.Dirs.PluginState("agents"), "sessions", session+".jsonl")))
	f.restartPlugin(t, workspace)
	restarted := f.session(t, workspace, session)
	assert.True(t, restarted.Asks)
	require.NotNil(t, restarted.PendingSummary)
	assert.Equal(t, "Ship it?", restarted.PendingSummary.Text)
}

func TestNewInWorktree(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := filepath.Join(repo, ".worktrees", "t1")

	f.h.Event(t, workspace, "agents.sidebar", "branch", "showBranch", nil)
	branchField := func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "textField"), func(f sdui.Node) bool { return f.ID == "branch" })
	}
	f.h.WaitSlot(t, workspace, "agents.sidebar", branchField)
	f.h.Event(t, workspace, "agents.sidebar", "newBranch", "newBranch", map[string]string{"branch": " t1 "})
	var list []record
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	require.Len(t, list, 1)
	assert.Equal(t, tree, list[0].Worktree)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool { return !branchField(n) })
}

func TestSessionsCarryRunningToolActivity(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{
		{"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "e1", "title": "Editing reserve.go", "kind": "edit", "status": "in_progress"}},
		{"sleep": 1500},
	}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)

	f.prompt(t, workspace, session, "go")

	var got struct {
		Title string    `json:"title"`
		At    time.Time `json:"at"`
	}
	type info struct {
		ID       string          `json:"id"`
		Activity json.RawMessage `json:"activity"`
	}
	testutil.Eventually(t, func() bool {
		var list []info
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
		i := slices.IndexFunc(list, func(s info) bool { return s.ID == session })
		return i >= 0 && json.Unmarshal(list[i].Activity, &got) == nil && got.Title != ""
	}, wait)
	assert.Equal(t, "Editing reserve.go", got.Title)
	assert.False(t, got.At.IsZero())
}

func (f *fixture) turns(t *testing.T, session string) []api.Turn {
	t.Helper()
	events, err := f.h.Bus.Read(t.Context(), api.TopicTurn, 0, 1000)
	require.NoError(t, err)
	var out []api.Turn
	for _, e := range events {
		var turn api.Turn
		require.NoError(t, json.Unmarshal(e.Payload, &turn))
		if turn.Session == session {
			out = append(out, turn)
		}
	}
	return out
}

func TestCronStartedSessionCarriesFlagsAndOrigin(t *testing.T) {
	f := start(t, nil, testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	assert.ErrorContains(t, f.runAs(t, workspace, "", "agents.new", map[string]any{"cronJob": "cj_1"}, nil), "only the cron plugin may set cronJob")
	assert.ErrorContains(t, f.runAs(t, workspace, "", "agents.new", map[string]any{"finish": "merge"}, nil), "only the user may set finish")
	var merging child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": "merge"}, &merging))
	assert.Equal(t, "merge", f.session(t, workspace, merging.Session).Finish)

	var job struct {
		ID string `json:"id"`
	}
	require.NoError(t, f.run(t, workspace, "cron.add", map[string]any{"name": "Tidy", "command": "agents.new", "args": `{"prompt":"tidy","afk":true}`, "every": "1h"}, &job))
	require.NoError(t, f.run(t, workspace, "cron.run", map[string]any{"id": job.ID}, nil))
	var started record
	testutil.Eventually(t, func() bool {
		var list []record
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
		i := slices.IndexFunc(list, func(r record) bool { return r.CronJob == job.ID })
		if i >= 0 {
			started = list[i]
		}
		return i >= 0
	}, wait)
	f.waitStatus(t, workspace, started.ID, "done")

	assert.True(t, started.AFK)
	assert.Contains(t, string(f.calls(t, "session/prompt")[0].Params), "[Clubmate AFK mode]")
	testutil.Eventually(t, func() bool { return len(f.turns(t, started.ID)) > 0 }, wait)
	assert.Equal(t, api.Turn{Workspace: workspace, Session: started.ID, Status: "done", Finished: true, CronJob: job.ID}, f.turns(t, started.ID)[0])
}

func TestTurnFinishedOnlyWithoutActiveChildren(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "slow", "sleep": 3000}}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "slow"}).Session
	f.prompt(t, workspace, parent, "quick")
	f.waitStatus(t, workspace, kid, "done")

	testutil.Eventually(t, func() bool { return len(f.turns(t, parent)) >= 2 }, wait)
	finished := []bool{}
	for _, turn := range f.turns(t, parent) {
		finished = append(finished, turn.Finished)
	}
	assert.Equal(t, []bool{false, true}, finished[:2])
}
