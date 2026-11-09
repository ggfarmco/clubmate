package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/paths"
	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

type fakeProc struct {
	mu      sync.Mutex
	specs   []stepSpec
	results []stepResult
	onRun   func(i int)
	block   bool
}

func (f *fakeProc) Run(ctx context.Context, s stepSpec, started func(int)) stepResult {
	f.mu.Lock()
	i := len(f.specs)
	f.specs = append(f.specs, s)
	var res stepResult
	if len(f.results) > 0 {
		res, f.results = f.results[0], f.results[1:]
	}
	hook, block := f.onRun, f.block
	f.mu.Unlock()
	started(1000 + i)
	if hook != nil {
		hook(i)
	}
	if block {
		<-ctx.Done()
		return stepResult{ExitCode: exitUnknown, Reason: reasonStopped}
	}
	return res
}

func (f *fakeProc) ran() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.specs)
}

func stepApp(t *testing.T, results ...stepResult) (*app, *fakeCore, *fakeProc, *Run) {
	t.Helper()
	core := &fakeCore{worktrees: gateWorktrees}
	a := testApp(t, core)
	a.sessions = gateSessions
	p := &fakeProc{results: results}
	a.proc = p
	a.spawn = func(f func()) { f() }
	r := runNamed(t, a, "gate")
	return a, core, p, r
}

func gateSessions(context.Context, string) ([]api.Session, error) {
	return []api.Session{{ID: "agt_boss", Worktree: "testdata", Mode: modeBypass}, {ID: "agt_1", Worktree: "/wt/coder", ParentSession: "agt_boss"}, {ID: "agt_2", Worktree: "testdata", ParentSession: "agt_boss"}, {ID: "agt_9", Worktree: "/wt/coder", ParentSession: "agt_boss", HandoffFrom: "agt_1"}}, nil
}

func turnDone(t *testing.T, a *app, session string, turn int) {
	t.Helper()
	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: session, Turn: turn, Status: "done", Reply: "did it"}, agentsSource)
}

func prompts(t *testing.T, core *fakeCore, session string) []string {
	t.Helper()
	var out []string
	for _, c := range core.calls {
		if c.ID != "agents.prompt" {
			continue
		}
		var args promptArgs
		require.NoError(t, json.Unmarshal(c.Args, &args))
		if args.Session == session {
			out = append(out, args.Text)
		}
	}
	return out
}

func stepMoves(a *app, runID, node string) []Move {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Move
	for _, m := range cloneRun(a.runs[runID]).Moves {
		if m.Node == node && m.Step != nil {
			out = append(out, m)
		}
	}
	return out
}

func TestCoderDoneStartsTests(t *testing.T) {
	a, _, p, r := stepApp(t)
	turnDone(t, a, "agt_1", 1)
	require.Equal(t, 1, p.ran())
	assert.Equal(t, "make test", p.specs[0].Shell)
	moves := stepMoves(a, r.ID, "tests")
	require.Len(t, moves, 1)
	assert.Equal(t, "finished", moves[0].Step.Status)
	assert.Equal(t, "pass", moves[0].Step.Outcome)
	assert.False(t, moves[0].Open)
}

func TestPassReachesReviewer(t *testing.T) {
	a, core, _, _ := stepApp(t)
	turnDone(t, a, "agt_1", 1)
	assert.Len(t, prompts(t, core, "agt_2"), 1)
}

func TestFailRoutesBackWithFencedTail(t *testing.T) {
	a, core, _, _ := stepApp(t, stepResult{ExitCode: 1, Tail: "FAIL x"})
	turnDone(t, a, "agt_1", 1)
	got := prompts(t, core, "agt_1")
	require.Len(t, got, 2)
	assert.Contains(t, got[1], "Tests failed:\n```text\nFAIL x\n```\nFix them and commit.")
}

func TestFinishedMovesGetSummaries(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: 1, Tail: "FAIL x"})
	var inputs []string
	a.summarize = func(_ context.Context, _, input string) (string, error) {
		inputs = append(inputs, input)
		return "summary " + strconv.Itoa(len(inputs)), nil
	}

	turnDone(t, a, "agt_1", 1)

	run := cloneRun(a.runs[r.ID])
	coder := run.lastTurn("coder")
	_, tests := run.outputStep("tests")
	require.Len(t, inputs, 2)
	assert.Contains(t, inputs[0], "edited x.go")
	assert.Equal(t, "$ make test\nexit 1\n\nFAIL x", inputs[1])
	assert.Equal(t, "summary 1", run.Moves[coder].Summary)
	assert.Equal(t, "did it", run.Moves[coder].Reply)
	assert.Equal(t, "summary 2", outputPanel(run, "tests", tests, nil, false).Children[1].Props["text"])
}

