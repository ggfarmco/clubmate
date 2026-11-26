package main

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/testutil"
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
	a.overlapCache.landedCheck = func(_ context.Context, _, tip, _ string) bool {
		mu.Lock()
		defer mu.Unlock()
		checked = append(checked, tip)
		return tip == "landed"
	}
	a.overlapCache.touched = map[string]touched{}
	for id, file := range map[string]string{"shared": "a.go", "landed": "a.go", "apart": "b.go"} {
		a.sessions[id] = newSession(record{ID: id, Status: statusRunning, Worktree: t.TempDir()}, nil, true, nil)
		a.overlapCache.touched[id] = touched{at: time.Now(), ok: true, primary: "/repo", real: "/w/" + id, branch: id, def: "main", base: "main", head: id, baseTip: "tip", files: []string{file}}
	}
	self := touched{primary: "/repo", real: "/w/self", branch: "self", base: "main"}

	for range 2 {
		got := a.neighboursOf(t.Context(), "self", self, []string{"a.go"})
		assert.Len(t, got, 1)
		assert.Equal(t, "shared", got[0].id)
	}
	assert.ElementsMatch(t, []string{"shared", "landed"}, checked)
}

func TestTouchedFilesRunsOnceForConcurrentCallers(t *testing.T) {
	t.Parallel()
	c := &overlapCache{}
	started, release := make(chan struct{}), make(chan struct{})
	var runs atomic.Int32
	go c.shared(t.Context(), "one", func() touched {
		runs.Add(1)
		close(started)
		<-release
		return touched{at: time.Now(), ok: true}
	})
	<-started
	got := make(chan touched, 1)
	go func() {
		got <- c.shared(t.Context(), "one", func() touched {
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
