package main_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/pluginmigrate"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	checkedGraph = `{"title":"Checked","nodes":[{"id":"lead","role":"Lead","start":true,"prompt":"done"},{"id":"check","role":"Check","run":"true"}],"edges":[{"from":"lead","to":"check","on":"done"}]}`
	checkedHash  = "5f899172758d47d4cd24aff63a4e9f694cc05ae50ed4c4bb738255b0ea99a856"
)

type graphList struct {
	Graphs []struct {
		Name      string `json:"name"`
		Untrusted bool   `json:"untrusted"`
	} `json:"graphs"`
	Runs []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"runs"`
}

type graphRun struct {
	ID    string `json:"id"`
	Nodes []struct {
		ID      string `json:"id"`
		Session string `json:"session"`
		Status  string `json:"status"`
		Last    *struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
			Output string `json:"output"`
		} `json:"last"`
	} `json:"nodes"`
}

func graphRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	write(t, filepath.Join(repo, ".clubmate", "graphs", "checked.json"), checkedGraph)
	return repo
}

func seedRow(t *testing.T, h *testutil.PluginHarness, plugin, key string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, h.KV.Set(t.Context(), plugin, key, raw))
}

func repoKey(prefix, root, id string) string {
	sum := sha256.Sum256([]byte(root + "\n" + id))
	return prefix + hex.EncodeToString(sum[:])
}

func startMigrated(t *testing.T, repo string, seed func(h *testutil.PluginHarness, workspace string), dirs ...string) (*fixture, string) {
	t.Helper()
	var workspace string
	f := startSeeded(t, nil, map[string]any{"prompt": []map[string]any{{"echo": true}}}, func(h *testutil.PluginHarness) {
		p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{repo})
		require.NoError(t, err)
		workspace = p.ID
		seed(h, workspace)
		require.NoError(t, pluginmigrate.Graphs(t.Context(), h.KV, h.Dirs))
	}, dirs...)
	f.subscribe(t, workspace)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	return f, workspace
}

func TestGraphCommandsAnswerOldIds(t *testing.T) {
	t.Parallel()
	repo := graphRepo(t)
	write(t, filepath.Join(repo, ".clubmate", "graphs", "solo.json"), `{"title":"Solo","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"}]}`)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	require.NoError(t, f.run(t, workspace, "graphs.list", map[string]any{}, nil))
	var run graphRun
	require.NoError(t, f.run(t, workspace, "graphs.run", map[string]any{"graph": "solo"}, &run))
	assert.Regexp(t, `^gr_`, run.ID)
	require.Len(t, run.Nodes, 1)
	var sessions []struct {
		ID  string `json:"id"`
		Run *struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &sessions))
	i := slices.IndexFunc(sessions, func(s struct {
		ID  string `json:"id"`
		Run *struct {
			ID string `json:"id"`
		} `json:"run"`
	}) bool {
		return s.ID == run.Nodes[0].Session
	})
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, run.ID, sessions[i].Run.ID)
}

func TestUpdateKeepsRunsTrustAndJobs(t *testing.T) {
	t.Parallel()
	repo := graphRepo(t)
	f, workspace := startMigrated(t, repo, func(h *testutil.PluginHarness, workspace string) {
		seedRow(t, h, "graphs", "run/"+workspace+"/gr_0123456789abcdef", map[string]any{"id": "gr_0123456789abcdef", "graph": "checked", "title": "Checked", "workspace": workspace, "status": "stopped", "reason": "stopped", "maxTurns": 20, "startedAt": time.Now().Add(-time.Hour), "nodes": []any{}, "edges": []any{}})
		seedRow(t, h, "graphs", repoKey("trust/", repo, checkedHash), true)
		seedRow(t, h, "cron", "job/"+workspace+"/cj_0123456789abcdef", map[string]any{"id": "cj_0123456789abcdef", "name": "nightly", "workspace": workspace, "schedule": map[string]any{"every": "24h"}, "command": "graphs.run", "args": map[string]any{"graph": "checked"}, "enabled": true, "missed": "skip", "nextRun": time.Now().Add(24 * time.Hour)})
	}, testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
	var list graphList
	require.NoError(t, f.run(t, workspace, "agents.graph-list", map[string]any{}, &list))
	require.Len(t, list.Runs, 1)
	assert.Equal(t, "gr_0123456789abcdef", list.Runs[0].ID)
	i := slices.IndexFunc(list.Graphs, func(g struct {
		Name      string `json:"name"`
		Untrusted bool   `json:"untrusted"`
	}) bool {
		return g.Name == "checked"
	})
	require.GreaterOrEqual(t, i, 0)
	assert.False(t, list.Graphs[i].Untrusted)
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "checked"}, nil))
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "cron.list", map[string]any{}, nil) == nil }, restartWait)
	var fired struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	require.NoError(t, f.run(t, workspace, "cron.run", map[string]any{"id": "cj_0123456789abcdef"}, &fired))
	assert.Equal(t, "ok", fired.Status, fired.Reason)
	require.NoError(t, f.run(t, workspace, "agents.graph-list", map[string]any{}, &list))
	assert.Len(t, list.Runs, 3)
}

func TestRecoveredStepAfterMigration(t *testing.T) {
	t.Parallel()
	repo := graphRepo(t)
	var old string
	f, workspace := startMigrated(t, repo, func(h *testutil.PluginHarness, workspace string) {
		old = filepath.Join(h.Dirs.PluginState("graphs"), "output", "gr_0123456789abcdef", "1.log")
		require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o700))
		require.NoError(t, os.WriteFile(old, []byte("half\n"), 0o600))
		started := time.Now().Add(-time.Minute)
		seedRow(t, h, "graphs", "run/"+workspace+"/gr_0123456789abcdef", map[string]any{
			"id": "gr_0123456789abcdef", "graph": "checked", "title": "Checked", "workspace": workspace, "status": "running", "maxTurns": 20, "startedAt": started,
			"nodes": []any{map[string]any{"id": "lead", "role": "Lead", "start": true, "session": ""}, map[string]any{"id": "check", "role": "Check", "step": true}},
			"edges": []any{},
			"moves": []any{map[string]any{"node": "check", "cause": -1, "edge": -1, "at": started, "open": true, "step": map[string]any{"status": "running", "command": "true", "output": old, "startedAt": started}}},
		})
	})
	moved := filepath.Join(f.h.Dirs.PluginState("agents"), "graphs", "output", "gr_0123456789abcdef", "1.log")
	testutil.Eventually(t, func() bool {
		var run graphRun
		if f.run(t, workspace, "agents.graph-show", map[string]any{"id": "gr_0123456789abcdef"}, &run) != nil {
			return false
		}
		i := slices.IndexFunc(run.Nodes, func(n struct {
			ID      string `json:"id"`
			Session string `json:"session"`
			Status  string `json:"status"`
			Last    *struct {
				Status string `json:"status"`
				Reason string `json:"reason"`
				Output string `json:"output"`
			} `json:"last"`
		}) bool {
			return n.ID == "check"
		})
		return i >= 0 && run.Nodes[i].Last != nil && run.Nodes[i].Last.Status == "finished" && run.Nodes[i].Last.Output == moved
	}, wait)
	data, err := os.ReadFile(moved)
	require.NoError(t, err)
	assert.Equal(t, "half\n", string(data))
}

func TestTriggerFlowFromRunEdges(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	write(t, filepath.Join(repo, ".clubmate", "graphs", "pair.json"), `{"title":"Pair","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"},{"id":"two","role":"Two","prompt":"done"}],"edges":[{"from":"one","to":"two","on":"done"}]}`)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}}, testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, repo)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	var run graphRun
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "pair"}, &run))
	f.waitStatus(t, workspace, run.Nodes[1].Session, "done")
	var listed struct {
		Triggers []struct {
			ID  string `json:"id"`
			Run string `json:"run"`
		} `json:"triggers"`
	}
	require.NoError(t, f.run(t, workspace, "triggers.list", map[string]any{}, &listed))
	require.Len(t, listed.Triggers, 1)
	edge := listed.Triggers[0]
	assert.Equal(t, run.ID, edge.Run)
	testutil.Eventually(t, func() bool {
		f.h.Event(t, workspace, "triggers.sidebar", "triggers", "select", edge.ID)
		return strings.Contains(f.slotTree(t, workspace, "triggers.main"), edge.ID+"/caption")
	}, wait)
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": run.ID}, nil))
	require.NoError(t, f.run(t, workspace, "triggers.list", map[string]any{}, &listed))
	assert.Empty(t, listed.Triggers)
}

func TestRunRowEmptiedAfterSessionsDeleted(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	write(t, filepath.Join(repo, ".clubmate", "graphs", "solo.json"), `{"title":"Solo","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"}]}`)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	var run graphRun
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "solo"}, &run))
	f.waitStatus(t, workspace, run.Nodes[0].Session, "done")
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": run.ID}, nil))
	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": run.Nodes[0].Session}, nil))
	type row struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Home  string `json:"home"`
	}
	testutil.Eventually(t, func() bool {
		var page struct {
			Runs []row `json:"runs"`
		}
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": ""}, &page))
		return slices.Contains(page.Runs, row{ID: run.ID, State: "emptied", Home: "closed"})
	}, wait)
	assert.NoError(t, f.run(t, workspace, "agents.graph-show", map[string]any{"id": run.ID}, nil))
}

func runPageFixture(t *testing.T) (*fixture, string, graphRun) {
	t.Helper()
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	write(t, filepath.Join(repo, ".clubmate", "graphs", "pair.json"), `{"title":"Pair","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"},{"id":"two","role":"Two","prompt":"done"}]}`)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	var run graphRun
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "pair"}, &run))
	return f, workspace, run
}

func buttonWith(n sdui.Node, shortcut string) bool {
	for _, b := range cardsOf(n, "button") {
		if b.Props["shortcut"] == shortcut {
			return true
		}
	}
	return false
}

func TestMainShowsRunPageForRunSelection(t *testing.T) {
	t.Parallel()
	f, workspace, run := runPageFixture(t)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", run.Nodes[0].Session)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "run:"+run.ID)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		raw, _ := json.Marshal(n)
		return strings.Contains(string(raw), "Agents / main / Pair") && buttonWith(n, "cmd+[")
	})
}

func TestBackReturnsToSession(t *testing.T) {
	t.Parallel()
	f, workspace, run := runPageFixture(t)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", run.Nodes[1].Session)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "run:"+run.ID)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return buttonWith(n, "cmd+[") })
	f.h.Event(t, workspace, "agents.main", "back", "back", nil)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		trees := cardsOf(n, "tree")
		return len(trees) > 0 && trees[0].Props["selected"] == run.Nodes[1].Session
	})
}

func TestRemoveRunOnlyWhenEnded(t *testing.T) {
	t.Parallel()
	f, workspace, run := runPageFixture(t)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "run:"+run.ID)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return buttonWith(n, "cmd+.") && !buttonWith(n, "cmd+delete") })
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": run.ID}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return buttonWith(n, "cmd+delete") })
}

func TestRevealSelectsRunInAgents(t *testing.T) {
	t.Parallel()
	f, workspace, run := runPageFixture(t)
	require.NoError(t, f.run(t, workspace, "agents.graph-reveal", map[string]any{"id": run.ID}, nil))
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		trees := cardsOf(n, "tree")
		return len(trees) > 0 && trees[0].Props["selected"] == "run:"+run.ID
	})
}

func TestGraphsAndGatesNotOnRail(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	var m struct {
		Contributes struct {
			Sections []struct {
				ID   string `json:"id"`
				Rail *bool  `json:"rail"`
			} `json:"sections"`
		} `json:"contributes"`
	}
	require.NoError(t, json.Unmarshal(raw, &m))
	off := map[string]bool{}
	for _, s := range m.Contributes.Sections {
		off[s.ID] = s.Rail != nil && !*s.Rail
	}
	assert.Equal(t, map[string]bool{"agents": false, "agents.graphs": true, "agents.gates": true}, off)
}

func TestTrustSheetOnDefinitionPage(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "graph:review-gate")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "header")) > 0 })
	f.h.Event(t, workspace, "agents.main", "graph:review-gate/trust", "trust", nil)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "sheet")) == 1 })
}

func TestGraphTurnGetsItsSummary(t *testing.T) {
	t.Parallel()
	bin := t.TempDir()
	write(t, filepath.Join(bin, "claude"), "#!/bin/sh\ncat >/dev/null\necho \"The agent ticked twice.\"\n")
	require.NoError(t, os.Chmod(filepath.Join(bin, "claude"), 0o755))
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	write(t, filepath.Join(repo, ".clubmate", "graphs", "solo.json"), `{"title":"Solo","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"}]}`)
	f := startEnv(t, []string{"PATH=" + bin + ":" + os.Getenv("PATH")}, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	var run graphRun
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "solo"}, &run))
	testutil.Eventually(t, func() bool {
		var shown json.RawMessage
		return f.run(t, workspace, "agents.graph-show", map[string]any{"id": run.ID}, &shown) == nil && strings.Contains(string(shown), "ticked twice")
	}, wait)
}

func TestRemovedRunLeavesItsPage(t *testing.T) {
	t.Parallel()
	f, workspace, run := runPageFixture(t)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", run.Nodes[0].Session)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", "run:"+run.ID)
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": run.ID}, nil))
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "removeRun", "run:"+run.ID)
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		trees := cardsOf(n, "tree")
		return len(trees) > 0 && trees[0].Props["selected"] == run.Nodes[0].Session
	})
}

func TestRunChangePublishesAgentsChanged(t *testing.T) {
	t.Parallel()
	f, workspace, run := runPageFixture(t)
	f.waitStatus(t, workspace, run.Nodes[0].Session, "done")
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": run.ID}, nil))
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "agents.changed", 0, 1000)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(ev bus.Event) bool {
			var p struct {
				Session string `json:"session"`
			}
			return json.Unmarshal(ev.Payload, &p) == nil && p.Session == run.ID
		})
	}, wait)
}

func TestTriggerFlowFromPreUpdateRun(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}}, testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	one, two := f.newSession(t, workspace), f.newSession(t, workspace)
	const run, edge = "gr_0123456789abcdef", "tr_0123456789abcdef"
	seedRow(t, f.h, "graphs", "run/"+workspace+"/"+run, map[string]any{
		"id": run, "graph": "pair", "title": "Pair", "workspace": workspace, "status": "running", "maxTurns": 10, "startedAt": time.Now(),
		"nodes": []any{map[string]any{"id": "one", "role": "One", "start": true, "session": one}, map[string]any{"id": "two", "role": "Two", "session": two}},
		"edges": []any{map[string]any{"from": "one", "to": "two", "on": "done", "trigger": edge}},
	})
	seedRow(t, f.h, "triggers", "trigger/"+workspace+"/"+edge, map[string]any{
		"id": edge, "name": "Pair: One done → Two", "workspace": workspace, "on": "agents.turn", "match": map[string]any{"session": one, "status": "done"},
		"command": "agents.prompt", "args": map[string]any{"session": two, "text": "your turn"}, "enabled": true, "run": run, "runTitle": "Pair",
	})
	require.NoError(t, pluginmigrate.Graphs(t.Context(), f.h.KV, f.h.Dirs))
	require.NoError(t, f.h.Sup.Restart("agents"))
	require.NoError(t, f.h.Sup.Restart("triggers"))
	testutil.Eventually(t, func() bool {
		return f.run(t, workspace, "agents.graph-show", map[string]any{"id": run}, nil) == nil && f.run(t, workspace, "triggers.show", map[string]any{"id": edge}, nil) == nil
	}, restartWait)
	testutil.Eventually(t, func() bool {
		f.h.Event(t, workspace, "triggers.sidebar", "triggers", "select", edge)
		return strings.Contains(f.slotTree(t, workspace, "triggers.main"), edge+"/caption")
	}, wait)
	f.prompt(t, workspace, one, "done")
	f.waitStatus(t, workspace, two, "done")
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": run}, nil))
	var listed struct {
		Triggers []struct {
			ID string `json:"id"`
		} `json:"triggers"`
	}
	require.NoError(t, f.run(t, workspace, "triggers.list", map[string]any{}, &listed))
	assert.Empty(t, listed.Triggers)
}
