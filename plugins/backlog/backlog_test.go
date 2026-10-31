package main_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

type task struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Priority      string `json:"priority"`
	ClaimedBy     string `json:"claimedBy"`
	ClaimedFrom   string `json:"claimedFrom"`
	LastClaimedBy string `json:"lastClaimedBy"`
	Note          string `json:"note"`
}

type env struct {
	h *testutil.PluginHarness
	p string
	t *testing.T
}

const agentsManifest = `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"agents.sessions","title":"Sessions","hidden":true},{"id":"agents.reveal","title":"Reveal","hidden":true}]},"echoResults":{"agents.reveal":null,"agents.sessions":[{"id":"agt_dead","status":"closed","closed":true},{"id":"agt_live","status":"running"},{"id":"agt_parent","status":"running"},{"id":"agt_child","status":"running","parentSession":"agt_parent"}]}}`

func newEnv(t *testing.T, extra ...string) env {
	dirs := append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/backlog", "plugins/backlog/plugin.json")}, extra...)
	h := testutil.NewPluginHarness(t, dirs...)
	require.Eventually(t, func() bool {
		events, err := h.Bus.Read(t.Context(), "backlog.ready", 0, 1)
		return err == nil && len(events) == 1
	}, 10*time.Second, 20*time.Millisecond)
	e := env{h: h, p: h.CreateProject(t, "demo").ID, t: t}
	if len(extra) > 0 {
		require.Eventually(t, func() bool {
			return h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "agents.sessions", "projectId": e.p, "args": map[string]any{}}, nil) == nil
		}, 10*time.Second, 20*time.Millisecond)
	}
	return e
}

func (e env) run(id, caller string, agent bool, args map[string]any) (task, error) {
	var out task
	err := e.h.Client(e.t).Call(e.t.Context(), "command.run", map[string]any{"id": id, "projectId": e.p, "args": args, "agent": agent, "caller": caller}, &out)
	return out, err
}

func (e env) user(id string, args map[string]any) (task, error) {
	return e.run(id, "", false, args)
}

func (e env) agent(caller, id string, args map[string]any) (task, error) {
	return e.run(id, caller, true, args)
}

func (e env) must(tk task, err error) task {
	require.NoError(e.t, err)
	return tk
}

func (e env) add(title string, extra map[string]any) task {
	args := map[string]any{"title": title}
	for k, v := range extra {
		args[k] = v
	}
	return e.must(e.user("backlog.add", args))
}

func TestAddAndListOrder(t *testing.T) {
	e := newEnv(t)
	low := e.add("low one", map[string]any{"priority": "low"})
	high := e.add("high one", map[string]any{"priority": "high"})
	normal := e.add("normal", nil)
	assert.Equal(t, "todo", normal.Status)
	var list struct {
		Tasks []task `json:"tasks"`
	}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "backlog.list", "projectId": e.p, "args": map[string]any{"status": "open"}}, &list))
	var ids []int
	for _, tk := range list.Tasks {
		ids = append(ids, tk.ID)
	}
	assert.Equal(t, []int{high.ID, normal.ID, low.ID}, ids)

	assert.Equal(t, "idea", e.must(e.agent("agt_a", "backlog.add", map[string]any{"title": "agent task"})).Status)
	_, err := e.agent("agt_a", "backlog.add", map[string]any{"title": "x", "status": "todo"})
	assert.ErrorContains(t, err, "only the user")
	_, err = e.user("backlog.add", map[string]any{"title": "a\nb"})
	assert.Error(t, err)
	_, err = e.user("backlog.add", map[string]any{"title": "  "})
	assert.Error(t, err)
}

func TestClaimIsExclusive(t *testing.T) {
	e := newEnv(t)
	tk := e.add("work", nil)
	claimed := e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))
	assert.Equal(t, "in_progress", claimed.Status)
	assert.Equal(t, "agt_a", claimed.ClaimedBy)
	assert.Equal(t, "todo", claimed.ClaimedFrom)
	_, err := e.agent("agt_b", "backlog.claim", map[string]any{"id": tk.ID})
	assert.ErrorContains(t, err, "in progress by agt_a")
	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))

	fresh := e.add("race", nil)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Go(func() {
			if _, err := e.agent(fmt.Sprintf("agt_%d", i), "backlog.claim", map[string]any{"id": fresh.ID}); err == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	assert.EqualValues(t, 1, wins.Load())
}

func TestOwnerRequiredWithoutCaller(t *testing.T) {
	e := newEnv(t)
	tk := e.add("work", nil)
	_, err := e.run("backlog.claim", "", true, map[string]any{"id": tk.ID})
	assert.ErrorContains(t, err, "pass owner")
	got := e.must(e.run("backlog.claim", "", true, map[string]any{"id": tk.ID, "owner": "term-1"}))
	assert.Equal(t, "term-1", got.ClaimedBy)
}

func TestReservedOwnerWithoutCaller(t *testing.T) {
	e := newEnv(t)
	tk := e.add("work", nil)
	for _, owner := range []string{"user", "agent", "agt_x"} {
		_, err := e.run("backlog.claim", "", true, map[string]any{"id": tk.ID, "owner": owner})
		assert.ErrorContains(t, err, "reserved", owner)
	}
}

func TestReleaseAndDonePermissions(t *testing.T) {
	e := newEnv(t)
	tk := e.must(e.agent("agt_a", "backlog.add", map[string]any{"title": "idea"}))
	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))
	_, err := e.agent("agt_b", "backlog.release", map[string]any{"id": tk.ID})
	assert.Error(t, err)
	released := e.must(e.user("backlog.release", map[string]any{"id": tk.ID}))
	assert.Equal(t, "idea", released.Status)
	assert.Equal(t, "agt_a", released.LastClaimedBy)
	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))
	done := e.must(e.agent("agt_a", "backlog.done", map[string]any{"id": tk.ID, "note": "merged abc"}))
	assert.Equal(t, "done", done.Status)
	assert.Equal(t, "merged abc", done.Note)
	assert.Empty(t, done.ClaimedBy)
}

