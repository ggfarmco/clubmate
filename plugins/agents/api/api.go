package api

import (
	"time"

	"clubmate/pkg/id"
)

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
	ChangeReview   = "review"
	ChangeUsage    = "usage"
	ChangeLinks    = "links"

	RequestPermission  = "permission"
	RequestElicitation = "elicitation"

	StatusClosed = "closed"
)

type Mode struct {
	ID    string
	Label string
}

var ClaudeModes = []Mode{
	{ID: "default", Label: "Ask before edits"},
	{ID: "acceptEdits", Label: "Accept edits"},
	{ID: "plan", Label: "Plan"},
	{ID: "bypassPermissions", Label: "Bypass permissions"},
}

type Session struct {
	ID            string `json:"id"`
	Workspace     string `json:"workspace"`
	Worktree      string `json:"worktree"`
	Title         string `json:"title"`
	ParentSession string `json:"parentSession"`
	HandoffFrom   string `json:"handoffFrom"`
	Status        string `json:"status"`
	Reminding     bool   `json:"reminding,omitempty"`
	Mode          string `json:"mode"`
	CronJob       string `json:"cronJob,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
}

type Run struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Number int        `json:"number,omitempty"`
	After  []RunAfter `json:"after,omitempty"`
}

type RunNeed struct {
	Run     string    `json:"run"`
	Title   string    `json:"title"`
	Number  int       `json:"number,omitempty"`
	Kind    string    `json:"kind"`
	Request string    `json:"request"`
	Node    string    `json:"node"`
	Role    string    `json:"role,omitempty"`
	Edge    int       `json:"edge,omitempty"`
	Text    string    `json:"text"`
	Command string    `json:"command,omitempty"`
	Since   time.Time `json:"since"`
}

type RunRow struct {
	ID       string   `json:"id"`
	Graph    string   `json:"graph"`
	Title    string   `json:"title"`
	Number   int      `json:"number,omitempty"`
	Worktree string   `json:"worktree,omitempty"`
	State    string   `json:"state"`
	Detail   string   `json:"detail"`
	NeedsYou bool     `json:"needsYou,omitempty"`
	Request  string   `json:"request,omitempty"`
	Pointer  string   `json:"pointer,omitempty"`
	Sessions []string `json:"sessions"`
	Home     string   `json:"home"`
	Group    string   `json:"group"`
	Status   string   `json:"status"`
}

type DockRow struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Icon        string `json:"icon"`
	Detail      string `json:"detail"`
	PhoneDetail string `json:"phoneDetail"`
	State       string `json:"state,omitempty"`
	Which       string `json:"which,omitempty"`
	Attention   bool   `json:"attention,omitempty"`
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
