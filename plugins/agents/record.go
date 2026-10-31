package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"
)

const (
	statusRunning = "running"
	statusWaiting = "waiting"
	statusDone    = "done"
	statusIdle    = "idle"
	statusError   = "error"
	statusClosed  = "closed"

	notifyAll        = "all"
	notifyNeedsInput = "needs-input"
	notifyMuted      = "muted"

	sessionKey = "session/"
	modeKey    = "mode/"
	configKey  = "config/"
	advisorKey = "advisor/"
	finishKey  = "finish/default"
	afkKey     = "afk/default"

	sessionPrefix = "agt"
	titleRunes    = 80
)

var (
	sessionIDPattern = regexp.MustCompile(`^agt_[0-9a-f]{16}$`)
	notifyPolicies   = []string{notifyAll, notifyNeedsInput, notifyMuted}
)

type record struct {
	ID             string `json:"id"`
	Workspace      string `json:"workspace"`
	Worktree       string `json:"worktree"`
	Agent          string `json:"agent"`
	AgentSessionID string `json:"agentSessionId"`
	lineage
	Title     string    `json:"title"`
	TitleSet  bool      `json:"titleSet,omitempty"`
	Status    string    `json:"status"`
	Mode      string    `json:"mode"`
	Finish    string    `json:"finish,omitempty"`
	AFK       bool      `json:"afk,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Closed    bool      `json:"closed"`
	Asks      bool      `json:"asks"`
	Notify    string    `json:"notify,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type sessionInfo struct {
	record
	Pending         []string `json:"pending,omitempty"`
	NotifyEffective string   `json:"notifyEffective"`
}

func titleOf(text string) string {
	return clipRunes(firstLine(text), titleRunes)
}

func storedNotify(policy string) string {
	if policy == notifyAll {
		return ""
	}
	return policy
}

func modeStorageKey(worktree string) string {
	return modeKey + worktreeHash(worktree)
}

func configStorageKey(id, worktree string) string {
	return configKey + id + "/" + worktreeHash(worktree)
}

func advisorStorageKey(worktree string) string {
	return advisorKey + worktreeHash(worktree)
}

func worktreeHash(worktree string) string {
	sum := sha256.Sum256([]byte(worktree))
	return hex.EncodeToString(sum[:])
}
