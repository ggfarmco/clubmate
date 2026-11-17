package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

const (
	seqPrompt     = `{"turn":1,"seq":1,"kind":"session/prompt","payload":{"prompt":[{"type":"text","text":"go"}]}}`
	seqToolOpen   = `{"turn":1,"seq":2,"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read","status":"in_progress"}}`
	seqAgentReply = `{"turn":1,"seq":3,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"Shipped. About Lists: your pick. Then rest."}}}`
	seqToolDone   = `{"turn":1,"seq":4,"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"t2","title":"Edit","status":"completed"}}`
)

func seqSession(t *testing.T, lines ...string) (*session, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return newSession(record{ID: "s"}, newEventLog(path), false, func(string, bool) {}), path
}

func restarted(t *testing.T, s *session, path string) *session {
	t.Helper()
	s.log.close()
	return newSession(record{ID: "s"}, newEventLog(path), false, func(string, bool) {})
}

func entryJSON(t *testing.T, v transcriptView) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, en := range v.Entries {
		raw, err := json.Marshal(en)
		require.NoError(t, err)
		out[en.Kind+"/"+en.ID] = string(raw)
	}
	return out
}

func foreign(t *testing.T, v transcriptView) cursor {
	t.Helper()
	pos, _, ok := parseCursor(v.Cursor)
	require.True(t, ok)
	pos.minor = 0
	return pos
}

func assertDeltaCovers(t *testing.T, before transcriptView, s *session, since cursor) []string {
	t.Helper()
	after := s.view(0)
	delta := s.viewSince(0, &since, nil)
	require.True(t, delta.Delta)
	old, got := entryJSON(t, before), entryJSON(t, delta)
	var changed []string
	for k, j := range entryJSON(t, after) {
		if old[k] != j {
			changed = append(changed, k)
			assert.JSONEq(t, j, got[k], k)
		}
	}
	return changed
}

func record1(s *session, kind string, payload any) {
	s.mu.Lock()
	s.recordLocked(actorClubmate, kind, payload)
	s.mu.Unlock()
}

