package graphs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/pluginsdk"
)

const testGateDir = "/repo/.worktrees/.gate-gt_1-k"

type testGate struct {
	a      *app
	e      *Engine
	core   *fakeCore
	p      *fakeProc
	repo   string
	mu     sync.Mutex
	checks []GateCheck
	nodes  []GateNode
}

func newTestGate(t *testing.T, results ...stepResult) *testGate {
	t.Helper()
	g := &testGate{core: &fakeCore{worktrees: gateWorktrees}, p: &fakeProc{results: results}, repo: t.TempDir()}
	g.a = testApp(t, g.core)
	g.a.proc, g.a.spawn = g.p, func(f func()) { f() }
	g.a.gateCheck = func(_ context.Context, c GateCheck) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.checks = append(g.checks, c)
	}
	g.e = &Engine{a: g.a}
	return g
}

func (g *testGate) restarted(t *testing.T, results ...stepResult) *testGate {
	t.Helper()
	next := newTestGate(t, results...)
	next.a.kvGet, next.a.kvSet, next.repo = g.a.kvGet, g.a.kvSet, g.repo
	return next
}

func (g *testGate) write(t *testing.T, name, graph string) {
	t.Helper()
	dir := filepath.Join(g.repo, graphsDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+graphExt), []byte(graph), 0o644))
}

func (g *testGate) start(t *testing.T, graph, tree string) (string, string) {
	t.Helper()
	pin, valid, err := g.e.GateGraph(t.Context(), g.repo, graph)
	require.NoError(t, err)
	require.True(t, valid)
	run, verdict, err := g.e.StartGate(t.Context(), GateRun{Workspace: "ws1", Gate: "gt_1", Graph: graph, Pin: pin, Repo: g.repo, Input: "Review it", Other: "Look at it", Dir: testGateDir, Test: "make test", Tree: tree, NewNode: func(_ context.Context, n GateNode) (string, error) {
		g.nodes = append(g.nodes, n)
		return fmt.Sprintf("agt_g%d", len(g.nodes)), nil
	}})
	require.NoError(t, err)
	return run, verdict
}

func (g *testGate) got() []GateCheck {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]GateCheck(nil), g.checks...)
}

func TestTestGateDecidesALandingWithoutAnAgent(t *testing.T) {
	g := newTestGate(t, stepResult{})

	run, verdict := g.start(t, "test-gate", "T1")

	assert.Empty(t, verdict)
	assert.Empty(t, g.nodes)
	require.Equal(t, 1, g.p.ran())
	assert.Equal(t, []string{"make test", testGateDir}, []string{g.p.specs[0].Shell, g.p.specs[0].Dir})
	assert.Equal(t, allowedGateTest, stepMoves(g.a, run, "tests")[0].Step.AllowedBy)
	assert.Equal(t, []GateCheck{{Workspace: "ws1", Run: run, Graph: "test-gate", Node: "tests", Command: "make test", Outcome: outPass, Tree: "T1"}}, g.got())
	assert.True(t, g.e.GateRunning(t.Context(), run))
}

func TestTestGateFailReportsTheTailAndTheNextRoundRerunsOnTheNewTree(t *testing.T) {
	g := newTestGate(t, stepResult{ExitCode: 1, Tail: "boom"}, stepResult{})
	run, _ := g.start(t, "test-gate", "T1")
	failed := g.got()

	require.NoError(t, g.e.PromptGate(t.Context(), run, "round two", "round two, no verdict", "T2"))

	require.Len(t, failed, 1)
	assert.Equal(t, []any{outFail, 1, "boom", "T1"}, []any{failed[0].Outcome, failed[0].ExitCode, failed[0].Tail, failed[0].Tree})
	assert.Equal(t, 2, g.p.ran())
	assert.Equal(t, []any{outPass, "T2"}, []any{g.got()[1].Outcome, g.got()[1].Tree})
	assert.True(t, g.e.GateRunning(t.Context(), run))
}

func TestTestReviewSendsOnlyAPassToTheReviewer(t *testing.T) {
	g := newTestGate(t, stepResult{ExitCode: 1, Tail: "boom"}, stepResult{})
	run, verdict := g.start(t, "test-review", "T1")
	failed := g.got()
	before := prompts(t, g.core, verdict)

	require.NoError(t, g.e.PromptGate(t.Context(), run, "Review round two", "round two, no verdict", "T2"))

	assert.Equal(t, "agt_g1", verdict)
	assert.Equal(t, []GateNode{{Title: "Reviewer", Model: "opus", Verdict: true}}, stripRun(g.nodes))
	require.Len(t, failed, 1)
	assert.Equal(t, outFail, failed[0].Outcome)
	assert.Empty(t, before)
	assert.Len(t, g.got(), 1)
	review := prompts(t, g.core, verdict)
	require.Len(t, review, 1)
	assert.Contains(t, review[0], "Review round two")
	assert.Contains(t, review[0], "The gate's tests passed on this tree.")
}

