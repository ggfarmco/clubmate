package api

import "clubmate/pkg/id"

const (
	IDPrefix = "agt"

	TopicTurn    = "agents.turn"
	TopicChanged = "agents.changed"
	TopicReady   = "agents.ready"

	ChangeCreated = "created"
	ChangeStatus  = "status"
	ChangeDeleted = "deleted"
)

type Session struct {
	ID            string `json:"id"`
	Workspace     string `json:"workspace"`
	Worktree      string `json:"worktree"`
	Title         string `json:"title"`
	ParentSession string `json:"parentSession"`
	HandoffFrom   string `json:"handoffFrom"`
	Status        string `json:"status"`
	Closed        bool   `json:"closed"`
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
}

func Valid(s string) bool {
	return id.Valid(IDPrefix, s)
}
