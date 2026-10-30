package main_test

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	restartWait = 15 * time.Second
	wait        = 10 * time.Second
	tickWait    = 35 * time.Second
)

type job struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
	Missed  string `json:"missed"`
	NextRun string `json:"nextRun"`
	History []run  `json:"history"`
}

type run struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type agentSession struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type fixture struct {
	h       *testutil.PluginHarness
	project string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	h := testutil.NewPluginHarness(t, cronDir(t))
	return ready(t, &fixture{h: h, project: h.CreateProject(t, "demo").ID})
}

func newAgentsFixture(t *testing.T, script map[string]any) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	raw, err := json.Marshal(script)
	require.NoError(t, err)
	scriptPath := filepath.Join(t.TempDir(), "script.json")
	require.NoError(t, os.WriteFile(scriptPath, raw, 0o600))
	t.Setenv("CLUBMATE_AGENTS_COMMAND", testutil.BuildBinary(t, "clubmate/plugins/agents/internal/fakeagent"))
	t.Setenv("FAKEAGENT_SCRIPT", scriptPath)
	t.Setenv("FAKEAGENT_LOG", filepath.Join(t.TempDir(), "fake.jsonl"))
	h := testutil.NewPluginHarness(t, cronDir(t), testutil.BuildPluginDir(t, "clubmate/plugins/agents", "plugins/agents/plugin.json"))
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{newRepo(t)})
	require.NoError(t, err)
	f := ready(t, &fixture{h: h, project: p.ID})
	testutil.Eventually(t, func() bool { return f.run(t, "agents.sessions", map[string]any{}, nil) == nil }, restartWait)
	return f
}

func cronDir(t *testing.T) string {
	t.Helper()
	return testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json")
}

func ready(t *testing.T, f *fixture) *fixture {
	t.Helper()
	testutil.Eventually(t, func() bool { return f.run(t, "cron.list", map[string]any{}, nil) == nil }, restartWait)
	return f
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("one\n"), 0o600))
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"add", "."},
		{"commit", "-qm", "init"},
	} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	return repo
}

func (f *fixture) newSession(t *testing.T) string {
	t.Helper()
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, "agents.new", map[string]any{}, &out))
	require.NotEmpty(t, out.Session)
	return out.Session
}

func (f *fixture) waitStatus(t *testing.T, session, status string) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		var sessions []agentSession
		require.NoError(t, f.run(t, "agents.sessions", map[string]any{}, &sessions))
		i := slices.IndexFunc(sessions, func(s agentSession) bool { return s.ID == session })
		return i >= 0 && sessions[i].Status == status
	}, wait)
}

func (f *fixture) run(t *testing.T, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "projectId": f.project, "args": args}, out)
}

func (f *fixture) runAs(t *testing.T, caller, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "projectId": f.project, "args": args, "agent": true, "caller": caller}, out)
}

func (f *fixture) list(t *testing.T) []job {
	t.Helper()
	var out struct {
		Jobs []job `json:"jobs"`
	}
	require.NoError(t, f.run(t, "cron.list", map[string]any{}, &out))
	return out.Jobs
}

func ping(extra map[string]any) map[string]any {
	args := map[string]any{"name": "ping", "command": "notify.post", "args": `{"message":"hi"}`, "every": "1h"}
	maps.Copy(args, extra)
	return args
}

func TestAddListRunDelete(t *testing.T) {
	f := newFixture(t)
	var added job
	require.NoError(t, f.run(t, "cron.add", ping(nil), &added))
	jobs := f.list(t)
	require.Len(t, jobs, 1)
	assert.Equal(t, added.ID, jobs[0].ID)
	assert.True(t, jobs[0].Enabled)
	next, err := time.Parse(time.RFC3339Nano, jobs[0].NextRun)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), next, time.Minute)

	var r run
	require.NoError(t, f.run(t, "cron.run", map[string]any{"id": added.ID}, &r))
	assert.Equal(t, "ok", r.Status, r.Reason)
	var shown job
	require.NoError(t, f.run(t, "cron.show", map[string]any{"id": added.ID}, &shown))
	assert.Len(t, shown.History, 1)
	assert.Equal(t, jobs[0].NextRun, shown.NextRun)

	key := "job/" + f.project + "/" + added.ID
	_, found, err := f.h.KV.Get(t.Context(), "cron", key)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, f.run(t, "cron.delete", map[string]any{"id": added.ID}, nil))
	assert.Empty(t, f.list(t))
	_, found, err = f.h.KV.Get(t.Context(), "cron", key)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSectionFollowsUIActions(t *testing.T) {
	f := newFixture(t)
	f.h.WaitSlot(t, f.project, "cron.sidebar", func(n sdui.Node) bool { return n.Type == "empty" })
	var added job
	require.NoError(t, f.runAs(t, "agt_0000000000000001", "cron.add", ping(nil), &added))
	f.h.WaitSlot(t, f.project, "cron.sidebar", func(n sdui.Node) bool { return firstItem(n)["icon"] == "clock" })
	f.h.Event(t, f.project, "cron.sidebar", "jobs", "pause", added.ID)
	f.h.WaitSlot(t, f.project, "cron.sidebar", func(n sdui.Node) bool { return firstItem(n)["icon"] == "pause.circle" })
	f.h.Event(t, f.project, "cron.sidebar", "jobs", "delete", added.ID)
	f.h.WaitSlot(t, f.project, "cron.sidebar", func(n sdui.Node) bool { return n.Type == "empty" })
}

