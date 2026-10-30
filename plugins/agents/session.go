package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clubmate/plugins/agents/acp"
)

const (
	statusRunning = "running"
	statusWaiting = "waiting"
	statusDone    = "done"
	statusIdle    = "idle"
	statusError   = "error"
	statusClosed  = "closed"

	entryUser       = "user"
	entryAgent      = "agent"
	entryThought    = "thought"
	entryTool       = "tool"
	entryPlan       = "plan"
	entryPermission = "permission"
	entryNotice     = "notice"

	changeUpdate  = "update"
	changeStatus  = "status"
	changeCreated = "created"
	changeDeleted = "deleted"

	sessionPrefix = "agt"
	titleRunes    = 80
)

var (
	errUnknownRequest = errors.New("unknown permission request")
	errUnknownOption  = errors.New("unknown permission option")

	sessionIDPattern = regexp.MustCompile(`^agt_[0-9a-f]{16}$`)
	chunkKinds       = map[string]string{acp.UpdateUserMessageChunk: entryUser, acp.UpdateAgentMessageChunk: entryAgent, acp.UpdateAgentThoughtChunk: entryThought}
	editKinds        = []string{"edit", "delete", "move"}
	openToolStatuses = []string{"", "pending", "in_progress"}
)

type record struct {
	ID             string    `json:"id"`
	Workspace      string    `json:"workspace"`
	Worktree       string    `json:"worktree"`
	Agent          string    `json:"agent"`
	AgentSessionID string    `json:"agentSessionId"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Mode           string    `json:"mode"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Closed         bool      `json:"closed"`
	Error          string    `json:"error,omitempty"`
}

type sessionInfo struct {
	record
	Pending []string `json:"pending,omitempty"`
}

type entry struct {
	Kind     string                     `json:"kind"`
	ID       string                     `json:"id"`
	Turn     int                        `json:"turn"`
	Text     string                     `json:"text,omitempty"`
	Content  []json.RawMessage          `json:"content,omitempty"`
	Tool     map[string]json.RawMessage `json:"tool,omitempty"`
	Plan     json.RawMessage            `json:"plan,omitempty"`
	ToolCall json.RawMessage            `json:"toolCall,omitempty"`
	Options  []acp.PermissionOption     `json:"options,omitempty"`
	Outcome  *acp.PermissionOutcome     `json:"outcome,omitempty"`
	Notice   json.RawMessage            `json:"notice,omitempty"`
}

type turnInfo struct {
	Turn          int    `json:"turn"`
	CheckpointSha string `json:"checkpointSha"`
	StopReason    string `json:"stopReason,omitempty"`
}

type transcriptView struct {
	Session  record                 `json:"session"`
	Modes    []acp.SessionMode      `json:"modes"`
	Commands []acp.AvailableCommand `json:"commands"`
	Turns    []turnInfo             `json:"turns"`
	Entries  []*entry               `json:"entries"`
}

type permissionPayload struct {
	ID        string          `json:"id"`
	RequestID string          `json:"requestId"`
	Params    json.RawMessage `json:"params"`
}

type answerPayload struct {
	Request string                `json:"request"`
	Outcome acp.PermissionOutcome `json:"outcome"`
}

type checkpointPayload struct {
	Turn int    `json:"turn"`
	Sha  string `json:"sha"`
}

type turnEndPayload struct {
	StopReason string `json:"stopReason,omitempty"`
	Error      string `json:"error,omitempty"`
}

type pendingPermission struct {
	request string
	reply   chan acp.PermissionOutcome
}

type queuedPrompt struct {
	ID          string
	Text        string
	Attachments []attachment
}

type session struct {
	mu       sync.Mutex
	attachMu sync.Mutex
	rec      record
	log      *eventLog
	loaded   bool
	deleted  bool
	changed  func(kind string, save bool)

	entries  []*entry
	index    map[string]int
	commands []acp.AvailableCommand
	modes    []acp.SessionMode
	turns    []turnInfo
	turn     int
	nextID   int
	edited   bool

	proc            *agentProc
	running         bool
	cancelRequested bool
	prior           string
	perms           map[string]*pendingPermission
	queue           []queuedPrompt
	queueSeq        int
}

func newSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return sessionPrefix + "_" + hex.EncodeToString(b)
}

func newSession(rec record, log *eventLog, loaded bool, changed func(kind string, save bool)) *session {
	return &session{rec: rec, log: log, loaded: loaded, changed: changed, index: map[string]int{}, perms: map[string]*pendingPermission{}}
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
	s.rec.UpdatedAt = time.Now().UTC()
	return envelope{Workspace: s.rec.Workspace, Worktree: s.rec.Worktree, Session: s.rec.ID, Turn: s.turn, Actor: actor, Kind: kind, Payload: payload}
}

func (s *session) update(params json.RawMessage) {
	var n acp.SessionNotification
	if json.Unmarshal(params, &n) != nil || len(n.Update) == 0 {
		return
	}
	var head acp.UpdateHeader
	if json.Unmarshal(n.Update, &head) != nil {
		return
	}
	s.mu.Lock()
	s.loadLocked()
	e := s.envelope(s.actor(), kindUpdate, n.Update)
	var chunk acp.ContentChunk
	if _, ok := chunkKinds[head.SessionUpdate]; ok && json.Unmarshal(n.Update, &chunk) == nil && chunk.Content.Type == "text" {
		s.log.chunk(e, head.SessionUpdate, chunk.MessageID, chunk.Content.Text)
	} else {
		s.log.write(e)
	}
	title, mode := s.rec.Title, s.rec.Mode
	s.apply(e)
	meta := title != s.rec.Title || mode != s.rec.Mode
	s.mu.Unlock()
	s.changed(changeUpdate, meta)
}

func (s *session) apply(e envelope) {
	if e.Turn > s.turn {
		s.turn = e.Turn
	}
	switch e.Kind {
	case kindUpdate:
		s.applyUpdate(e.Turn, e.Payload)
	case kindNew, kindResume:
		var res acp.NewSessionResponse
		if json.Unmarshal(e.Payload, &res) == nil && res.Modes != nil {
			s.modes = res.Modes.AvailableModes
			s.rec.Mode = res.Modes.CurrentModeID
		}
	case kindSetMode:
		var req acp.SetSessionModeRequest
		if json.Unmarshal(e.Payload, &req) == nil {
			s.rec.Mode = req.ModeID
		}
	case kindCheckpoint:
		var p checkpointPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.turnInfo(p.Turn).CheckpointSha = p.Sha
		}
	case kindPrompt:
		s.applyPrompt(e)
	case kindTurnEnd:
		var p turnEndPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.turnInfo(e.Turn).StopReason = p.StopReason
		}
	case kindCancel:
		s.cancelTools(e.Turn)
	case kindPermission:
		s.nextID++
		var p permissionPayload
		var req acp.RequestPermissionRequest
		if json.Unmarshal(e.Payload, &p) == nil && json.Unmarshal(p.Params, &req) == nil {
			en := s.upsert(entryPermission, p.ID, e.Turn)
			en.ToolCall, en.Options = req.ToolCall, req.Options
		}
	case kindAnswer:
		var p answerPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			if i, ok := s.index[key(entryPermission, p.Request)]; ok {
				s.entries[i].Outcome = &p.Outcome
			}
		}
	}
}

func (s *session) applyPrompt(e envelope) {
	var req acp.PromptRequest
	if json.Unmarshal(e.Payload, &req) != nil {
		return
	}
	s.turnInfo(e.Turn)
	s.edited = false
	en := s.upsert(entryUser, "prompt-"+strconv.Itoa(e.Turn), e.Turn)
	en.Content = nil
	for _, b := range req.Prompt {
		raw, err := json.Marshal(b)
		if err == nil {
			en.Content = append(en.Content, raw)
		}
		if b.Type == "text" && en.Text == "" {
			en.Text = b.Text
		}
	}
	if s.rec.Title == "" {
		s.rec.Title = titleOf(en.Text)
	}
}

