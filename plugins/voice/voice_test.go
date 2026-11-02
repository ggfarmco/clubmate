package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

const (
	testWorkspace = "ws"
	voiceSession  = "agt_00000000000000f0"
	fakeASREnv    = "VOICE_FAKE_ASR"
)

type fakeHost struct {
	mu        sync.Mutex
	storage   map[string]json.RawMessage
	published []changedEvent
	commands  map[string]func(args json.RawMessage) (any, error)
	methods   map[string]func(params any) (any, error)
	runs      []commandCall
	clock     *fakeClock
}

type commandCall struct {
	ID   string
	Args json.RawMessage
}

type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	d       time.Duration
	f       func()
	stopped bool
}

type fakeRecognizer struct {
	mu        sync.Mutex
	sizes     map[string]int
	hints     []string
	final     string
	fed       int
	cancelled []string
}

func TestMain(m *testing.M) {
	if os.Getenv(fakeASREnv) == "1" {
		fakeASR()
		return
	}
	os.Exit(m.Run())
}

func TestStartAudioEndGivesHeard(t *testing.T) {
	v, h, rec := newTestVoice(t)
	rec.final = "поправь мсп"
	_, err := v.command(t.Context(), nil, pluginsdk.CommandRun{ID: "voice.dictionary-set", Args: raw(t, map[string]any{"entries": []map[string]string{{"from": "мсп", "to": "MCP"}}})})
	require.NoError(t, err)

	uid := start(t, v)
	assert.Equal(t, "320", feed(t, v, uid, 320))
	assert.Equal(t, strconv.Itoa(320+minAudioBytes), feed(t, v, uid, minAudioBytes))
	res, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)

	assert.Equal(t, "поправь MCP", res.Heard)
	st, err := v.state(testWorkspace, uid)
	require.NoError(t, err)
	assert.Equal(t, phaseSending, st.Phase)
	sendAt, err := time.Parse(time.RFC3339Nano, st.SendAt)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(sendDelay), sendAt, time.Second)
	assert.Empty(t, h.calls("agents.prompt"))
}

func TestCancelWhileSendingSendsNothing(t *testing.T) {
	v, h, _ := newTestVoice(t)
	uid := start(t, v)
	feed(t, v, uid, minAudioBytes)
	_, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)

	require.NoError(t, v.cancel(testWorkspace, uid))
	h.clock.fire(sendDelay)

	assert.Equal(t, phaseCancelled, phaseOf(t, v, uid))
	assert.Empty(t, h.calls("agents.prompt"))
}

func TestNothingHeardIsNotSent(t *testing.T) {
	tests := []struct {
		name  string
		final string
		audio int
	}{
		{"no word", " … ", minAudioBytes},
		{"too short", "да", minAudioBytes - 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, h, rec := newTestVoice(t)
			rec.final = tt.final
			uid := start(t, v)
			feed(t, v, uid, tt.audio)
			_, err := v.end(t.Context(), testWorkspace, uid)
			require.NoError(t, err)
			h.clock.fire(sendDelay)

			st, err := v.state(testWorkspace, uid)
			require.NoError(t, err)
			assert.Equal(t, phaseError, st.Phase)
			assert.Equal(t, msgNothingHeard, st.Error)
			assert.Empty(t, h.calls("agents.prompt"))
		})
	}
}

func TestAudioOverLimitWaitsForEnd(t *testing.T) {
	v, _, rec := newTestVoice(t)
	uid := start(t, v)

	for range maxAudioBytes/maxChunkBytes + 2 {
		feed(t, v, uid, maxChunkBytes)
	}

	assert.Equal(t, phaseListening, phaseOf(t, v, uid))
	assert.Equal(t, maxAudioBytes, rec.sizes[uid])
}

func TestSeedDictionaryUntilUserSavesOwn(t *testing.T) {
	v, _, rec := newTestVoice(t)
	rec.final = "смержи ворк три в мсп"
	uid := start(t, v)
	res, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)
	assert.Equal(t, "смержи worktree в MCP", res.Heard)

	_, err = v.command(t.Context(), nil, pluginsdk.CommandRun{ID: "voice.dictionary-set", Args: raw(t, map[string]any{"entries": []map[string]string{}})})
	require.NoError(t, err)

	got, err := v.dictionary(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got.Entries)
}

func TestAudioAfterEndFails(t *testing.T) {
	v, _, rec := newTestVoice(t)
	uid := start(t, v)
	_, err := v.end(t.Context(), testWorkspace, uid)
	require.NoError(t, err)

	_, err = v.audio(testWorkspace, audioArgs{Utterance: uid, PCM: make([]byte, 320)})

	assert.ErrorIs(t, err, errUnknownUtterance)
	assert.Zero(t, rec.fed)
}

func TestCancelStopsUtterance(t *testing.T) {
	v, _, rec := newTestVoice(t)
	uid := start(t, v)

	require.NoError(t, v.cancel(testWorkspace, uid))

	st, err := v.state(testWorkspace, uid)
	require.NoError(t, err)
	assert.Equal(t, phaseCancelled, st.Phase)
	assert.Equal(t, []string{uid}, rec.cancelled)
}

