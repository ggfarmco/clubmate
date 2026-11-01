package main

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"clubmate/plugins/agents/api"
)

type session struct {
	mu        sync.Mutex
	attachMu  sync.Mutex
	changesMu sync.Mutex
	rec       record
	log       *eventLog
	loaded    bool
	deleted   bool
	changed   func(kind string, save bool)
	turned    func(api.Turn)
	resolved  func(request string)
	setup     *setup

	transcriptState
	runState
	changeState
}

type setup struct {
	done chan struct{}
	err  error
	args worktreeNewArgs
}

func newSession(rec record, log *eventLog, loaded bool, changed func(kind string, save bool)) *session {
	return &session{
		rec:             rec,
		log:             log,
		loaded:          loaded,
		changed:         changed,
		transcriptState: transcriptState{index: map[string]int{}},
		runState:        runState{perms: map[string]*pendingPermission{}},
	}
}

func (s *session) snapshot() record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec
}

func (s *session) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	_ = s.log.replay(s.apply)
}

func (s *session) actor() string {
	return "agent:" + s.rec.Agent
}

func (s *session) recordLocked(actor, kind string, payload any) {
	s.loadLocked()
	raw, ok := payload.(json.RawMessage)
	if !ok {
		var err error
		if raw, err = json.Marshal(payload); err != nil {
			return
		}
	}
	e := s.envelope(actor, kind, raw)
	s.log.write(e)
	s.apply(e)
}

func (s *session) envelope(actor, kind string, payload json.RawMessage) envelope {
	now := time.Now().UTC()
	s.rec.UpdatedAt = now
	return envelope{Workspace: s.rec.Workspace, Worktree: s.rec.Worktree, Session: s.rec.ID, ParentSession: s.rec.ParentSession, Turn: s.turn, Actor: actor, TS: now, Kind: kind, Payload: payload}
}

func (s *session) setStatusLocked(status, errText string) {
	s.rec.Status = status
	s.rec.Error = errText
	s.rec.Question = ""
	if status == statusDone {
		s.rec.Question = lastQuestion(s.entries)
	}
	s.rec.Asks = s.rec.Question != ""
	s.rec.UpdatedAt = time.Now().UTC()
}

func (s *session) settleLocked() {
	switch {
	case s.rec.closed():
	case len(s.perms) > 0:
		if s.rec.Status != statusWaiting {
			s.prior = s.rec.Status
		}
		s.setStatusLocked(statusWaiting, "")
	case s.running:
		s.setStatusLocked(statusRunning, "")
	case s.rec.Status == statusWaiting:
		s.setStatusLocked(cmpStatus(s.prior), "")
	}
}

func (s *session) settings() (string, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	values := make(map[string]string, len(s.configs))
	for _, o := range s.configs {
		values[o.ID] = o.Current()
	}
	return s.rec.Mode, values
}

func (s *session) detached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc == nil
}

func (s *session) turnStart() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *session) activity() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded || (s.rec.Status != statusRunning && s.rec.Status != statusWaiting) {
		return ""
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		en := s.entries[i]
		var title string
		switch {
		case s.rec.Status == statusWaiting && en.Kind == entryPermission && s.perms[en.ID] != nil:
			var f toolFields
			_ = json.Unmarshal(en.ToolCall, &f)
			title = f.Title
		case s.rec.Status == statusWaiting && en.Kind == entryElicitation && s.perms[en.ID] != nil && en.Elicitation != nil:
			title = en.Elicitation.Message
		case s.rec.Status == statusRunning && en.Kind == entryTool:
			s.refreshLocked(en)
			_ = json.Unmarshal(en.Tool["title"], &title)
		default:
			continue
		}
		return firstLine(title)
	}
	return ""
}

func newSetup(args worktreeNewArgs) *setup {
	return &setup{done: make(chan struct{}), args: args}
}

func (u *setup) finish(err error) {
	u.err = err
	close(u.done)
}

func (u *setup) wait() error {
	if u == nil {
		return nil
	}
	<-u.done
	return u.err
}

func (u *setup) failed() bool {
	if u == nil {
		return false
	}
	select {
	case <-u.done:
		return u.err != nil
	default:
		return false
	}
}

func lastQuestion(entries []*entry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind != entryAgent {
			continue
		}
		text := strings.TrimRight(entries[i].Text, " \t\r\n*_`")
		if !strings.HasSuffix(text, "?") {
			return ""
		}
		text = text[strings.LastIndex(text, "\n")+1:]
		start := 0
		for _, sep := range []string{". ", "! ", "? "} {
			if j := strings.LastIndex(text[:len(text)-1], sep); j >= 0 {
				start = max(start, j+len(sep))
			}
		}
		return clipRunes(strings.TrimLeft(text[start:], " \t*_`#>-"), summaryRunes)
	}
	return ""
}

func cmpStatus(prior string) string {
	if prior == "" || prior == statusRunning || prior == statusWaiting {
		return statusDone
	}
	return prior
}
