package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"clubmate/pkg/id"
)

const (
	phaseLoading   = "loading"
	phaseListening = "listening"
	phaseSending   = "sending"
	phaseThinking  = "thinking"
	phaseConfirm   = "confirm"
	phaseDone      = "done"
	phaseError     = "error"
	phaseCancelled = "cancelled"

	utterancePrefix = "vu"
	bytesPerSecond  = 16000 * 2
	maxAudioBytes   = 60 * bytesPerSecond
	minAudioBytes   = bytesPerSecond / 2
	sendDelay       = time.Second
	sendAtLayout    = "2006-01-02T15:04:05.000Z07:00"
	maxChunkBytes   = 64 << 10
	finishedTTL     = 10 * time.Minute

	msgRecognizerStopped = "recognizer stopped"
	msgStillLoading      = "the recognizer is still loading"
	msgNothingHeard      = "nothing heard"
)

var (
	errUnknownUtterance  = errors.New("unknown utterance")
	errRecognizerStopped = errors.New(msgRecognizerStopped)
	errNothingHeard      = errors.New(msgNothingHeard)
	errChunkTooLarge     = fmt.Errorf("pcm chunk over %d bytes", maxChunkBytes)

	languages = []string{"ru", "auto"}
)

type Pointer struct {
	Device    string    `json:"device"`
	Section   string    `json:"section"`
	Selection Selection `json:"selection"`
}

type Selection struct {
	Session string `json:"session,omitempty"`
	Task    int    `json:"task,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

type State struct {
	Utterance string   `json:"utterance"`
	Phase     string   `json:"phase"`
	Partial   string   `json:"partial,omitempty"`
	Heard     string   `json:"heard,omitempty"`
	SendAt    string   `json:"sendAt,omitempty"`
	Plan      string   `json:"plan,omitempty"`
	Result    string   `json:"result,omitempty"`
	Error     string   `json:"error,omitempty"`
	Confirm   *Confirm `json:"confirm,omitempty"`
	Session   string   `json:"session,omitempty"`
	Rev       int      `json:"rev"`
}

type Confirm struct {
	ID      int             `json:"id"`
	Title   string          `json:"title"`
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args,omitempty"`
}

type startArgs struct {
	Pointer Pointer `json:"pointer"`
	Lang    string  `json:"lang"`
}

type audioArgs struct {
	Utterance string `json:"utterance"`
	PCM       []byte `json:"pcm"`
}

type utteranceArgs struct {
	Utterance string `json:"utterance"`
}

type startResult struct {
	Utterance string `json:"utterance"`
}

type audioResult struct {
	Partial string `json:"partial"`
}

type endResult struct {
	Heard string `json:"heard"`
}

type utterance struct {
	workspace  string
	where      Context
	state      State
	audioBytes int
	peak       int
	closing    bool
	ended      time.Time
	done       chan struct{}
	confirm    chan bool
	confirms   int
	turn       stopper
	feed       sync.Mutex
}

func (v *voice) start(ctx context.Context, workspace string, args startArgs) (startResult, error) {
	lang := cmp.Or(args.Lang, languages[0])
	if !slices.Contains(languages, lang) {
		return startResult{}, fmt.Errorf("unknown lang %q, want ru or auto", lang)
	}
	where, err := resolve(ctx, v.h, workspace, args.Pointer)
	logError("resolve context", err)
	uid := id.New(utterancePrefix)
	if err := v.rec.Start(uid, lang, hintWords(where)); err != nil {
		return startResult{}, err
	}
	phase := phaseLoading
	if v.rec.Ready() {
		phase = phaseListening
	}
	u := &utterance{workspace: workspace, where: where, done: make(chan struct{}), state: State{Utterance: uid, Phase: phase, Rev: 1}}
	v.mu.Lock()
	v.prune()
	v.utterances[uid] = u
	v.mu.Unlock()
	v.publish(workspace, uid, 1)
	return startResult{Utterance: uid}, nil
}

func (v *voice) audio(workspace string, args audioArgs) (audioResult, error) {
	if len(args.PCM) > maxChunkBytes {
		return audioResult{}, errChunkTooLarge
	}
	u := v.lookup(workspace, args.Utterance)
	if u == nil {
		return audioResult{}, errUnknownUtterance
	}
	u.feed.Lock()
	defer u.feed.Unlock()
	v.mu.Lock()
	if !u.accepting() {
		v.mu.Unlock()
		return audioResult{}, errUnknownUtterance
	}
	pcm := args.PCM[:min(len(args.PCM), maxAudioBytes-u.audioBytes)]
	u.audioBytes += len(pcm)
	u.peak = max(u.peak, peakLevel(pcm))
	v.mu.Unlock()
	partial, err := v.rec.Feed(u.state.Utterance, pcm)
	if err != nil {
		v.fail(u, err)
		return audioResult{}, err
	}
	v.mu.Lock()
	u.state.Partial = partial
	loaded := u.state.Phase == phaseLoading && v.rec.Ready()
	v.mu.Unlock()
	if loaded {
		v.update(u, func(s *State) { s.Phase = phaseListening })
	}
	return audioResult{Partial: partial}, nil
}

