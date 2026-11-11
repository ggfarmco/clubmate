package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeClaudeEnv = "VOICE_FAKE_CLAUDE"

type fakeCleaner struct {
	mu     sync.Mutex
	text   string
	err    error
	got    string
	closed bool
}

func (c *fakeCleaner) Clean(_ context.Context, text string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = text
	return c.text, c.err
}

func (c *fakeCleaner) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

func (c *fakeCleaner) state() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got, c.closed
}

func TestDictationGivesCleanedTextAndSendsNothing(t *testing.T) {
	v, h, rec := newTestVoice(t)
	rec.final = "ну сделай мерж"
	c := &fakeCleaner{text: "Сделай merge."}
	v.cleaner = func(dictionary) cleaner { return c }

	uid := dictate(t, v)
	res, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)
	assert.Equal(t, "ну сделай merge", res.Heard)
	st := waitDone(t, v, uid)

	assert.Equal(t, phaseDone, st.Phase)
	assert.Equal(t, modeDictation, st.Mode)
	assert.Equal(t, "Сделай merge.", st.Text)
	assert.Empty(t, st.SendAt)
	got, closed := c.state()
	assert.Equal(t, "ну сделай merge", got)
	assert.True(t, closed)
	h.clock.fire(sendDelay)
	assert.Empty(t, h.calls("agents.prompt"))
	assert.Empty(t, h.calls("agents.sessions"))
}

func TestDictationFallsBackToHeard(t *testing.T) {
	tests := []struct {
		name    string
		cleaner *fakeCleaner
	}{
		{"error", &fakeCleaner{err: context.DeadlineExceeded}},
		{"answer instead of cleanup", &fakeCleaner{text: "Конечно! Вот подробный план того, как запустить тесты в этом проекте."}},
		{"empty", &fakeCleaner{text: " … "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _, rec := newTestVoice(t)
			rec.final = "запусти тесты"
			v.cleaner = func(dictionary) cleaner { return tt.cleaner }

			uid := dictate(t, v)
			_, err := v.end(t.Context(), testWorkspace, uid)
			require.NoError(t, err)

			assert.Equal(t, "запусти тесты", waitDone(t, v, uid).Text)
		})
	}
}

func TestCancelledDictationStopsCleaner(t *testing.T) {
	v, _, _ := newTestVoice(t)
	c := &fakeCleaner{}
	v.cleaner = func(dictionary) cleaner { return c }

	uid := dictate(t, v)
	require.NoError(t, v.cancel(testWorkspace, uid))

	_, closed := c.state()
	assert.True(t, closed)
}

func TestClaudeCleanerReadsResultEvent(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  string
		err   bool
	}{
		{"result", `{"type":"result","subtype":"success","is_error":false,"result":" Сделай merge. \n"}`, "Сделай merge.", false},
		{"error", `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Not logged in"}`, "", true},
		{"exit without result", `{"type":"assistant"}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), fakeClaudeEnv+"="+tt.reply)
			c := newClaudeCleaner(cmd)
			t.Cleanup(c.Close)

			text, err := c.Clean(t.Context(), "ну сделай мерж")

			assert.Equal(t, tt.err, err != nil, err)
			assert.Equal(t, tt.want, text)
		})
	}
}

func fakeClaude(reply string) {
	in := bufio.NewScanner(os.Stdin)
	if !in.Scan() {
		os.Exit(1)
	}
	var msg streamUserMessage
	if err := json.Unmarshal(in.Bytes(), &msg); err != nil || msg.Message.Content != "<dictation>ну сделай мерж</dictation>" {
		os.Exit(2)
	}
	fmt.Println(`{"type":"system","subtype":"init"}`)
	fmt.Println(reply)
}

func dictate(t *testing.T, v *voice) string {
	t.Helper()
	res, err := v.start(t.Context(), testWorkspace, startArgs{Pointer: Pointer{Device: "mac", Section: "agents"}, Mode: modeDictation})
	require.NoError(t, err)
	feed(t, v, res.Utterance, minAudioBytes)
	return res.Utterance
}

func waitDone(t *testing.T, v *voice, uid string) State {
	t.Helper()
	v.mu.Lock()
	u := v.utterances[uid]
	v.mu.Unlock()
	select {
	case <-u.done:
	case <-time.After(5 * time.Second):
		require.Fail(t, "utterance did not end")
	}
	st, err := v.state(testWorkspace, uid)
	require.NoError(t, err)
	return st
}
