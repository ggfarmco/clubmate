package core_test

import (
	"context"
	"os"
	"os/exec"
	"syscall"
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

func TestKillProcessesRefusesInvalidRequests(t *testing.T) {
	svc := testutil.NewService(t)
	_, err := svc.KillProcesses(t.Context(), core.KillRequest{PIDs: []int{os.Getpid()}})
	assert.ErrorIs(t, err, core.ErrNotTracked)
	_, err = svc.KillProcesses(t.Context(), core.KillRequest{OrphansOnly: true})
	assert.ErrorIs(t, err, core.ErrOrphansNeedSession)
}

func TestDeleteProjectKillsItsSessions(t *testing.T) {
	svc, sess, orphanPID := sessionWithOrphan(t)
	require.NoError(t, svc.DeleteProject(t.Context(), sess.ProjectID))
	testutil.Eventually(t, func() bool { return !testutil.Alive(orphanPID) }, 5*time.Second)
	_, err := svc.GetProject(t.Context(), sess.ProjectID)
	assert.ErrorIs(t, err, project.ErrNotFound)
}

func TestProcessTreeIgnoresStaleRoots(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	t0 := time.Now().Add(-time.Hour)
	self, code := os.Getpid(), 0
	bystander := startBystander(t)
	writeMeta(t, dirs.SessionMeta("ses_exited"), sessionhost.Meta{ID: "ses_exited", ProjectID: "prj_x", HostPID: 1, ChildPID: 777, PGID: 777, StartedAt: t0, ExitCode: &code})
	writeMeta(t, dirs.SessionMeta("ses_stale"), sessionhost.Meta{ID: "ses_stale", ProjectID: "prj_x", HostPID: self, ChildPID: 778, PGID: 778, StartedAt: t0.Add(-time.Hour)})
	writeMeta(t, dirs.SessionMeta("ses_live"), sessionhost.Meta{ID: "ses_live", ProjectID: "prj_x", HostPID: self, ChildPID: 779, PGID: 779, StartedAt: t0})
	snap := []proc.Process{
		{PID: self, PPID: 1, PGID: self, Comm: "host", StartTime: t0.Add(-time.Minute)},
		{PID: 900, PPID: 1, PGID: 777, Comm: "unrelated", StartTime: t0},
		{PID: 778, PPID: self, PGID: 778, Comm: "reused", StartTime: t0},
		{PID: bystander, PPID: 1, PGID: 778, Comm: "unrelated", StartTime: t0},
		{PID: 779, PPID: self, PGID: 779, Comm: "child", StartTime: t0},
		{PID: 901, PPID: 1, PGID: 779, Comm: "member", StartTime: t0},
	}
	svc := fakeService(t, dirs, snap)
	nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	var got []int
	for _, n := range nodes {
		got = append(got, n.Process.PID)
	}
	assert.ElementsMatch(t, []int{779, 901}, got)
	_, err = svc.KillProcesses(t.Context(), core.KillRequest{PIDs: []int{bystander}})
	assert.ErrorIs(t, err, core.ErrNotTracked)
	assert.True(t, testutil.Alive(bystander))
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

func startBystander(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	return cmd.Process.Pid
}

func writeMeta(t *testing.T, path string, m sessionhost.Meta) {
	t.Helper()
	require.NoError(t, sessionhost.WriteMeta(path, m))
}
