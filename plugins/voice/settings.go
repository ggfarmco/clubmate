package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

const (
	settingsKey   = "settings"
	settingsTopic = "voice.settings-changed"
	defaultModel  = "nemotron"
)

var (
	errVoiceOff = errors.New("voice is off; turn it on in Settings → General → Voice")

	models = []model{
		{ID: "nemotron", Title: "Nemotron 0.6B", Detail: "Shows text while you speak · about 2.5 GB of memory", engine: "nemotron", repo: "nvidia/nemotron-3.5-asr-streaming-0.6b"},
		{ID: "whisper-turbo", Title: "Whisper large-v3 turbo", Detail: "Most accurate · about 1.8 GB of memory", engine: "whisper", repo: "mlx-community/whisper-large-v3-turbo"},
		{ID: "whisper-small", Title: "Whisper small", Detail: "Least memory · about 0.9 GB", engine: "whisper", repo: "mlx-community/whisper-small-mlx"},
	}
)

type model struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	engine string
	repo   string
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
	res := settingsResult{settings: s, Models: models}
	_, err = v.h.Publish(ctx, settingsTopic, res)
	logError("publish "+settingsTopic, err)
	return res, nil
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
