package main_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
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
	ID       int      `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Priority string   `json:"priority"`
	Tags     []string `json:"tags"`
	Links    []struct {
		Kind   string `json:"kind"`
		Task   int    `json:"task"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"links"`
	Attachments []map[string]string `json:"attachments"`
	Owners      []struct {
		Owner string `json:"owner"`
		Why   string `json:"why"`
	} `json:"owners"`
	ClaimedBy       string `json:"claimedBy"`
	ClaimedFrom     string `json:"claimedFrom"`
	LastClaimedBy   string `json:"lastClaimedBy"`
	Note            string `json:"note"`
	DescriptionHTML string `json:"descriptionHtml"`
	Primary         *struct {
		Action  string `json:"action"`
		Label   string `json:"label"`
		Summary string `json:"summary"`
		Session string `json:"session"`
	} `json:"primary"`
}

type env struct {
	h *testutil.PluginHarness
	p string
	t *testing.T
}

const agentsManifest = `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"agents.sessions","title":"Sessions","hidden":true},{"id":"agents.reveal","title":"Reveal","hidden":true},{"id":"agents.new","title":"New","hidden":true}]},"echoResults":{"agents.reveal":null,"agents.new":{"session":"agt_new"},"agents.sessions":[{"id":"agt_dead","status":"closed"},{"id":"agt_live","status":"running"},{"id":"agt_parent","status":"running"},{"id":"agt_child","status":"running","parentSession":"agt_parent"}]}}`

func newEnv(t *testing.T, extra ...string) env {
	dirs := append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/backlog", "plugins/backlog/plugin.json")}, extra...)
	h := testutil.NewPluginHarness(t, dirs...)
	require.Eventually(t, func() bool {
		events, err := h.Bus.Read(t.Context(), "backlog.ready", 0, 1)
		return err == nil && len(events) == 1
	}, 10*time.Second, 20*time.Millisecond)
	e := env{h: h, p: h.CreateWorkspace(t, "demo").ID, t: t}
	if len(extra) > 0 {
		require.Eventually(t, func() bool {
			return h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "agents.sessions", "workspaceId": e.p, "args": map[string]any{}}, nil) == nil
		}, 10*time.Second, 20*time.Millisecond)
	}
	return e
}

func (e env) run(id, caller string, agent bool, args map[string]any) (task, error) {
	var out task
	err := e.h.Client(e.t).Call(e.t.Context(), "command.run", map[string]any{"id": id, "workspaceId": e.p, "args": args, "agent": agent, "caller": caller}, &out)
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
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "backlog.list", "workspaceId": e.p, "args": map[string]any{"status": "open"}}, &list))
	var ids []int
	for _, tk := range list.Tasks {
		ids = append(ids, tk.ID)
	}
	assert.Equal(t, []int{high.ID, normal.ID, low.ID}, ids)

	assert.Equal(t, "idea", e.must(e.agent("agt_a", "backlog.add", map[string]any{"title": "agent task"})).Status)
	assert.Equal(t, "todo", e.must(e.agent("agt_a", "backlog.add", map[string]any{"title": "x", "status": "todo"})).Status)
	_, err := e.user("backlog.add", map[string]any{"title": "a\nb"})
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

	moved := e.must(e.agent("agt_a", "backlog.update", map[string]any{"id": tk.ID, "status": "todo"}))
	assert.Equal(t, "todo", moved.Status)
	assert.Empty(t, moved.ClaimedBy)
	assert.Equal(t, "agt_a", moved.LastClaimedBy)
}

func TestWorkspaceDeletedDropsTasks(t *testing.T) {
	e := newEnv(t)
	e.add("one", nil)
	e.add("two", nil)
	keys, err := e.h.KV.List(t.Context(), "backlog", "")
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "workspace.delete", map[string]string{"id": e.p}, nil))
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
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	e.h.Event(t, e.p, "backlog.sidebar", "new", "add", map[string]any{"title": "From UI"})
	e.h.WaitSlot(t, e.p, "backlog.sidebar", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"label":"From UI"`)
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

