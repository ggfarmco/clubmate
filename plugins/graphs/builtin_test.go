package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinGraphsWiring(t *testing.T) {
	tests := []struct {
		graph string
		dirs  []string
		steps []string
		edges []string
	}{
		{
			graph: "review-gate",
			steps: []string{
				"coder agent branch=graph/{{run}} end=[]",
				"tests run=make test in=coder timeout=20m0s end=[]",
				"reviewer agent branch= end=[]",
				"squash run=scripts/graph/squash.sh in=main timeout=10m0s end=[done]",
			},
			edges: []string{
				"coder done: -> tests ×0",
				"tests outcome:fail -> coder ×3",
				"tests outcome:pass -> reviewer ×0",
				"reviewer outcome:approved -> squash ×0",
				"reviewer outcome:changes -> coder ×0",
			},
		},
		{
			graph: "nightly",
			dirs:  []string{"../.."},
			steps: []string{
				"tidy agent branch= end=[nothing]",
				"tests run=make test in=main timeout=1h0m0s end=[]",
				"phone run=scripts/graph/companion-ui.sh in=main timeout=1h0m0s end=[]",
				"mac run=scripts/graph/mac-ui.sh in=main timeout=1h0m0s end=[]",
				"push run=git push origin main in=main timeout=10m0s end=[]",
				"ship run=scripts/graph/ship-next.sh in=main timeout=2h0m0s end=[done]",
			},
			edges: []string{
				"tidy outcome:changed -> tests ×0",
				"tests done: -> phone ×0",
				"phone done: -> mac ×0",
				"mac done: -> push ×0",
				"push done: -> ship ×0",
			},
		},
		{
			graph: "design-check",
			dirs:  []string{"../.."},
			steps: []string{
				"scope agent branch= end=[]",
				"capture run=scripts/design/capture.sh in=main timeout=2h0m0s end=[]",
				"judge agent branch= end=[]",
				"fixer agent branch= end=[]",
				"tests run=scripts/design/tests.sh in=main timeout=1h30m0s end=[]",
				"revert run=scripts/design/revert.sh in=main timeout=10m0s end=[]",
				"report agent branch= end=[]",
				"cleanup run=scripts/design/cleanup.sh in=main timeout=10m0s end=[done]",
			},
			edges: []string{
				"scope done: -> capture ×0",
				"scope error: -> cleanup ×0",
				"capture outcome:pass -> judge ×0",
				"capture outcome:nothing -> report ×0",
				"capture outcome:fail -> report ×0",
				"capture error: -> report ×0",
				"judge outcome:fixes -> fixer ×0",
				"judge outcome:nothing -> report ×0",
				"judge error: -> report ×0",
				"fixer outcome:landed -> tests ×0",
				"fixer outcome:nothing -> report ×0",
				"fixer error: -> report ×0",
				"tests outcome:pass -> report ×0",
				"tests outcome:fail -> revert ×0",
				"tests error: -> revert ×0",
				"revert done: -> report ×0",
				"revert error: -> report ×0",
				"report done: -> cleanup ×0",
				"report error: -> cleanup ×0",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.graph, func(t *testing.T) {
			g, from, err := loadGraph(tt.dirs, "", tt.graph)
			require.NoError(t, err)
			require.Equal(t, tt.dirs == nil, from == fromBuiltin)
			var steps, edges []string
			for _, n := range g.Nodes {
				if n.step() {
					steps = append(steps, fmt.Sprintf("%s run=%s in=%s timeout=%s end=%v", n.ID, n.Run, n.In, n.timeout(), n.End))
				} else {
					steps = append(steps, fmt.Sprintf("%s agent branch=%s end=%v", n.ID, n.Branch, n.End))
				}
			}
			for _, e := range g.Edges {
				edges = append(edges, fmt.Sprintf("%s %s:%s -> %s ×%d", e.From, e.On, e.Outcome, e.To, e.MaxFires))
			}
			assert.Equal(t, tt.steps, steps)
			assert.Equal(t, tt.edges, edges)
		})
	}
}

func TestSquashScript(t *testing.T) {
	script, err := filepath.Abs("../../scripts/graph/squash.sh")
	require.NoError(t, err)
	git := func(dir string, args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	commit := func(dir, file, msg string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(msg), 0o644))
		git(dir, "add", file)
		git(dir, "commit", "-q", "-m", msg)
	}
	repo := func(t *testing.T) string {
		dir := t.TempDir()
		git(dir, "init", "-q", "-b", "main")
		git(dir, "config", "user.email", "t@example.com")
		git(dir, "config", "user.name", "t")
		commit(dir, "a", "chore: init")
		git(dir, "branch", "graph/gr_1")
		return dir
	}
	squash := func(dir string) (string, error) {
		cmd := exec.Command(script)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CLUBMATE_GRAPH_BRANCH_CODER=graph/gr_1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("squashes into one commit", func(t *testing.T) {
		dir := repo(t)
		git(dir, "checkout", "-q", "graph/gr_1")
		commit(dir, "b", "feat(x): add b")
		commit(dir, "c", "test(x): cover b")
		git(dir, "checkout", "-q", "main")

		out, err := squash(dir)
		require.NoError(t, err, out)
		assert.Equal(t, "2", git(dir, "rev-list", "--count", "HEAD"))
		assert.Equal(t, "feat(x): add b\n\n- test(x): cover b", git(dir, "log", "-1", "--format=%B"))
		assert.Equal(t, "a\nb\nc", git(dir, "ls-tree", "--name-only", "HEAD"))
	})

	t.Run("refuses", func(t *testing.T) {
		dir := repo(t)
		out, err := squash(dir)
		require.Error(t, err)
		assert.Contains(t, out, "no commits that main lacks")

		git(dir, "checkout", "-q", "graph/gr_1")
		commit(dir, "b", "feat(x): add b")
		git(dir, "checkout", "-q", "main")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte("dirty"), 0o644))
		out, err = squash(dir)
		require.Error(t, err)
		assert.Contains(t, out, "uncommitted changes")
		assert.Equal(t, "1", git(dir, "rev-list", "--count", "HEAD"))
	})
}
