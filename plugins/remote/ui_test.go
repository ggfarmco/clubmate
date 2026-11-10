package main

import (
	"bufio"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

func TestStreamIDFirstFrame(t *testing.T) {
	f := newFixture(t)
	_, r := f.openEvents(t, f.pair(t, "phone"), "")

	assert.Equal(t, ": connected", frame(t, r))
	assert.Regexp(t, `^event: stream\ndata: \{"id":"[A-Z2-7]{26}"\}$`, frame(t, r))
}

func TestWatchFiltersSlots(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	r := f.watchStream(t, token, "prj_1", "cron.main", popupPattern)

	f.srv.uiUpdated(view("agents.main", 1))
	f.srv.uiUpdated(view("cron.main", 2))
	f.srv.uiUpdated(view("docs.popup", 3))

	assert.Equal(t, `event: ui`+"\n"+`data: {"workspace":"prj_1","slot":"cron.main","rev":2,"tree":{"type":"empty"}}`, frame(t, r))
	assert.Contains(t, frame(t, r), `"slot":"docs.popup"`)
}

func TestPresenceOnlyForNonPopupSlots(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	_, r := f.openEvents(t, token, "")
	frame(t, r)
	id := streamID(t, r)

	f.watch(t, token, id, "prj_1", popupPattern)
	f.watch(t, token, id, "prj_1", "cron.main", popupPattern)
	subs, unsubs := f.backend.uiCalls()

	assert.Equal(t, []pluginsdk.UISubscribe{{WorkspaceID: "prj_1"}, {WorkspaceID: "prj_1", Presence: true}}, subs)
	assert.Equal(t, []string{"uisub_1"}, unsubs)
}

func TestWatchingSections(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	res, r := f.openEvents(t, token, "")
	frame(t, r)
	id := streamID(t, r)

	f.watch(t, token, id, "prj_1", "cron.main", "agents.main", "agents.sidebar", popupPattern)
	f.watch(t, token, id, "prj_1", "agents.sidebar", "cron.main")
	res.Body.Close()

	assert.Eventually(t, func() bool { return len(f.watched.all()) == 2 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, []contract.RemoteWatching{
		{Workspace: "prj_1", Sections: []string{"agents", "cron"}},
		{Workspace: "prj_1", Sections: []string{}},
	}, f.watched.all())
}

func TestClearWatchingOnStart(t *testing.T) {
	f := newFixture(t)

	require.NoError(t, f.srv.clearWatching(t.Context()))

	assert.Equal(t, []contract.RemoteWatching{{Workspace: "prj_1", Sections: []string{}}}, f.watched.all())
}

func TestSharedSubscription(t *testing.T) {
	f := newFixture(t)
	first, second := f.pair(t, "phone"), f.pair(t, "ipad")
	res, _ := f.openEvents(t, first, "")
	firstStream := bufio.NewReader(res.Body)
	frame(t, firstStream)
	f.watch(t, first, streamID(t, firstStream), "prj_1", "cron.main")
	r := f.watchStream(t, second, "prj_1", "cron.main")

	res.Body.Close()
	assert.Eventually(t, func() bool { return f.srv.hub.streamCount() == 1 }, time.Second, 10*time.Millisecond)
	f.srv.uiUpdated(view("cron.main", 1))
	assert.Contains(t, frame(t, r), `"slot":"cron.main"`)
	subs, unsubs := f.backend.uiCalls()
	assert.Len(t, subs, 1)
	assert.Empty(t, unsubs)

	f.srv.hub.revoke("ipad")
	assert.Eventually(t, func() bool {
		_, unsubs := f.backend.uiCalls()
		return len(unsubs) == 1
	}, time.Second, 10*time.Millisecond)
}

func TestCoalescesSlowStream(t *testing.T) {
	q := newUIQueue()
	q.push(view("cron.main", 1))
	q.push(view("docs.popup", 2))
	q.push(view("cron.main", 3))

	assert.Equal(t, []pluginsdk.SlotView{view("cron.main", 3), view("docs.popup", 2)}, q.take())
}

func TestUIEventRoutesOnlySlotsTheDeviceWatches(t *testing.T) {
	f := newFixture(t)
	token, other := f.pair(t, "phone"), f.pair(t, "ipad")
	f.watchStream(t, token, "prj_1", "cron.main", popupPattern)
	event := func(token, slot string) reply {
		_, got := f.call(t, token, "uiEvent", map[string]any{"workspace": "prj_1", "slot": slot, "nodeId": "jobs", "action": "select", "value": "j1"})
		return got
	}

	assert.JSONEq(t, `{"ok":true}`, string(event(token, "cron.main").Result))
	assert.JSONEq(t, `{"ok":true}`, string(event(token, "docs.popup").Result))
	for _, refused := range []reply{event(token, "graphs.main"), event(other, "cron.main")} {
		require.NotNil(t, refused.Error)
		assert.Equal(t, codeNotAllowed, refused.Error.Code)
	}
	assert.Equal(t, []string{"cron.main", "docs.popup"}, slotsOf(f.backend.uiEvents))
}

func slotsOf(events []pluginsdk.UIEvent) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Slot)
	}
	return out
}

func TestUISectionsAndGetAreFiltered(t *testing.T) {
	f := newFixture(t)
	token := f.pair(t, "phone")
	f.backend.views = []pluginsdk.SlotView{view("agents.main", 1), view("cron.main", 2), view("docs.popup", 3)}

	_, sections := f.call(t, token, "uiSections", nil)
	_, views := f.call(t, token, "uiGet", map[string]any{"workspace": "prj_1", "slots": []string{"cron.main", popupPattern}})

	assert.JSONEq(t, `[{"id":"cron","scope":"workspace"}]`, string(sections.Result))
	assert.JSONEq(t, `[{"workspaceId":"prj_1","slot":"cron.main","owner":"p","rev":2,"tree":{"type":"empty"}},{"workspaceId":"prj_1","slot":"docs.popup","owner":"p","rev":3,"tree":{"type":"empty"}}]`, string(views.Result))
}

func TestWatchRefusesForeignStreams(t *testing.T) {
	f := newFixture(t)
	owner, other := f.pair(t, "phone"), f.pair(t, "ipad")
	_, r := f.openEvents(t, owner, "")
	frame(t, r)
	id := streamID(t, r)
	tests := []struct {
		name, token, stream string
	}{
		{"unknown stream", owner, "nope"},
		{"another device's stream", other, id},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := f.call(t, tt.token, "uiWatch", map[string]any{"stream": tt.stream, "workspace": "prj_1", "slots": []string{"cron.main"}})
			require.NotNil(t, got.Error)
			assert.Equal(t, codeInvalidParams, got.Error.Code)
		})
	}
}

func (f *fixture) watchStream(t *testing.T, token, ws string, slots ...string) *bufio.Reader {
	t.Helper()
	_, r := f.openEvents(t, token, "")
	frame(t, r)
	f.watch(t, token, streamID(t, r), ws, slots...)
	return r
}

func (f *fixture) watch(t *testing.T, token, stream, ws string, slots ...string) {
	t.Helper()
	_, got := f.call(t, token, "uiWatch", map[string]any{"stream": stream, "workspace": ws, "slots": slots})
	require.Nil(t, got.Error)
}

func (h *hub) streamCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.streams)
}

func view(slot string, rev int64) pluginsdk.SlotView {
	return pluginsdk.SlotView{WorkspaceID: "prj_1", Slot: slot, Owner: "p", Rev: rev, Tree: json.RawMessage(`{"type":"empty"}`)}
}