func TestTags(t *testing.T) {
	e := newEnv(t)
	voice := e.add("Mic", map[string]any{"tags": []string{" Voice ", "#phone", "voice"}})
	assert.Equal(t, []string{"voice", "phone"}, voice.Tags)
	e.add("Other", nil)
	var list struct {
		Tasks []task `json:"tasks"`
	}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "backlog.list", "workspaceId": e.p, "args": map[string]any{"tag": "voice"}}, &list))
	require.Len(t, list.Tasks, 1)
	assert.Equal(t, voice.ID, list.Tasks[0].ID)

	assert.Equal(t, []string{"voice", "phone"}, e.must(e.agent("agt_a", "backlog.update", map[string]any{"id": voice.ID, "note": "x"})).Tags)
	_, err := e.agent("agt_a", "backlog.update", map[string]any{"id": voice.ID, "tags": []string{"two words"}})
	assert.ErrorContains(t, err, "one word")
	assert.Empty(t, e.must(e.agent("agt_a", "backlog.update", map[string]any{"id": voice.ID, "tags": []string{}})).Tags)

	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", "1")
	e.h.Event(t, e.p, "backlog.main", "edit", "save.1", map[string]any{"title": "Mic", "tagsText": "ui, Mac  ui"})
	e.h.WaitSlot(t, e.p, "backlog.sidebar", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"id":"tag:mac"`)
	})
	assert.Equal(t, []string{"ui", "mac"}, e.must(e.user("backlog.get", map[string]any{"id": 1})).Tags)
}

func TestEmptyTitleIsAFieldError(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	e.add("Keep me", nil)
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", "1")

	e.h.Event(t, e.p, "backlog.main", "edit", "save.1", map[string]any{"title": " ", "priority": "high"})

	e.h.WaitSlot(t, e.p, "backlog.main", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"error":"Title must not be empty"`)
	})
	got := e.must(e.user("backlog.get", map[string]any{"id": 1}))
	assert.Equal(t, "Keep me", got.Title)
}

func TestShowSessionRevealsTheClaimer(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	e.add("Task", nil)
	e.must(e.agent("agt_live", "backlog.claim", map[string]any{"id": 1}))
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", "1")
	e.h.WaitSlot(t, e.p, "backlog.main", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), `"label":"Open agt_live"`)
	})

	e.h.Event(t, e.p, "backlog.main", "open", "reveal", nil)

	require.Eventually(t, func() bool {
		events, err := e.h.Bus.Read(t.Context(), "echo.command", 0, 100)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(ev bus.Event) bool {
			return strings.Contains(string(ev.Payload), `"id":"agents.reveal"`) && strings.Contains(string(ev.Payload), `"session":"agt_live"`)
		})
	}, 10*time.Second, 20*time.Millisecond)
}

func TestDescriptionRendersAsDocument(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	e.add("Table", map[string]any{"description": "| a | b |\n|---|---|\n| 1 | 2 |"})
	assert.Contains(t, e.must(e.user("backlog.get", map[string]any{"id": 1})).DescriptionHTML, "<table>")
	assert.Empty(t, e.must(e.agent("agt_a", "backlog.get", map[string]any{"id": 1})).DescriptionHTML)

	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", "1")
	slotHas := func(text string) {
		e.h.WaitSlot(t, e.p, "backlog.main", func(n sdui.Node) bool {
			raw, err := json.Marshal(n)
			return err == nil && strings.Contains(string(raw), text)
		})
	}
	slotHas(`\u003ctable\u003e`)
	e.must(e.user("backlog.update", map[string]any{"id": 1, "description": "**bold**"}))
	slotHas(`\u003cstrong\u003ebold`)
	e.h.Event(t, e.p, "backlog.main", "describe", "describe", nil)
	slotHas(`"label":"Done editing"`)
}

func (e env) slotHas(slot, text string) {
	e.h.WaitSlot(e.t, e.p, slot, func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), text)
	})
}

func (e env) echoed(id, text string) bool {
	events, err := e.h.Bus.Read(e.t.Context(), "echo.command", 0, 100)
	require.NoError(e.t, err)
	return slices.ContainsFunc(events, func(ev bus.Event) bool {
		return strings.Contains(string(ev.Payload), `"id":"`+id+`"`) && strings.Contains(string(ev.Payload), text)
	})
}

func (e env) owners(id int) []string {
	var out []string
	for _, m := range e.must(e.user("backlog.get", map[string]any{"id": id})).Owners {
		out = append(out, m.Owner+" "+m.Why)
	}
	return out
}

func TestLinksStaySymmetric(t *testing.T) {
	e := newEnv(t)
	a, b, c := e.add("A", nil), e.add("B", nil), e.add("C", nil)
	e.must(e.agent("agt_a", "backlog.update", map[string]any{"id": a.ID, "links": []map[string]any{{"kind": "blocks", "task": b.ID}, {"kind": "relates", "task": c.ID}}}))
	got := e.must(e.user("backlog.get", map[string]any{"id": b.ID}))
	require.Len(t, got.Links, 1)
	assert.Equal(t, "blocked_by", got.Links[0].Kind)
	assert.Equal(t, "A", got.Links[0].Title)
	assert.Equal(t, "todo", got.Links[0].Status)
	assert.Equal(t, "relates", e.must(e.user("backlog.get", map[string]any{"id": c.ID})).Links[0].Kind)

	e.must(e.user("backlog.update", map[string]any{"id": b.ID, "links": []map[string]any{}}))
	assert.Len(t, e.must(e.user("backlog.get", map[string]any{"id": a.ID})).Links, 1)

	_, err := e.user("backlog.update", map[string]any{"id": a.ID, "links": []map[string]any{{"kind": "blocks", "task": 99}}})
	assert.ErrorContains(t, err, "#99")
	_, err = e.user("backlog.update", map[string]any{"id": a.ID, "links": []map[string]any{{"kind": "blocks", "task": a.ID}}})
	assert.ErrorContains(t, err, "itself")

	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "delete", strconv.Itoa(c.ID))
	require.Eventually(t, func() bool {
		return len(e.must(e.user("backlog.get", map[string]any{"id": a.ID})).Links) == 0
	}, 10*time.Second, 20*time.Millisecond)
}

