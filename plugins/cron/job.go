package main

import (
	"encoding/json"
	"time"
)

const (
	statusOK      = "ok"
	statusError   = "error"
	statusSkipped = "skipped"
)

const (
	missedOnce = "once"
	missedSkip = "skip"
)

type Run struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	Result string    `json:"result,omitempty"`
}

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
