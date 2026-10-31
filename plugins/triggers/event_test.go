package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestMatches(t *testing.T) {
	tr := &Trigger{Workspace: "ws1", On: "agents.turn", Match: json.RawMessage(`{"status":["done","error"],"session":"agt_1"}`)}
	prefix := &Trigger{Workspace: "ws1", On: "event.*"}
	tests := []struct {
		name    string
		trigger *Trigger
		topic   string
		payload string
		want    bool
	}{
		{"all fields match", tr, "agents.turn", `{"workspace":"ws1","session":"agt_1","status":"error"}`, true},
		{"value outside options", tr, "agents.turn", `{"workspace":"ws1","session":"agt_1","status":"running"}`, false},
		{"missing field", tr, "agents.turn", `{"workspace":"ws1","status":"done"}`, false},
		{"other topic", tr, "agents.changed", `{"workspace":"ws1","session":"agt_1","status":"done"}`, false},
		{"prefix", prefix, "event.build.done", `{"workspace":"ws1"}`, true},
		{"other workspace", prefix, "event.x", `{"workspace":"ws2"}`, false},
		{"nested workspace", prefix, "event.x", `{"project":{"id":"ws2"}}`, false},
		{"no workspace", prefix, "event.x", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matches(tt.trigger, tt.topic, decodePayload(json.RawMessage(tt.payload))))
		})
	}
}

func TestExpand(t *testing.T) {
	ev := pluginsdk.Event{Seq: 7, Topic: "event.x", Payload: json.RawMessage(`{"n":3,"who":{"id":"agt_1"}}`)}
	out, err := expand(json.RawMessage(`{"text":"{{topic}}#{{seq}} by {{payload.who.id}} n={{ payload.n }} {{payload.who}}","count":2}`), ev)
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"event.x#7 by agt_1 n=3 {\"id\":\"agt_1\"}","count":2}`, string(out))

	_, err = expand(json.RawMessage(`{"text":"{{payload.missing}}"}`), ev)
	assert.ErrorContains(t, err, "{{payload.missing}}: not in the event")
	_, err = expand(json.RawMessage(`{"text":"{{who}}"}`), ev)
	assert.ErrorContains(t, err, "unknown placeholder")
}
