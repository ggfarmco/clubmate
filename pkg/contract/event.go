package contract

import (
	"encoding/json"
	"time"
)

const (
	TopicWorkspaceCreated = "workspace.created"
	TopicWorkspaceUpdated = "workspace.updated"
	TopicWorkspaceDeleted = "workspace.deleted"
	TopicSessionCreated   = "session.created"
	TopicSessionExited    = "session.exited"
	TopicSessionKilled    = "session.killed"
	TopicSessionRemoved   = "session.removed"
	TopicUIOpened         = "ui.opened"
	TopicUIIdle           = "ui.idle"
	TopicUISection        = "ui.section"
)

type Event struct {
	Seq       int64           `json:"seq"`
	Topic     string          `json:"topic"`
	Source    string          `json:"source"`
	Time      time.Time       `json:"time"`
	Workspace string          `json:"workspace,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type WorkspaceChanged struct {
	Workspace string    `json:"workspace"`
	Info      Workspace `json:"info"`
}

type WorkspaceDeleted struct {
	Workspace string `json:"workspace"`
}

type SessionCreated struct {
	Session TerminalSession `json:"session"`
}

type SessionExited struct {
	ID        string       `json:"id"`
	Workspace string       `json:"workspace"`
	Owner     string       `json:"owner"`
	State     SessionState `json:"state"`
	ExitCode  *int         `json:"exitCode"`
}

type SessionGone struct {
	ID string `json:"id"`
}

type UIPresence struct {
	Workspace string `json:"workspace"`
}

type UISection struct {
	Workspace string `json:"workspace"`
	Section   string `json:"section"`
}
