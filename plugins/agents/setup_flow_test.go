package main_test

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	gitTitle      = "git is needed for worktrees and checkpoints"
	firstRunTitle = "Start your first agent"
)

func TestSetupFieldAndEmptyNodeAgree(t *testing.T) {
	f := startEnv(t, []string{"CLUBMATE_AGENTS_SETUP=git_missing"}, nil)
	workspace := f.workspace(t, newRepo(t))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return n.Props["title"] == gitTitle })
	var st struct {
		State string `json:"state"`
		Title string `json:"title"`
		Fix   struct {
			Label string `json:"label"`
			Run   string `json:"run"`
		} `json:"fix"`
	}
	require.NoError(t, f.run(t, "", "agents.setup", map[string]any{"check": true}, &st))
	assert.Equal(t, "git_missing", st.State)
	assert.Equal(t, gitTitle, st.Title)
	assert.Equal(t, "Install…", st.Fix.Label)
	assert.Equal(t, "xcode-select --install", st.Fix.Run)
	require.Error(t, f.runAs(t, "", "agt_x", "agents.setup-fix", map[string]any{}, nil))

	f.h.Event(t, workspace, "agents.main", "main/new", "new", nil)
	testutil.Eventually(t, func() bool {
		var list []map[string]any
		return f.run(t, workspace, "agents.sessions", map[string]any{}, &list) == nil && len(list) == 1
	}, restartWait)
}

func TestSetupFixExitAfterRestartRunsTheCheck(t *testing.T) {
	dir := t.TempDir()
	state, done := filepath.Join(dir, "state"), filepath.Join(dir, "done")
	require.NoError(t, os.WriteFile(state, []byte("git_missing"), 0o600))
	terminals := testutil.BuildPluginDir(t, "clubmate/plugins/terminals", "plugins/terminals/plugin.json")
	f := startEnv(t, []string{
		"CLUBMATE_AGENTS_SETUP=@" + state,
		"CLUBMATE_AGENTS_SETUP_FIX=while [ ! -e '" + done + "' ]; do sleep 0.1; done",
	}, nil, terminals)
	workspace := f.workspace(t, newRepo(t))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return n.Props["title"] == gitTitle })

	f.h.Event(t, workspace, "agents.main", "main/setup-fix", "setupFix", nil)
	testutil.Eventually(t, func() bool {
		list, err := f.h.Svc.ListSessions(t.Context(), session.ListFilter{WorkspaceID: workspace})
		return err == nil && slices.ContainsFunc(list, func(s session.Session) bool {
			return s.Title == "Install git" && s.State == session.StateRunning
		})
	}, restartWait)

	agents := func() int {
		i := slices.IndexFunc(f.h.Sup.Statuses(), func(s plugin.Status) bool { return s.ID == "agents" })
		if i < 0 {
			return 0
		}
		return f.h.Sup.Statuses()[i].PID
	}
	old := agents()
	require.NoError(t, syscall.Kill(old, syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		pid := agents()
		return pid != 0 && pid != old && f.run(t, workspace, "agents.setup", map[string]any{}, nil) == nil
	}, restartWait)
	f.subscribe(t, workspace)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return n.Props["title"] == gitTitle })

	require.NoError(t, os.WriteFile(state, []byte("ok"), 0o600))
	require.NoError(t, os.WriteFile(done, nil, 0o600))
	testutil.Eventually(t, func() bool {
		require.NoError(t, f.h.Svc.CheckTransitions(t.Context()))
		list, err := f.h.Svc.ListSessions(t.Context(), session.ListFilter{WorkspaceID: workspace})
		return err == nil && !slices.ContainsFunc(list, func(s session.Session) bool { return s.State == session.StateRunning })
	}, restartWait)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return n.Props["title"] == firstRunTitle })
}
