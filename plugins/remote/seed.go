package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	pushTriggerKey        = "pushTrigger/"
	triggersAddCommand    = "triggers.add"
	workspaceCreatedTopic = "workspace.created"
	workspaceDeletedTopic = "workspace.deleted"
	seedEvery             = 5 * time.Second
	seedFor               = 2 * time.Minute
	pushTriggerLimit      = 60
	pushTriggerCallArgs   = `{"notification":"{{payload}}"}`
)

var pushTriggerKinds = []string{"needs-input", "permission", "question", "stuck", "done"}

type triggerAdd struct {
	Name     string `json:"name"`
	On       string `json:"on"`
	Match    string `json:"match"`
	Command  string `json:"command"`
	Args     string `json:"args"`
	Owner    string `json:"owner"`
	Limit    int    `json:"limit"`
	Parallel bool   `json:"parallel"`
}

type kindMatch struct {
	Kind []string `json:"kind"`
}

type addedTrigger struct {
	ID string `json:"id"`
}

type workspaceEvent struct {
	Workspace string `json:"workspace"`
}

func (a *app) seedPushTriggers(ctx context.Context) {
	a.retrySeed(ctx, func() error {
		list, err := a.srv.backend.Workspaces(ctx)
		if err != nil {
			return err
		}
		var errs []error
		for _, w := range list {
			errs = append(errs, a.seedPushTrigger(ctx, w.ID))
		}
		return errors.Join(errs...)
	})
}

func (a *app) seedPushTrigger(ctx context.Context, workspace string) error {
	a.seedMu.Lock()
	defer a.seedMu.Unlock()
	key := pushTriggerKey + workspace
	var id string
	if seeded, err := a.store.StorageGet(ctx, key, &id); err != nil || seeded {
		return err
	}
	out, err := a.srv.backend.Run(ctx, triggersAddCommand, workspace, pushTriggerArgs())
	if err != nil {
		return fmt.Errorf("%s in %s: %w", triggersAddCommand, workspace, err)
	}
	var t addedTrigger
	if err := json.Unmarshal(out, &t); err != nil || t.ID == "" {
		return fmt.Errorf("%s in %s returned no trigger: %s", triggersAddCommand, workspace, out)
	}
	return a.store.StorageSet(ctx, key, t.ID)
}

func (a *app) retrySeed(ctx context.Context, seed func() error) {
	deadline := time.Now().Add(a.seedFor)
	for {
		err := seed()
		if err == nil {
			return
		}
		if time.Now().Add(a.seedEvery).After(deadline) {
			logf("seed push trigger: %v", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(a.seedEvery):
		}
	}
}

func (a *app) workspaceEvent(ctx context.Context, ev pluginsdk.Event) {
	var w workspaceEvent
	if err := json.Unmarshal(ev.Payload, &w); err != nil || w.Workspace == "" {
		return
	}
	if ev.Topic == workspaceDeletedTopic {
		if err := a.store.StorageDelete(ctx, pushTriggerKey+w.Workspace); err != nil {
			logf("clear push trigger marker of %s: %v", w.Workspace, err)
		}
		return
	}
	go a.retrySeed(a.ctx, func() error { return a.seedPushTrigger(a.ctx, w.Workspace) })
}

func pushTriggerArgs() json.RawMessage {
	match, _ := json.Marshal(kindMatch{Kind: pushTriggerKinds})
	raw, _ := json.Marshal(triggerAdd{
		Name:     "Push to phone",
		On:       notifyTopic,
		Match:    string(match),
		Command:  "remote.push",
		Args:     pushTriggerCallArgs,
		Owner:    "none",
		Limit:    pushTriggerLimit,
		Parallel: true,
	})
	return raw
}
