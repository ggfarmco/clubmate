package contract

import "time"

type SessionState string

const (
	SessionStateRunning SessionState = "running"
	SessionStateExited  SessionState = "exited"
	SessionStateLost    SessionState = "lost"
)

type SessionInfo struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	Owner     string    `json:"owner"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd"`
	HostPID   int       `json:"hostPid"`
	ChildPID  int       `json:"childPid"`
	PGID      int       `json:"pgid"`
	StartedAt time.Time `json:"startedAt"`
	ExitCode  *int      `json:"exitCode"`
}

type TerminalSession struct {
	SessionInfo
	State SessionState `json:"state"`
}
