package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type gateSetup struct {
	f         *fixture
	repo      string
	workspace string
	lander    string
	worktree  string
}

func startGate(t *testing.T) gateSetup {
	t.Helper()
	never := filepath.Join(t.TempDir(), "never")
	f := start(t, map[string]any{"prompt": []map[string]any{{"match": "Clubmate land review", "wait": never}}}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo}, nil))
	g := gateSetup{f: f, repo: repo, workspace: workspace, lander: f.newSession(t, workspace), worktree: filepath.Join(repo, ".worktrees", "feat")}
	git(t, repo, "worktree", "add", "-q", "-b", "feat", g.worktree)
	g.commit(t, "b.go", "two\n")
	return g
}

func (g gateSetup) commit(t *testing.T, file, content string) {
	t.Helper()
	write(t, filepath.Join(g.worktree, file), content)
	git(t, g.worktree, "add", ".")
	git(t, g.worktree, "commit", "-qm", "work on "+file)
}

func (g gateSetup) land(t *testing.T) error {
	t.Helper()
	return g.f.runAs(t, g.workspace, g.lander, "git.land", map[string]any{"path": g.worktree, "message": "feat: b"}, nil)
}

func (g gateSetup) reviewer(t *testing.T) record {
	t.Helper()
	var list []record
	require.NoError(t, g.f.run(t, g.workspace, "agents.sessions", map[string]any{}, &list))
	i := slices.IndexFunc(list, func(r record) bool { return r.GateFor != nil && r.GateFor.Lander == g.lander })
	require.GreaterOrEqual(t, i, 0, "no land reviewer")
	return list[i]
}

func (g gateSetup) verdict(t *testing.T, outcome string, findings ...string) {
	t.Helper()
	r := g.reviewer(t)
	require.NoError(t, g.f.runAs(t, g.workspace, r.ID, "agents.land-verdict", map[string]any{"tree": r.GateFor.Tree, "outcome": outcome, "findings": findings}, nil))
}

func TestLandWithoutVerdictStartsReviewAndRefuses(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	main := git(t, g.repo, "rev-parse", "main")

	err := g.land(t)

	require.ErrorContains(t, err, "review pending")
	r := g.reviewer(t)
	assert.Equal(t, "fable", r.StartModel)
	assert.Contains(t, r.DisallowedTools, "Edit")
	assert.Equal(t, main, git(t, g.repo, "rev-parse", "main"))
}

func TestApprovedVerdictLands(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.Error(t, g.land(t))

	g.verdict(t, "approved")
	require.NoError(t, g.land(t))

	assert.Equal(t, "feat: b", git(t, g.repo, "log", "-1", "--format=%s", "main"))
	assert.Equal(t, "two", git(t, g.repo, "show", "main:b.go"))
	_, err := os.Stat(g.worktree)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, git(t, g.repo, "branch", "--list", "feat"))
}

func TestMovedTipVoidsVerdict(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.Error(t, g.land(t))
	g.verdict(t, "approved")

	write(t, filepath.Join(g.repo, "c.go"), "three\n")
	git(t, g.repo, "add", "c.go")
	git(t, g.repo, "commit", "-qm", "user moved main")

	require.ErrorContains(t, g.land(t), "review pending")
}

func TestChangesRoundsCapWaitsForUser(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	for i, file := range []string{"d.go", "e.go"} {
		require.ErrorContains(t, g.land(t), "review pending", "round %d", i+1)
		g.verdict(t, "changes", "b.go:1 — breaks")
		g.commit(t, file, "fix\n")
	}

	require.ErrorContains(t, g.land(t), "waits for the user")
	lander := g.f.session(t, g.workspace, g.lander)
	require.True(t, lander.NeedsYou)
	require.Len(t, lander.Pending, 1)

	require.NoError(t, g.f.run(t, g.workspace, "agents.land-answer", map[string]any{"request": lander.Pending[0], "action": "merge"}, nil))
	require.NoError(t, g.land(t))
	assert.False(t, g.f.session(t, g.workspace, g.lander).NeedsYou)
}