func TestExitCodeNamesOutcome(t *testing.T) {
	a, core, _, r := stepApp(t, stepResult{ExitCode: 3})
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, "flaky", stepMoves(a, r.ID, "tests")[0].Step.Outcome)
	assert.Len(t, prompts(t, core, "agt_1"), 2)
}

func TestAgentOutcomeStartsStep(t *testing.T) {
	a, _, p, r := stepApp(t)
	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: eventPrefix + r.outcomeEvent("reviewer"), Source: triggersSource, Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_2","outcome":"approved"}`)})
	require.Equal(t, 1, p.ran())
	assert.Equal(t, "git merge --squash x", p.specs[0].Shell)
	assert.Equal(t, statusStopped, a.runs[r.ID].Status)
	assert.Equal(t, "merge reported done", a.runs[r.ID].Reason)
}

func TestErrorWithoutEdgeStopsRun(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: exitUnknown, Reason: reasonTimedOut})
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, statusStopped, a.runs[r.ID].Status)
	assert.Equal(t, "Tests error · timed out", a.runs[r.ID].Reason)
}

func TestStepBudgetStopsRun(t *testing.T) {
	a, _, p, r := stepApp(t, stepResult{ExitCode: 1}, stepResult{ExitCode: 1}, stepResult{ExitCode: 1})
	a.runs[r.ID].MaxSteps = 2
	for turn := 1; turn <= 3; turn++ {
		turnDone(t, a, "agt_1", turn)
	}
	assert.Equal(t, 2, p.ran())
	assert.Equal(t, statusStopped, a.runs[r.ID].Status)
	assert.Equal(t, "step budget of 2 reached", a.runs[r.ID].Reason)
}

func TestRefireWhileRunningRunsOnceMore(t *testing.T) {
	a, _, p, _ := stepApp(t)
	p.onRun = func(i int) {
		if i == 0 {
			turnDone(t, a, "agt_1", 2)
			turnDone(t, a, "agt_1", 3)
		}
	}
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, 2, p.ran())
}

func TestOwnedEdgeFollowsHandoff(t *testing.T) {
	a, core, _, r := stepApp(t, stepResult{ExitCode: 1}, stepResult{ExitCode: 1})
	turnDone(t, a, "agt_1", 1)
	a.runs[r.ID].node("coder").Session = "agt_9"
	turnDone(t, a, "agt_9", 2)
	assert.Len(t, prompts(t, core, "agt_9"), 1)
}

func TestStepSec_O2_ForgedTurnFiresNothing(t *testing.T) {
	a, _, p, _ := stepApp(t)
	a.countTurn(t.Context(), api.Turn{Workspace: "ws1", Session: "agt_1", Turn: 1, Status: "done"}, "cli")
	assert.Zero(t, p.ran())
}

func TestStepSec_O3_BusOutcomeIgnored(t *testing.T) {
	for _, source := range []string{"cli", triggersSource} {
		a, core, p, r := stepApp(t)
		a.busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: eventPrefix + r.outcomeEvent("tests"), Source: source, Payload: json.RawMessage(`{"workspace":"ws1","outcome":"pass"}`)})
		a.busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: eventPrefix + r.outcomeEvent("reviewer"), Source: "cli", Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_2","outcome":"approved"}`)})
		assert.Empty(t, prompts(t, core, "agt_2"), source)
		assert.Zero(t, p.ran(), source)
		assert.Empty(t, stepMoves(a, r.ID, "tests"), source)
	}
}

func TestStepSec_O4_NoExitIsNotPass(t *testing.T) {
	a, _, _, r := stepApp(t, stepResult{ExitCode: exitUnknown, Reason: "could not start: x"})
	turnDone(t, a, "agt_1", 1)
	m := stepMoves(a, r.ID, "tests")[0]
	assert.Equal(t, onError, m.Step.Outcome)
	assert.Equal(t, "could not start: x", m.Step.Reason)
}

const gateWorktrees = `[{"repo":"testdata","path":"/repo","branch":"main","main":true},{"repo":"testdata","path":"/wt/coder","branch":"feat/x"},{"repo":"testdata","path":"testdata","branch":"boss"}]`

func folderApp(t *testing.T) (*app, *fakeProc, *Run) {
	t.Helper()
	core := &fakeCore{worktrees: gateWorktrees}
	a := testApp(t, core)
	a.sessions = gateSessions
	p := &fakeProc{}
	a.proc, a.spawn = p, func(f func()) { f() }
	return a, p, runWith(t, a, "gate", `"; rm -rf ~`)
}

func envOf(spec stepSpec, key string) (string, bool) {
	for _, kv := range spec.Env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			return v, true
		}
	}
	return "", false
}

func TestStepRunsInNamedFolder(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "testdata"},
		{"coder", "/wt/coder"},
		{"main", "/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			a, p, r := folderApp(t)
			a.runs[r.ID].Spec.node("tests").In = tt.in
			turnDone(t, a, "agt_1", 1)
			require.Equal(t, 1, p.ran())
			assert.Equal(t, tt.want, p.specs[0].Dir)
		})
	}
}

