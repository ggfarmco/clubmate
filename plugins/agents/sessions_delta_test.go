package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type sessionsPage struct {
	Sessions []struct {
		ID     string `json:"id"`
		Notify string `json:"notify"`
		Older  bool   `json:"older"`
	} `json:"sessions"`
	Removed    []string `json:"removed"`
	Cursor     string   `json:"cursor"`
	Delta      bool     `json:"delta"`
	OlderCount int      `json:"olderCount"`
}

func (p sessionsPage) ids() []string {
	out := []string{}
	for _, s := range p.Sessions {
		out = append(out, s.ID)
	}
	return out
}

func TestSessionsSinceReturnsOnlyChangedAndRemovedSessions(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	kept := f.newSession(t, workspace)
	muted := f.newSession(t, workspace)
	deleted := f.newSession(t, workspace)
	for _, id := range []string{kept, muted, deleted} {
		f.waitStarted(t, workspace, id)
	}
	page := func(since string) sessionsPage {
		var p sessionsPage
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": since}, &p))
		return p
	}
	full := page("")
	assert.False(t, full.Delta)
	assert.Equal(t, []string{kept, muted, deleted}, full.ids())
	assert.Empty(t, page(full.Cursor).ids())

	require.NoError(t, f.run(t, workspace, "agents.notify", map[string]any{"session": muted, "policy": "muted"}, nil))
	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": deleted}, nil))

	changed := page(full.Cursor)
	assert.True(t, changed.Delta)
	assert.Equal(t, []string{muted}, changed.ids())
	assert.Equal(t, "muted", changed.Sessions[0].Notify)
	assert.Equal(t, []string{deleted}, changed.Removed)
	assert.Empty(t, page(changed.Cursor).ids())
}

func TestAClosedSessionLeavesTheDefaultListWhenItBecomesOlderAndComesBackWhenResumed(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_OLDER_AFTER=1s"}, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	kept, old := f.titledSession(t, workspace, "find kept"), f.titledSession(t, workspace, "find old")
	f.waitStarted(t, workspace, old)
	page := func(args map[string]any) sessionsPage {
		var p sessionsPage
		require.NoError(t, f.run(t, workspace, "agents.sessions", args, &p))
		return p
	}
	cursor := page(map[string]any{"since": ""}).Cursor
	f.close(t, workspace, old)
	var crossed sessionsPage
	testutil.Eventually(t, func() bool {
		crossed = page(map[string]any{"since": cursor})
		cursor = crossed.Cursor
		return slices.Contains(crossed.Removed, old)
	}, wait)
	assert.NotContains(t, crossed.ids(), old)
	assert.Equal(t, map[string]bool{kept: false, old: true}, f.searchOlder(t, workspace, "find"))

	full := page(map[string]any{"since": ""})
	assert.Equal(t, []string{kept}, full.ids())
	assert.Equal(t, 1, full.OlderCount)
	all := page(map[string]any{"since": "", "older": true})
	assert.Equal(t, []string{kept, old}, all.ids())
	assert.True(t, all.Sessions[1].Older)
	one := page(map[string]any{"since": "", "session": old})
	assert.Equal(t, []string{old}, one.ids())
	var older struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
		Count int `json:"count"`
	}
	require.NoError(t, f.run(t, workspace, "agents.sessions-older", map[string]any{}, &older))
	assert.Equal(t, 1, older.Count)
	require.Len(t, older.Sessions, 1)
	assert.Equal(t, old, older.Sessions[0].ID)

	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": old}, nil))
	var back sessionsPage
	testutil.Eventually(t, func() bool {
		back = page(map[string]any{"since": cursor})
		return slices.Equal(back.ids(), []string{old})
	}, wait)
	assert.False(t, back.Sessions[0].Older)
	assert.Zero(t, back.OlderCount)
}

func TestOlderAfterZeroTurnsOlderOff(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_OLDER_AFTER=0"}, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	old := f.titledSession(t, workspace, "find old")
	f.waitStarted(t, workspace, old)
	f.close(t, workspace, old)

	var p sessionsPage
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": ""}, &p))
	assert.Equal(t, []string{old}, p.ids())
	assert.False(t, p.Sessions[0].Older)
	assert.Zero(t, p.OlderCount)
	assert.Equal(t, map[string]bool{old: false}, f.searchOlder(t, workspace, "find"))
}

