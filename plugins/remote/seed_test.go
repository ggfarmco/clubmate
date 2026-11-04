package main

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

const seededArgs = `{"name":"Push to phone","on":"notify.posted","match":"{\"kind\":[\"needs-input\",\"permission\",\"question\",\"stuck\",\"done\"]}","command":"remote.push","args":"{\"notification\":\"{{payload}}\"}","owner":"none","limit":60,"parallel":true}`

var errTriggersDown = errors.New("triggers not running")

func TestSeedAddsThePushTriggerOncePerWorkspace(t *testing.T) {
	f := newFixture(t)
	f.backend.workspaces = []pluginsdk.Workspace{{ID: "prj_1"}, {ID: "prj_2"}}
	a := f.app(t)

	a.seedPushTriggers(t.Context())
	a.seedPushTriggers(t.Context())

	adds := f.triggerAdds()
	require.Len(t, adds, 2)
	for i, ws := range []string{"prj_1", "prj_2"} {
		assert.Equal(t, ws, adds[i].workspace)
		assert.JSONEq(t, seededArgs, adds[i].args)
	}
	assert.Equal(t, `"tr_1"`, f.kv.get(pushTriggerKey+"prj_1"))
}

func TestSeedSkipsAWorkspaceWhoseTriggerWasDeleted(t *testing.T) {
	f := newFixture(t)
	f.kv.data[pushTriggerKey+"prj_1"] = []byte(`"tr_gone"`)

	f.app(t).seedPushTriggers(t.Context())

	assert.Empty(t, f.triggerAdds())
}

func TestWorkspaceDeletedClearsTheMarker(t *testing.T) {
	f := newFixture(t)
	f.kv.data[pushTriggerKey+"prj_1"] = []byte(`"tr_1"`)

	f.app(t).busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: workspaceDeletedTopic, Payload: json.RawMessage(`{"workspace":"prj_1"}`)})

	assert.Empty(t, f.kv.get(pushTriggerKey+"prj_1"))
}

func TestWorkspaceCreatedSeeds(t *testing.T) {
	f := newFixture(t)

	f.app(t).busEvent(t.Context(), nil, "", pluginsdk.Event{Topic: workspaceCreatedTopic, Payload: json.RawMessage(`{"workspace":"prj_2","info":{"id":"prj_2"}}`)})

	assert.Eventually(t, func() bool { return f.kv.get(pushTriggerKey+"prj_2") == `"tr_1"` }, time.Second, time.Millisecond)
	assert.Equal(t, "prj_2", f.triggerAdds()[0].workspace)
}

func TestSeedRetriesUntilTriggersAnswers(t *testing.T) {
	f := newFixture(t)
	f.backend.addFails = 2

	f.app(t).seedPushTriggers(t.Context())

	assert.Len(t, f.triggerAdds(), 3)
	assert.Equal(t, `"tr_3"`, f.kv.get(pushTriggerKey+"prj_1"))
}

func TestSeededMatchIsTodaysKinds(t *testing.T) {
	var args struct {
		Match string `json:"match"`
	}
	require.NoError(t, json.Unmarshal(pushTriggerArgs(), &args))
	var match struct {
		Kind []string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal([]byte(args.Match), &match))

	assert.ElementsMatch(t, slices.Collect(maps.Keys(pushKinds)), match.Kind)
}

func TestWorkspaceEventsAreNotForwardedToPhones(t *testing.T) {
	f := newFixture(t)
	a := f.app(t)
	_, r := f.openEvents(t, f.pair(t, "phone"), "")
	frame(t, r)
	streamID(t, r)

	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Seq: 1, Topic: workspaceCreatedTopic, Payload: json.RawMessage(`{"workspace":"prj_2"}`)})
	a.busEvent(t.Context(), nil, "", pluginsdk.Event{Seq: 2, Topic: "backlog.changed", Payload: json.RawMessage(`{}`)})

	assert.Contains(t, frame(t, r), `"topic":"backlog.changed"`)
}

func (f *fixture) app(t *testing.T) *app {
	t.Helper()
	return &app{ctx: t.Context(), srv: f.srv, store: f.kv, seedEvery: time.Millisecond, seedFor: time.Second}
}

func (f *fixture) triggerAdds() []runCall {
	return slices.DeleteFunc(f.backend.calls(), func(c runCall) bool { return c.id != triggersAddCommand })
}