func TestAddRejectsInvalidTarget(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown", map[string]any{"command": "agents.promt"}, `unknown command "agents.promt"`},
		{"not agent", map[string]any{"command": "diagnostics.collect", "args": "{}"}, `command "diagnostics.collect" is not agent-visible`},
		{"schema", map[string]any{"args": "{}"}, "args.message: required"},
		{"argless primitive", map[string]any{"command": "workspace.list", "args": `{"x":1}`}, "args.x: not allowed"},
		{"invalid json", map[string]any{"args": "{"}, "args: invalid JSON"},
		{"cron", map[string]any{"command": "cron.add", "args": "{}"}, `command "cron.add" cannot be scheduled`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, f.run(t, "cron.add", ping(tt.args), nil), tt.want)
		})
	}
	assert.Empty(t, f.list(t))
}

func TestAgentCannotManageForeignJob(t *testing.T) {
	f := newFixture(t)
	var added job
	require.NoError(t, f.runAs(t, "agt_a", "cron.add", ping(nil), &added))
	assert.Equal(t, "skip", added.Missed)
	id := map[string]any{"id": added.ID}
	assert.ErrorContains(t, f.runAs(t, "agt_b", "cron.pause", id, nil), "job "+added.ID+" belongs to another agent session")
	require.NoError(t, f.run(t, "cron.pause", id, nil))
	require.NoError(t, f.runAs(t, "agt_a", "cron.resume", id, nil))
	assert.True(t, f.list(t)[0].Enabled)
	var mine job
	require.NoError(t, f.run(t, "cron.add", ping(nil), &mine))
	assert.ErrorContains(t, f.runAs(t, "", "cron.pause", map[string]any{"id": mine.ID}, nil), "belongs to another agent session")
}

func TestJobsSurviveRestart(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.run(t, "cron.add", ping(nil), nil))
	before := f.list(t)
	var pid int
	for _, st := range f.h.Sup.Statuses() {
		if st.ID == "cron" {
			pid = st.PID
		}
	}
	require.NotZero(t, pid)
	require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
	var out struct {
		Jobs []job `json:"jobs"`
	}
	testutil.Eventually(t, func() bool {
		pids := f.h.Sup.PIDs()
		return len(pids) > 0 && !slices.Contains(pids, pid) && f.run(t, "cron.list", map[string]any{}, &out) == nil
	}, restartWait)
	assert.Equal(t, before, out.Jobs)
}

func TestDueJobFires(t *testing.T) {
	f := newFixture(t)
	at := time.Now().Add(time.Second).Format(time.RFC3339Nano)
	require.NoError(t, f.run(t, "cron.add", ping(map[string]any{"every": "", "at": at}), nil))

	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "notify.posted", 0, 10)
		require.NoError(t, err)
		return len(events) > 0
	}, tickWait)
	testutil.Eventually(t, func() bool { return len(f.list(t)) == 0 }, wait)
}

func TestReminderSkippedWhileTargetRunning(t *testing.T) {
	f := newAgentsFixture(t, map[string]any{"prompt": []map[string]any{{"match": "hold", "sleep": 4000}}})
	session := f.newSession(t)
	var added job
	require.NoError(t, f.run(t, "cron.add", map[string]any{"name": "remind", "command": "agents.prompt", "args": `{"session":"` + session + `","text":"check"}`, "every": "1h"}, &added))
	id := map[string]any{"id": added.ID}

	require.NoError(t, f.run(t, "agents.prompt", map[string]any{"session": session, "text": "hold"}, nil))
	f.waitStatus(t, session, "running")
	var skipped, ran run
	require.NoError(t, f.run(t, "cron.run", id, &skipped))
	assert.Equal(t, run{Status: "skipped", Reason: "busy"}, skipped)

	f.waitStatus(t, session, "done")
	require.NoError(t, f.run(t, "cron.run", id, &ran))
	assert.Equal(t, run{Status: "ok"}, ran)
}

func TestOwnerClosedDeletesJobs(t *testing.T) {
	f := newAgentsFixture(t, map[string]any{})
	session := f.newSession(t)
	require.NoError(t, f.runAs(t, session, "cron.add", ping(nil), nil))
	var kept job
	require.NoError(t, f.runAs(t, session, "cron.add", ping(map[string]any{"owner": "none"}), &kept))
	require.Len(t, f.list(t), 2)

	require.NoError(t, f.run(t, "agents.close", map[string]any{"session": session}, nil))

	testutil.Eventually(t, func() bool {
		jobs := f.list(t)
		return len(jobs) == 1 && jobs[0].ID == kept.ID
	}, wait)
}

func TestOnlyRealProjectDeletionDropsJobs(t *testing.T) {
	f := newFixture(t)
	var added job
	require.NoError(t, f.run(t, "cron.add", ping(nil), &added))
	key := "job/" + f.project + "/" + added.ID
	stored := func() bool {
		_, found, err := f.h.KV.Get(t.Context(), "cron", key)
		require.NoError(t, err)
		return found
	}

	_, err := f.h.Bus.Publish(t.Context(), "project.deleted", "plugin:spoof", json.RawMessage(`{"id":"`+f.project+`"}`))
	require.NoError(t, err)
	testutil.Never(t, func() bool { return !stored() }, time.Second)

	require.NoError(t, f.h.Svc.DeleteProject(t.Context(), f.project))
	testutil.Eventually(t, func() bool { return !stored() }, wait)
}

func firstItem(n sdui.Node) map[string]any {
	items, _ := n.Props["items"].([]any)
	if len(items) == 0 {
		return nil
	}
	item, _ := items[0].(map[string]any)
	return item
}
