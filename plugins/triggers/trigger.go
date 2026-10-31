package main

import (
	"encoding/json"
	"slices"

	"clubmate/plugins/internal/automation"
)

const (
	triggerPrefix = "trigger/"
	idPrefix      = "tr_"
)

type Run = automation.Run

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

func (t *Trigger) Action() automation.Action {
	return automation.Action{
		ID:        t.ID,
		Name:      t.Name,
		Workspace: t.Workspace,
		Command:   t.Command,
		Args:      t.Args,
		Owner:     t.Owner,
		CreatedBy: t.CreatedBy,
	}
}

func (t *Trigger) Clone() *Trigger {
	c := *t
	c.History = slices.Clone(t.History)
	return &c
}
