package main_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func startTestGate(t *testing.T, graph, test string, rounds int) gateSetup {
	t.Helper()
	f := startEnv(t, nil, map[string]any{}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	var added gateListed
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "graph": graph, "test": test, "rounds": rounds}, &added))
	g := gateSetup{f: f, repo: repo, workspace: workspace, lander: f.newSession(t, workspace), worktree: filepath.Join(repo, ".worktrees", "feat"), gate: added.ID}
	git(t, repo, "worktree", "add", "-q", "-b", "feat", g.worktree)
	g.commit(t, "b.go", "two\n")
	return g
}

func (g gateSetup) waitLanderTold(t *testing.T, text string) {
	t.Helper()
	testutil.Eventually(t, func() bool { return g.f.transcriptHas(t, g.workspace, g.lander, text) }, wait)
}

func (g gateSetup) verdicts(t *testing.T) []gateVerdictRow {
	t.Helper()
	var h gateHistory
	require.NoError(t, g.f.run(t, g.workspace, "agents.gate-verdicts", map[string]any{"id": g.gate}, &h))
	return h.Verdicts
}

func TestTestGateApprovesTheLandingTreeWithoutAReviewer(t *testing.T) {
	t.Parallel()
	release := filepath.Join(t.TempDir(), "release")
	g := startTestGate(t, "test-gate", "until [ -f "+release+" ]; do sleep 0.05; done; test -f b.go", 0)

	first := g.land(t)
	again := g.land(t)
	testutil.Eventually(t, func() bool {
		l := g.f.session(t, g.workspace, g.lander).Landing
		return l != nil && l.Verdict == "reviewing" && g.f.gates(t, g.workspace)[0].State == "reviewing"
	}, wait)
	var h gateHistory
	require.NoError(t, g.f.run(t, g.workspace, "agents.gate-verdicts", map[string]any{"id": g.gate}, &h))
	require.Len(t, h.Landings, 1)
	assert.WithinDuration(t, time.Now(), h.Landings[0].Since, time.Minute)
	write(t, release, "")

	require.ErrorContains(t, first, "review pending: the gate runs tests (until [")
	require.ErrorContains(t, again, "review pending: the gate runs tests (until [")
	g.waitLanderTold(t, "approved by test-gate/tests")
	require.NoError(t, g.land(t))
	assert.Empty(t, g.nodes(t))
	v := g.verdicts(t)
	require.NotEmpty(t, v)
	assert.Equal(t, []string{"approved", "check", "test-gate/tests"}, []string{v[0].Outcome, v[0].By, v[0].Reviewer})
}

func TestTestGateFailAsksForChangesAndTheNextRoundTestsTheNewTree(t *testing.T) {
	t.Parallel()
	g := startTestGate(t, "test-gate", "if [ -f bad.go ]; then echo boom; exit 1; fi", 2)
	g.commit(t, "bad.go", "x\n")

	require.ErrorContains(t, g.land(t), "review pending")
	g.waitLanderTold(t, "tests: if [ -f bad.go ]; then echo boom; exit 1; fi failed (exit 1)")
	require.ErrorContains(t, g.land(t), "asked for changes")
	git(t, g.worktree, "rm", "-q", "bad.go")
	git(t, g.worktree, "commit", "-qm", "fix")
	require.ErrorContains(t, g.land(t), "review pending")

	g.waitLanderTold(t, "approved by test-gate/tests")
	assert.True(t, slices.ContainsFunc(g.verdicts(t), func(v gateVerdictRow) bool {
		return v.By == "check" && len(v.Findings) == 1 && strings.Contains(v.Findings[0], "boom")
	}))
	require.NoError(t, g.land(t))
}

func TestTestGateOpensTheCardAfterItsRoundsOfFails(t *testing.T) {
	t.Parallel()
	g := startTestGate(t, "test-gate", "exit 1", 1)

	require.ErrorContains(t, g.land(t), "review pending")
	g.waitLanderTold(t, "failed (exit 1)")

	require.ErrorContains(t, g.land(t), "waits for the user")
	assert.True(t, g.f.session(t, g.workspace, g.lander).NeedsYou)
}

func TestTestGatePassOnTheSameTreeIsCachedAcrossARestart(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "marker")
	g := startTestGate(t, "test-gate", "echo ran >> "+marker, 0)
	require.ErrorContains(t, g.land(t), "review pending")
	g.waitLanderTold(t, "approved by test-gate/tests")
	write(t, filepath.Join(g.repo, "c.go"), "three\n")
	git(t, g.repo, "add", "c.go")
	git(t, g.repo, "commit", "-qm", "c")
	git(t, g.repo, "revert", "--no-edit", "HEAD")
	g.f.restartAgents(t, g.workspace)

	require.ErrorContains(t, g.land(t), "review pending: the gate")

	testutil.Eventually(t, func() bool { return len(g.verdicts(t)) == 2 }, wait)
	assert.Equal(t, []string{"approved", "approved"}, []string{g.verdicts(t)[0].Outcome, g.verdicts(t)[1].Outcome})
	ran, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(ran), "ran"))
}

func TestGatesSheetKeepsAndSavesTheTestCommand(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	refused := f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "graph": "test-gate"}, nil)

	require.NoError(t, f.run(t, workspace, "agents.gates.new", map[string]any{}, nil))
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/graph", "gatePick", "test-gate")
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		return sheetFieldError(n, "gateSheet/test") == "required: this graph runs it"
	})
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/test", "gateTest", "make test")
	f.h.Event(t, workspace, "agents.sheet", "gateSheet/save", "gateSave", nil)

	assert.ErrorContains(t, refused, "this graph runs the gate's test command: set Test command")
	gates := f.gates(t, workspace)
	require.Len(t, gates, 1)
	assert.Equal(t, []string{"test-gate", "make test"}, []string{gates[0].Graph, gates[0].Test})
}

func sheetFieldError(n sdui.Node, id string) string {
	if n.ID == id {
		if e, ok := n.Props["error"].(string); ok {
			return e
		}
	}
	for _, c := range n.Children {
		if e := sheetFieldError(c, id); e != "" {
			return e
		}
	}
	return ""
}

func TestDoneAndToolCallGatesRefuseAGateGraph(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	var done gateListed
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "turn"}, &done))

	added := f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo, "point": "tool", "tool": "Bash", "how": "command", "text": "git push", "graph": "test-gate"}, nil)
	updated := f.run(t, workspace, "agents.gate-update", map[string]any{"id": done.ID, "graph": "test-review"}, nil)

	assert.ErrorContains(t, added, "test-gate is a gate graph: only a land gate runs it")
	assert.ErrorContains(t, updated, "test-review is a gate graph: only a land gate runs it")
}
