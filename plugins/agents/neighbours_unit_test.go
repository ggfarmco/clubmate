package main

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
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