func (s *session) applyUpdate(turn int, payload json.RawMessage) {
	var head acp.UpdateHeader
	if json.Unmarshal(payload, &head) != nil {
		return
	}
	switch head.SessionUpdate {
	case acp.UpdateUserMessageChunk, acp.UpdateAgentMessageChunk, acp.UpdateAgentThoughtChunk:
		var c acp.ContentChunk
		if json.Unmarshal(payload, &c) != nil {
			return
		}
		s.messageEntry(chunkKinds[head.SessionUpdate], c.MessageID, turn).Text += c.Content.Text
	case acp.UpdateToolCall, acp.UpdateToolCallUpdate:
		s.applyTool(head.SessionUpdate == acp.UpdateToolCall, turn, payload)
	case acp.UpdatePlan:
		s.upsert(entryPlan, "plan-"+strconv.Itoa(turn), turn).Plan = payload
	case acp.UpdateAvailableCommands:
		var u acp.AvailableCommandsUpdate
		if json.Unmarshal(payload, &u) == nil {
			s.commands = u.AvailableCommands
		}
	case acp.UpdateCurrentMode:
		var u acp.CurrentModeUpdate
		if json.Unmarshal(payload, &u) == nil {
			s.rec.Mode = u.CurrentModeID
		}
	case acp.UpdateSessionInfo:
		var u acp.SessionInfoUpdate
		if json.Unmarshal(payload, &u) == nil && u.Title != nil && *u.Title != "" {
			s.rec.Title = titleOf(*u.Title)
		}
	case acp.UpdateNotice:
		s.nextID++
		s.upsert(entryNotice, "notice-"+strconv.Itoa(s.nextID), turn).Notice = payload
	}
}

func (s *session) applyTool(create bool, turn int, payload json.RawMessage) {
	var fields map[string]json.RawMessage
	var head acp.ToolCallHeader
	if json.Unmarshal(payload, &fields) != nil || json.Unmarshal(payload, &head) != nil || head.ToolCallID == "" {
		return
	}
	delete(fields, "sessionUpdate")
	en := s.upsert(entryTool, head.ToolCallID, turn)
	if create || en.Tool == nil {
		en.Tool = map[string]json.RawMessage{}
	}
	for k, v := range fields {
		en.Tool[k] = v
	}
	var kind string
	if json.Unmarshal(en.Tool["kind"], &kind) == nil && slices.Contains(editKinds, kind) {
		s.edited = true
	}
}

func (s *session) cancelTools(turn int) {
	for _, en := range s.entries {
		if en.Kind != entryTool || en.Turn != turn {
			continue
		}
		var status string
		_ = json.Unmarshal(en.Tool["status"], &status)
		if slices.Contains(openToolStatuses, status) {
			en.Tool["status"] = json.RawMessage(`"cancelled"`)
		}
	}
}

func (s *session) messageEntry(kind, messageID string, turn int) *entry {
	if messageID != "" {
		return s.upsert(kind, messageID, turn)
	}
	if n := len(s.entries); n > 0 {
		if last := s.entries[n-1]; last.Kind == kind && last.Turn == turn {
			return last
		}
	}
	s.nextID++
	return s.upsert(kind, kind+"-"+strconv.Itoa(s.nextID), turn)
}

func (s *session) upsert(kind, id string, turn int) *entry {
	k := key(kind, id)
	if i, ok := s.index[k]; ok {
		return s.entries[i]
	}
	en := &entry{Kind: kind, ID: id, Turn: turn}
	s.index[k] = len(s.entries)
	s.entries = append(s.entries, en)
	return en
}

func (s *session) turnInfo(n int) *turnInfo {
	for i := range s.turns {
		if s.turns[i].Turn == n {
			return &s.turns[i]
		}
	}
	s.turns = append(s.turns, turnInfo{Turn: n})
	return &s.turns[len(s.turns)-1]
}