func TestAttachmentsValidated(t *testing.T) {
	e := newEnv(t)
	tk := e.add("Files", map[string]any{"attachments": []map[string]any{{"kind": "file", "ref": "design/check/fix-list.md"}, {"kind": "url", "ref": "https://example.com/a", "title": "Spec"}}})
	assert.Equal(t, []map[string]string{{"kind": "file", "ref": "design/check/fix-list.md"}, {"kind": "url", "ref": "https://example.com/a", "title": "Spec"}}, tk.Attachments)
	for _, bad := range []map[string]any{{"kind": "file", "ref": "/etc/passwd"}, {"kind": "file", "ref": "../x"}, {"kind": "url", "ref": "ftp://x"}} {
		_, err := e.user("backlog.update", map[string]any{"id": tk.ID, "attachments": []map[string]any{bad}})
		assert.Error(t, err, bad)
	}
}

func TestOwnerMovesAreRecorded(t *testing.T) {
	e := newEnv(t)
	tk := e.add("Work", nil)
	e.must(e.agent("agt_a", "backlog.claim", map[string]any{"id": tk.ID}))
	e.must(e.agent("agt_a", "backlog.release", map[string]any{"id": tk.ID}))
	e.must(e.user("backlog.claim", map[string]any{"id": tk.ID}))
	e.must(e.user("backlog.done", map[string]any{"id": tk.ID}))
	assert.Equal(t, []string{"user created", "agt_a claimed", "user released"}, e.owners(tk.ID))
}

func TestStartAgentHandsTheTaskToASession(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	dep := e.add("Record missed runs", nil)
	tk := e.add("Show missed runs", map[string]any{"description": "Builds on #1.", "links": []map[string]any{{"kind": "blocked_by", "task": dep.ID}}, "attachments": []map[string]any{{"kind": "file", "ref": "design/check/fix-list.md"}}})
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", strconv.Itoa(tk.ID))
	e.slotHas("backlog.main", `"text":"description, 1 link, 1 file"`)

	e.h.Event(t, e.p, "backlog.main", "start", "start", nil)

	require.Eventually(t, func() bool {
		return e.echoed("agents.new", `Builds on #1.`) && e.echoed("agents.new", `blocked by #1 Record missed runs (todo)`) && e.echoed("agents.new", `file design/check/fix-list.md`)
	}, 10*time.Second, 20*time.Millisecond)
	got := e.must(e.user("backlog.get", map[string]any{"id": tk.ID}))
	assert.Equal(t, "in_progress", got.Status)
	assert.Equal(t, "agt_new", got.ClaimedBy)
	assert.Equal(t, []string{"user created", "agt_new started"}, e.owners(tk.ID))
}

func TestStartAgentOptions(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	tk := e.add("Fix the flaky switcher test", map[string]any{"description": "Fails on CI."})
	_, err := e.user("backlog.start", map[string]any{"id": tk.ID, "mode": "yolo"})
	assert.ErrorContains(t, err, "args.mode")
	err = e.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": e.p, "slot": "backlog.main", "nodeId": "start-1", "action": "start", "value": map[string]any{"mode": "bypassPermissions"}, "agent": true}, nil)
	assert.ErrorContains(t, err, `"start" is the user's`)
	err = e.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": e.p, "slot": "backlog.sidebar", "nodeId": "tasks", "action": "start.x", "value": "1", "agent": true}, nil)
	assert.ErrorContains(t, err, `"start.x" is the user's`)

	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", strconv.Itoa(tk.ID))
	e.h.Event(t, e.p, "backlog.main", "start-edit", "editPrompt", nil)
	e.slotHas("backlog.main", `"id":"prompt"`)
	e.h.Event(t, e.p, "backlog.main", "start-1", "start", map[string]any{"agent": "claude", "mode": "plan", "where": "worktree", "prompt": "Only the CI part."})

	require.Eventually(t, func() bool {
		return e.echoed("agents.new", `"firstMode":"plan"`) && e.echoed("agents.new", `"branch":"backlog/1-fix-the-flaky-switcher-test"`) && e.echoed("agents.new", `Only the CI part.\n\nYou own backlog task #1.`)
	}, 10*time.Second, 20*time.Millisecond)
	assert.False(t, e.echoed("agents.new", "Fails on CI."))
}

