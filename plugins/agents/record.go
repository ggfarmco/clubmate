package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"clubmate/plugins/agents/api"
)

const (
	statusRunning = "running"
	statusWaiting = "waiting"
	statusDone    = "done"
	statusIdle    = "idle"
	statusError   = "error"
	statusClosed  = api.StatusClosed

	pendingQuestion = "question"

	notifyAll        = "all"
	notifyNeedsInput = "needs-input"
	notifyMuted      = "muted"

	sessionKey = "session/"
	modeKey    = "mode/"
	configKey  = "config/"
	advisorKey = "advisor/"
	finishKey  = "finish/default"
	afkKey     = "afk/default"

	titleRunes   = 80
	summaryRunes = 200
)

var notifyPolicies = []string{notifyAll, notifyNeedsInput, notifyMuted}

type record struct {
	ID             string `json:"id"`
	Workspace      string `json:"workspace"`
	Worktree       string `json:"worktree"`
	Agent          string `json:"agent"`
	AgentSessionID string `json:"agentSessionId"`
	lineage
	Run             *api.Run  `json:"run,omitempty"`
	CronJob         string    `json:"cronJob,omitempty"`
	Trigger         string    `json:"trigger,omitempty"`
	Title           string    `json:"title"`
	TitleSet        bool      `json:"titleSet,omitempty"`
	Status          string    `json:"status"`
	Mode            string    `json:"mode"`
	Finish          string    `json:"finish,omitempty"`
	AFK             bool      `json:"afk,omitempty"`
	Suspended       bool      `json:"suspended,omitempty"`
	AutoClosed      bool      `json:"autoClosed,omitempty"`
	Review          bool      `json:"review,omitempty"`
	AsksParent      bool      `json:"asksParent,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	Asks            bool      `json:"asks"`
	Question        string    `json:"question,omitempty"`
	Notify          string    `json:"notify,omitempty"`
	Error           string    `json:"error,omitempty"`
	DisallowedTools []string  `json:"disallowedTools,omitempty"`
	SystemPrompt    string    `json:"systemPrompt,omitempty"`
	StartMode       string    `json:"startMode,omitempty"`
	StartModel      string    `json:"startModel,omitempty"`
}

type sessionInfo struct {
	record
	Pending         []string        `json:"pending,omitempty"`
	PendingSummary  *pendingSummary `json:"pendingSummary,omitempty"`
	TurnStartedAt   *time.Time      `json:"turnStartedAt,omitempty"`
	NotifyEffective string          `json:"notifyEffective"`
	NeedsYou        bool            `json:"needsYou"`
	Service         bool            `json:"service,omitempty"`
	NeedsYouSince   *time.Time      `json:"needsYouSince,omitempty"`
	Activity        *toolActivity   `json:"activity,omitempty"`
	ChildStatus     string          `json:"childStatus,omitempty"`
	Failure         *sessionFailure `json:"failure,omitempty"`
	ModeLabel       string          `json:"modeLabel,omitempty"`
}

type sessionFailure struct {
	Word    string          `json:"word"`
	Badge   string          `json:"badge"`
	Line    string          `json:"line"`
	Actions []failureAction `json:"actions,omitempty"`
}

type toolActivity struct {
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

type pendingSummary struct {
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	Tool string    `json:"tool,omitempty"`
	At   time.Time `json:"at"`
}

type sessionStore interface {
	StorageList(ctx context.Context, prefix string) ([]string, error)
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
	StorageDelete(ctx context.Context, key string) error
}

func loadRecords(ctx context.Context, kv sessionStore) ([]record, error) {
	keys, err := kv.StorageList(ctx, sessionKey)
	if err != nil {
		return nil, err
	}
	var ids []string
	latest := map[string]record{}
	legacy := map[string]string{}
	for _, k := range keys {
		var rec record
		if found, err := kv.StorageGet(ctx, k, &rec); err != nil || !found || !api.Valid(rec.ID) {
			continue
		}
		if !strings.Contains(strings.TrimPrefix(k, sessionKey), "/") {
			legacy[rec.ID] = k
		}
		prev, seen := latest[rec.ID]
		if !seen {
			ids = append(ids, rec.ID)
		}
		if !seen || rec.UpdatedAt.After(prev.UpdatedAt) {
			latest[rec.ID] = rec
		}
	}
	out := make([]record, 0, len(ids))
	for _, id := range ids {
		rec := latest[id]
		out = append(out, rec)
		old, ok := legacy[id]
		if !ok {
			continue
		}
		err := kv.StorageSet(ctx, sessionStorageKey(rec), rec)
		if err == nil {
			err = kv.StorageDelete(ctx, old)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: migrate session:", err)
		}
	}
	return out, nil
}

func (r record) closed() bool {
	return r.Status == statusClosed
}

func (r record) restricted() bool {
	return len(r.DisallowedTools) > 0 || r.SystemPrompt != "" || r.StartMode != "" || r.StartModel != ""
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

func sessionStorageKey(rec record) string {
	return sessionKey + rec.Workspace + "/" + rec.ID
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
