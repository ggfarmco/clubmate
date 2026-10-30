package core_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/core"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

type procTable struct {
	mu     sync.Mutex
	procs  []proc.Process
	starts map[int]time.Time
}

func (p *procTable) set(procs []proc.Process, starts map[int]time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.procs, p.starts = procs, starts
}

func (p *procTable) snapshot() ([]proc.Process, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.procs == nil {
		return proc.Snapshot()
	}
	return slices.Clone(p.procs), nil
}

func (p *procTable) startTime(pid int) (time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.starts == nil {
		return proc.StartTime(pid)
	}
	start, ok := p.starts[pid]
	if !ok {
		return time.Time{}, syscall.ESRCH
	}
	return start, nil
}

func rootService(t *testing.T) (*core.Service, *procTable, string) {
	t.Helper()
	dirs := testutil.ShortDirs(t)
	store, err := project.Open(dirs.DB())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	table := &procTable{}
	tracker := proc.NewTracker(func(int) ([]string, error) { return nil, nil }, os.Getpid())
	svc := core.NewService(store, session.NewManager(dirs, "unused"), tracker, table.snapshot, core.WithStartTime(table.startTime), core.WithKillGrace(time.Second))
	p, err := svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	return svc, table, p.ID
}

func TestRegisteredRootSurvivesStaleSnapshot(t *testing.T) {
	svc, table, projectID := rootService(t)
	child := startBystander(t)
	owner := core.RootOwner{Plugin: "agents", PID: os.Getpid()}
	root, err := svc.RegisterProcess(t.Context(), owner, core.RegisterRequest{ProjectID: projectID, PID: child, Label: "Claude · repo"})
	require.NoError(t, err)

	live, err := proc.Snapshot()
	require.NoError(t, err)
	table.set(slices.DeleteFunc(live, func(p proc.Process) bool { return p.PID == child }), nil)
	_, err = svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	assert.Equal(t, []core.ProcessRoot{root}, svc.ProcessRoots(projectID))

	table.set(nil, nil)
	nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{SessionID: root.ID})
	require.NoError(t, err)
	assert.Equal(t, []int{child}, pids(nodes))

	require.NoError(t, svc.DeleteProject(t.Context(), projectID))
	assert.True(t, testutil.Alive(child))
}

func TestDeadRootLeavesKillableOrphans(t *testing.T) {
	svc, _, projectID := rootService(t)
	cmd := exec.Command("/bin/sh", "-c", "sleep 300 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	root, err := svc.RegisterProcess(t.Context(), core.RootOwner{Plugin: "agents", PID: os.Getpid()}, core.RegisterRequest{ProjectID: projectID, PID: cmd.Process.Pid, Label: "Claude · repo"})
	require.NoError(t, err)

	leftover := 0
	testutil.Eventually(t, func() bool {
		nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{SessionID: root.ID})
		require.NoError(t, err)
		i := slices.IndexFunc(nodes, func(n proc.Node) bool { return n.Process.PPID == cmd.Process.Pid })
		if i >= 0 {
			leftover = nodes[i].Process.PID
		}
		return i >= 0
	}, 5*time.Second)
	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	testutil.Eventually(t, func() bool {
		nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{ProjectID: projectID})
		require.NoError(t, err)
		return slices.ContainsFunc(nodes, func(n proc.Node) bool { return n.Process.PID == leftover && n.Orphan && n.SessionID == root.ID })
	}, 5*time.Second)
	roots := svc.ProcessRoots(projectID)
	require.Len(t, roots, 1)
	assert.True(t, roots[0].Ended)
	assert.True(t, strings.HasSuffix(roots[0].Label, "(exited)"))

	killed, err := svc.KillProcesses(t.Context(), core.KillRequest{ProjectID: projectID, OrphansOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []int{leftover}, killed)
	testutil.Eventually(t, func() bool {
		_, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
		require.NoError(t, err)
		return len(svc.ProcessRoots(projectID)) == 0
	}, 5*time.Second)
}

func TestRootLifecycle(t *testing.T) {
	svc, table, projectID := rootService(t)
	t0 := time.Unix(1000, 0)
	owner := core.RootOwner{Plugin: "agents", PID: 50}
	table.set([]proc.Process{
		{PID: 50, PPID: 1, PGID: 50, Comm: "agents", StartTime: t0},
		{PID: 60, PPID: 50, PGID: 60, Comm: "node", StartTime: t0},
		{PID: 61, PPID: 50, PGID: 61, Comm: "node", StartTime: t0},
		{PID: 70, PPID: 1, PGID: 50, Comm: "stray", StartTime: t0},
	}, map[int]time.Time{50: t0, 60: t0, 61: t0})
	register := func(pid int) error {
		_, err := svc.RegisterProcess(t.Context(), owner, core.RegisterRequest{ProjectID: projectID, PID: pid, Label: "x"})
		return err
	}
	ended := func(pid int) []bool {
		var out []bool
		for _, r := range svc.ProcessRoots(projectID) {
			if r.PID == pid {
				out = append(out, r.Ended)
			}
		}
		return out
	}

	require.ErrorIs(t, register(70), core.ErrNotOwned)
	require.NoError(t, register(60))
	svc.UnregisterProcess(core.RootOwner{Plugin: "other", PID: 51}, 60)
	assert.Equal(t, []bool{false}, ended(60))

	table.set([]proc.Process{
		{PID: 50, PPID: 1, PGID: 50, Comm: "agents", StartTime: t0},
		{PID: 60, PPID: 50, PGID: 60, Comm: "reused", StartTime: t0.Add(time.Hour)},
		{PID: 61, PPID: 50, PGID: 61, Comm: "node", StartTime: t0},
	}, map[int]time.Time{50: t0, 60: t0.Add(time.Hour), 61: t0})
	_, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	assert.Empty(t, ended(60))

	require.NoError(t, register(61))
	svc.DropProcesses(owner)
	assert.Equal(t, []bool{true}, ended(61))
	require.ErrorIs(t, register(61), core.ErrOwnerClosed)
}
