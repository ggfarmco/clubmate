package session_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/paths"
	"clubmate/internal/proc"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
	"clubmate/internal/testutil"
	"clubmate/pkg/id"
)

func spawn(t *testing.T, m *session.Manager, command ...string) session.Session {
	t.Helper()
	s, err := m.Create(t.Context(), session.CreateRequest{ProjectID: "prj_x", Command: command})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = unix.Kill(-s.PGID, unix.SIGKILL)
		_ = unix.Kill(s.HostPID, unix.SIGKILL)
	})
	return s
}

func TestManagerSessionsSurviveManagerRestart(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	s := spawn(t, session.NewManager(dirs, bin), "sh", "-c", "exec cat")
	list, err := session.NewManager(dirs, bin).List(t.Context(), session.ListFilter{ProjectID: "prj_x"})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, s.ID, list[0].ID)
	assert.Equal(t, session.StateRunning, list[0].State)
}

func TestManagerHostDoesNotInheritMarkers(t *testing.T) {
	t.Setenv("CLUBMATE_SESSION_ID", "ses_outer")
	t.Setenv("CLUBMATE_PROJECT_ID", "prj_outer")
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	s := spawn(t, session.NewManager(dirs, bin), "sh", "-c", "exec cat")
	env, err := proc.ReadEnv(s.HostPID)
	require.NoError(t, err)
	assert.False(t, slices.ContainsFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, "CLUBMATE_SESSION_ID=") || strings.HasPrefix(kv, "CLUBMATE_PROJECT_ID=")
	}), env)
}

func TestManagerSessionEnvDropsDefaultDirs(t *testing.T) {
	defaults, err := paths.Defaults()
	require.NoError(t, err)
	tests := map[string]struct {
		state, runtime string
		kept           bool
	}{
		"flavor defaults": {state: defaults.State, runtime: defaults.Runtime},
		"overrides":       {state: "/x/state", runtime: "/tmp/cm-rt", kept: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLUBMATE_STATE_DIR", tt.state)
			t.Setenv("CLUBMATE_RUNTIME_DIR", tt.runtime)
			dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
			out := filepath.Join(t.TempDir(), "env")
			s := spawn(t, session.NewManager(dirs, bin), "sh", "-c", `env > "$0.tmp" && mv "$0.tmp" "$0"; exec cat`, out)
			var env []string
			testutil.Eventually(t, func() bool {
				raw, err := os.ReadFile(out)
				env = strings.Split(string(raw), "\n")
				return err == nil
			}, 3*time.Second)
			require.Contains(t, env, "CLUBMATE_SESSION_ID="+s.ID)
			assert.Equal(t, tt.kept, slices.Contains(env, "CLUBMATE_STATE_DIR="+tt.state), env)
			assert.Equal(t, tt.kept, slices.Contains(env, "CLUBMATE_RUNTIME_DIR="+tt.runtime), env)
		})
	}
}

func TestManagerExitedAndLost(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	exited := spawn(t, m, "sh", "-c", "sleep 0.2; exit 3")
	lost := spawn(t, m, "sh", "-c", "exec cat")
	require.NoError(t, unix.Kill(lost.HostPID, unix.SIGKILL))
	testutil.Eventually(t, func() bool {
		e, _ := m.Get(t.Context(), exited.ID)
		l, _ := m.Get(t.Context(), lost.ID)
		return e.State == session.StateExited && l.State == session.StateLost
	}, 3*time.Second)
	e, _ := m.Get(t.Context(), exited.ID)
	assert.Equal(t, 3, *e.ExitCode)
}

