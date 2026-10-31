package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	statusOK      = "ok"
	statusError   = "error"
	statusSkipped = "skipped"
)

const (
	triggerPrefix = "trigger/"
	idPrefix      = "tr_"
)

type Run struct {
	At     time.Time `json:"at"`
	Topic  string    `json:"topic"`
	Seq    int64     `json:"seq"`
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	Result string    `json:"result,omitempty"`
}

type Trigger struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Workspace string          `json:"workspace"`
	On        string          `json:"on"`
	Match     json.RawMessage `json:"match,omitempty"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args,omitempty"`
	Enabled   bool            `json:"enabled"`
	Owner     string          `json:"owner,omitempty"`
	CreatedBy string          `json:"createdBy,omitempty"`
	Failures  int             `json:"failures,omitempty"`
	LastError string          `json:"lastError,omitempty"`
	History   []Run           `json:"history,omitempty"`
}

type store struct {
	h *pluginsdk.Host
}

func (st *store) load(ctx context.Context) ([]*Trigger, error) {
	keys, err := st.h.StorageList(ctx, triggerPrefix)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	triggers := make([]*Trigger, 0, len(keys))
	for _, key := range keys {
		var t Trigger
		found, err := st.h.StorageGet(ctx, key, &t)
		if err != nil {
			fmt.Fprintln(os.Stderr, "triggers: skip stored trigger", key+":", err)
			continue
		}
		if found {
			triggers = append(triggers, &t)
		}
	}
	return triggers, nil
}

func (st *store) save(ctx context.Context, t *Trigger) error {
	if err := st.h.StorageSet(ctx, triggerKey(t), t); err != nil {
		return fmt.Errorf("save trigger %s: %w", t.ID, err)
	}
	return nil
}

func (st *store) remove(ctx context.Context, t *Trigger) error {
	if err := st.h.StorageDelete(ctx, triggerKey(t)); err != nil {
		return fmt.Errorf("delete trigger %s: %w", t.ID, err)
	}
	return nil
}

func triggerKey(t *Trigger) string {
	return triggerPrefix + t.Workspace + "/" + t.ID
}

func newTriggerID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(b), nil
}

func clone(t *Trigger) *Trigger {
	c := *t
	c.History = slices.Clone(t.History)
	return &c
}
