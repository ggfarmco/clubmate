package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/automation"
)

type memStore struct{}

func (memStore) Save(context.Context, *Trigger) error { return nil }

func (memStore) Remove(context.Context, *Trigger) error { return nil }

func testApp(t *testing.T, run func(context.Context, automation.CommandRequest) (json.RawMessage, error)) (*app, *[]string) {
	t.Helper()
	a := newApp()
	a.store = memStore{}
	now := time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	a.runCommand = run
	a.render = func(context.Context, string) error { return nil }
	var notified []string
	a.notify = func(_ context.Context, tr *Trigger) { notified = append(notified, tr.LastError) }
	a.triggers["tr_1"] = &Trigger{ID: "tr_1", Workspace: "ws1", On: "event.ping", Command: "notify.post", Args: json.RawMessage(`{"message":"{{payload.n}}"}`), Enabled: true}
	return a, &notified
}

func ping(seq int64) pluginsdk.Event {
	return pluginsdk.Event{Seq: seq, Topic: "event.ping", Payload: json.RawMessage(`{"workspace":"ws1","n":"x"}`)}
}

func TestDispatchSkipsWhileRunning(t *testing.T) {
	started, release := make(chan automation.CommandRequest, 1), make(chan struct{})
	a, _ := testApp(t, func(_ context.Context, req automation.CommandRequest) (json.RawMessage, error) {
		started <- req
		<-release
		return json.RawMessage(`{}`), nil
	})
	a.dispatch(t.Context(), ping(1))
	req := <-started
	assert.JSONEq(t, `{"message":"x"}`, string(req.Args))
	a.dispatch(t.Context(), ping(2))
	close(release)
	a.runs.Wait()
	h := a.triggers["tr_1"].History
	require.Len(t, h, 2)
	assert.Equal(t, Run{At: h[0].At, Topic: "event.ping", Seq: 2, Status: automation.StatusSkipped, Reason: automation.ReasonInFlight}, h[0])
	assert.Equal(t, automation.StatusOK, h[1].Status)
}

func TestDispatchPausesALoop(t *testing.T) {
	a, notified := testApp(t, func(context.Context, automation.CommandRequest) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})
	for i := range maxFires + 1 {
		a.dispatch(t.Context(), ping(int64(i)))
		a.runs.Wait()
	}
	a.dispatch(t.Context(), ping(99))
	a.runs.Wait()
	tr := a.triggers["tr_1"]
	assert.False(t, tr.Enabled)
	assert.Equal(t, reasonLoop, tr.LastError)
	assert.Len(t, tr.History, maxFires+1)
	assert.Equal(t, []string{reasonLoop}, *notified)
}

func TestBusySkipRefiresWhenTargetTurnEnds(t *testing.T) {
	var sent []automation.CommandRequest
	a, _ := testApp(t, func(_ context.Context, req automation.CommandRequest) (json.RawMessage, error) {
		sent = append(sent, req)
		return json.RawMessage(`{}`), nil
	})
	a.triggers["tr_1"].Command = automation.PromptCommand
	a.triggers["tr_1"].Args = json.RawMessage(`{"session":"agt_o","text":"{{payload.n}}"}`)
	status := "running"
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_o", Status: status}}, nil
	}
	turnEnd := pluginsdk.Event{Seq: 9, Topic: api.TopicTurn, Payload: json.RawMessage(`{"workspace":"ws1","session":"agt_o"}`)}

	a.dispatch(t.Context(), ping(1))
	a.runs.Wait()
	a.dispatch(t.Context(), turnEnd)
	a.runs.Wait()
	status = "done"
	a.dispatch(t.Context(), turnEnd)
	a.runs.Wait()

	require.Len(t, sent, 1)
	assert.JSONEq(t, `{"session":"agt_o","text":"x"}`, string(sent[0].Args))
	h := a.triggers["tr_1"].History
	require.Len(t, h, 2)
	assert.Equal(t, automation.ReasonBusy, h[0].Reason)
	assert.Equal(t, Run{At: h[1].At, Topic: "event.ping", Seq: 1, Status: automation.StatusOK, Result: "{}"}, h[1])
}

func TestForeignEmitterSkipsOwnedTemplatedTrigger(t *testing.T) {
	sessions := []api.Session{{ID: "agt_owner"}, {ID: "agt_child", ParentSession: "agt_owner"}, {ID: "agt_other"}}
	templated := &Trigger{Owner: "agt_owner", Args: json.RawMessage(`{"session":"{{payload.session}}"}`)}
	fixed := &Trigger{Owner: "agt_owner", Args: json.RawMessage(`{"session":"agt_child"}`)}
	unowned := &Trigger{Args: templated.Args}
	emitted := func(by string) pluginsdk.Event {
		return pluginsdk.Event{Topic: "event.x", Payload: json.RawMessage(`{"by":"` + by + `"}`)}
	}
	tests := []struct {
		name    string
		trigger *Trigger
		ev      pluginsdk.Event
		want    bool
	}{
		{"other agent", templated, emitted("agt_other"), true},
		{"agent without session", templated, emitted("agent"), true},
		{"owner's child", templated, emitted("agt_child"), false},
		{"user", templated, pluginsdk.Event{Topic: "event.x", Payload: json.RawMessage(`{}`)}, false},
		{"no placeholders", fixed, emitted("agt_other"), false},
		{"no owner", unowned, emitted("agt_other"), false},
		{"not emitted", templated, pluginsdk.Event{Topic: "agents.turn", Payload: json.RawMessage(`{"by":"agt_other"}`)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, foreign(tt.trigger, tt.ev, sessions))
		})
	}
}
