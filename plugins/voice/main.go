package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const changedTopic = "voice.changed"

type host interface {
	Call(ctx context.Context, method string, params, result any) error
	RunCommand(ctx context.Context, id, workspaceID string, args any, out any) error
	Publish(ctx context.Context, topic string, payload any) (pluginsdk.Event, error)
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
	Render(ctx context.Context, workspaceID, slot string, n sdui.Node) (int64, error)
}

type voice struct {
	h          host
	asr        func(model) func() (*exec.Cmd, error)
	cleaner    func(dictionary) cleaner
	recMu      sync.Mutex
	rec        recognizer
	applied    *settings
	settingsMu sync.Mutex
	ctx        context.Context
	now        func() time.Time
	after      func(time.Duration, func()) stopper
	mu         sync.Mutex
	sessionMu  sync.Mutex
	utterances map[string]*utterance
}

type changedEvent struct {
	Workspace string `json:"workspace"`
	Utterance string `json:"utterance"`
	Rev       int    `json:"rev"`
}

func main() {
	v := newVoice()
	pluginsdk.Main(pluginsdk.Plugin{Init: v.init, Command: v.command, UIEvent: v.uiEvent, Shutdown: v.shutdown})
}

func newVoice() *voice {
	return &voice{asr: asrCommand, cleaner: startCleaner, rec: offRecognizer{}, now: time.Now, after: afterFunc, utterances: map[string]*utterance{}}
}

func (v *voice) init(ctx context.Context, h *pluginsdk.Host) error {
	v.h = h
	v.ctx = ctx
	s, err := v.loadSettings(ctx)
	logError("load settings", err)
	v.apply(s)
	logError("render settings", v.renderSettings(ctx, s))
	_, err = h.Publish(ctx, h.PluginID()+".ready", nil)
	return err
}

func (v *voice) shutdown(context.Context, *pluginsdk.Host) {
	if p, ok := v.recognizer().(*procRecognizer); ok {
		p.stop()
	}
}

func (v *voice) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	switch cmd.ID {
	case "voice.start":
		var args startArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.start(ctx, cmd.WorkspaceID, args)
	case "voice.audio":
		var args audioArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.audio(cmd.WorkspaceID, args)
	case "voice.end":
		var args utteranceArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.end(ctx, cmd.WorkspaceID, args.Utterance)
	case "voice.cancel":
		var args utteranceArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, v.cancel(cmd.WorkspaceID, args.Utterance)
	case "voice.state":
		var args utteranceArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.state(cmd.WorkspaceID, args.Utterance)
	case "voice.confirm":
		var args confirmArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, v.confirmed(cmd.WorkspaceID, args)
	case "voice.plan", "voice.answer", "voice.run", "voice.done":
		return v.agentCommand(ctx, cmd)
	case "voice.dictionary":
		return v.dictionary(ctx)
	case "voice.settings":
		return v.settings(ctx)
	case "voice.settings-set":
		var args settingsArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.setSettings(ctx, args)
	case "voice.dictionary-set":
		var args dictionary
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return v.setDictionary(ctx, args)
	}
	return nil, fmt.Errorf("unknown command %q", cmd.ID)
}

func (v *voice) publish(workspace, utterance string, rev int) {
	_, err := v.h.Publish(v.ctx, changedTopic, changedEvent{Workspace: workspace, Utterance: utterance, Rev: rev})
	logError("publish "+changedTopic, err)
}

func afterFunc(d time.Duration, f func()) stopper {
	return time.AfterFunc(d, f)
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "voice: %s: %v\n", what, err)
	}
}