func (f *fixture) titledSession(t *testing.T, workspace, title string) string {
	t.Helper()
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"title": title}, &out))
	require.NotEmpty(t, out.Session)
	return out.Session
}

func (f *fixture) searchOlder(t *testing.T, workspace, query string) map[string]bool {
	t.Helper()
	var hits []struct {
		Session string `json:"session"`
		Older   bool   `json:"older"`
	}
	require.NoError(t, f.run(t, workspace, "agents.search", map[string]any{"query": query}, &hits))
	out := map[string]bool{}
	for _, h := range hits {
		out[h.Session] = h.Older
	}
	return out
}

func TestSessionsSinceACursorFromBeforeARestartReturnsTheFullList(t *testing.T) {
	t.Parallel()
	f := start(t, nil)
	workspace := f.workspace(t, newRepo(t))
	id := f.newSession(t, workspace)

	var p sessionsPage
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": "before-restart.1"}, &p))

	assert.False(t, p.Delta)
	assert.Equal(t, []string{id}, p.ids())
	assert.NotEmpty(t, p.Cursor)
}

func TestASessionRowIsTheSameWhetherItsTranscriptIsLoadedOrNot(t *testing.T) {
	t.Parallel()
	modes := map[string]any{"currentModeId": "loose", "availableModes": []map[string]string{{"id": "loose", "name": "Loose"}}}
	f := start(t, map[string]any{"fork": true, "resume": true, "modes": modes})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "one")
	f.waitStatus(t, workspace, session, "done")
	row := func() map[string]any {
		var list []map[string]any
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
		require.Len(t, list, 1)
		return list[0]
	}
	f.prompt(t, workspace, session, "two")
	testutil.Eventually(t, func() bool {
		r := row()
		return r["status"] == "done" && r["turns"] == 2.0
	}, wait)
	page := func(since string) sessionsPage {
		var p sessionsPage
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": since}, &p))
		return p
	}
	loaded := row()
	assert.Equal(t, "Loose", loaded["modeLabel"])
	assert.Equal(t, true, loaded["canAside"])

	f.restartPlugin(t, workspace)
	assert.Equal(t, loaded, row())
	cursor := page("").Cursor
	require.NoError(t, f.run(t, workspace, "agents.docs", map[string]any{}, nil))

	assert.Empty(t, page(cursor).ids())
	assert.Equal(t, loaded, row())
}

func TestSessionsLeanDeltaSendsRunsAndDockOnlyWhenTheyChanged(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	graphsDir := filepath.Join(repo, ".clubmate", "graphs")
	require.NoError(t, os.MkdirAll(graphsDir, 0o755))
	write(t, filepath.Join(graphsDir, "solo.json"), `{"title":"Solo","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"}]}`)
	f := start(t, map[string]any{"prompt": []map[string]any{{"echo": true}}})
	workspace := f.workspace(t, repo)
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	page := func(args map[string]any) map[string]json.RawMessage {
		var p map[string]json.RawMessage
		require.NoError(t, f.run(t, workspace, "agents.sessions", args, &p))
		return p
	}
	cursor := func(p map[string]json.RawMessage) string {
		var c string
		require.NoError(t, json.Unmarshal(p["cursor"], &c))
		return c
	}

	full := page(map[string]any{"since": "", "lean": true})
	assert.Contains(t, full, "runs")
	assert.Contains(t, full, "dock")
	base := cursor(full)
	plain := page(map[string]any{"since": base})
	assert.JSONEq(t, "true", string(plain["delta"]))
	assert.JSONEq(t, "[]", string(plain["runs"]))
	assert.JSONEq(t, string(full["dock"]), string(plain["dock"]))
	lean := page(map[string]any{"since": base, "lean": true})
	assert.NotContains(t, lean, "runs")
	assert.NotContains(t, lean, "dock")

	write(t, filepath.Join(graphsDir, "pair.json"), `{"title":"Pair","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"}]}`)
	var docked map[string]json.RawMessage
	testutil.Eventually(t, func() bool {
		docked = page(map[string]any{"since": base, "lean": true})
		_, ok := docked["dock"]
		return ok
	}, wait)
	assert.NotContains(t, docked, "runs")

	var run graphRun
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "solo"}, &run))
	testutil.Eventually(t, func() bool {
		var p struct {
			Runs []struct {
				ID string `json:"id"`
			} `json:"runs"`
		}
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"since": cursor(docked), "lean": true}, &p))
		return len(p.Runs) == 1 && p.Runs[0].ID == run.ID
	}, wait)
}
