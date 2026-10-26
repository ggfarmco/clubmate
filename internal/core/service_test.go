package core_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/core"
	"clubmate/internal/paths"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
	"clubmate/internal/testutil"
)

func TestKillSessionKillsDetachedDescendants(t *testing.T) {
	svc, sess, orphanPID := sessionWithOrphan(t)
	require.NoError(t, svc.KillSession(t.Context(), sess.ID))
	testutil.Eventually(t, func() bool { return !testutil.Alive(orphanPID) }, 5*time.Second)
	testutil.Eventually(t, func() bool {
		s, _ := svc.GetSession(t.Context(), sess.ID)
		return s.State == session.StateExited
	}, 5*time.Second)
}

func TestKillOrphansOnlyKeepsSession(t *testing.T) {
	svc, sess, orphanPID := sessionWithOrphan(t)
	killed, err := svc.KillProcesses(t.Context(), core.KillRequest{SessionID: sess.ID, OrphansOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []int{orphanPID}, killed)
	testutil.Eventually(t, func() bool { return !testutil.Alive(orphanPID) }, 5*time.Second)
	s, _ := svc.GetSession(t.Context(), sess.ID)
	assert.Equal(t, session.StateRunning, s.State)
}

func TestKillProcessesRefusesUntracked(t *testing.T) {
	svc := testutil.NewService(t)
	_, err := svc.KillProcesses(t.Context(), core.KillRequest{PIDs: []int{os.Getpid()}})
	assert.ErrorIs(t, err, core.ErrNotTracked)
}

func TestDeleteProjectKillsItsSessions(t *testing.T) {
	svc, sess, orphanPID := sessionWithOrphan(t)
	require.NoError(t, svc.DeleteProject(t.Context(), sess.ProjectID))
	testutil.Eventually(t, func() bool { return !testutil.Alive(orphanPID) }, 5*time.Second)
	_, err := svc.GetProject(t.Context(), sess.ProjectID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestProcessTreeIgnoresPGIDOfStoppedSessions(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	code := 0
	writeMeta(t, dirs.SessionMeta("ses_exited"), sessionhost.Meta{ID: "ses_exited", ProjectID: "prj_x", HostPID: 1, ChildPID: 777, PGID: 777, ExitCode: &code})
	writeMeta(t, dirs.SessionMeta("ses_running"), sessionhost.Meta{ID: "ses_running", ProjectID: "prj_x", HostPID: os.Getpid(), ChildPID: 778, PGID: 778})
	snap := []proc.Process{{PID: 900, PPID: 1, PGID: 777, Comm: "unrelated"}, {PID: 901, PPID: 1, PGID: 778, Comm: "child"}}
	svc := fakeService(t, dirs, snap)
	nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, 901, nodes[0].Process.PID)
}

func TestCreateSessionRequiresProject(t *testing.T) {
	svc := testutil.NewService(t)
	_, err := svc.CreateSession(t.Context(), session.CreateRequest{ProjectID: "prj_missing", Command: []string{"true"}})
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestCreateSessionDefaultsCwdToFirstRepo(t *testing.T) {
	svc := testutil.NewService(t)
	repo := t.TempDir()
	p, err := svc.CreateProject(t.Context(), "p", []string{repo})
	require.NoError(t, err)
	s, err := svc.CreateSession(t.Context(), session.CreateRequest{ProjectID: p.ID, Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	assert.Equal(t, repo, s.Cwd)
}

func TestRefreshLoopStopsOnCancel(t *testing.T) {
	svc := fakeService(t, testutil.ShortDirs(t), nil)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		svc.RefreshLoop(ctx, time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RefreshLoop did not stop")
	}
}

func sessionWithOrphan(t *testing.T) (*core.Service, session.Session, int) {
	t.Helper()
	svc := testutil.NewService(t)
	detachBin := testutil.BuildBinary(t, "clubmate/internal/testutil/detach")
	p, err := svc.CreateProject(t.Context(), "p", nil)
	require.NoError(t, err)
	sess, err := svc.CreateSession(t.Context(), session.CreateRequest{ProjectID: p.ID, Command: []string{"sh", "-c", detachBin + "; exec cat"}})
	require.NoError(t, err)
	orphanPID := 0
	testutil.Eventually(t, func() bool {
		nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{SessionID: sess.ID})
		if err != nil {
			return false
		}
		for _, n := range nodes {
			if n.Process.Comm == "detach" && n.Orphan {
				orphanPID = n.Process.PID
				return true
			}
		}
		return false
	}, 5*time.Second)
	t.Cleanup(func() { _ = unix.Kill(orphanPID, unix.SIGKILL) })
	return svc, sess, orphanPID
}

func fakeService(t *testing.T, dirs paths.Dirs, snap []proc.Process) *core.Service {
	t.Helper()
	store, err := project.Open(dirs.DB())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	tracker := proc.NewTracker(func(int) ([]string, error) { return nil, nil }, os.Getpid())
	return core.NewService(store, session.NewManager(dirs, "unused"), tracker, func() ([]proc.Process, error) { return snap, nil })
}

func writeMeta(t *testing.T, path string, m sessionhost.Meta) {
	t.Helper()
	require.NoError(t, sessionhost.WriteMeta(path, m))
}
