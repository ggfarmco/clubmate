package contract

import (
	"encoding/json"
	"time"
)

const (
	TopicProjectCreated = "project.created"
	TopicProjectUpdated = "project.updated"
	TopicProjectDeleted = "project.deleted"
	TopicSessionCreated = "session.created"
	TopicSessionExited  = "session.exited"
	TopicSessionKilled  = "session.killed"
	TopicSessionRemoved = "session.removed"
	TopicUIOpened       = "ui.opened"
	TopicUIIdle         = "ui.idle"
	TopicUISection      = "ui.section"
)

type Event struct {
	Seq     int64           `json:"seq"`
	Topic   string          `json:"topic"`
	Source  string          `json:"source"`
	Time    time.Time       `json:"time"`
	Payload json.RawMessage `json:"payload"`
}

type ProjectChanged struct {
	Project Workspace `json:"project"`
}

type ProjectDeleted struct {
	ID string `json:"id"`
}

type SessionCreated struct {
	Session TerminalSession `json:"session"`
}

type SessionExited struct {
	ID        string       `json:"id"`
	ProjectID string       `json:"projectId"`
	Owner     string       `json:"owner"`
	State     SessionState `json:"state"`
	ExitCode  *int         `json:"exitCode"`
}

type SessionGone struct {
	ID string `json:"id"`
}

type UIPresence struct {
	ProjectID string `json:"projectId"`
}

type UISection struct {
	ProjectID string `json:"projectId"`
	Section   string `json:"section"`
}
