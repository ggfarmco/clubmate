package main

import (
	"encoding/json"
	"slices"
	"time"

	"clubmate/plugins/internal/automation"
)

const (
	missedOnce = "once"
	missedSkip = "skip"
)

type Run = automation.Run

type Job struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Workspace string          `json:"workspace"`
	Schedule  Schedule        `json:"schedule"`
	Command   string          `json:"command"`
	Args      json.RawMessage `json:"args,omitempty"`
	Enabled   bool            `json:"enabled"`
	Owner     string          `json:"owner,omitempty"`
	CreatedBy string          `json:"createdBy,omitempty"`
	Missed    string          `json:"missed"`
	NextRun   time.Time       `json:"nextRun,omitzero"`
	Failures  int             `json:"failures,omitempty"`
	LastError string          `json:"lastError,omitempty"`
	History   []Run           `json:"history,omitempty"`
}

func (j *Job) Action() automation.Action {
	return automation.Action{
		ID:        j.ID,
		Name:      j.Name,
		Workspace: j.Workspace,
		Command:   j.Command,
		Args:      j.Args,
		Owner:     j.Owner,
		CreatedBy: j.CreatedBy,
	}
}

func (j *Job) Clone() *Job {
	c := *j
	c.History = slices.Clone(j.History)
	return &c
}