func TestManagerListFiltersByProject(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	spawn(t, m, "sh", "-c", "exec cat")
	require.NoError(t, os.WriteFile(dirs.SessionMeta("ses_bad"), []byte("{"), 0o600))
	all, err := m.List(t.Context(), session.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
	other, err := m.List(t.Context(), session.ListFilter{ProjectID: "prj_other"})
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestManagerUnsignalableHostIsLost(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	sid := id.New("ses")
	require.NoError(t, sessionhost.WriteMeta(dirs.SessionMeta(sid), sessionhost.Meta{ID: sid, HostPID: 1}))
	s, err := session.NewManager(dirs, "unused").Get(t.Context(), sid)
	require.NoError(t, err)
	assert.Equal(t, session.StateLost, s.State)
}

func TestSessionLostWhenHostPIDReused(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	sid := id.New("ses")
	start, err := proc.StartTime(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, sessionhost.WriteMeta(dirs.SessionMeta(sid), sessionhost.Meta{ID: sid, HostPID: os.Getpid(), HostStart: start.Add(-time.Hour)}))
	s, err := session.NewManager(dirs, "unused").Get(t.Context(), sid)
	require.NoError(t, err)
	assert.Equal(t, session.StateLost, s.State)
}

func TestManagerGetUnknown(t *testing.T) {
	m := session.NewManager(testutil.ShortDirs(t), "unused")
	_, err := m.Get(t.Context(), id.New("ses"))
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestManagerRejectsMalformedID(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	require.NoError(t, os.WriteFile(filepath.Join(dirs.State, "x.json"), []byte(`{"hostPid":1}`), 0o600))
	_, err := session.NewManager(dirs, "unused").Get(t.Context(), "../x")
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestManagerRemove(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	s := spawn(t, m, "sh", "-c", "exec cat")
	assert.ErrorIs(t, m.Remove(t.Context(), s.ID), session.ErrRunning)
	require.NoError(t, unix.Kill(-s.PGID, unix.SIGKILL))
	testutil.Eventually(t, func() bool {
		got, _ := m.Get(t.Context(), s.ID)
		return got.State == session.StateExited
	}, 3*time.Second)
	require.NoError(t, m.Remove(t.Context(), s.ID))
	_, err := m.Get(t.Context(), s.ID)
	assert.ErrorIs(t, err, session.ErrNotFound)
	assert.NoFileExists(t, dirs.SessionLog(s.ID))
}

func TestManagerCreateFailsWhenHostDies(t *testing.T) {
	metaOnly := filepath.Join(t.TempDir(), "host")
	script := `#!/bin/sh
printf '{"hostPid":1}' > "$(sed -n 's/.*"metaPath":"\([^"]*\)".*/\1/p')"
`
	require.NoError(t, os.WriteFile(metaOnly, []byte(script), 0o700))
	for _, host := range []string{"/usr/bin/false", metaOnly} {
		m := session.NewManager(testutil.ShortDirs(t), host)
		_, err := m.Create(t.Context(), session.CreateRequest{ProjectID: "prj_x", Command: []string{"true"}})
		assert.ErrorIs(t, err, session.ErrHostStartTimeout, host)
	}
}

func TestManagerCreateFastExit(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	s := spawn(t, m, "true")
	testutil.Eventually(t, func() bool {
		got, _ := m.Get(t.Context(), s.ID)
		return got.State == session.StateExited && got.ExitCode != nil && *got.ExitCode == 0
	}, 3*time.Second)
}

func TestManagerPersistsOwner(t *testing.T) {
	dirs, bin := testutil.ShortDirs(t), testutil.BuildBinary(t, "clubmate/cmd/clubmate-session")
	m := session.NewManager(dirs, bin)
	s, err := m.Create(t.Context(), session.CreateRequest{ProjectID: "prj_0123456789abcdef", Owner: "terminals", Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = unix.Kill(-s.PGID, unix.SIGKILL)
		_ = unix.Kill(s.HostPID, unix.SIGKILL)
	})
	got, err := session.NewManager(dirs, bin).Get(t.Context(), s.ID)
	require.NoError(t, err)
	assert.Equal(t, "terminals", got.Owner)
	mine, err := m.List(t.Context(), session.ListFilter{Owner: "terminals"})
	require.NoError(t, err)
	assert.Len(t, mine, 1)
	other, err := m.List(t.Context(), session.ListFilter{Owner: "other"})
	require.NoError(t, err)
	assert.Empty(t, other)
}
