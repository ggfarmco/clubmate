package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	coalesceDelay = 2 * time.Second
	coalesceLimit = 64 << 10
	sessionsDir   = "sessions"
	logExt        = ".jsonl"

	kindUpdate     = "session/update"
	kindNew        = "session/new"
	kindResume     = "session/resume"
	kindPrompt     = "session/prompt"
	kindCancel     = "session/cancel"
	kindClose      = "session/close"
	kindSetMode    = "session/set_mode"
	kindSetConfig  = "session/set_config_option"
	kindPermission = "session/request_permission"
	kindAnswer     = "permission"
	kindCheckpoint = "checkpoint"
	kindCommits    = "commits"
	kindTurnEnd    = "turn_end"
	kindError      = "error"

	actorUser     = "user"
	actorClubmate = "clubmate"
)

type envelope struct {
	Workspace     string          `json:"workspace"`
	Worktree      string          `json:"worktree"`
	Session       string          `json:"session"`
	ParentSession string          `json:"parentSession"`
	Role          string          `json:"role"`
	Turn          int             `json:"turn"`
	Seq           int64           `json:"seq"`
	Actor         string          `json:"actor"`
	TS            time.Time       `json:"ts"`
	Kind          string          `json:"kind"`
	Payload       json.RawMessage `json:"payload"`
}

type pendingChunk struct {
	env       envelope
	update    string
	messageID string
	text      strings.Builder
}

type eventLog struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	seq     int64
	pending *pendingChunk
	timer   *time.Timer
	removed bool
}

func logPath(stateDir, session string) string {
	return filepath.Join(stateDir, sessionsDir, session+logExt)
}

func newEventLog(path string) *eventLog {
	return &eventLog{path: path}
}

func (l *eventLog) write(e envelope) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked()
	l.appendLocked(e)
}

func (l *eventLog) chunk(e envelope, update, messageID, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.removed {
		return
	}
	if p := l.pending; p != nil && (p.update != update || p.messageID != messageID || p.env.Turn != e.Turn) {
		l.flushLocked()
	}
	if l.pending == nil {
		e.TS = time.Now().UTC()
		l.pending = &pendingChunk{env: e, update: update, messageID: messageID}
		l.timer = time.AfterFunc(coalesceDelay, l.flush)
	}
	l.pending.text.WriteString(text)
	if l.pending.text.Len() >= coalesceLimit {
		l.flushLocked()
	}
}

func (l *eventLog) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked()
}

func (l *eventLog) flushLocked() {
	p := l.pending
	if p == nil {
		return
	}
	l.pending = nil
	l.timer.Stop()
	payload := map[string]any{"sessionUpdate": p.update, "content": map[string]string{"type": "text", "text": p.text.String()}}
	if p.messageID != "" {
		payload["messageId"] = p.messageID
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	p.env.Payload = raw
	l.appendLocked(p.env)
}

func (l *eventLog) appendLocked(e envelope) {
	if l.removed {
		return
	}
	l.seq++
	e.Seq = l.seq
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	line, err := json.Marshal(e)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: encode log entry:", err)
		return
	}
	if l.file == nil {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "agents: log dir:", err)
			return
		}
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: open log:", err)
			return
		}
		l.file = f
	}
	if _, err := l.file.Write(append(line, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "agents: write log:", err)
	}
}

func (l *eventLog) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushLocked()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

func (l *eventLog) remove() error {
	l.mu.Lock()
	l.flushLocked()
	l.removed = true
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	l.mu.Unlock()
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (l *eventLog) replay(fn func(envelope)) error {
	f, err := os.Open(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var e envelope
			if json.Unmarshal(line, &e) == nil {
				l.mu.Lock()
				l.seq = max(l.seq, e.Seq)
				l.mu.Unlock()
				fn(e)
			}
		}
		if err != nil {
			return nil
		}
	}
}
