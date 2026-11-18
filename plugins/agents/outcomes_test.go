package main_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type outcomeRow struct {
	Session       string  `json:"session"`
	Parent        string  `json:"parent"`
	Kind          string  `json:"kind"`
	Size          string  `json:"size"`
	Arm           string  `json:"arm"`
	Model         string  `json:"model"`
	Turns         int     `json:"turns"`
	Cost          float64 `json:"cost"`
	ReviewersCost float64 `json:"reviewersCost"`
	CostToLanding float64 `json:"costToLanding"`
	Landing       *struct {
		Lander    string   `json:"lander"`
		Builder   string   `json:"builder"`
		Fixers    []string `json:"fixers"`
		Squash    string   `json:"squash"`
		Into      string   `json:"into"`
		Rounds    int      `json:"rounds"`
		Reviewers []string `json:"reviewers"`
	} `json:"landing"`
}

func usageCost(match string, amount float64) map[string]any {
	return map[string]any{"match": match, "update": map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100, "cost": map[string]any{"amount": amount, "currency": "USD"}}}
}

func (f *fixture) outcome(t *testing.T, workspace, session string, ready func(outcomeRow) bool) outcomeRow {
	t.Helper()
	var got outcomeRow
	testutil.Eventually(t, func() bool {
		var out struct {
			Outcomes []outcomeRow `json:"outcomes"`
		}
		if f.run(t, workspace, "agents.outcomes", map[string]any{}, &out) != nil {
			return false
		}
		i := slices.IndexFunc(out.Outcomes, func(r outcomeRow) bool { return r.Session == session })
		if i < 0 || !ready(out.Outcomes[i]) {
			return false
		}
		got = out.Outcomes[i]
		return true
	}, wait)
	return got
}

func TestOutcomeBindsLandingToChildBuilderWithReviewCost(t *testing.T) {
	t.Parallel()
	release := filepath.Join(t.TempDir(), "release")
	f := start(t, map[string]any{
		"configs": []map[string]any{
			{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "opus", "options": []map[string]string{{"value": "opus", "name": "Opus"}, {"value": "fable", "name": "Fable"}}},
		},
		"prompt": []map[string]any{
			usageCost("[exp", 2),
			usageCost("Clubmate land review", 0.5),
			{"match": "Clubmate land review", "wait": release},
		},
	}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo}, nil))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "orchestrate")
	f.waitStatus(t, workspace, parent, "done")
	kid := f.newChild(t, workspace, parent, map[string]any{"branch": "c1", "prompt": "[exp kind=feature size=M arm=opus/high]\nbuild it"})
	f.waitStatus(t, workspace, kid.Session, "done")
	helper := f.newChild(t, workspace, kid.Session, map[string]any{"prompt": "help"})
	require.Equal(t, kid.Worktree, helper.Worktree)
	f.waitStatus(t, workspace, helper.Session, "done")
	write(t, filepath.Join(kid.Worktree, "c.go"), "c\n")
	git(t, kid.Worktree, "add", ".")
	git(t, kid.Worktree, "commit", "-qm", "c")
	land := func() error {
		return f.runAs(t, workspace, parent, "git.land", map[string]any{"path": kid.Worktree, "message": "feat: c"}, nil)
	}
	require.ErrorContains(t, land(), "review pending")
	g := gateSetup{f: f, workspace: workspace, lander: parent}
	reviewer := g.reviewer(t).ID
	g.verdict(t, "approved")
	require.NoError(t, land())
	squash := git(t, repo, "rev-parse", "main")
	f.waitStatus(t, workspace, kid.Session, "closed")
	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": kid.Session}, nil))

	row := f.outcome(t, workspace, kid.Session, func(r outcomeRow) bool {
		return r.Landing != nil && r.ReviewersCost > 0 && f.session(t, workspace, kid.Session).ID == ""
	})

	assert.Equal(t, parent, row.Parent)
	assert.Equal(t, []string{"feature", "M", "opus/high", "opus"}, []string{row.Kind, row.Size, row.Arm, row.Model})
	assert.InDelta(t, 2, row.Cost, 1e-9)
	assert.InDelta(t, 4.5, row.CostToLanding, 1e-9)
	assert.Equal(t, parent, row.Landing.Lander)
	assert.Equal(t, kid.Session, row.Landing.Builder)
	assert.Equal(t, []string{helper.Session}, row.Landing.Fixers)
	assert.Equal(t, squash, row.Landing.Squash)
	assert.Equal(t, "main", row.Landing.Into)
	assert.Equal(t, 1, row.Landing.Rounds)
	assert.Contains(t, row.Landing.Reviewers, reviewer)
	var blamed struct {
		Attributions []struct {
			Builder string `json:"builder"`
		} `json:"attributions"`
	}
	require.NoError(t, f.run(t, workspace, "agents.blame", map[string]any{"commit": squash, "cwd": repo}, &blamed))
	require.Len(t, blamed.Attributions, 1)
	assert.Equal(t, kid.Session, blamed.Attributions[0].Builder)
}

func TestOutcomeKeepsCostAfterSessionIsDeleted(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{usageCost("[exp", 1), usageCost("[exp", 1.25)}})
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "orchestrate")
	f.waitStatus(t, workspace, parent, "done")
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "[exp kind=fix size=S arm=sonnet/low]\ngo"}).Session
	f.waitStatus(t, workspace, kid, "done")

	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": kid}, nil))

	row := f.outcome(t, workspace, kid, func(outcomeRow) bool { return f.session(t, workspace, kid).ID == "" })
	assert.Equal(t, "fix", row.Kind)
	assert.InDelta(t, 1.25, row.Cost, 1e-9)
}

func TestOutcomeIndexLandingIsBuiltByTheLander(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	lander := f.newSession(t, workspace)
	f.newSession(t, workspace)
	write(t, filepath.Join(repo, "x.go"), "x\n")
	git(t, repo, "add", ".")

	require.NoError(t, f.runAs(t, workspace, lander, "git.land", map[string]any{"path": repo, "mode": "index", "message": "feat: x"}, nil))

	row := f.outcome(t, workspace, lander, func(r outcomeRow) bool { return r.Landing != nil })
	assert.Equal(t, lander, row.Landing.Builder)
	assert.Empty(t, row.Landing.Fixers)
}
