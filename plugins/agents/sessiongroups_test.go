package main

import (
	"testing"

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
	}, got)
	assert.Equal(t, []string{"kid", "busy", "helper"}, active)
}