func TestUpdateCannotStartWork(t *testing.T) {
	e := newEnv(t)
	tk := e.add("work", nil)
	_, err := e.user("backlog.update", map[string]any{"id": tk.ID, "status": "in_progress"})
	assert.Error(t, err)

	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))
	renamed := e.must(e.agent("agt_b", "backlog.update", map[string]any{"id": tk.ID, "title": "renamed"}))
	assert.Equal(t, "renamed", renamed.Title)
	_, err = e.agent("agt_b", "backlog.update", map[string]any{"id": tk.ID, "status": "not_now"})
	assert.Error(t, err)

	moved := e.must(e.user("backlog.update", map[string]any{"id": tk.ID, "status": "todo"}))
	assert.Equal(t, "todo", moved.Status)
	assert.Empty(t, moved.ClaimedBy)
	assert.Equal(t, "agt_a", moved.LastClaimedBy)
}

func TestProjectDeletedDropsTasks(t *testing.T) {
	e := newEnv(t)
	e.add("one", nil)
	e.add("two", nil)
	keys, err := e.h.KV.List(t.Context(), "backlog", "")
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "project.delete", map[string]string{"id": e.p}, nil))
	testutil.Eventually(t, func() bool {
		keys, err := e.h.KV.List(t.Context(), "backlog", "")
		if err != nil {
			return false
		}
		for _, k := range keys {
			if strings.Contains(k, e.p) {
				return false
			}
		}
		return true
	}, 5*time.Second)
}

func TestStaleClaimIsTakenOver(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	stale := e.add("stale", nil)
	e.must(e.agent("agt_dead", "backlog.claim", map[string]any{"id": stale.ID}))
	taken := e.must(e.agent("agt_live", "backlog.claim", map[string]any{"id": stale.ID}))
	assert.Equal(t, "agt_live", taken.ClaimedBy)
	assert.Equal(t, "agt_dead", taken.LastClaimedBy)

	held := e.add("held", nil)
	e.must(e.agent("agt_live", "backlog.claim", map[string]any{"id": held.ID}))
	_, err := e.agent("agt_dead", "backlog.claim", map[string]any{"id": held.ID})
	assert.ErrorContains(t, err, "in progress by agt_live")
}

func TestAncestorReleasesChildClaim(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	tk := e.add("child work", nil)
	e.must(e.agent("agt_child", "backlog.claim", map[string]any{"id": tk.ID}))
	_, err := e.agent("agt_live", "backlog.release", map[string]any{"id": tk.ID})
	assert.ErrorContains(t, err, "ancestor")
	assert.Equal(t, "todo", e.must(e.agent("agt_parent", "backlog.release", map[string]any{"id": tk.ID})).Status)
}

func TestClaimsLiveWithoutAgentsPlugin(t *testing.T) {
	e := newEnv(t)
	tk := e.add("work", nil)
	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))
	_, err := e.agent("agt_b", "backlog.claim", map[string]any{"id": tk.ID})
	assert.ErrorContains(t, err, "in progress by agt_a")
}

func TestAddFromSidebar(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": e.p}, nil))
	e.h.Event(t, e.p, "backlog.sidebar", "new", "add", map[string]any{"title": "From UI"})
	e.h.WaitSlot(t, e.p, "backlog.sidebar", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"#1 From UI"`)
	})
	e.h.WaitSlot(t, e.p, "backlog.main", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"id":"edit"`)
	})

	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": 1}))
	e.h.Event(t, e.p, "backlog.sidebar", "new", "add", map[string]any{"title": "Second"})
	e.h.Event(t, e.p, "backlog.main", "edit", "save.1", map[string]any{"title": "Renamed", "description": "more", "status": "todo", "priority": "high"})
	got := e.must(e.user("backlog.get", map[string]any{"id": 1}))
	assert.Equal(t, "Renamed", got.Title)
	assert.Equal(t, "high", got.Priority)
	assert.Equal(t, "agt_a", got.ClaimedBy)
}

func TestShowSessionRevealsTheClaimer(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": e.p}, nil))
	e.add("Task", nil)
	e.must(e.agent("agt_live", "backlog.claim", map[string]any{"id": 1}))
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", "1")
	e.h.WaitSlot(t, e.p, "backlog.main", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"id":"claimer"`)
	})

	e.h.Event(t, e.p, "backlog.main", "claimer", "reveal", nil)

	require.Eventually(t, func() bool {
		events, err := e.h.Bus.Read(t.Context(), "echo.command", 0, 100)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(ev bus.Event) bool {
			return strings.Contains(string(ev.Payload), `"id":"agents.reveal"`) && strings.Contains(string(ev.Payload), `"session":"agt_live"`)
		})
	}, 10*time.Second, 20*time.Millisecond)
}