func (s *session) requestPermission(ctx context.Context, request string, params json.RawMessage) acp.PermissionOutcome {
	perm := &pendingPermission{request: request, reply: make(chan acp.PermissionOutcome, 1)}
	s.mu.Lock()
	s.loadLocked()
	id := entryPermission + "-" + strconv.Itoa(s.nextID+1)
	s.perms[id] = perm
	s.recordLocked(s.actor(), kindPermission, permissionPayload{ID: id, RequestID: request, Params: params})
	s.settleLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	var out acp.PermissionOutcome
	select {
	case out = <-perm.reply:
	case <-ctx.Done():
		out = acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
	}
	s.mu.Lock()
	delete(s.perms, id)
	s.recordLocked(actorUser, kindAnswer, answerPayload{Request: id, Outcome: out})
	s.settleLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	return out
}

func (s *session) answer(id, option string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	perm, ok := s.perms[id]
	if !ok {
		return errUnknownRequest
	}
	en := s.entries[s.index[key(entryPermission, id)]]
	if !slices.ContainsFunc(en.Options, func(o acp.PermissionOption) bool { return o.OptionID == option }) {
		return errUnknownOption
	}
	delete(s.perms, id)
	perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: option}
	return nil
}

func (s *session) pending() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := slices.Collect(maps.Keys(s.perms))
	slices.Sort(ids)
	return ids
}

func (s *session) cancelRequest(request string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, perm := range s.perms {
		if perm.request == request {
			delete(s.perms, id)
			perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
			return true
		}
	}
	return false
}

func (s *session) cancelPermissionsLocked() {
	for id, perm := range s.perms {
		delete(s.perms, id)
		perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
	}
}

func (s *session) beginTurnLocked() int {
	s.running = true
	s.cancelRequested = false
	s.turn++
	s.settleLocked()
	return s.turn
}

func (s *session) enqueueLocked(args promptArgs) {
	s.queueSeq++
	s.queue = append(s.queue, queuedPrompt{ID: "q" + strconv.Itoa(s.queueSeq), Text: args.Text, Attachments: args.Attachments})
}

func (s *session) dequeueLocked(extra *promptArgs) (promptArgs, bool) {
	parts := s.queue
	if extra != nil {
		parts = append(slices.Clone(parts), queuedPrompt{Text: extra.Text, Attachments: extra.Attachments})
	}
	if len(parts) == 0 {
		return promptArgs{}, false
	}
	s.queue = nil
	var args promptArgs
	var texts []string
	for _, q := range parts {
		if strings.TrimSpace(q.Text) != "" {
			texts = append(texts, q.Text)
		}
		args.Attachments = append(args.Attachments, q.Attachments...)
	}
	args.Text = strings.Join(texts, "\n\n")
	return args, true
}

func (s *session) unqueue(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = slices.DeleteFunc(s.queue, func(q queuedPrompt) bool { return q.ID == id })
}

func (s *session) queued() []queuedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.queue)
}

func (s *session) setStatusLocked(status, errText string) {
	s.rec.Status = status
	s.rec.Error = errText
	s.rec.UpdatedAt = time.Now().UTC()
}

func (s *session) settleLocked() {
	switch {
	case s.rec.Closed:
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

func cmpStatus(prior string) string {
	if prior == "" || prior == statusRunning || prior == statusWaiting {
		return statusDone
	}
	return prior
}

func (s *session) view(fromTurn int) transcriptView {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	v := transcriptView{Session: s.rec, Modes: slices.Clone(s.modes), Commands: slices.Clone(s.commands), Turns: slices.Clone(s.turns)}
	for _, en := range s.entries {
		if en.Turn >= fromTurn {
			c := *en
			if en.Tool != nil {
				c.Tool = make(map[string]json.RawMessage, len(en.Tool))
				for k, val := range en.Tool {
					c.Tool[k] = val
				}
			}
			v.Entries = append(v.Entries, &c)
		}
	}
	return v
}

func (s *session) snapshot() record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec
}

func key(kind, id string) string {
	return kind + "/" + id
}

func titleOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if utf8.RuneCountInString(line) <= titleRunes {
		return line
	}
	return string([]rune(line)[:titleRunes]) + "…"
}