func TestDeltaSinceACursorCoversEveryChangedEntry(t *testing.T) {
	t.Parallel()
	running := func(s *session, on bool) {
		s.mu.Lock()
		s.running = on
		s.mu.Unlock()
	}
	cases := []struct {
		name    string
		lines   []string
		prepare func(*session)
		change  func(t *testing.T, s *session, path string) (*session, func(transcriptView) cursor)
		want    []string
	}{
		{
			name:  "streaming chunks not yet in the log",
			lines: []string{seqPrompt},
			prepare: func(s *session) {
				s.update(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk","messageId":"m9","content":{"type":"text","text":"Hel"}}}`))
			},
			change: func(t *testing.T, s *session, _ string) (*session, func(transcriptView) cursor) {
				s.update(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk","messageId":"m9","content":{"type":"text","text":"lo"}}}`))
				return s, nil
			},
			want: []string{"agent/m9"},
		},
		{
			name:    "a restart closes the tool a live process held open",
			lines:   []string{seqPrompt, seqToolOpen},
			prepare: func(s *session) { running(s, true) },
			change: func(t *testing.T, s *session, path string) (*session, func(transcriptView) cursor) {
				return restarted(t, s, path), func(v transcriptView) cursor { return foreign(t, v) }
			},
			want: []string{"tool/t1"},
		},
		{
			name:    "the turn ending closes a tool without a log line",
			lines:   []string{seqPrompt, seqToolOpen},
			prepare: func(s *session) { running(s, true) },
			change: func(t *testing.T, s *session, _ string) (*session, func(transcriptView) cursor) {
				running(s, false)
				return s, nil
			},
			want: []string{"tool/t1"},
		},
		{
			name:  "a running compaction is cancelled when the turn is over",
			lines: []string{seqPrompt, `{"turn":1,"seq":2,"kind":"session/update","payload":{"sessionUpdate":"compaction_update","compactionId":"c1","status":"in_progress"}}`},
			prepare: func(s *session) {
				running(s, true)
			},
			change: func(t *testing.T, s *session, _ string) (*session, func(transcriptView) cursor) {
				running(s, false)
				return s, nil
			},
			want: []string{"compaction/c1"},
		},
		{
			name:  "posting open items marks the agent message",
			lines: []string{seqPrompt, seqAgentReply},
			change: func(t *testing.T, s *session, _ string) (*session, func(transcriptView) cursor) {
				record1(s, kindOpenItems, openItemsPayload{Items: []openItem{{ID: "oi-1", Kind: itemOffer, Label: "x", Quote: "About Lists", State: itemOpen}}})
				return s, nil
			},
			want: []string{"agent/m1", "openItems/open-1"},
		},
		{
			name:  "an item resolved before a restart unmarks the message after it",
			lines: []string{seqPrompt, seqAgentReply, `{"turn":1,"seq":4,"kind":"open-items","payload":{"items":[{"id":"oi-1","kind":"offer","label":"x","quote":"About Lists","state":"open"}]}}`},
			change: func(t *testing.T, s *session, path string) (*session, func(transcriptView) cursor) {
				record1(s, kindOpenChanged, openChange{Resolved: []openResolution{{Item: "oi-1", State: itemNo}}})
				return restarted(t, s, path), func(v transcriptView) cursor { return foreign(t, v) }
			},
			want: []string{"agent/m1", "openItems/open-1"},
		},
		{
			name:    "a subagent's progress moves its card",
			lines:   []string{seqPrompt, `{"turn":1,"seq":2,"kind":"session/update","payload":{"sessionUpdate":"subagent_update","sessionId":"sub1","title":"Explore","state":{"state":"running"}}}`},
			prepare: func(s *session) { running(s, true) },
			change: func(t *testing.T, s *session, _ string) (*session, func(transcriptView) cursor) {
				s.update(json.RawMessage(`{"sessionId":"sub1","update":{"sessionUpdate":"tool_call","toolCallId":"x1","title":"Grep","status":"in_progress"}}`))
				return s, nil
			},
			want: []string{"tool/sub1"},
		},
		{
			name:  "task titles arriving after a restart reach a cached copy",
			lines: []string{seqPrompt, `{"turn":1,"seq":2,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"Filed #12."}}}`, `{"turn":1,"seq":3,"kind":"turn_end","payload":{}}`},
			prepare: func(s *session) {
				s.view(0)
				s.setTaskTitles(map[int]string{12: "Old title"})
			},
			change: func(t *testing.T, s *session, path string) (*session, func(transcriptView) cursor) {
				next := restarted(t, s, path)
				next.view(0)
				next.setTaskTitles(map[int]string{})
				return next, func(v transcriptView) cursor { return foreign(t, v) }
			},
			want: []string{"agent/m1"},
		},
		{
			name:  "a task renamed before an idle unload reaches a cached copy",
			lines: []string{seqPrompt, `{"turn":1,"seq":2,"kind":"session/update","payload":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"Filed #12."}}}`, `{"turn":1,"seq":3,"kind":"turn_end","payload":{}}`},
			prepare: func(s *session) {
				s.view(0)
				s.setTaskTitles(map[int]string{12: "Old title"})
				s.notice("Later", "a line after the turn")
			},
			change: func(t *testing.T, s *session, _ string) (*session, func(transcriptView) cursor) {
				s.setTaskTitles(map[int]string{12: "New title"})
				s.mu.Lock()
				s.unloadLocked()
				s.mu.Unlock()
				return s, nil
			},
			want: []string{"agent/m1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, path := seqSession(t, tc.lines...)
			s.rec.AgentSessionID = "main"
			if tc.prepare != nil {
				tc.prepare(s)
			}
			before := s.view(0)
			pos, _, ok := parseCursor(before.Cursor)
			require.True(t, ok)
			next, since := tc.change(t, s, path)
			if since != nil {
				pos = since(before)
			}
			assert.ElementsMatch(t, tc.want, assertDeltaCovers(t, before, next, pos))
		})
	}
}

func TestACursorOutlivesARestart(t *testing.T) {
	t.Parallel()
	s, path := seqSession(t, seqPrompt, seqToolOpen, seqAgentReply)
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"completed"}}`))
	record1(s, kindTurnEnd, turnEndPayload{StopReason: "end_turn"})
	before := s.view(0)
	require.Len(t, before.Entries, 3)

	next := restarted(t, s, path)
	since := foreign(t, before)
	assert.Empty(t, next.viewSince(0, &since, nil).Entries)

	next.update(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk","messageId":"m2","content":{"type":"text","text":"more"}}}`))
	delta := next.viewSince(0, &since, nil)
	assert.True(t, delta.Delta)
	assert.Equal(t, []string{"agent/m2"}, slicesKeys(entryJSON(t, delta)))
}