func (v *voice) end(ctx context.Context, workspace, uid string) (endResult, error) {
	u := v.lookup(workspace, uid)
	if u == nil {
		return endResult{}, errUnknownUtterance
	}
	heard, err := v.finish(ctx, u)
	return endResult{Heard: heard}, err
}

func (v *voice) cancel(workspace, uid string) error {
	u := v.lookup(workspace, uid)
	if u == nil {
		return errUnknownUtterance
	}
	v.mu.Lock()
	recognizing := u.recognizing()
	v.mu.Unlock()
	if !v.update(u, func(s *State) { s.Phase = phaseCancelled }) {
		return nil
	}
	if recognizing {
		v.rec.Cancel(uid)
	}
	return nil
}

func (v *voice) state(workspace, uid string) (State, error) {
	u := v.lookup(workspace, uid)
	if u == nil {
		return State{}, errUnknownUtterance
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return u.state, nil
}

func (v *voice) lost(uid string) {
	v.mu.Lock()
	u := v.utterances[uid]
	v.mu.Unlock()
	if u != nil {
		v.fail(u, errRecognizerStopped)
	}
}

func (v *voice) finish(ctx context.Context, u *utterance) (string, error) {
	u.feed.Lock()
	v.mu.Lock()
	if !u.accepting() {
		v.mu.Unlock()
		u.feed.Unlock()
		return "", errUnknownUtterance
	}
	u.closing = true
	v.mu.Unlock()
	u.feed.Unlock()
	began := v.now()
	text, err := v.rec.Finish(ctx, u.state.Utterance)
	if err != nil {
		v.fail(u, err)
		return "", err
	}
	heard := v.loadDictionary(ctx).apply(text)
	v.logUtterance(u, heard, v.now().Sub(began))
	v.mu.Lock()
	short := u.audioBytes < minAudioBytes
	v.mu.Unlock()
	if short || strings.IndexFunc(heard, isWordRune) < 0 {
		v.fail(u, errNothingHeard)
		return heard, nil
	}
	if !v.update(u, func(s *State) {
		s.Heard = heard
		s.Phase = phaseSending
		s.SendAt = v.now().Add(sendDelay).UTC().Format(sendAtLayout)
		u.turn = v.after(sendDelay, func() { v.send(u) })
	}) {
		return "", errUnknownUtterance
	}
	return heard, nil
}

func (v *voice) send(u *utterance) {
	v.mu.Lock()
	sending := u.state.Phase == phaseSending
	v.mu.Unlock()
	if !sending || !v.update(u, func(s *State) { s.Phase = phaseThinking }) {
		return
	}
	v.routeAsync(u)
}

func (v *voice) logUtterance(u *utterance, heard string, final time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	fmt.Fprintf(os.Stderr, "voice: utterance %s device=%s audio=%dB/%.1fs peak=%.2f heard=%d final=%dms\n",
		u.state.Utterance, u.where.Device, u.audioBytes, float64(u.audioBytes)/bytesPerSecond, float64(u.peak)/32768, len([]rune(heard)), final.Milliseconds())
}

func (v *voice) fail(u *utterance, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, errRecognizerStopped):
		msg = msgRecognizerStopped
	case errors.Is(err, context.DeadlineExceeded) && !v.rec.Ready():
		msg = msgStillLoading
	}
	v.mu.Lock()
	recognizing := u.recognizing()
	v.mu.Unlock()
	if v.update(u, func(s *State) {
		s.Phase = phaseError
		s.Error = msg
	}) && recognizing && !errors.Is(err, errRecognizerStopped) {
		v.rec.Cancel(u.state.Utterance)
	}
}

func (v *voice) update(u *utterance, change func(*State)) bool {
	v.mu.Lock()
	if u.terminal() {
		v.mu.Unlock()
		return false
	}
	change(&u.state)
	u.state.Rev++
	if u.terminal() {
		u.ended = v.now()
		close(u.done)
		if u.turn != nil {
			u.turn.Stop()
		}
	}
	rev := u.state.Rev
	v.mu.Unlock()
	v.publish(u.workspace, u.state.Utterance, rev)
	return true
}

func (v *voice) lookup(workspace, uid string) *utterance {
	v.mu.Lock()
	defer v.mu.Unlock()
	u := v.utterances[uid]
	if u == nil || u.workspace != workspace {
		return nil
	}
	return u
}

func (v *voice) prune() {
	for uid, u := range v.utterances {
		if u.terminal() && v.now().Sub(u.ended) > finishedTTL {
			delete(v.utterances, uid)
		}
	}
}

func peakLevel(pcm []byte) int {
	peak := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		sample := int(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
		peak = max(peak, sample, -sample)
	}
	return peak
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (u *utterance) accepting() bool {
	return !u.closing && u.recognizing()
}

func (u *utterance) recognizing() bool {
	return u.state.Phase == phaseLoading || u.state.Phase == phaseListening
}

func (u *utterance) terminal() bool {
	return u.state.Phase == phaseDone || u.state.Phase == phaseError || u.state.Phase == phaseCancelled
}
