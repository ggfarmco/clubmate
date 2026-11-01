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
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
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
	store, err := workspace.Open(dirs.DB())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	table := &procTable{}
	tracker := proc.NewTracker(func(int) ([]string, error) { return nil, nil }, os.Getpid())
	svc := core.NewService(store, session.NewManager(dirs, "unused"), tracker, table.snapshot, core.WithStartTime(table.startTime), core.WithKillGrace(time.Second))
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	return svc, table, p.ID
}

func TestRegisteredRootSurvivesStaleSnapshot(t *testing.T) {
	svc, table, workspaceID := rootService(t)
	child := startBystander(t)
	owner := core.RootOwner{Plugin: "agents", PID: os.Getpid()}
	root, err := svc.RegisterProcess(t.Context(), owner, core.RegisterRequest{WorkspaceID: workspaceID, PID: child, Label: "Claude · repo"})
	require.NoError(t, err)

	live, err := proc.Snapshot()
	require.NoError(t, err)
	table.set(slices.DeleteFunc(live, func(p proc.Process) bool { return p.PID == child }), nil)
	_, err = svc.ProcessTree(t.Context(), core.TreeFilter{})
	require.NoError(t, err)
	assert.Equal(t, []core.ProcessRoot{root}, svc.ProcessRoots(workspaceID))

	table.set(nil, nil)
	nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{SessionID: root.ID})
	require.NoError(t, err)
	assert.Equal(t, []int{child}, pids(nodes))

	require.NoError(t, svc.DeleteWorkspace(t.Context(), workspaceID))
	assert.True(t, testutil.Alive(child))
}

func rootWithChild(t *testing.T, svc *core.Service, workspaceID string) (*exec.Cmd, core.ProcessRoot, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "sleep 300 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	root, err := svc.RegisterProcess(t.Context(), core.RootOwner{Plugin: "agents", PID: os.Getpid()}, core.RegisterRequest{WorkspaceID: workspaceID, PID: cmd.Process.Pid, Label: "Claude · repo"})
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
	return cmd, root, leftover
}

func endedRootLeftover(t *testing.T, svc *core.Service, workspaceID string) int {
	t.Helper()
	cmd, root, leftover := rootWithChild(t, svc, workspaceID)
	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()

	testutil.Eventually(t, func() bool {
		nodes, err := svc.ProcessTree(t.Context(), core.TreeFilter{WorkspaceID: workspaceID})
		require.NoError(t, err)
		return slices.ContainsFunc(nodes, func(n proc.Node) bool { return n.Process.PID == leftover && n.Orphan && n.SessionID == root.ID })
	}, 5*time.Second)
	return leftover
}

func TestDeadRootLeavesKillableOrphans(t *testing.T) {
	svc, _, workspaceID := rootService(t)
	leftover := endedRootLeftover(t, svc, workspaceID)
	roots := svc.ProcessRoots(workspaceID)
	require.Len(t, roots, 1)
	assert.True(t, roots[0].Ended)
	assert.True(t, strings.HasSuffix(roots[0].Label, "(exited)"))

	killed, err := svc.KillProcesses(t.Context(), core.KillRequest{WorkspaceID: workspaceID, OrphansOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []int{leftover}, killed)
	testutil.Eventually(t, func() bool {
		_, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
		require.NoError(t, err)
		return len(svc.ProcessRoots(workspaceID)) == 0
	}, 5*time.Second)
}

func TestDeleteWorkspaceKillsEndedRootLeftovers(t *testing.T) {
	svc, _, workspaceID := rootService(t)
	leftover := endedRootLeftover(t, svc, workspaceID)

	require.NoError(t, svc.DeleteWorkspace(t.Context(), workspaceID))
	testutil.Eventually(t, func() bool { return !testutil.Alive(leftover) }, 5*time.Second)
}

func TestDeletedWorkspaceRootLeftoversDieWhenRootEnds(t *testing.T) {
	svc, _, workspaceID := rootService(t)
	cmd, _, leftover := rootWithChild(t, svc, workspaceID)

	require.NoError(t, svc.DeleteWorkspace(t.Context(), workspaceID))
	assert.True(t, testutil.Alive(leftover))

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	testutil.Eventually(t, func() bool {
		_, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
		require.NoError(t, err)
		return !testutil.Alive(leftover)
	}, 5*time.Second)
}

func TestRootLifecycle(t *testing.T) {
	svc, table, workspaceID := rootService(t)
	t0 := time.Unix(1000, 0)
	owner := core.RootOwner{Plugin: "agents", PID: 50}
	table.set([]proc.Process{
		{PID: 50, PPID: 1, PGID: 50, Comm: "agents", StartTime: t0},
		{PID: 60, PPID: 50, PGID: 60, Comm: "node", StartTime: t0},
		{PID: 61, PPID: 50, PGID: 61, Comm: "node", StartTime: t0},
		{PID: 70, PPID: 1, PGID: 50, Comm: "stray", StartTime: t0},
	}, map[int]time.Time{50: t0, 60: t0, 61: t0})
	register := func(pid int) error {
		_, err := svc.RegisterProcess(t.Context(), owner, core.RegisterRequest{WorkspaceID: workspaceID, PID: pid, Label: "x"})
		return err
	}
	ended := func(pid int) []bool {
		var out []bool
		for _, r := range svc.ProcessRoots(workspaceID) {
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

func TestProcessTreeSharesSnapshotWithinTTL(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	store, err := workspace.Open(dirs.DB())
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	var mu sync.Mutex
	snapshots, lists, argReads := 0, 0, 0
	t0 := time.Unix(1000, 0)
	procs := []proc.Process{{PID: 60, PPID: 1, PGID: 60, Comm: "node", StartTime: t0}}
	snapshot := func() ([]proc.Process, error) {
		mu.Lock()
		defer mu.Unlock()
		snapshots++
		return procs, nil
	}
	list := func() ([]proc.Process, error) {
		mu.Lock()
		defer mu.Unlock()
		lists++
		return procs, nil
	}
	readArgs := func(int) []string {
		mu.Lock()
		defer mu.Unlock()
		argReads++
		return []string{"node", "acp"}
	}
	tracker := proc.NewTracker(func(int) ([]string, error) { return nil, nil }, os.Getpid())
	svc := core.NewService(store, session.NewManager(dirs, "unused"), tracker, snapshot, core.WithSnapshotTTL(time.Hour), core.WithProcessList(list), core.WithArgsReader(readArgs))
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)

	for range 2 {
		_, err := svc.ProcessTree(t.Context(), core.TreeFilter{})
		require.NoError(t, err)
	}
	assert.Equal(t, 1, snapshots)

	_, err = svc.RegisterProcess(t.Context(), core.RootOwner{Plugin: "agents", PID: 50}, core.RegisterRequest{WorkspaceID: p.ID, PID: 60, Label: "x"})
	require.ErrorIs(t, err, core.ErrNotOwned)
	assert.Equal(t, 1, snapshots)
	assert.Equal(t, 1, lists)

	node := proc.Process{PID: 60, Comm: "node", StartTime: t0}
	assert.Equal(t, []string{"node", "acp"}, svc.ProcessArgs(node))
	svc.ProcessArgs(node)
	assert.Equal(t, 1, argReads)
	node.StartTime = t0.Add(time.Second)
	svc.ProcessArgs(node)
	assert.Equal(t, 2, argReads)
}