func TestStepSec_C6_EnvNotShell(t *testing.T) {
	t.Setenv("CLUBMATE_PLUGIN_SOCKET", "/tmp/x.sock")
	t.Setenv("CLUBMATE_AGENT_SESSION", "agt_x")
	t.Setenv("HOME", t.TempDir())
	defaults, err := paths.Defaults()
	require.NoError(t, err)
	t.Setenv("CLUBMATE_STATE_DIR", defaults.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", "/tmp/dev/run")
	a, p, _ := folderApp(t)
	turnDone(t, a, "agt_1", 1)
	spec := p.specs[0]
	assert.Equal(t, "make test", spec.Shell)
	input, _ := envOf(spec, "CLUBMATE_GRAPH_INPUT")
	assert.Equal(t, `"; rm -rf ~`, input)
	runtime, _ := envOf(spec, "CLUBMATE_RUNTIME_DIR")
	assert.Equal(t, "/tmp/dev/run", runtime)
	_, ok := envOf(spec, "CLUBMATE_STATE_DIR")
	assert.False(t, ok)
	allowed := []string{"CLUBMATE_WORKSPACE_ID", "CLUBMATE_GRAPH", "CLUBMATE_GRAPH_RUN", "CLUBMATE_GRAPH_INPUT", "CLUBMATE_GRAPH_FROM", "CLUBMATE_STATE_DIR", "CLUBMATE_RUNTIME_DIR"}
	for _, kv := range spec.Env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "CLUBMATE_") && !strings.HasPrefix(k, "CLUBMATE_GRAPH_NODE_") && !strings.HasPrefix(k, "CLUBMATE_GRAPH_WORKTREE_") && !strings.HasPrefix(k, "CLUBMATE_GRAPH_BRANCH_") {
			assert.True(t, slices.Contains(allowed, k), k)
		}
	}
}

func TestStepEnvNamesNodes(t *testing.T) {
	a, p, r := folderApp(t)
	turnDone(t, a, "agt_1", 1)
	spec := p.specs[0]
	for key, want := range map[string]string{
		"CLUBMATE_GRAPH_NODE_CODER":     "agt_1",
		"CLUBMATE_GRAPH_WORKTREE_CODER": "/wt/coder",
		"CLUBMATE_GRAPH_BRANCH_CODER":   "feat/x",
		"CLUBMATE_GRAPH_FROM":           "coder",
		"CLUBMATE_GRAPH_RUN":            r.ID,
		"CLUBMATE_GRAPH":                "gate",
		"CLUBMATE_WORKSPACE_ID":         "ws1",
	} {
		got, ok := envOf(spec, key)
		assert.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}
	_, ok := envOf(spec, "CLUBMATE_GRAPH_NODE_TESTS")
	assert.False(t, ok)
}

func TestStepSec_C1_FrozenCommand(t *testing.T) {
	dir := t.TempDir()
	graphs := filepath.Join(dir, graphsDir)
	require.NoError(t, os.MkdirAll(graphs, 0o755))
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	path := filepath.Join(graphs, "gate.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	core := &fakeCore{}
	a := testApp(t, core)
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: dir, Mode: modeBypass}, {ID: "agt_1", Worktree: dir}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: dir}}}, nil
	}
	p := &fakeProc{}
	a.proc, a.spawn = p, func(f func()) { f() }
	runNamed(t, a, "gate")
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(data), "make test", "exit 0")), 0o644))
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, "make test", p.specs[0].Shell)
}

func TestCheckFilesChangedInRun(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, graphsDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, graphsDir, "gate.json"), data, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644))
	git("add", ".")
	git("commit", "-qm", "init")
	core := &fakeCore{worktrees: `[{"repo":"` + dir + `","path":"` + dir + `","branch":"main","main":true}]`}
	a := testApp(t, core)
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: dir, Mode: modeBypass}, {ID: "agt_1", Worktree: dir}, {ID: "agt_2", Worktree: dir}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: dir}}}, nil
	}
	a.proc, a.spawn = &fakeProc{}, func(f func()) { f() }
	r := runNamed(t, a, "gate")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\ttrue\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package b\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rules.mk"), []byte("x:\n"), 0o644))
	git("commit", "-qam", "weaken")
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, []string{"Makefile", "rules.mk"}, stepMoves(a, r.ID, "tests")[0].Step.CheckFiles)
	got := prompts(t, core, "agt_2")
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "```\nThe check's own files changed in this run: `Makefile`, `rules.mk`.")
}