func TestRecognizerCrashEndsUtterance(t *testing.T) {
	v, _, _ := newTestVoice(t)
	proc := newProcRecognizer(fakeASRCommand, v.lost)
	t.Cleanup(proc.stop)
	v.rec = proc
	uid := start(t, v)

	_, err := v.audio(testWorkspace, audioArgs{Utterance: uid, PCM: []byte("die")})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		st, err := v.state(testWorkspace, uid)
		return err == nil && st.Phase == phaseError && st.Error == msgRecognizerStopped
	}, 5*time.Second, 10*time.Millisecond)
	next := start(t, v)
	_, err = v.audio(testWorkspace, audioArgs{Utterance: next, PCM: make([]byte, 320)})
	require.NoError(t, err)
	res, err := v.end(t.Context(), testWorkspace, next)
	require.NoError(t, err)
	assert.Equal(t, "fake 320", res.Heard)
}

func newTestVoice(t *testing.T) (*voice, *fakeHost, *fakeRecognizer) {
	h := &fakeHost{
		storage:  map[string]json.RawMessage{},
		commands: map[string]func(json.RawMessage) (any, error){},
		methods:  map[string]func(any) (any, error){},
	}
	var sessions []map[string]any
	stubAgents(h, nil, nil)
	h.commands["agents.sessions"] = func(json.RawMessage) (any, error) { return sessions, nil }
	h.commands["agents.new"] = func(json.RawMessage) (any, error) {
		sessions = append(sessions, map[string]any{"id": voiceSession, "title": "Voice", "status": "idle", "startModel": voiceModel})
		return map[string]string{"session": voiceSession}, nil
	}
	h.commands["agents.prompt"] = func(json.RawMessage) (any, error) { return nil, nil }
	h.methods["command.list"] = func(any) (any, error) { return testCommands, nil }
	rec := &fakeRecognizer{sizes: map[string]int{}, final: "ответь да"}
	h.clock = &fakeClock{}
	v := newVoice()
	v.h, v.rec, v.ctx, v.after = h, rec, t.Context(), h.clock.after
	return v, h, rec
}

func start(t *testing.T, v *voice) string {
	t.Helper()
	res, err := v.start(t.Context(), testWorkspace, startArgs{Pointer: Pointer{Device: "mac", Section: "agents"}})
	require.NoError(t, err)
	return res.Utterance
}

func feed(t *testing.T, v *voice, uid string, n int) string {
	t.Helper()
	res, err := v.audio(testWorkspace, audioArgs{Utterance: uid, PCM: make([]byte, n)})
	require.NoError(t, err)
	return res.Partial
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func fakeASRCommand() (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), fakeASREnv+"=1")
	return cmd, nil
}

func fakeASR() {
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"ev": "ready", "engine": "fake", "streaming": true})
	sizes := map[string]int{}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var msg struct {
			Op  string `json:"op"`
			ID  string `json:"id"`
			PCM []byte `json:"pcm"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil {
			continue
		}
		switch msg.Op {
		case "start":
			sizes[msg.ID] = 0
		case "audio":
			if string(msg.PCM) == "die" {
				os.Exit(3)
			}
			sizes[msg.ID] += len(msg.PCM)
			_ = out.Encode(map[string]any{"ev": "partial", "id": msg.ID, "text": strconv.Itoa(sizes[msg.ID])})
		case "end":
			_ = out.Encode(map[string]any{"ev": "final", "id": msg.ID, "text": fmt.Sprintf("fake %d", sizes[msg.ID]), "ms": 1})
			delete(sizes, msg.ID)
		case "cancel":
			delete(sizes, msg.ID)
		}
	}
}

func (h *fakeHost) Call(_ context.Context, method string, params, result any) error {
	h.mu.Lock()
	fn := h.methods[method]
	h.mu.Unlock()
	if fn == nil {
		return fmt.Errorf("unexpected call %s", method)
	}
	out, err := fn(params)
	if err != nil {
		return err
	}
	return roundTrip(out, result)
}

func (h *fakeHost) RunCommand(_ context.Context, id, _ string, args any, out any) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.runs = append(h.runs, commandCall{ID: id, Args: b})
	fn := h.commands[id]
	h.mu.Unlock()
	if fn == nil {
		return fmt.Errorf("unexpected command %s", id)
	}
	res, err := fn(b)
	if err != nil {
		return err
	}
	return roundTrip(res, out)
}

func (h *fakeHost) Publish(_ context.Context, topic string, payload any) (pluginsdk.Event, error) {
	if ev, ok := payload.(changedEvent); ok && topic == changedTopic {
		h.mu.Lock()
		h.published = append(h.published, ev)
		h.mu.Unlock()
	}
	return pluginsdk.Event{Topic: topic}, nil
}

func (h *fakeHost) StorageGet(_ context.Context, key string, out any) (bool, error) {
	h.mu.Lock()
	b, ok := h.storage[key]
	h.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (h *fakeHost) StorageSet(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.storage[key] = b
	h.mu.Unlock()
	return nil
}

func (h *fakeHost) calls(id string) []json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []json.RawMessage
	for _, c := range h.runs {
		if c.ID == id {
			out = append(out, c.Args)
		}
	}
	return out
}

func (c *fakeClock) after(d time.Duration, f func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, d: d, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

func (r *fakeRecognizer) Start(id, _, hint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sizes[id] = 0
	r.hints = append(r.hints, hint)
	return nil
}

func (r *fakeRecognizer) Feed(id string, pcm []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fed++
	r.sizes[id] += len(pcm)
	return strconv.Itoa(r.sizes[id]), nil
}

func (r *fakeRecognizer) Finish(context.Context, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.final, nil
}

func (r *fakeRecognizer) Cancel(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelled = append(r.cancelled, id)
}

func (r *fakeRecognizer) Ready() bool {
	return true
}

func roundTrip(in, out any) error {
	if out == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