func TestOnlyGatedSessionsGetHookEnv(t *testing.T) {
	t.Parallel()
	gated, open := newRepo(t), newRepo(t)
	f := start(t, map[string]any{"prompt": []map[string]any{{"run": "git config --get core.hooksPath || echo none"}}}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	workspace := f.workspace(t, gated)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": gated}, nil))
	other := f.workspace(t, open)

	env := func(workspace string) map[string]string {
		var res struct {
			Session string `json:"session"`
		}
		require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"prompt": "go"}, &res))
		var meta struct {
			Meta struct {
				ClaudeCode struct {
					Options struct {
						Env map[string]string `json:"env"`
					} `json:"options"`
				} `json:"claudeCode"`
			} `json:"_meta"`
		}
		require.NoError(t, json.Unmarshal(f.waitCall(t, "session/new", func(c fakeCall) bool { return strings.Contains(string(c.Params), res.Session) }).Params, &meta))
		return meta.Meta.ClaudeCode.Options.Env
	}

	got := env(workspace)
	assert.Equal(t, "refs/heads/main", got["CLUBMATE_LAND_PROTECT"])
	assert.Equal(t, filepath.Join(gated, ".git"), got["CLUBMATE_LAND_COMMON"])
	assert.Equal(t, "core.hooksPath", got["GIT_CONFIG_KEY_"+got["CLUBMATE_GIT_CONFIG_COUNT"]])
	assert.True(t, strings.HasSuffix(got["GIT_CONFIG_VALUE_"+got["CLUBMATE_GIT_CONFIG_COUNT"]], "githooks"))
	assert.NotContains(t, env(other), "GIT_CONFIG_COUNT")
	testutil.Eventually(t, func() bool {
		var seen []string
		for _, c := range f.calls(t, "run") {
			var out struct {
				Output string `json:"output"`
			}
			require.NoError(t, json.Unmarshal(c.Params, &out))
			seen = append(seen, strings.TrimSpace(out.Output))
		}
		slices.Sort(seen)
		return len(seen) == 2 && strings.HasSuffix(seen[0], "githooks") && seen[1] == "none"
	}, wait)
}

func TestClosedReviewerWakesLander(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.Error(t, g.land(t))

	require.NoError(t, g.f.run(t, g.workspace, "agents.close", map[string]any{"session": g.reviewer(t).ID}, nil))

	g.f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "ended without a verdict") })
}

func TestFinalLandingReviewsWholeChangeAndNamesLandedCommits(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	own := git(t, g.worktree, "rev-parse", "--short=10", "HEAD")
	kid := filepath.Join(g.repo, ".worktrees", "kid")
	git(t, g.worktree, "worktree", "add", "-q", "-b", "kid", kid)
	write(t, filepath.Join(kid, "k.go"), "kid\n")
	git(t, kid, "add", ".")
	git(t, kid, "commit", "-qm", "kid work")
	landKid := func() error {
		return g.f.runAs(t, g.workspace, g.lander, "git.land", map[string]any{"path": kid, "into": "feat", "message": "feat: kid"}, nil)
	}
	require.ErrorContains(t, landKid(), "review pending")
	g.verdict(t, "approved")
	require.NoError(t, landKid())
	landed := git(t, g.worktree, "rev-parse", "--short=10", "feat")
	main, tree := git(t, g.repo, "rev-parse", "main"), git(t, g.repo, "rev-parse", "feat^{tree}")

	require.ErrorContains(t, g.land(t), "review pending")

	g.f.waitCall(t, "session/prompt", func(c fakeCall) bool {
		p := string(c.Params)
		return strings.Contains(p, "git diff "+main+" "+tree) && strings.Contains(p, "each for its own diff: "+landed+" (reviewed).") && strings.Contains(p, "other commits: "+own+".")
	})
}

func TestVerdictBindsToTheReviewedTree(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.ErrorContains(t, g.land(t), "review pending")
	reviewed := g.reviewer(t).GateFor.Tree
	g.commit(t, "c.go", "unreviewed\n")

	require.ErrorContains(t, g.land(t), "still running")
	g.verdict(t, "approved")

	require.ErrorContains(t, g.land(t), "review pending")
	assert.NotEqual(t, reviewed, g.reviewer(t).GateFor.Tree)
	assert.Empty(t, git(t, g.repo, "ls-tree", "main", "c.go"))
}