func TestCheckOwnFiles(t *testing.T) {
	changed := []string{"Makefile", "sub/Makefile", "sub/x.mk", "scripts/check.sh", "scripts/other.sh", "a.go"}
	tests := []struct {
		run  string
		want []string
	}{
		{"make test", []string{"Makefile", "sub/Makefile", "sub/x.mk"}},
		{"./scripts/check.sh --fast", []string{"scripts/check.sh"}},
		{"go vet ./... && bash scripts/check.sh", []string{"scripts/check.sh"}},
		{"go test ./...", nil},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, checkOwnFiles(tt.run, changed), tt.run)
	}
}

func approvalApp(t *testing.T, mode string, allow ...string) (*app, *fakeCore, *fakeProc, *Run) {
	t.Helper()
	core := &fakeCore{worktrees: gateWorktrees}
	a := testApp(t, core)
	home := a.home
	if len(allow) > 0 {
		writeSettings(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"allow": allow}})
	}
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: "testdata", Mode: mode}, {ID: "agt_1", Worktree: "/wt/coder", ParentSession: "agt_boss"}, {ID: "agt_2", Worktree: "testdata", ParentSession: "agt_boss"}}, nil
	}
	p := &fakeProc{}
	a.proc, a.spawn = p, func(f func()) { f() }
	return a, core, p, runNamed(t, a, "gate")
}

func approve(t *testing.T, a *app, r *Run, node, decision string) error {
	t.Helper()
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.approve", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `","node":"` + node + `","decision":"` + decision + `"}`)})
	return err
}

func called(core *fakeCore, id string) []json.RawMessage {
	var out []json.RawMessage
	for _, c := range core.calls {
		if c.ID == id {
			out = append(out, c.Args)
		}
	}
	return out
}

func TestBypassModeRunsWithoutAsking(t *testing.T) {
	a, _, p, r := approvalApp(t, modeBypass)
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, 1, p.ran())
	assert.Equal(t, "mode bypass", stepMoves(a, r.ID, "tests")[0].Step.AllowedBy)
}

func TestAllowRuleRunsWithoutAsking(t *testing.T) {
	a, _, p, r := approvalApp(t, "auto", "Bash(make test)")
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, 1, p.ran())
	assert.Equal(t, "rule Bash(make test)", stepMoves(a, r.ID, "tests")[0].Step.AllowedBy)
}

func TestAutoModeAsksWithoutRule(t *testing.T) {
	a, core, p, r := approvalApp(t, "auto")
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, stepWaiting, stepMoves(a, r.ID, "tests")[0].Step.Status)
	notes := called(core, "notify.post")
	require.Len(t, notes, 1)
	assert.Contains(t, string(notes[0]), "A graph step wants to run a command")
	assert.NotContains(t, string(notes[0]), "make test")
}

func TestApproveOnceRunsPendingStep(t *testing.T) {
	a, _, p, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	require.NoError(t, approve(t, a, r, "tests", "once"))
	assert.Equal(t, 1, p.ran())
	assert.Equal(t, "you", stepMoves(a, r.ID, "tests")[0].Step.AllowedBy)
}

func TestRejectEndsStepRejected(t *testing.T) {
	a, _, p, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	require.NoError(t, approve(t, a, r, "tests", "reject"))
	assert.Zero(t, p.ran())
	m := stepMoves(a, r.ID, "tests")[0]
	assert.Equal(t, onError, m.Step.Outcome)
	assert.Equal(t, reasonRejected, m.Step.Reason)
}

func TestApproveAfterStopFails(t *testing.T) {
	a, _, p, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	assert.ErrorContains(t, approve(t, a, r, "tests", "once"), "run is stopped")
	assert.Zero(t, p.ran())
}

func TestRefireWhileWaitingKeepsOnePending(t *testing.T) {
	a, core, p, r := approvalApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	turnDone(t, a, "agt_1", 2)
	assert.Len(t, called(core, "notify.post"), 1)
	require.NoError(t, approve(t, a, r, "tests", "once"))
	assert.Equal(t, 1, p.ran())
}

func TestStepSec_F2_ApproveNotAgentVisible(t *testing.T) {
	data, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	var manifest struct {
		Contributes struct {
			Commands []struct {
				ID    string `json:"id"`
				Agent bool   `json:"agent"`
			} `json:"commands"`
		} `json:"contributes"`
	}
	require.NoError(t, json.Unmarshal(data, &manifest))
	seen := map[string]bool{}
	for _, c := range manifest.Contributes.Commands {
		if c.ID == "graphs.approve" {
			seen[c.ID] = true
			assert.False(t, c.Agent)
		}
	}
	assert.True(t, seen["graphs.approve"])
	a, _, _, r := approvalApp(t, "default")
	_, err = a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.approve", WorkspaceID: "ws1", Agent: true, Caller: "agt_boss", Args: json.RawMessage(`{"id":"` + r.ID + `","node":"tests","decision":"once"}`)})
	assert.Error(t, err)
}

func TestUserCommandsRefuseASessionCaller(t *testing.T) {
	for _, id := range []string{"graphs.approve", "graphs.continue", "graphs.trust"} {
		t.Run(id, func(t *testing.T) {
			a, _, _, r := approvalApp(t, "default")

			_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: id, WorkspaceID: "ws1", Caller: "agt_1", Args: json.RawMessage(`{"id":"` + r.ID + `","node":"tests","decision":"always","edge":0,"graph":"gate"}`)})

			require.ErrorIs(t, err, agentcall.ErrUnknownCommand)
		})
	}
}

func TestCommandStepRunsAsCaller(t *testing.T) {
	a, core, _, r := approvalApp(t, "default")
	a.runs[r.ID].Spec.node("tests").Run = ""
	a.runs[r.ID].Spec.node("tests").Command = "notify.post"
	a.runs[r.ID].Spec.node("tests").Args = json.RawMessage(`{"message":"{{run}}"}`)
	turnDone(t, a, "agt_1", 1)
	var got []automation.CommandRequest
	for _, c := range core.calls {
		if c.ID == "notify.post" {
			got = append(got, c)
		}
	}
	require.Len(t, got, 1)
	assert.True(t, got[0].Agent)
	assert.Equal(t, "agt_boss", got[0].Caller)
	assert.JSONEq(t, `{"message":"`+r.ID+`"}`, string(got[0].Args))
	assert.Equal(t, "command", stepMoves(a, r.ID, "tests")[0].Step.AllowedBy)
}

func continueEdge(t *testing.T, a *app, r *Run, edge int) error {
	t.Helper()
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.continue", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `","edge":` + strconv.Itoa(edge) + `}`)})
	return err
}

