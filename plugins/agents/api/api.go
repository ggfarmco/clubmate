package api

import "clubmate/pkg/id"

const (
	IDPrefix = "agt"

	TopicTurn    = "agents.turn"
	TopicChanged = "agents.changed"
	TopicReady   = "agents.ready"
	TopicUsage   = "agents.usage"

	ChangeCreated  = "created"
	ChangeStatus   = "status"
	ChangeDeleted  = "deleted"
	ChangeResolved = "resolved"

	RequestPermission  = "permission"
	RequestElicitation = "elicitation"

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
	Mode          string `json:"mode"`
	CronJob       string `json:"cronJob,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
}

type Run struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	After []RunAfter `json:"after,omitempty"`
}

type RunAfter struct {
	Role    string `json:"role"`
	On      string `json:"on"`
	Outcome string `json:"outcome,omitempty"`
}

type Turn struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Turn      int    `json:"turn"`
	Status    string `json:"status"`
	Asks      bool   `json:"asks"`
	Finished  bool   `json:"finished"`
	Reply     string `json:"reply"`
	CronJob   string `json:"cronJob,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
}

type Changed struct {
	Workspace   string `json:"workspace"`
	Session     string `json:"session"`
	Kind        string `json:"kind"`
	Status      string `json:"status,omitempty"`
	Request     string `json:"request,omitempty"`
	RequestKind string `json:"requestKind,omitempty"`
}

func (s Session) Closed() bool {
	return s.Status == StatusClosed
}

func Valid(s string) bool {
	return id.Valid(IDPrefix, s)
}
