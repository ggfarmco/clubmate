package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

const testWorkspace = "ws_1"

type fakeHost struct {
	mu        sync.Mutex
	storage   map[string]json.RawMessage
	published []stateView
	sessions  map[string][]agentSession
	onRun     func()
}

func (f *fakeHost) StorageGet(_ context.Context, key string, out any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.storage[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (f *fakeHost) StorageSet(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.storage[key] = raw
	return nil
}

func (f *fakeHost) Publish(_ context.Context, topic string, payload any) (pluginsdk.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := payload.(stateView); ok && topic == changedTopic {
		f.published = append(f.published, v)
	}
	return pluginsdk.Event{}, nil
}

func (f *fakeHost) Render(context.Context, string, string, sdui.Node) (int64, error) {
	return 1, nil
}

func (f *fakeHost) RunCommand(_ context.Context, id, workspaceID string, _ any, out any) error {
	if id != "agents.sessions" {
		return fmt.Errorf("unexpected command %s", id)
	}
	f.mu.Lock()
	raw, err := json.Marshal(f.sessions[workspaceID])
	onRun := f.onRun
	f.mu.Unlock()
	if onRun != nil {
		onRun()
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (f *fakeHost) ListWorkspaces(context.Context) ([]pluginsdk.Workspace, error) {
	return []pluginsdk.Workspace{{ID: testWorkspace}}, nil
}

func (f *fakeHost) setSessions(list ...agentSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[testWorkspace] = list
}

type fakeAssertion struct {
	types    []string
	released bool
}

func (p *fakeAssertion) Release() { p.released = true }

type env struct {
	t    *testing.T
	h    *fakeHost
	a    *awake
	now  time.Time
	held []*fakeAssertion
	dead map[int]bool
}

func newEnv(t *testing.T) *env {
	return newEnvWith(t, &fakeHost{storage: map[string]json.RawMessage{}, sessions: map[string][]agentSession{}})
}

func newEnvWith(t *testing.T, h *fakeHost) *env {
	e := &env{t: t, h: h, now: time.Date(2026, 11, 16, 17, 0, 0, 0, time.Local), dead: map[int]bool{}}
	assert := func(types []string) (assertion, error) {
		p := &fakeAssertion{types: types}
		e.held = append(e.held, p)
		return p, nil
	}
	e.a = &awake{h: h, k: newKeeper(assert, func() time.Time { return e.now }, func(pid int) bool { return !e.dead[pid] })}
	e.a.load(t.Context())
	e.a.start(t.Context())
	return e
}

func (e *env) current() *fakeAssertion {
	if len(e.held) == 0 {
		return nil
	}
	p := e.held[len(e.held)-1]
	if p.released {
		return nil
	}
	return p
}

func (e *env) run(id string, args any) (json.RawMessage, error) {
	raw, err := json.Marshal(args)
	require.NoError(e.t, err)
	out, err := e.a.command(e.t.Context(), pluginsdk.CommandRun{ID: id, WorkspaceID: testWorkspace, Args: raw, Caller: "agt_0000000000000001"})
	if err != nil {
		return nil, err
	}
	res, err := json.Marshal(out)
	require.NoError(e.t, err)
	return res, nil
}

func (e *env) agentsChanged(kind, session, status string) {
	payload, err := json.Marshal(api.Changed{Workspace: testWorkspace, Session: session, Kind: kind, Status: status})
	require.NoError(e.t, err)
	e.a.busEvent(e.t.Context(), pluginsdk.Event{Topic: api.TopicChanged, Payload: payload})
}

func TestManualHoldEndsAtItsDeadline(t *testing.T) {
	e := newEnv(t)
	_, err := e.run("awake.set", map[string]any{"on": true, "minutes": 60})
	require.NoError(t, err)
	require.NotNil(t, e.current())
	assert.Equal(t, []string{assertSystem, assertIdle}, e.current().types)
	v := e.a.state()
	assert.Equal(t, "Awake until 18:00", v.Text)
	assert.True(t, v.Manual)

	e.now = e.now.Add(61 * time.Minute)
	e.a.tick(t.Context())
	assert.Nil(t, e.current())
	last := e.h.published[len(e.h.published)-1]
	assert.False(t, last.On)
	assert.False(t, last.Manual)
	assert.Equal(t, "The Mac sleeps as usual.", last.Detail)
}

func TestAgentsRuleHoldsOnPowerUnlessBatteryIsOn(t *testing.T) {
	e := newEnv(t)
	e.h.setSessions(agentSession{ID: "agt_a", Status: "running", Active: true}, agentSession{ID: "agt_b", Status: "waiting", Active: true}, agentSession{ID: "agt_c", Status: "done"})
	e.agentsChanged(api.ChangeStatus, "agt_a", "running")
	require.NotNil(t, e.current())
	assert.Equal(t, []string{assertSystem}, e.current().types)
	v := e.a.state()
	assert.Equal(t, "Awake · 2 agents", v.Text)
	assert.Contains(t, v.Detail, "on power only")

	_, err := e.run("awake.settings-set", map[string]any{"battery": true, "display": true})
	require.NoError(t, err)
	assert.Equal(t, []string{assertSystem, assertIdle, assertDisplay}, e.current().types)

	e.h.setSessions(agentSession{ID: "agt_a", Status: "done"})
	e.agentsChanged(api.ChangeStatus, "agt_a", "done")
	assert.Nil(t, e.current())
}

func TestAgentsRuleRereadsSessionsWithoutEvents(t *testing.T) {
	e := newEnv(t)
	e.h.setSessions(agentSession{ID: "agt_a", Status: "running", Active: true})
	e.agentsChanged(api.ChangeStatus, "agt_a", "running")
	require.NotNil(t, e.current())
	e.h.setSessions(agentSession{ID: "agt_a", Status: "done"})
	e.a.tick(t.Context())
	require.NotNil(t, e.current())
	e.now = e.now.Add(reseedEvery)
	e.a.tick(t.Context())
	assert.Nil(t, e.current())
}

func TestAgentHoldEndsWhenItsProcessExitsOrSessionCloses(t *testing.T) {
	e := newEnv(t)
	_, err := e.run("awake.hold", map[string]any{"reason": "release build", "pid": 4242})
	require.NoError(t, err)
	require.NotNil(t, e.current())
	assert.Equal(t, "Awake · release build", e.a.state().Text)
	e.dead[4242] = true
	e.a.tick(t.Context())
	assert.Nil(t, e.current())

	e.h.setSessions(agentSession{ID: "agt_0000000000000001", Status: "running"})
	_, err = e.run("awake.hold", map[string]any{"reason": "tests"})
	require.NoError(t, err)
	require.NotNil(t, e.current())
	e.h.setSessions(agentSession{ID: "agt_0000000000000001", Status: api.StatusClosed})
	var during stateView
	e.h.onRun = func() { during = e.a.state() }
	e.agentsChanged(api.ChangeStatus, "agt_0000000000000001", api.StatusClosed)
	assert.False(t, during.On)
	assert.Nil(t, e.current())
	assert.Empty(t, e.a.state().Holds)
}

func TestRestartKeepsHoldsAndDropsExpiredOnes(t *testing.T) {
	e := newEnv(t)
	_, err := e.run("awake.set", map[string]any{"on": true})
	require.NoError(t, err)
	_, err = e.run("awake.hold", map[string]any{"reason": "short", "minutes": 1})
	require.NoError(t, err)
	_, err = e.run("awake.hold", map[string]any{"reason": "long", "minutes": 600})
	require.NoError(t, err)

	e.h.setSessions(agentSession{ID: "agt_0000000000000001", Status: "running", Active: true})
	later := e.now.Add(5 * time.Minute)
	next := newEnvWith(t, e.h)
	next.now = later
	next.a.tick(t.Context())
	v := next.a.state()
	assert.True(t, v.On)
	assert.Equal(t, "Awake +2", v.Text)
	require.Len(t, v.Holds, 3)
	assert.Equal(t, "long", v.Holds[1].Label)
}

func TestPluginAnswersStateAndRendersSettings(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/awake", "plugins/awake/plugin.json"))
	testutil.WaitEvents(t, h.Bus, "awake.changed", func(events []bus.Event) bool { return len(events) > 0 })
	var v stateView
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "awake.state", "args": map[string]any{}}, &v))
	assert.False(t, v.On)
	assert.True(t, v.Agents)
	assert.Equal(t, "The Mac sleeps as usual.", v.Detail)
	h.WaitSlot(t, "", settingsSlot, func(n sdui.Node) bool { return len(n.Children) > 0 })
}