func TestCapHoldsThirdRound(t *testing.T) {
	a, core, _, r := stepApp(t, stepResult{ExitCode: 1}, stepResult{ExitCode: 1}, stepResult{ExitCode: 1})
	for turn := 1; turn <= 3; turn++ {
		turnDone(t, a, "agt_1", turn)
	}
	assert.Len(t, prompts(t, core, "agt_1"), 3)
	require.Len(t, a.runs[r.ID].Held, 1)
	assert.Equal(t, 1, a.runs[r.ID].Held[0].Edge)
	notes := called(core, "notify.post")
	require.Len(t, notes, 1)
	assert.Contains(t, string(notes[0]), `"kind":"permission"`)
	assert.Contains(t, string(notes[0]), `"session":"`+r.ID+`"`)
	assert.Contains(t, string(notes[0]), "Tests → Coder reached its limit of 2 rounds")
}

func TestContinueDeliversHeldRound(t *testing.T) {
	a, core, _, r := stepApp(t, stepResult{ExitCode: 1}, stepResult{ExitCode: 1}, stepResult{ExitCode: 1}, stepResult{ExitCode: 1})
	for turn := 1; turn <= 3; turn++ {
		turnDone(t, a, "agt_1", turn)
	}
	require.NoError(t, continueEdge(t, a, r, 1))
	assert.Len(t, prompts(t, core, "agt_1"), 4)
	assert.Empty(t, a.runs[r.ID].Held)
	turnDone(t, a, "agt_1", 4)
	assert.Len(t, prompts(t, core, "agt_1"), 4)
	assert.Len(t, a.runs[r.ID].Held, 1)
	assert.ErrorContains(t, continueEdge(t, a, r, 0), "edge 0 is not held")
}

func TestCappedAgentEdgeIsOwned(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, graphsDir), 0o755))
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	capped := strings.Replace(string(data), `"outcome": "changes"}`, `"outcome": "changes", "maxFires": 1}`, 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, graphsDir, "gate.json"), []byte(capped), 0o644))
	core := &fakeCore{}
	a := testApp(t, core)
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: dir, Mode: modeBypass}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: dir}}}, nil
	}
	r := runNamed(t, a, "gate")
	assert.NotContains(t, core.ids(), "triggers.add")
	changes := pluginsdk.Event{Topic: eventPrefix + r.outcomeEvent("reviewer"), Source: triggersSource, Payload: json.RawMessage(`{"workspace":"ws1","by":"agt_2","outcome":"changes"}`)}
	a.busEvent(t.Context(), nil, "", changes)
	a.busEvent(t.Context(), nil, "", changes)
	assert.Len(t, prompts(t, core, "agt_1"), 2)
	assert.Len(t, a.runs[r.ID].Held, 1)
}

func TestBypassStillHonoursDeny(t *testing.T) {
	a, _, p, r := approvalApp(t, modeBypass)
	writeSettings(t, filepath.Join(a.home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"deny": []string{"Bash(make:*)"}}})
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, "denied by rule Bash(make:*)", stepMoves(a, r.ID, "tests")[0].Step.Reason)
}

func blockedStep(t *testing.T) (*app, *fakeProc, *Run) {
	t.Helper()
	a, _, p, r := stepApp(t)
	p.block = true
	a.spawn = func(f func()) { go f() }
	turnDone(t, a, "agt_1", 1)
	require.Eventually(t, func() bool { return p.ran() == 1 }, time.Second, 5*time.Millisecond)
	return a, p, r
}

