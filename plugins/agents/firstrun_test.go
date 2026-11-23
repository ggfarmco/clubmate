package main_test

import (
	"slices"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	firstPrompt  = "Find one small bug, fix it and run the tests"
	needsYouNote = "The agent waits until you answer. Anything waiting also shows under Needs you and in notifications."
	phoneOffer   = "Answer Claude from your iPhone when you step away."
)

func TestFirstRunHintsShowOncePerMacAcrossARestart(t *testing.T) {
	t.Parallel()
	remote := testutil.BuildPluginDir(t, "clubmate/plugins/remote", "plugins/remote/plugin.json")
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript(), "match": "ask"}}}, remote)
	workspace := f.workspace(t, newRepo(t))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return n.Props["title"] == firstRunTitle })

	first := f.newSession(t, workspace)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return suggestion(n) == firstPrompt })
	second := f.newSession(t, workspace)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "composer")) == 1 && suggestion(n) == "" })
	assert.Equal(t, firstPrompt, f.session(t, workspace, first).Suggestion)
	assert.Empty(t, f.session(t, workspace, second).Suggestion)

	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", first)
	f.prompt(t, workspace, first, "ask")
	f.waitStatus(t, workspace, first, "waiting")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return permissionNote(n) == needsYouNote })
	f.answerPermission(t, workspace, first, ofKind(f.transcript(t, workspace, first).Entries, "permission")[0].ID)
	f.waitStatus(t, workspace, first, "done")
	done := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "offer")) == 1 })
	assert.Equal(t, phoneOffer, cardsOf(done, "offer")[0].Props["text"])
	assert.Contains(t, lines(done), "Turn 1 · checkpoint saved")
	assert.Empty(t, suggestion(done))

	f.restartAgentsAmong(t, workspace)
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", first)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "offer")) == 1 })
	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", second)
	f.prompt(t, workspace, second, "ask")
	f.waitStatus(t, workspace, second, "waiting")
	waiting := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "permissionCard")) == 1 })
	assert.Empty(t, permissionNote(waiting))
	f.answerPermission(t, workspace, second, ofKind(f.transcript(t, workspace, second).Entries, "permission")[0].ID)
	f.waitStatus(t, workspace, second, "done")

	f.h.Event(t, workspace, "agents.sidebar", "sessions", "select", first)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "offer")) == 1 })
	f.h.Event(t, workspace, "agents.main", first+"/phone/not-now", "phoneNotNow", nil)
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return slices.Contains(lines(n), "Turn 1 · checkpoint saved") && len(cardsOf(n, "offer")) == 0
	})
	for _, id := range []string{first, second} {
		require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": id}, nil))
	}
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return n.Type == "empty" && n.Props["title"] != firstRunTitle
	})
}

func lines(n sdui.Node) []string {
	var out []string
	for _, c := range cardsOf(n, "text") {
		out = append(out, c.Props["text"].(string))
	}
	return out
}

func suggestion(n sdui.Node) string {
	for _, c := range cardsOf(n, "composer") {
		if s, ok := c.Props["suggestion"].(string); ok {
			return s
		}
	}
	return ""
}

func permissionNote(n sdui.Node) string {
	for _, c := range cardsOf(n, "permissionCard") {
		if s, ok := c.Props["note"].(string); ok {
			return s
		}
	}
	return ""
}

func (f *fixture) restartAgentsAmong(t *testing.T, workspace string) {
	t.Helper()
	pid := func() int {
		i := slices.IndexFunc(f.h.Sup.Statuses(), func(s plugin.Status) bool { return s.ID == "agents" })
		if i < 0 {
			return 0
		}
		return f.h.Sup.Statuses()[i].PID
	}
	old := pid()
	require.NoError(t, syscall.Kill(old, syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		now := pid()
		return now != 0 && now != old && f.run(t, workspace, "agents.sessions", map[string]any{}, nil) == nil
	}, restartWait)
	f.subscribe(t, workspace)
}
