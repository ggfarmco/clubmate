package api

import "clubmate/pkg/id"

const (
	IDPrefix = "agt"

	TopicTurn    = "agents.turn"
	TopicChanged = "agents.changed"
	TopicReady   = "agents.ready"
	TopicUsage   = "agents.usage"

	ChangeCreated = "created"
	ChangeStatus  = "status"
	ChangeDeleted = "deleted"

	StatusClosed = "closed"
)

type Session struct {
	ID            string `json:"id"`
	Workspace     string `json:"workspace"`
	Worktree      string `json:"worktree"`
	Title         string `json:"title"`
	ParentSession string `json:"parentSession"`
	HandoffFrom   string `json:"handoffFrom"`
	Status        string `json:"status"`
}

type Turn struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Status    string `json:"status"`
	Asks      bool   `json:"asks"`
	Reply     string `json:"reply"`
}

type Changed struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Kind      string `json:"kind"`
	Status    string `json:"status,omitempty"`
}

func (s Session) Closed() bool {
	return s.Status == StatusClosed
}

func Valid(s string) bool {
	return id.Valid(IDPrefix, s)
}
