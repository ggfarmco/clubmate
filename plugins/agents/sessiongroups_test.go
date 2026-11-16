package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOpenItemsGroupBetweenRunningAndDone(t *testing.T) {
	t.Parallel()
	info := func(id, parent, status string, open int, needsYou bool) sessionInfo {
		return sessionInfo{record: record{ID: id, lineage: lineage{ParentSession: parent}, Status: status, OpenCount: open}, NeedsYou: needsYou}
	}
	infos := []sessionInfo{
		info("open", "", statusDone, 2, false),
		info("waiting", "", statusWaiting, 2, true),
		info("working", "", statusRunning, 2, false),
		info("archived", "", statusClosed, 0, false),
		info("leftover", "archived", statusDone, 1, false),
	}
	setGroups(infos)
	got := map[string]string{}
	for _, i := range infos {
		got[i.ID] = i.Group
	}
	assert.Equal(t, map[string]string{"open": "open", "waiting": "needsYou", "working": "running", "archived": "open", "leftover": "open"}, got)
}

func TestSetGroupsLiftsParentsToTheMostUrgentChildGroup(t *testing.T) {
	t.Parallel()
	info := func(id, parent, status string, needsYou, service bool) sessionInfo {
		return sessionInfo{record: record{ID: id, lineage: lineage{ParentSession: parent}, Status: status}, NeedsYou: needsYou, Service: service}
	}
	infos := []sessionInfo{
		info("root", "", statusDone, false, false),
		info("kid", "root", statusWaiting, true, false),
		info("grandkid", "kid", statusDone, false, false),
		info("orphan", "gone", statusDone, false, false),
		info("archived", "", statusClosed, false, false),
		info("leftover", "archived", statusDone, false, false),
		info("closing", "", statusClosed, false, false),
		info("busy", "closing", statusRunning, false, false),
		info("voice", "", statusRunning, false, true),
		info("helper", "voice", statusRunning, false, false),
		info("asking", "", statusWaiting, true, true),
		info("loop-a", "loop-b", statusClosed, false, false),
		info("loop-b", "loop-a", statusClosed, false, false),
		info("lander", "", statusDone, false, false),
		{record: record{ID: "reviewer", Status: statusRunning, GateFor: &gateFor{Lander: "lander", Old: "a", Tree: "b"}}},
	}
	setGroups(infos)
	got := map[string]string{}
	active := []string{}
	for _, i := range infos {
		got[i.ID] = i.Group
		if i.Active {
			active = append(active, i.ID)
		}
	}
	assert.Equal(t, map[string]string{
		"root": "needsYou", "kid": "needsYou", "grandkid": "done", "orphan": "done",
		"archived": "closed", "leftover": "done", "closing": "running", "busy": "running",
		"voice": "service", "helper": "running", "asking": "needsYou", "loop-a": "closed", "loop-b": "closed",
		"lander": "running", "reviewer": "running",
	}, got)
	assert.Equal(t, []string{"kid", "busy", "helper", "reviewer"}, active)
}

func TestPausedGroupHoldsLimitStoppedSessionsAndLiftsIdleParents(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 16, 19, 4, 30, 0, time.UTC)
	info := func(id, parent, status string, until time.Time) sessionInfo {
		rec := record{ID: id, lineage: lineage{ParentSession: parent}, Status: status}
		if !until.IsZero() {
			rec.LimitWait = &limitWait{At: until}
		}
		return sessionInfo{record: rec}
	}
	fired := info("fired", "", statusError, at)
	fired.LimitWait.Fired = true
	infos := []sessionInfo{
		info("stopped", "", statusError, at),
		fired,
		info("idle", "", statusDone, time.Time{}),
		info("soon", "idle", statusError, at),
		info("later", "idle", statusError, at.Add(72*time.Hour)),
		info("busy", "", statusRunning, time.Time{}),
		info("held", "busy", statusError, at),
	}
	setGroups(infos)
	got := map[string]string{}
	until := map[string]int64{}
	var paused []string
	for _, i := range infos {
		got[i.ID] = i.Group
		if i.PausedUntil != nil {
			until[i.ID] = *i.PausedUntil
		}
		if i.Paused {
			paused = append(paused, i.ID)
		}
	}
	minute := at.Truncate(time.Minute).Unix()
	assert.Equal(t, map[string]string{"stopped": "paused", "fired": "done", "idle": "paused", "soon": "paused", "later": "paused", "busy": "running", "held": "paused"}, got)
	assert.Equal(t, map[string]int64{"stopped": minute, "idle": minute, "soon": minute, "later": minute + 72*3600, "held": minute}, until)
	assert.Equal(t, []string{"stopped", "soon", "later", "held"}, paused)
}