func TestStopCancelsRunningStep(t *testing.T) {
	a, _, r := blockedStep(t)
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	assert.Eventually(t, func() bool {
		m := stepMoves(a, r.ID, "tests")
		return len(m) == 1 && m[0].Step.Reason == reasonStopped
	}, time.Second, 5*time.Millisecond)
}

func TestStopClosesQueuedStepMoves(t *testing.T) {
	a, _, r := blockedStep(t)
	turnDone(t, a, "agt_1", 2)
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	assert.Eventually(t, func() bool {
		m := stepMoves(a, r.ID, "tests")
		return len(m) == 2 && !m[1].Open && m[1].Step.Outcome == onError && m[1].Step.Reason == reasonStopped
	}, time.Second, 5*time.Millisecond)
}

func TestWorkspaceDeleteKillsSteps(t *testing.T) {
	a, _, _ := blockedStep(t)
	a.workspaces = func(context.Context) ([]pluginsdk.Workspace, error) { return nil, nil }
	a.dropWorkspace(t.Context(), "ws1")
	assert.Eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.cancels) == 0
	}, time.Second, 5*time.Millisecond)
}

func TestRestartMarksOpenStepInterrupted(t *testing.T) {
	a, _, _, r := stepApp(t)
	live := a.runs[r.ID]
	live.fire(0, 0, a.now())
	live.Moves[len(live.Moves)-1].Step = &StepMove{Status: stepRunning, Command: "make test"}
	live.fire(0, 0, a.now())
	var killed []int
	a.roots = func(context.Context, string) ([]pluginsdk.ProcessRoot, error) {
		return []pluginsdk.ProcessRoot{{PID: 42, Label: stepLabel + r.ID + "/tests"}, {PID: 7, Label: "nvim"}, {PID: 9, Label: stepLabel + "x", Ended: true}}, nil
	}
	a.kill = func(_ context.Context, req pluginsdk.KillRequest) error {
		killed = append(killed, req.PIDs...)
		return nil
	}
	a.recoverSteps(t.Context())
	moves := stepMoves(a, r.ID, "tests")
	require.Len(t, moves, 2)
	for _, m := range moves {
		assert.False(t, m.Open)
		assert.Equal(t, onError, m.Step.Outcome)
		assert.Equal(t, reasonInterrupted, m.Step.Reason)
	}
	assert.Equal(t, statusStopped, live.Status)
	assert.Equal(t, "Tests error · interrupted", live.Reason)
	assert.Equal(t, []int{42}, killed)
}

func TestRestartDeliversOwedEdgesAfterReady(t *testing.T) {
	a, core, _, r := stepApp(t)
	live := a.runs[r.ID]
	live.Spec.Edges = append(live.Spec.Edges, Edge{From: "tests", To: "coder", On: onError})
	live.Edges = append(live.Edges, RunEdge{From: "tests", To: "coder", On: onError, Owned: true})
	live.fire(0, 0, a.now())
	live.Moves[len(live.Moves)-1].Step = &StepMove{Status: stepRunning, Command: "make test"}
	coder := live.node("coder").Session
	before := len(prompts(t, core, coder))
	resume := a.recoverSteps(t.Context())
	assert.Len(t, prompts(t, core, coder), before)
	resume(t.Context())
	assert.Len(t, prompts(t, core, coder), before+1)
}

func TestStepSec_C7_CommandStepIsAnAgentCall(t *testing.T) {
	core := &fakeCore{worktrees: gateWorktrees}
	a := testApp(t, core)
	a.sessions = gateSessions
	a.proc, a.spawn = &fakeProc{}, func(f func()) { f() }
	require.NoError(t, trustGraph(t, a, "gate"))
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.run", WorkspaceID: "ws1", Args: json.RawMessage(`{"graph":"gate"}`)})
	require.NoError(t, err)
	r := out.(*Run)
	a.runs[r.ID].Spec.node("tests").Run = ""
	a.runs[r.ID].Spec.node("tests").In = ""
	a.runs[r.ID].Spec.node("tests").Command = "graphs.trust"
	a.runs[r.ID].Spec.node("tests").Args = json.RawMessage(`{"graph":"gate"}`)
	turnDone(t, a, r.node("coder").Session, 1)
	var got []automation.CommandRequest
	for _, c := range core.calls {
		if c.ID == "graphs.trust" {
			got = append(got, c)
		}
	}
	require.Len(t, got, 1)
	assert.True(t, got[0].Agent)
	assert.Empty(t, got[0].Caller)
}

func TestMainCheckoutOnlyForKnownFolders(t *testing.T) {
	var trees []worktreeInfo
	require.NoError(t, json.Unmarshal([]byte(gateWorktrees), &trees))
	assert.Equal(t, "/repo", mainCheckout(trees, "/wt/coder/sub"))
	assert.Empty(t, mainCheckout(trees, "/elsewhere"))
}

