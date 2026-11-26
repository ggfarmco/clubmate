package main

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/plugins/internal/gitrepo"
)

func TestNeighboursTextClipsSessionsAndFiles(t *testing.T) {
	t.Parallel()
	var ns []neighbour
	for i := range 5 {
		ns = append(ns, neighbour{id: "s" + strconv.Itoa(i), title: "T", branch: "b", worktree: "/w", mergeBase: "mb", files: []string{"a.go"}})
	}
	for i := range 11 {
		ns[0].files = append(ns[0].files, "f"+strconv.Itoa(i)+".go")
	}

	got := neighboursText(record{ParentSession: "p"}, ns)

	assert.Contains(t, got, "f8.go, and 2 more; ")
	assert.NotContains(t, got, "f9.go")
	assert.NotContains(t, got, "s4")
	assert.Contains(t, got, `"T" (session s3, branch b): a.go; and 1 more.`)
	assert.Contains(t, got, "If your changes clash, tell your parent session.")
}

func TestClosedChildLineUsesLandOverlap(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	a.sessions["two"] = newSession(record{ID: "two", Title: "Two", Status: statusRunning, Worktree: t.TempDir()}, nil, true, nil)
	kid := record{ID: "one", Status: statusClosed, Worktree: t.TempDir(), LandOverlap: []overlapEntry{{Session: "two", Branch: "ktwo", Files: []string{"a.go"}}}}

	line, keys := a.childNeighbours(t.Context(), nil, kid)

	assert.Equal(t, `It changes files that open session "Two" (two, branch ktwo) changes too: a.go.`, line)
	assert.Equal(t, []string{"one\x00two\x00a.go"}, keys)
}

func TestLandingCacheKeepsOneEntryPerLander(t *testing.T) {
	t.Parallel()
	a := &app{}
	a.rememberLanding(landGateArgs{Session: "agt_1", Old: "o1", Tree: "t"}, landTold{lander: "agt_1"})
	a.rememberLanding(landGateArgs{Session: "agt_1", Old: "o2", Tree: "t"}, landTold{lander: "agt_1"})
	a.rememberLanding(landGateArgs{Session: "agt_2", Old: "o1", Tree: "t"}, landTold{lander: "agt_2"})
	assert.Len(t, a.overlapCache.landing, 2)
	assert.Contains(t, a.overlapCache.landing, landingKey(landGateArgs{Session: "agt_1", Old: "o2", Tree: "t"}))
}

func TestNeighboursCheckLandedOnlyForSharedFilesOnce(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	var mu sync.Mutex
	var checked []string
	c := &a.overlapCache
	c.landedCheck = func(_ context.Context, _, tip, _ string) bool {
		mu.Lock()
		defer mu.Unlock()
		checked = append(checked, tip)
		return tip == "landed"
	}
	c.touched = map[string]touched{}
	files := map[string]string{"shared": "a.go", "landed": "a.go", "apart": "b.go"}
	for id, file := range files {
		a.sessions[id] = newSession(record{ID: id, Status: statusRunning, Worktree: t.TempDir()}, nil, true, nil)
		c.touched[id] = touched{at: time.Now(), ok: true, primary: "/repo", real: "/w/" + id, branch: id, def: "main", base: "main", head: id, baseTip: "tip", files: []string{file}}
	}
	self := touched{primary: "/repo", real: "/w/self", branch: "self", base: "main"}

	for range 2 {
		var set overlapSet
		for id := range files {
			set.recs, set.touched = append(set.recs, a.sessions[id].snapshot()), append(set.touched, c.touched[id])
		}
		got := a.neighboursIn(t.Context(), set, "self", self, []string{"a.go"})
		assert.Len(t, got, 1)
		assert.Equal(t, "shared", got[0].id)
	}
	assert.ElementsMatch(t, []string{"shared", "landed"}, checked)
}

func overlapTrees(t *testing.T, a *app, names ...string) (string, map[string]*atomic.Int32) {
	t.Helper()
	repo := unitRepo(t)
	runs := map[string]*atomic.Int32{}
	a.branches.names, a.baseCache.bases = map[string]map[string]string{"w": {}}, map[string]map[string]string{"w": {}}
	for _, name := range names {
		tree := filepath.Join(repo, ".worktrees", name)
		for _, args := range [][]string{{"-C", repo, "worktree", "add", "-q", "-b", name, tree}} {
			out, err := exec.Command("git", args...).CombinedOutput()
			require.NoError(t, err, string(out))
		}
		require.NoError(t, os.WriteFile(filepath.Join(tree, "a.go"), []byte(name), 0o600))
		a.sessions[name] = newSession(record{ID: name, Workspace: "w", Status: statusRunning, Worktree: tree}, nil, true, nil)
		a.branches.names["w"][tree], a.baseCache.bases["w"][tree+"\x00"+name] = name, ""
		runs[tree] = &atomic.Int32{}
	}
	a.overlapCache.viewAge = time.Hour
	a.overlapCache.touchedOf = func(ctx context.Context, tree, base string) (gitrepo.Touched, error) {
		runs[tree].Add(1)
		return gitrepo.TouchedFiles(ctx, tree, base)
	}
	return repo, runs
}

