package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	settingsKey   = "settings"
	settingsSlot  = "voice.settings"
	settingsTopic = "voice.settings-changed"
	defaultModel  = "nemotron"
)

var (
	errVoiceOff = errors.New("voice is off; turn it on in Settings → Voice")

	models = []model{
		{ID: "nemotron", Title: "Nemotron 0.6B", Detail: "Shows text while you speak · about 0.35 GB of memory", engine: "transcribe", repo: "handy-computer/nemotron-3.5-asr-streaming-0.6b-gguf/nemotron-3.5-asr-streaming-0.6b-Q8_0.gguf", revision: "53ce288f69326b415aeef030cfcb02a08a8096dd", sha256: "b94545b313b3223fda7b2857a52681da813935c2127643d1e9ff0c23d988089c"},
		{ID: "whisper-turbo", Title: "Whisper large-v3 turbo", Detail: "Most accurate, text comes once a second · about 0.75 GB of memory", engine: "transcribe", repo: "handy-computer/whisper-large-v3-turbo-gguf/whisper-large-v3-turbo-Q8_0.gguf", revision: "18ada752005f5fddef499256f9a458d0b37a203a", sha256: "257aa7ec9121c22c167d5e2c49e3e76b4ba0e6e27ebc13074eba0a80f8190220"},
	}
)

type model struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	engine   string
	repo     string
	revision string
	sha256   string
}

type settings struct {
	Enabled bool   `json:"enabled"`
	Model   string `json:"model"`
}

type settingsResult struct {
	settings
	Models []model `json:"models"`
}

type settingsArgs struct {
	Enabled *bool   `json:"enabled"`
	Model   *string `json:"model"`
}

type offRecognizer struct{}

func (offRecognizer) Start(string, string, string) error { return errVoiceOff }

func (offRecognizer) Feed(string, []byte) (string, error) { return "", errRecognizerStopped }

func (offRecognizer) Finish(context.Context, string) (string, error) {
	return "", errRecognizerStopped
}

func (offRecognizer) Cancel(string) {}

func (offRecognizer) Ready() bool { return false }

func (v *voice) settings(ctx context.Context) (settingsResult, error) {
	s, err := v.loadSettings(ctx)
	return settingsResult{settings: s, Models: models}, err
}

func (v *voice) setSettings(ctx context.Context, args settingsArgs) (settingsResult, error) {
	v.settingsMu.Lock()
	defer v.settingsMu.Unlock()
	s, err := v.loadSettings(ctx)
	if err != nil {
		return settingsResult{}, err
	}
	if args.Model != nil {
		if _, ok := findModel(*args.Model); !ok {
			return settingsResult{}, fmt.Errorf("unknown voice model %q", *args.Model)
		}
		s.Model = *args.Model
	}
	if args.Enabled != nil {
		s.Enabled = *args.Enabled
	}
	if err := v.h.StorageSet(ctx, settingsKey, s); err != nil {
		return settingsResult{}, err
	}
	v.apply(s)
	logError("render settings", v.renderSettings(ctx, s))
	res := settingsResult{settings: s, Models: models}
	_, err = v.h.Publish(ctx, settingsTopic, res)
	logError("publish "+settingsTopic, err)
	return res, nil
}

func (v *voice) renderSettings(ctx context.Context, s settings) error {
	options := make([]sdui.Option, 0, len(models))
	for _, m := range models {
		options = append(options, sdui.Option{Value: m.ID, Label: m.Title + " · " + m.Detail})
	}
	_, err := v.h.Render(ctx, "", settingsSlot, sdui.VStack(
		sdui.SettingRow("Voice input", "Hold ⌥Space or click the mic in the status bar to talk. Off stops the recognizer and frees its memory.", sdui.WithAction(sdui.Toggle("enabled", s.Enabled), "enabled")),
		sdui.SettingRow("Model", "", sdui.WithAction(sdui.Select("model", options, s.Model), "model")),
	))
	return err
}

func (v *voice) uiEvent(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	var args settingsArgs
	switch {
	case ev.Slot == settingsSlot && ev.Action == "enabled":
		var on bool
		if err := json.Unmarshal(ev.Value, &on); err != nil {
			return nil, fmt.Errorf("enabled: %w", err)
		}
		args.Enabled = &on
	case ev.Slot == settingsSlot && ev.Action == "model":
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return nil, fmt.Errorf("model: %w", err)
		}
		args.Model = &id
	default:
		return nil, fmt.Errorf("unknown action %q", ev.Action)
	}
	return v.setSettings(ctx, args)
}

func (v *voice) loadSettings(ctx context.Context) (settings, error) {
	s := settings{Model: defaultModel}
	if _, err := v.h.StorageGet(ctx, settingsKey, &s); err != nil {
		return settings{Model: defaultModel}, err
	}
	if _, ok := findModel(s.Model); !ok {
		s.Model = defaultModel
	}
	return s, nil
}

func (v *voice) apply(s settings) {
	v.recMu.Lock()
	if v.applied != nil && *v.applied == s {
		v.recMu.Unlock()
		return
	}
	prev := v.rec
	var next *procRecognizer
	v.rec = offRecognizer{}
	if s.Enabled {
		m, _ := findModel(s.Model)
		next = newProcRecognizer(v.asr(m), v.lost)
		v.rec = next
	}
	v.applied = &s
	v.recMu.Unlock()
	if p, ok := prev.(*procRecognizer); ok {
		p.stop()
	}
	if next != nil {
		logError("start recognizer", next.warm())
	}
}

func (v *voice) recognizer() recognizer {
	v.recMu.Lock()
	defer v.recMu.Unlock()
	return v.rec
}

func findModel(id string) (model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return model{}, false
}

func asrCommand(m model) func() (*exec.Cmd, error) {
	return func() (*exec.Cmd, error) {
		return asrCmd(m)
	}
}