func TestAnUnloadedSessionSendsOnlyWhatChanged(t *testing.T) {
	t.Parallel()
	s, _ := seqSession(t, seqPrompt, seqToolDone)
	s.update(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk","messageId":"m1","content":{"type":"text","text":"ok"}}}`))
	before := s.view(0)
	pos, _, ok := parseCursor(before.Cursor)
	require.True(t, ok)

	s.mu.Lock()
	require.True(t, s.unloadableLocked(0))
	s.unloadLocked()
	s.mu.Unlock()

	assert.Equal(t, []string{"agent/m1"}, slicesKeys(entryJSON(t, s.viewSince(0, &pos, nil))))
}

func TestCursorsItCannotReadGiveTheWholeTranscript(t *testing.T) {
	t.Parallel()
	s, _ := seqSession(t, seqPrompt, seqToolDone)
	full := s.view(0)

	for _, text := range []string{"", "1abc.4", "x:0:" + transcriptEpoch + ":", "1:0:" + transcriptEpoch} {
		_, _, ok := parseCursor(text)
		assert.False(t, ok, text)
	}
	pos, _, ok := parseCursor(full.Cursor)
	require.True(t, ok)
	pos.seq += 5
	beyond := s.viewSince(0, &pos, nil)
	assert.False(t, beyond.Delta)
	assert.Len(t, beyond.Entries, len(full.Entries))
}

func slicesKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDecoratedEntriesComeBackWhenTheirContextChanges(t *testing.T) {
	t.Parallel()
	s, _ := seqSession(t, seqPrompt,
		`{"turn":1,"seq":2,"kind":"session/request_permission","payload":{"id":"permission-1","requestId":"1","params":{"toolCall":{"toolCallId":"t1"},"options":[]}}}`,
		`{"turn":1,"seq":3,"kind":"session/update","payload":{"sessionUpdate":"notice","title":"Repo","description":"d","repo":"web","path":"/r/web"}}`,
		`{"turn":1,"seq":4,"kind":"session/update","payload":{"sessionUpdate":"notice","title":"Plain","description":"d"}}`,
	)
	full := s.view(0)
	pos, _, ok := parseCursor(full.Cursor)
	require.True(t, ok)

	assert.Empty(t, s.viewSince(0, &pos, nil).Entries)
	assert.ElementsMatch(t, []string{"permission/permission-1", "notice/notice-2"}, slicesKeys(entryJSON(t, s.viewSince(0, &pos, decorated))))

	rec := record{Task: &taskRecord{Repos: []taskRepo{{Path: "/r/web"}}}}
	w := pluginsdk.Workspace{Name: "Shop"}
	key := decorationKey(rec, w)
	assert.Equal(t, key, decorationKey(rec, w))
	assert.NotEqual(t, key, decorationKey(rec, pluginsdk.Workspace{Name: "Store"}))
	rec.Task.Repos[0].State = landPROpen
	assert.NotEqual(t, key, decorationKey(rec, w))
}

func TestLineChangesMatchThePhonesLineDiff(t *testing.T) {
	t.Parallel()
	added, removed := lineChanges("one\ntwo\nthree\n", "one\n2\nthree\nfour")
	assert.Equal(t, [2]int{2, 1}, [2]int{added, removed})
	added, removed = lineChanges("", "a\n\n")
	assert.Equal(t, [2]int{2, 0}, [2]int{added, removed})
	big := strings.Repeat("x\n", 501)
	added, removed = lineChanges(big, big)
	assert.Equal(t, [2]int{501, 501}, [2]int{added, removed})
}