func TestTakeBack(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	tk := e.add("Work", nil)
	e.must(e.agent("agt_live", "backlog.claim", map[string]any{"id": tk.ID}))
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", strconv.Itoa(tk.ID))

	e.h.Event(t, e.p, "backlog.main", "takeback", "takeback", nil)

	got := e.must(e.user("backlog.get", map[string]any{"id": tk.ID}))
	assert.Equal(t, "in_progress", got.Status)
	assert.Equal(t, "user", got.ClaimedBy)
	assert.Equal(t, "agt_live", got.LastClaimedBy)
	assert.Equal(t, []string{"user created", "agt_live claimed", "user taken back"}, e.owners(tk.ID))

	e.h.Event(t, e.p, "backlog.main", "owners", "openMove", "agt_live")
	require.Eventually(t, func() bool { return e.echoed("agents.reveal", `"session":"agt_live"`) }, 10*time.Second, 20*time.Millisecond)
}

func TestPhoneStartsAndTakesBack(t *testing.T) {
	e := newEnv(t, testutil.EchoPluginDirAs(t, agentsManifest))
	tk := e.add("Work", map[string]any{"description": "Builds on #1."})
	got := e.must(e.user("backlog.get", map[string]any{"id": tk.ID}))
	require.NotNil(t, got.Primary)
	assert.Equal(t, "start", got.Primary.Action)
	assert.Equal(t, "description", got.Primary.Summary)
	assert.Contains(t, got.DescriptionHTML, `href="clubmate-doc:/task/1"`)
	assert.Nil(t, e.must(e.agent("agt_a", "backlog.get", map[string]any{"id": tk.ID})).Primary)

	_, err := e.agent("agt_a", "backlog.start", map[string]any{"id": tk.ID})
	assert.Error(t, err)
	var started struct {
		Session string `json:"session"`
	}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "backlog.start", "workspaceId": e.p, "args": map[string]any{"id": tk.ID}}, &started))
	assert.Equal(t, "agt_new", started.Session)
	assert.Equal(t, "agt_new", e.must(e.user("backlog.get", map[string]any{"id": tk.ID})).ClaimedBy)

	other := e.add("Other", nil)
	e.must(e.agent("agt_live", "backlog.claim", map[string]any{"id": other.ID}))
	got = e.must(e.user("backlog.get", map[string]any{"id": other.ID}))
	require.NotNil(t, got.Primary)
	assert.Equal(t, "reveal", got.Primary.Action)
	assert.Equal(t, "agt_live", got.Primary.Session)

	assert.Equal(t, "user", e.must(e.user("backlog.takeback", map[string]any{"id": other.ID})).ClaimedBy)
	assert.Equal(t, "start", e.must(e.user("backlog.get", map[string]any{"id": other.ID})).Primary.Action)
}

func TestClosedSessionHandsTheTaskBack(t *testing.T) {
	e := newEnv(t)
	tk := e.add("Work", nil)
	e.must(e.agent("agt_gone", "backlog.claim", map[string]any{"id": tk.ID}))

	_, err := e.h.Bus.Publish(t.Context(), "agents.changed", "test", json.RawMessage(`{"workspace":"`+e.p+`","session":"agt_gone","kind":"status","status":"closed"}`))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return e.must(e.user("backlog.get", map[string]any{"id": tk.ID})).Status == "todo"
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, []string{"user created", "agt_gone claimed", "user stopped"}, e.owners(tk.ID))
}

func TestLinksFromTheForm(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": e.p}, nil))
	dep := e.add("Dep", nil)
	tk := e.add("Main", map[string]any{"description": "See #1"})
	e.h.Event(t, e.p, "backlog.sidebar", "tasks", "select", strconv.Itoa(tk.ID))
	e.slotHas("backlog.main", `href=\"clubmate-doc:/task/1\"`)

	e.h.Event(t, e.p, "backlog.main", "linkText", "link", "blocks #1")
	e.slotHas("backlog.main", `"label":"#1 Dep"`)
	e.h.Event(t, e.p, "backlog.main", "linkText", "link", "blocks nothing")
	e.slotHas("backlog.main", `"error":"Write a link as blocks #3, blocked by #5 or relates #7"`)
	e.h.Event(t, e.p, "backlog.main", "attachText", "attach", "design/a.md")
	e.slotHas("backlog.main", `"label":"design/a.md"`)

	e.h.Event(t, e.p, "backlog.main", "doc", "openDoc", "/task/1")
	e.slotHas("backlog.main", `"title":"Dep"`)
	assert.Equal(t, "blocked_by", e.must(e.user("backlog.get", map[string]any{"id": dep.ID})).Links[0].Kind)
}
