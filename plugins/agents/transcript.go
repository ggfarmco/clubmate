package main

import (
	"encoding/json"
	"slices"
	"strconv"

	"clubmate/plugins/agents/acp"
)

const (
	entryUser       = "user"
	entryAgent      = "agent"
	entryThought    = "thought"
	entryTool       = "tool"
	entryPlan       = "plan"
	entryPermission = "permission"
	entryNotice     = "notice"
)

var (
	chunkKinds       = map[string]string{acp.UpdateUserMessageChunk: entryUser, acp.UpdateAgentMessageChunk: entryAgent, acp.UpdateAgentThoughtChunk: entryThought}
	editKinds        = []string{"edit", "delete", "move"}
	openToolStatuses = []string{"", "pending", "in_progress"}
)

type transcriptState struct {
	entries  []*entry
	index    map[string]int
	commands []acp.AvailableCommand
	modes    []acp.SessionMode
	configs  []acp.SessionConfigOption
	turns    []turnInfo
	turn     int
	nextID   int
	edited   bool
	prompted bool
}

type transcriptView struct {
	Session  record                    `json:"session"`
	Modes    []acp.SessionMode         `json:"modes"`
	Configs  []acp.SessionConfigOption `json:"configOptions"`
	Commands []acp.AvailableCommand    `json:"commands"`
	Turns    []turnInfo                `json:"turns"`
	Entries  []*entry                  `json:"entries"`
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
	From     string                     `json:"from,omitempty"`
}

type turnInfo struct {
	Turn          int      `json:"turn"`
	CheckpointSha string   `json:"checkpointSha"`
	StopReason    string   `json:"stopReason,omitempty"`
	Commits       []string `json:"commits,omitempty"`
}

type promptPayload struct {
	acp.PromptRequest
	From    string `json:"from,omitempty"`
	Steered bool   `json:"steered,omitempty"`
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

type configPayload struct {
	ConfigID      string                    `json:"configId"`
	Value         string                    `json:"value"`
	ConfigOptions []acp.SessionConfigOption `json:"configOptions,omitempty"`
}

type checkpointPayload struct {
	Turn int    `json:"turn"`
	Sha  string `json:"sha"`
}

type turnEndPayload struct {
	StopReason string `json:"stopReason,omitempty"`
	Error      string `json:"error,omitempty"`
}

type noticeUpdate struct {
	SessionUpdate string `json:"sessionUpdate,omitempty"`
	Title         string `json:"title"`
	Description   string `json:"description"`
}

func (s *session) view(fromTurn int) transcriptView {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	v := transcriptView{Session: s.rec, Modes: slices.Clone(s.modes), Configs: slices.Clone(s.configs), Commands: slices.Clone(s.commands), Turns: slices.Clone(s.turns)}
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

func dropImageData(entries []*entry) {
	for _, en := range entries {
		content := make([]json.RawMessage, 0, len(en.Content))
		for _, raw := range en.Content {
			var b acp.ContentBlock
			if json.Unmarshal(raw, &b) == nil && b.Type == "image" {
				b.Data = ""
				if stripped, err := json.Marshal(b); err == nil {
					raw = stripped
				}
			}
			content = append(content, raw)
		}
		en.Content = content
	}
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

func (s *session) notice(title, description string) {
	s.mu.Lock()
	s.recordLocked(actorClubmate, kindUpdate, noticeUpdate{SessionUpdate: acp.UpdateNotice, Title: title, Description: description})
	s.mu.Unlock()
	s.changed(changeUpdate, false)
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
		if json.Unmarshal(e.Payload, &res) != nil {
			return
		}
		if res.Modes != nil {
			s.modes = res.Modes.AvailableModes
			s.rec.Mode = res.Modes.CurrentModeID
		}
		if res.ConfigOptions != nil {
			s.configs = res.ConfigOptions
		}
	case kindSetMode:
		var req acp.SetSessionModeRequest
		if json.Unmarshal(e.Payload, &req) == nil {
			s.rec.Mode = req.ModeID
		}
	case kindSetConfig:
		var p configPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.applyConfig(p)
		}
	case kindCheckpoint:
		var p checkpointPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.turnInfo(p.Turn).CheckpointSha = p.Sha
		}
	case kindCommits:
		var p commitsPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.turnInfo(p.Turn).Commits = p.Commits
		}
	case kindPrompt:
		s.applyPrompt(e)
	case kindTurnEnd:
		s.changesFresh = false
		var p turnEndPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.turnInfo(e.Turn).StopReason = p.StopReason
		}
	case kindError:
		s.changesFresh = false
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

func (s *session) applyConfig(p configPayload) {
	if p.ConfigOptions != nil {
		s.configs = p.ConfigOptions
		return
	}
	for i, o := range s.configs {
		if o.ID == p.ConfigID {
			raw, _ := json.Marshal(p.Value)
			s.configs[i].CurrentValue = raw
		}
	}
}

func (s *session) applyPrompt(e envelope) {
	var req promptPayload
	if json.Unmarshal(e.Payload, &req) != nil {
		return
	}
	s.turnInfo(e.Turn)
	s.prompted = true
	id := "prompt-" + strconv.Itoa(e.Turn)
	if req.Steered {
		s.nextID++
		id += "-" + strconv.Itoa(s.nextID)
	} else {
		s.edited = false
	}
	en := s.upsert(entryUser, id, e.Turn)
	en.Content = nil
	en.From = req.From
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
	case acp.UpdateConfigOption:
		var u acp.ConfigOptionUpdate
		if json.Unmarshal(payload, &u) == nil && u.ConfigOptions != nil {
			s.configs = u.ConfigOptions
		}
	case acp.UpdateSessionInfo:
		var u acp.SessionInfoUpdate
		if json.Unmarshal(payload, &u) == nil && !s.rec.TitleSet && u.Title != nil && *u.Title != "" {
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

func key(kind, id string) string {
	return kind + "/" + id
}