func TestTouchedFilesAreKeptUntilTheWorktreeChanges(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	repo, runs := overlapTrees(t, a, "one", "two")
	one, two := filepath.Join(repo, ".worktrees", "one"), filepath.Join(repo, ".worktrees", "two")
	counts := func() []int32 {
		got := a.neighbours(t.Context(), a.sessions["one"].snapshot())
		require.Len(t, got, 1)
		assert.Equal(t, []string{"a.go"}, got[0].files)
		return []int32{runs[one].Load(), runs[two].Load()}
	}

	assert.Equal(t, []int32{1, 1}, counts())
	assert.Equal(t, []int32{1, 1}, counts())

	a.overlapCache.move(one)
	assert.Equal(t, []int32{2, 1}, counts())

	a.overlapCache.move(filepath.Join(one, "sub"))
	assert.Equal(t, []int32{3, 1}, counts())

	out, err := exec.Command("git", "-C", two, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "next").CombinedOutput()
	require.NoError(t, err, string(out))
	a.overlapCache.mu.Lock()
	a.overlapCache.views = nil
	a.overlapCache.mu.Unlock()
	assert.Equal(t, []int32{3, 2}, counts())
}

func TestNeighbourToolCallCountsOnlyAfterTheFloor(t *testing.T) {
	t.Parallel()
	was, now := touchedKey{head: "h", moves: 1}, touchedKey{head: "h", moves: 2}

	assert.True(t, touched{at: time.Now(), key: was}.fresh(now, false))
	assert.False(t, touched{at: time.Now(), key: was}.fresh(now, true))
	assert.False(t, touched{at: time.Now().Add(-overlapsAge), key: was}.fresh(now, false))
	assert.False(t, touched{at: time.Now(), key: was}.fresh(touchedKey{head: "next", moves: 1}, false))
}

func TestOverlapPassListsWorktreesOncePerRepo(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry(), uiState: newUIState()}
	a.goneWorkspaces["w"] = true
	_, runs := overlapTrees(t, a, "one", "two", "three", "four")
	var lists atomic.Int32
	a.overlapCache.treesOf = func(ctx context.Context, top string) ([]gitrepo.Worktree, error) {
		lists.Add(1)
		return gitrepo.Worktrees(ctx, top)
	}
	for i := range 20 {
		id := "closed" + strconv.Itoa(i)
		a.sessions[id] = newSession(record{ID: id, Workspace: "w", Status: statusClosed, Worktree: t.TempDir()}, nil, true, nil)
	}

	a.refreshOverlaps("w")
	a.refreshOverlaps("w")

	assert.EqualValues(t, 1, lists.Load())
	for tree, n := range runs {
		assert.EqualValues(t, 1, n.Load(), tree)
	}
	a.overlapCache.mu.Lock()
	defer a.overlapCache.mu.Unlock()
	assert.Len(t, a.overlapCache.overlaps, 4)
	assert.Len(t, a.overlapCache.overlaps["one"].list, 3)
}

func TestRepoViewIsSharedByEveryWorktreeOfItsRepo(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	repo, _ := overlapTrees(t, a, "one", "two")
	var lists atomic.Int32
	a.overlapCache.treesOf = func(ctx context.Context, top string) ([]gitrepo.Worktree, error) {
		lists.Add(1)
		return gitrepo.Worktrees(ctx, top)
	}
	one, two := gitrepo.Canonical(filepath.Join(repo, ".worktrees", "one")), gitrepo.Canonical(filepath.Join(repo, ".worktrees", "two"))

	first, _ := a.overlapCache.view(t.Context(), one, one, false)
	second, _ := a.overlapCache.view(t.Context(), two, two, false)

	assert.Same(t, first, second)
	assert.EqualValues(t, 1, lists.Load())
}

func TestTouchedFilesRunsOnceForConcurrentCallers(t *testing.T) {
	t.Parallel()
	c := &overlapCache{}
	started, release := make(chan struct{}), make(chan struct{})
	var runs atomic.Int32
	go c.shared(t.Context(), "one", touchedKey{}, true, func() touched {
		runs.Add(1)
		close(started)
		<-release
		return touched{at: time.Now(), ok: true}
	})
	<-started
	got := make(chan touched, 1)
	go func() {
		got <- c.shared(t.Context(), "one", touchedKey{}, true, func() touched {
			runs.Add(1)
			return touched{}
		})
	}()

	testutil.Never(t, func() bool { return runs.Load() > 1 || len(got) > 0 }, 200*time.Millisecond)
	close(release)

	var res touched
	testutil.Eventually(t, func() bool {
		select {
		case res = <-got:
			return true
		default:
			return false
		}
	}, time.Second)
	assert.True(t, res.ok)
	assert.EqualValues(t, 1, runs.Load())
}

func TestRefreshOverlapsDropsGoneSessions(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	a.sessions["one"] = newSession(record{ID: "one", Workspace: "w", Status: statusRunning, Worktree: t.TempDir()}, nil, true, nil)
	c := &a.overlapCache
	c.overlaps = map[string]overlapsAt{"one": {}, "gone": {}}
	c.touched = map[string]touched{"one": {at: time.Now()}, "gone": {at: time.Now()}}
	c.landing = map[string]landTold{"k1": {lander: "one"}, "k2": {lander: "gone"}}

	a.refreshOverlaps("w")

	assert.Equal(t, []string{"one"}, slices.Sorted(maps.Keys(c.overlaps)))
	assert.Equal(t, []string{"one"}, slices.Sorted(maps.Keys(c.touched)))
	assert.Equal(t, []string{"k1"}, slices.Sorted(maps.Keys(c.landing)))
}