func capIntoStepApp(t *testing.T) (*app, *fakeProc, *Run) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, graphsDir), 0o755))
	graph := `{"nodes":[{"id":"coder","prompt":"p","start":true},{"id":"tests","role":"Tests","run":"make test"}],"edges":[{"from":"coder","to":"tests","maxFires":1},{"from":"tests","to":"coder","on":"outcome","outcome":"fail"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, graphsDir, "capped.json"), []byte(graph), 0o644))
	a := testApp(t, &fakeCore{})
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: dir, Mode: modeBypass}, {ID: "agt_1", Worktree: dir}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: dir}}}, nil
	}
	p := &fakeProc{}
	a.proc, a.spawn = p, func(f func()) { f() }
	return a, p, runNamed(t, a, "capped")
}

func TestHeldEdgeIntoStepWaitsForContinue(t *testing.T) {
	a, p, r := capIntoStepApp(t)
	p.onRun = func(i int) {
		if i == 0 {
			turnDone(t, a, "agt_1", 2)
		}
	}
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, 1, p.ran())
	require.Len(t, a.runs[r.ID].Held, 1)
	require.NoError(t, continueEdge(t, a, r, 0))
	assert.Equal(t, 2, p.ran())
	assert.Error(t, continueEdge(t, a, r, 0))
	assert.Equal(t, 2, p.ran())
}

func TestStopWhileResolvingEndsStep(t *testing.T) {
	a, _, p, r := stepApp(t)
	sessions := a.sessions
	a.sessions = func(ctx context.Context, ws string) ([]api.Session, error) {
		a.mu.Lock()
		live := a.runs[r.ID]
		a.mu.Unlock()
		if live.Status == statusRunning && len(stepMoves(a, r.ID, "tests")) == 1 {
			_, err := a.command(ctx, nil, pluginsdk.CommandRun{ID: "graphs.stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
			require.NoError(t, err)
		}
		return sessions(ctx, ws)
	}
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	m := stepMoves(a, r.ID, "tests")[0]
	assert.Equal(t, stepFinished, m.Step.Status)
	assert.Equal(t, reasonStopped, m.Step.Reason)
	assert.False(t, m.Open)
}

func TestBypassAsksWhenDenyWordAppears(t *testing.T) {
	a, _, p, r := approvalApp(t, modeBypass)
	writeSettings(t, filepath.Join(a.home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"deny": []string{"Bash(test:*)"}}})
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, stepWaiting, stepMoves(a, r.ID, "tests")[0].Step.Status)
}

func TestCommandStepFailuresAreErrors(t *testing.T) {
	pluginErr := &jsonrpc2.Error{Code: contract.CodePluginError, Message: "plugin notify command.run: boom"}
	tests := []struct {
		name    string
		err     error
		block   bool
		outcome string
		reason  string
	}{
		{"unknown command", errors.New("command not found"), false, onError, "could not run: command not found"},
		{"command failed", pluginErr, false, outFail, ""},
		{"timeout", nil, true, onError, reasonTimedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, core, _, r := approvalApp(t, "default")
			core.errs = map[string]error{"notify.post": tt.err}
			core.block = map[string]bool{"notify.post": tt.block}
			n := a.runs[r.ID].Spec.node("tests")
			n.Run, n.In, n.Command, n.Timeout = "", "", "notify.post", 1
			turnDone(t, a, "agt_1", 1)
			m := stepMoves(a, r.ID, "tests")[0]
			assert.Equal(t, tt.outcome, m.Step.Outcome)
			assert.Equal(t, tt.reason, m.Step.Reason)
		})
	}
}

func TestRemovedRunTakesItsOutputAndSelection(t *testing.T) {
	for _, how := range []string{"delete", "workspace"} {
		t.Run(how, func(t *testing.T) {
			t.Setenv("CLUBMATE_PLUGIN_STATE_DIR", t.TempDir())
			a, _, _, r := stepApp(t)
			turnDone(t, a, "agt_1", 1)
			out := stepMoves(a, r.ID, "tests")[0].Step.Output
			require.Equal(t, outputDir(r.ID), filepath.Dir(out))
			require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o700))
			require.NoError(t, os.WriteFile(out, []byte("x"), 0o600))
			a.stepSel[r.ID] = pick{node: "tests"}
			if how == "delete" {
				_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.delete", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
				require.NoError(t, err)
			} else {
				a.workspaces = func(context.Context) ([]pluginsdk.Workspace, error) { return nil, nil }
				a.dropWorkspace(t.Context(), "ws1")
			}
			assert.NoDirExists(t, outputDir(r.ID))
			assert.NotContains(t, a.stepSel, r.ID)
		})
	}
}

func splitApp(t *testing.T, mode string) (*app, *fakeProc, *Run, string, string) {
	t.Helper()
	main, wt := t.TempDir(), t.TempDir()
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(wt, graphsDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wt, graphsDir, "gate.json"), data, 0o644))
	core := &fakeCore{worktrees: `[{"repo":"` + main + `","path":"` + main + `","branch":"main","main":true},{"repo":"` + main + `","path":"` + wt + `","branch":"feat"}]`}
	a := testApp(t, core)
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: wt, Mode: mode}, {ID: "agt_1", Worktree: wt}, {ID: "agt_2", Worktree: wt}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: wt}}}, nil
	}
	p := &fakeProc{}
	a.proc, a.spawn = p, func(f func()) { f() }
	return a, p, runNamed(t, a, "gate"), main, wt
}

func TestStepSec_C8_WorktreeAllowRuleIsIgnored(t *testing.T) {
	allow := map[string]any{"permissions": map[string]any{"allow": []string{"Bash(make test)"}}}
	a, p, r, _, wt := splitApp(t, "default")
	writeSettings(t, filepath.Join(wt, ".claude", "settings.local.json"), allow)
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, stepWaiting, stepMoves(a, r.ID, "tests")[0].Step.Status)

	a, p, r, main, _ := splitApp(t, "default")
	writeSettings(t, filepath.Join(main, ".claude", "settings.local.json"), allow)
	a.runs[r.ID].Spec.node("tests").In = inMain
	turnDone(t, a, "agt_1", 1)
	assert.Equal(t, 1, p.ran())
}

func TestAlwaysAllowGoesToMainCheckout(t *testing.T) {
	a, _, r, main, wt := splitApp(t, "default")
	turnDone(t, a, "agt_1", 1)
	require.NoError(t, approve(t, a, r, "tests", "always"))
	assert.NoFileExists(t, filepath.Join(wt, ".claude", "settings.local.json"))
	data, err := os.ReadFile(filepath.Join(main, ".claude", "settings.local.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "Bash(make test)")
}

func TestBypassAsksOnQuotedCommandWithDeny(t *testing.T) {
	a, _, p, r := approvalApp(t, modeBypass)
	writeSettings(t, filepath.Join(a.home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"deny": []string{"Bash(rm:*)"}}})
	a.runs[r.ID].Spec.node("tests").Run = `r\m -rf x`
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, stepWaiting, stepMoves(a, r.ID, "tests")[0].Step.Status)
}

func TestStepSec_C9_UnknownFolderTrustsNoProjectRules(t *testing.T) {
	a, p, r, _, wt := splitApp(t, "default")
	a.runCommand = (&fakeCore{worktrees: `[]`}).run
	writeSettings(t, filepath.Join(wt, ".claude", "settings.local.json"), map[string]any{"permissions": map[string]any{"allow": []string{"Bash(make test)"}}})
	a.runs[r.ID].Spec.node("tests").In = ""
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, stepWaiting, stepMoves(a, r.ID, "tests")[0].Step.Status)
	assert.ErrorContains(t, approve(t, a, r, "tests", "always"), "no main checkout")
}

func TestBypassAsksOnCompoundCommandWithDeny(t *testing.T) {
	a, _, p, r := approvalApp(t, modeBypass)
	writeSettings(t, filepath.Join(a.home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"deny": []string{"Bash(rm:*)"}}})
	a.runs[r.ID].Spec.node("tests").Run = "make test && echo ok"
	turnDone(t, a, "agt_1", 1)
	assert.Zero(t, p.ran())
	assert.Equal(t, stepWaiting, stepMoves(a, r.ID, "tests")[0].Step.Status)
}

func TestRunningStepFollowsItsLog(t *testing.T) {
	a, _, r := blockedStep(t)
	out := filepath.Join(t.TempDir(), "1.log")
	require.NoError(t, os.WriteFile(out, []byte(strings.Repeat("ok\n", 40)), 0o600))
	a.mu.Lock()
	moves := a.runs[r.ID].Moves
	moves[len(moves)-1].Step.Output = out
	a.mu.Unlock()
	view := runView(t, a, r.ID)
	assert.Equal(t, "Open Tests output", view.Children[0].Children[0].Props["label"])
	assert.Contains(t, texts(graphChildren(view)[0]), "last 12 of 40 lines · Tests is still running: the tail follows its output.")
	var opened []string
	a.runCommand = func(_ context.Context, req automation.CommandRequest) (json.RawMessage, error) {
		opened = append(opened, req.ID+" "+string(req.Args))
		return json.RawMessage(`{}`), nil
	}
	_, err := a.uiEvent(t.Context(), nil, pluginsdk.UIEvent{WorkspaceID: "ws1", Slot: mainSlot, NodeID: r.ID + "/" + actStepOutput, Action: actStepOutput})
	require.NoError(t, err)
	assert.Equal(t, []string{`editor.open {"path":"` + out + `","focus":true}`}, opened)
}
