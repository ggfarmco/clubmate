package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/acp"
)

func user(turn int, text string) entry {
	return userAt(turn, text, time.Now())
}

func userAt(turn int, text string, at time.Time) entry {
	return entry{Kind: entryUser, Turn: turn, Text: text, At: at}
}

func agent(turn int, text string) entry {
	return entry{Kind: entryAgent, Turn: turn, Text: text, At: time.Now()}
}

func tool(turn int, name, output, status string) entry {
	content, _ := json.Marshal([]map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": output}}})
	fields := map[string]json.RawMessage{"name": quoted(name), "status": quoted(status), "content": content}
	return entry{Kind: entryTool, Turn: turn, Tool: fields, At: time.Now()}
}

func command(turn int, title string) entry {
	en := tool(turn, "Bash", "", "completed")
	en.Tool["title"] = quoted(title)
	return en
}

func cancelled(turn int) entry {
	return entry{Kind: entryCancelled, Turn: turn, At: time.Now()}
}

func denied(turn int) entry {
	return answered(turn, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "no"})
}

func answered(turn int, outcome acp.PermissionOutcome) entry {
	options := []acp.PermissionOption{{OptionID: "yes", Kind: "allow_once"}, {OptionID: "no", Kind: "reject_once"}}
	return entry{Kind: entryPermission, Turn: turn, Options: options, Outcome: &outcome, At: time.Now()}
}

func quoted(text string) json.RawMessage {
	raw, _ := json.Marshal(text)
	return raw
}

func TestDigestKeepsPromptsAndLastReply(t *testing.T) {
	t.Parallel()
	out := digestSession(record{Title: "t"}, []entry{
		user(1, "fix it"), agent(1, "looking"), tool(1, "Bash", "secret output", "completed"), agent(1, "done"),
	}, time.Time{})
	assert.Contains(t, out, "fix it")
	assert.Contains(t, out, "done")
	assert.NotContains(t, out, "looking")
	assert.NotContains(t, out, "secret output")
}

func TestDigestCountsFacts(t *testing.T) {
	t.Parallel()
	out := digestSession(record{}, []entry{
		tool(1, "Bash", "", "failed"), tool(1, "Bash", "", "failed"), cancelled(1), denied(1),
	}, time.Time{})
	assert.Contains(t, out, "failed tool calls: Bash ×2")
	assert.Contains(t, out, "cancelled turns: 1")
	assert.Contains(t, out, "denied permissions: 1")
}

func TestDigestSkipsOlderEntries(t *testing.T) {
	t.Parallel()
	since := time.Now()
	assert.Empty(t, digestSession(record{}, []entry{userAt(1, "old", since.Add(-time.Hour))}, since))
}

func TestDigestIsCappedPerSession(t *testing.T) {
	t.Parallel()
	out := digestSession(record{}, []entry{user(1, strings.Repeat("x", digestSessionCap*2))}, time.Time{})
	assert.LessOrEqual(t, len(out), digestSessionCap)
}

func TestDigestCutKeepsFactsAndWholeRunes(t *testing.T) {
	t.Parallel()
	entries := []entry{denied(1)}
	for turn := range 100 {
		entries = append(entries, user(turn, strings.Repeat("é", digestSessionCap)))
	}
	out := digestSession(record{}, entries, time.Time{})
	assert.LessOrEqual(t, len(out), digestSessionCap)
	assert.True(t, utf8.ValidString(out))
	assert.Contains(t, out, "denied permissions: 1")
}

func TestDigestLeavesOutEverythingButPromptsRepliesAndTitles(t *testing.T) {
	t.Parallel()
	attached := user(1, "the prompt")
	attached.Content = []json.RawMessage{quoted("attached file")}
	out := digestSession(record{}, []entry{
		attached,
		{Kind: entryThought, Turn: 1, Text: "a thought"},
		{Kind: entryPlan, Turn: 1, Plan: quoted("a plan")},
		{Kind: entryNotice, Turn: 1, Notice: quoted("a notice")},
		{Kind: entryUser, Turn: 2, Text: "from a parent", From: "agt_parent"},
	}, time.Time{})
	assert.Contains(t, out, "the prompt")
	for _, left := range []string{"attached file", "a thought", "a plan", "a notice", "from a parent"} {
		assert.NotContains(t, out, left)
	}
}

func TestDigestListsCommandsRepeatedThreeTimes(t *testing.T) {
	t.Parallel()
	out := digestSession(record{}, []entry{
		command(1, "go test"), command(1, "go test"), command(2, "go test"), command(2, "ls"), command(2, "ls"),
	}, time.Time{})
	assert.Contains(t, out, "repeated commands: go test ×3")
	assert.NotContains(t, out, "ls")
}

func TestDigestCountsARejectedOptionOnlyAsDenied(t *testing.T) {
	t.Parallel()
	allowed := answered(1, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "yes"})
	stopped := answered(1, acp.PermissionOutcome{Outcome: acp.OutcomeCancelled})
	assert.Empty(t, digestSession(record{}, []entry{allowed, stopped}, time.Time{}))
}

func TestDigestWalksUpdatedSessionsNewestFirst(t *testing.T) {
	t.Parallel()
	since := time.Now()
	a := &app{registry: newRegistry()}
	add := func(id string, updated time.Time, edit func(*record)) *session {
		rec := record{ID: id, Workspace: "w", Title: id, UpdatedAt: updated}
		if edit != nil {
			edit(&rec)
		}
		s := newSession(rec, nil, true, nil)
		s.at = time.Now()
		s.upsert(entryUser, "prompt", 1).Text = "asked in " + id
		a.sessions[id] = s
		return s
	}
	add("older", since.Add(time.Minute), nil)
	stopped := add("newer", since.Add(time.Hour), nil)
	payload, err := json.Marshal(turnEndPayload{StopReason: acp.StopCancelled})
	require.NoError(t, err)
	stopped.apply(envelope{Kind: kindTurnEnd, Turn: 1, Payload: payload})
	add("stale", since.Add(-time.Hour), nil)
	add("service", since.Add(time.Hour), func(r *record) { r.SystemPrompt = "night" })
	add("elsewhere", since.Add(time.Hour), func(r *record) { r.Workspace = "other" })

	out := a.digest("w", since)
	assert.Less(t, strings.Index(out, "asked in newer"), strings.Index(out, "asked in older"))
	assert.Equal(t, 1, strings.Count(out, "cancelled turns: 1"))
	for _, left := range []string{"stale", "service", "elsewhere"} {
		assert.NotContains(t, out, left)
	}
	assert.Empty(t, a.digest("nobody", since))
}

func TestDigestStopsAtTheWorkspaceCap(t *testing.T) {
	t.Parallel()
	since := time.Now()
	a := &app{registry: newRegistry()}
	for i := range 40 {
		id := "s" + strconv.Itoa(i)
		s := newSession(record{ID: id, Workspace: "w", UpdatedAt: since.Add(time.Duration(i+1) * time.Minute)}, nil, true, nil)
		s.at = time.Now()
		for turn := range 40 {
			s.upsert(entryUser, "p"+strconv.Itoa(turn), turn).Text = strings.Repeat("x", digestSessionCap)
		}
		a.sessions[id] = s
	}
	out := a.digest("w", since)
	assert.LessOrEqual(t, len(out), digestWorkspaceCap)
	assert.Contains(t, out, "s39")
	assert.NotContains(t, out, "(s0)")
}
