package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutcomeFixersSkipSessionsClosedBeforeTheBuilder(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	dir, logs := t.TempDir(), t.TempDir()
	add := func(rec record) {
		rec.Worktree = dir
		a.sessions[rec.ID] = newSession(rec, newEventLog(filepath.Join(logs, rec.ID+".jsonl")), true, func(string, bool) {})
	}
	at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	add(record{ID: "earlier", Status: statusClosed, CreatedAt: at, UpdatedAt: at.Add(time.Hour)})
	add(record{ID: "builder", Status: statusDone, CreatedAt: at.Add(2 * time.Hour)})
	add(record{ID: "fixer", ParentSession: "builder", Status: statusClosed, CreatedAt: at.Add(3 * time.Hour), UpdatedAt: at.Add(4 * time.Hour)})

	builder, fixers := a.builderOf(landGateArgs{Session: "lander", Worktree: dir})

	require.NotNil(t, builder)
	assert.Equal(t, "builder", builder.snapshot().ID)
	require.Len(t, fixers, 1)
	assert.Equal(t, "fixer", fixers[0].snapshot().ID)
}

func TestOutcomeJournalIsPrivate(t *testing.T) {
	t.Parallel()
	a := &app{stateDir: t.TempDir()}
	at := time.Now().UTC()

	require.NoError(t, a.appendOutcome(outcomeEntry{Type: outcomeVerdict, At: at, Verdict: &gateVerdict{}}))

	dir, err := os.Stat(a.outcomesDir())
	require.NoError(t, err)
	file, err := os.Stat(filepath.Join(a.outcomesDir(), at.Format(outcomesMonth)+".jsonl"))
	require.NoError(t, err)
	assert.Equal(t, []os.FileMode{0o700, 0o600}, []os.FileMode{dir.Mode().Perm(), file.Mode().Perm()})
}

func TestOutcomeReadGoesPastAnOverlongLine(t *testing.T) {
	t.Parallel()
	a := &app{stateDir: t.TempDir()}
	require.NoError(t, os.MkdirAll(a.outcomesDir(), 0o700))
	long := append(bytes.Repeat([]byte("x"), 5<<20), '\n')
	entry := []byte(`{"type":"session","at":"2026-11-19T12:00:00Z","session":{"session":"s"}}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(a.outcomesDir(), "2026-11.jsonl"), append(long, entry...), 0o600))

	got := a.readOutcomes()

	require.Len(t, got, 1)
	assert.Equal(t, "s", got[0].Session.Session)
}

func TestOutcomeSessionIsJournaledAgainAfterAFailedWrite(t *testing.T) {
	t.Parallel()
	a := &app{stateDir: t.TempDir()}
	s := newSession(record{ID: "s", Workspace: "w"}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	require.NoError(t, os.WriteFile(a.outcomesDir(), nil, 0o600))
	a.journalSession(s)
	require.NoError(t, os.Remove(a.outcomesDir()))

	a.journalSession(s)

	got := a.readOutcomes()
	require.Len(t, got, 1)
	assert.Equal(t, "s", got[0].Session.Session)
}
