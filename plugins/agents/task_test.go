package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
)

type taskRecord struct {
	Slug  string     `json:"slug"`
	Repos []taskRepo `json:"repos"`
}

type taskRepo struct {
	Path    string `json:"path"`
	Order   int    `json:"order"`
	LandsBy string `json:"landsBy"`
	Source  string `json:"source"`
}

func (f *fixture) declareTask(t *testing.T, workspace, caller string, slug string, repos ...map[string]any) error {
	t.Helper()
	return f.runFrom(t, workspace, caller, "agents.task", map[string]any{"slug": slug, "repos": repos}, nil)
}

func TestTaskKeepsOrderAcrossRestart(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, second := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)

	require.NoError(t, f.declareTask(t, workspace, session, "campaign-pause",
		map[string]any{"path": second, "order": 2, "landsBy": "merge", "source": "the plan"},
		map[string]any{"path": first, "order": 1, "landsBy": "pr", "source": "its CLAUDE.md"},
	))
	f.restartAgents(t, workspace)

	got := f.session(t, workspace, session).Task
	require.NotNil(t, got)
	assert.Equal(t, "campaign-pause", got.Slug)
	assert.Equal(t, []taskRepo{
		{Path: first, Order: 1, LandsBy: "pr", Source: "its CLAUDE.md"},
		{Path: second, Order: 2, LandsBy: "merge", Source: "the plan"},
	}, got.Repos)
}

func TestTaskRefusesWhatDoesNotFit(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.doneSession(t, workspace)
	tests := []struct {
		name  string
		slug  string
		repos []map[string]any
	}{
		{"unknown landing rule", "x", []map[string]any{{"path": repo, "order": 1, "landsBy": "force-push", "source": "s"}}},
		{"relative path", "x", []map[string]any{{"path": "ads", "order": 1, "landsBy": "pr", "source": "s"}}},
		{"bad slug", "a..b", []map[string]any{{"path": repo, "order": 1, "landsBy": "pr", "source": "s"}}},
		{"no repos", "x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, f.declareTask(t, workspace, session, tt.slug, tt.repos...))

			assert.Nil(t, f.session(t, workspace, session).Task)
		})
	}
}

func TestRepoChildCorrectsOnlyItsOwnRule(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, second := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, second, contract.FolderMeta{})
	require.NoError(t, err)
	parent := f.doneSession(t, workspace)
	require.NoError(t, f.declareTask(t, workspace, parent, "pause",
		map[string]any{"path": first, "order": 1, "landsBy": "merge", "source": "the plan"},
		map[string]any{"path": second, "order": 2, "landsBy": "merge", "source": "the plan"},
	))
	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause"}, &kid))

	require.NoError(t, f.declareTask(t, workspace, kid.Session, "pause", map[string]any{"path": second, "landsBy": "pr", "source": "its CLAUDE.md"}))
	require.Error(t, f.declareTask(t, workspace, kid.Session, "pause", map[string]any{"path": first, "landsBy": "pr", "source": "its CLAUDE.md"}))
	plain := f.newChild(t, workspace, parent, map[string]any{}).Session
	require.Error(t, f.declareTask(t, workspace, plain, "pause", map[string]any{"path": second, "landsBy": "branch", "source": "a guess"}))

	got := f.session(t, workspace, parent).Task
	require.NotNil(t, got)
	assert.Equal(t, []taskRepo{
		{Path: first, Order: 1, LandsBy: "merge", Source: "the plan"},
		{Path: second, Order: 2, LandsBy: "pr", Source: "its CLAUDE.md"},
	}, got.Repos)
}