func TestGatePassIsCachedByTreeAcrossARestartAndAFailIsNot(t *testing.T) {
	g := newTestGate(t, stepResult{})
	first, _ := g.start(t, "test-gate", "T1")
	require.NoError(t, g.e.StopGate(t.Context(), first))
	r := g.restarted(t, stepResult{ExitCode: 1}, stepResult{ExitCode: 1})

	cached, _ := r.start(t, "test-gate", "T1")
	failed, _ := r.start(t, "test-gate", "T2")
	require.NoError(t, r.e.StopGate(t.Context(), failed))
	r.start(t, "test-gate", "T2")

	note := stepMoves(r.a, cached, "tests")[0].Step.Note
	assert.True(t, strings.HasPrefix(note, "cached: passed on this tree at "), note)
	assert.Equal(t, outPass, r.got()[0].Outcome)
	assert.Equal(t, 2, r.p.ran())
}

func TestGateChecksDropWithTheGateAndWithAPinChange(t *testing.T) {
	g := newTestGate(t, stepResult{}, stepResult{})
	first, _ := g.start(t, "test-gate", "T1")
	run := g.a.runs[first]
	other := cloneRun(run)
	other.GatePin = "another pin"
	_, pinned := g.a.cachedPass(t.Context(), other, run.Spec.node("tests"))
	require.NoError(t, g.e.StopGate(t.Context(), first))

	require.NoError(t, g.e.DropGateChecks(t.Context(), "gt_1"))
	g.start(t, "test-gate", "T1")

	assert.False(t, pinned)
	assert.Equal(t, 2, g.p.ran())
}

func TestGateTestBypassesApprovalOnlyWhenRunIsExactlyThePlaceholder(t *testing.T) {
	g := newTestGate(t)
	g.write(t, "wrapped", `{"gate":true,"nodes":[{"id":"tests","start":true,"run":"cd . && {{gate.test}}","decide":["pass","fail"]}]}`)

	run, _ := g.start(t, "wrapped", "T1")

	assert.Zero(t, g.p.ran())
	assert.Equal(t, stepWaiting, stepMoves(g.a, run, "tests")[0].Step.Status)
	assert.Equal(t, "cd . && make test", g.a.pending[run+"/tests"].spec.Shell)
}

func TestOneGateTestRunsAtATimePerRepo(t *testing.T) {
	g := newTestGate(t, stepResult{}, stepResult{})
	g.a.spawn = func(f func()) { go f() }
	release := make(chan struct{})
	g.p.onRun = func(i int) {
		if i == 0 {
			<-release
		}
	}
	first, _ := g.start(t, "test-gate", "T1")
	testutil.Eventually(t, func() bool { return g.p.ran() == 1 }, 5*time.Second)
	second, _ := g.start(t, "test-gate", "T2")

	testutil.Eventually(t, func() bool {
		step, open := g.e.GateStepOpen(t.Context(), second)
		return open && step.Waiting
	}, 5*time.Second)
	ranWhileHeld := g.p.ran()
	close(release)

	testutil.Eventually(t, func() bool { return len(g.got()) == 2 }, 5*time.Second)
	assert.Equal(t, 1, ranWhileHeld)
	assert.ElementsMatch(t, []string{first, second}, []string{g.got()[0].Run, g.got()[1].Run})
}

func TestGateGraphsArePickedOnlyForAGate(t *testing.T) {
	g := newTestGate(t)

	_, err := g.a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "agents.graph-run", WorkspaceID: "ws1", Args: []byte(`{"graph":"test-gate"}`)})

	assert.EqualError(t, err, fmt.Sprintf(errGateOnly, "test-gate"))
	var listed, choices []string
	for _, f := range plainGraphs(listGraphs(nil, "")) {
		listed = append(listed, f.Name)
	}
	for _, c := range g.e.GateGraphChoices(t.Context(), g.repo) {
		choices = append(choices, c.Name)
	}
	assert.NotContains(t, listed, "test-gate")
	assert.Subset(t, choices, []string{"test-gate", "test-review"})
}

func TestGateGraphFieldsAreRefusedOutsideTheirPlace(t *testing.T) {
	tests := []struct {
		name  string
		graph string
		err   string
	}{
		{"decide on an agent", `{"gate":true,"nodes":[{"id":"r","prompt":"p","start":true,"decide":["pass"]}]}`, "decide is only for steps"},
		{"unknown decide word", `{"gate":true,"nodes":[{"id":"t","run":"true","start":true,"decide":["maybe"]}]}`, `decide "maybe"`},
		{"test outside a gate graph", `{"nodes":[{"id":"r","prompt":"p","start":true},{"id":"t","run":"{{gate.test}}"}],"edges":[{"from":"r","to":"t"}]}`, "{{gate.test}} is only for a gate graph"},
		{"start step in a plain graph", `{"nodes":[{"id":"t","run":"true","start":true}]}`, "a step can't be a start node"},
		{"end on a deciding outcome", `{"gate":true,"nodes":[{"id":"t","run":"true","start":true,"decide":["pass","fail"],"end":["fail"]}]}`, `end "fail": it is in decide`},
		{"decide outside a gate graph", `{"nodes":[{"id":"r","prompt":"p","start":true},{"id":"t","run":"true","decide":["fail"]}],"edges":[{"from":"r","to":"t"}]}`, "decide is only for a gate graph"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGraph("g", []byte(tt.graph))
			assert.ErrorContains(t, err, tt.err)
		})
	}
}
