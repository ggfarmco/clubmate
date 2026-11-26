package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func rosterApp(closed int) *app {
	a := &app{registry: newRegistry(), uiState: newUIState(), reviews: newReviewBoard(), watchdog: newWatchdog()}
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for i := range closed {
		id := "old" + strconv.Itoa(i)
		a.sessions[id] = newSession(record{ID: id, Workspace: "w", Status: statusClosed, Title: id, CreatedAt: at.Add(time.Duration(i) * time.Second)}, nil, true, nil)
	}
	a.sessions["live"] = newSession(record{ID: "live", Workspace: "w", Status: statusRunning, CreatedAt: at.Add(-time.Second)}, nil, true, nil)
	return a
}

func TestListReadsOnlyChangedSessions(t *testing.T) {
	t.Parallel()
	a := rosterApp(3)
	titles := func() []string {
		var out []string
		for _, info := range a.infos("w") {
			out = append(out, info.Title)
		}
		return out
	}

	titles()
	assert.Equal(t, []string{"", "old0", "old1", "old2"}, titles())
	assert.Equal(t, 4, a.roster.taken)

	s := a.sessions["live"]
	s.mu.Lock()
	s.rec.Title = "now"
	s.mu.Unlock()
	delete(a.sessions, "old1")

	assert.Equal(t, []string{"now", "old0", "old2"}, titles())
	assert.Equal(t, 5, a.roster.taken)
}

func BenchmarkListWithClosedSessions(b *testing.B) {
	for _, closed := range []int{2000, 10000} {
		b.Run(strconv.Itoa(closed), func(b *testing.B) {
			a := rosterApp(closed)
			s := a.sessions["live"]
			b.ReportAllocs()
			for b.Loop() {
				s.mu.Lock()
				s.rec.Turns++
				s.mu.Unlock()
				a.list("w")
			}
		})
	}
}
