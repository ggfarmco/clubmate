package contract

import "time"

type ProcessInfo struct {
	PID       int       `json:"pid"`
	PPID      int       `json:"ppid"`
	PGID      int       `json:"pgid"`
	Comm      string    `json:"comm"`
	Args      []string  `json:"args"`
	StartTime time.Time `json:"startTime"`
	RSSKiB    int64     `json:"rssKiB"`
	CPU       float64   `json:"cpu"`
}

type ProcessNode struct {
	Process   ProcessInfo `json:"process"`
	SessionID string      `json:"sessionId"`
	Orphan    bool        `json:"orphan"`
	Duplicate bool        `json:"duplicate"`
}

type ProcessRoot struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Label       string `json:"label"`
	Owner       string `json:"owner"`
	PID         int    `json:"pid"`
	Ended       bool   `json:"ended"`
}

type KillRequest struct {
	PIDs        []int  `json:"pids"`
	SessionID   string `json:"sessionId"`
	WorkspaceID string `json:"workspaceId"`
	OrphansOnly bool   `json:"orphansOnly"`
}
