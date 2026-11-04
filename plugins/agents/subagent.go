package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/agents/acp"
)

const (
	subagentKind      = "agent"
	subagentFallback  = "Subagent"
	subagentWaiting   = "waiting for approval"
	subagentThinking  = "Thinking"
	commandActivity   = "Running a command"
	promptLines       = 3
	activitySeparator = " · "
)

var errNotStoppable = errors.New("subagent cannot be stopped")

type subagent struct {
	id      string
	parent  *subagent
	entry   *entry
	title   string
	role    string
	state   *acp.SubagentState
	prompt  string
	reply   string
	entries []*entry
	index   map[string]int
	nextID  int
	dirty   bool
	cancel  bool
}

func (s *session) applySubagent(turn int, agentSession string, payload json.RawMessage) {
	sub := s.subagents[agentSession]
	if sub == nil {
		return
	}
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
		sub.message(chunkKinds[head.SessionUpdate], c.MessageID).Text += c.Content.Text
	case acp.UpdateToolCall, acp.UpdateToolCallUpdate:
		if sub.tool(head.SessionUpdate == acp.UpdateToolCall, payload, s.at) {
			s.edited = true
		}
	case acp.UpdateSubagent:
		s.announceSubagent(sub, turn, payload)
		return
	case acp.UpdateSessionMessage:
		s.sessionMessage(payload)
		return
	default:
		return
	}
	sub.touch()
}

func (s *session) announceSubagent(parent *subagent, turn int, payload json.RawMessage) {
	var u acp.SubagentUpdate
	if json.Unmarshal(payload, &u) != nil || u.SessionID == "" {
		return
	}
	if s.subagents == nil {
		s.subagents = map[string]*subagent{}
	}
	sub := s.subagents[u.SessionID]
	if sub == nil {
		sub = &subagent{id: u.SessionID, parent: parent, index: map[string]int{}}
		if parent == nil {
			sub.entry = s.upsert(entryTool, u.SessionID, turn)
		} else {
			sub.entry = parent.upsert(entryTool, u.SessionID)
		}
		sub.entry.sub = sub
		s.subagents[u.SessionID] = sub
	}
	if u.Title != nil {
		sub.title = *u.Title
	}
	if u.Description != nil {
		sub.role = *u.Description
	}
	if u.State != nil {
		sub.state = u.State
	}
	if u.Capabilities != nil {
		sub.cancel = u.Capabilities.Cancel != nil
	}
	sub.touch()
}

func (s *session) sessionMessage(payload json.RawMessage) {
	var m acp.SessionMessage
	if json.Unmarshal(payload, &m) != nil || m.MessageID == "" {
		return
	}
	var parts []string
	for _, b := range m.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	text := strings.Join(parts, "\n")
	if from := s.subagents[m.SenderSessionID]; from != nil && from.repliesTo(m.RecipientSessionID, s.subagents) {
		from.reply = text
		from.touch()
		return
	}
	to := s.subagents[m.RecipientSessionID]
	if to == nil {
		return
	}
	to.upsert(entryUser, m.MessageID).Text = text
	if to.prompt == "" {
		to.prompt = text
	}
	to.touch()
}

func (s *session) loseSubagents() {
	for _, sub := range s.subagents {
		if sub.working() {
			sub.state = &acp.SubagentState{State: acp.SubagentUnknown}
			sub.touch()
		}
	}
}

func (s *session) refreshLocked(en *entry) {
	if en.sub != nil && en.sub.dirty {
		en.Tool = en.sub.toolFields()
		en.Stoppable = en.sub.parent == nil && en.sub.cancel && en.sub.working()
		en.sub.dirty = false
	}
}

func (s *session) stopSubagent(id string) error {
	s.mu.Lock()
	sub, p := s.subagents[id], s.proc
	stoppable := sub != nil && sub.cancel && sub.working()
	s.mu.Unlock()
	if !stoppable || p == nil {
		return fmt.Errorf("%w: subagent %q", errNotStoppable, id)
	}
	return p.conn.Notify(acp.MethodSessionCancel, acp.SessionRequest{SessionID: id})
}

func (sub *subagent) repliesTo(recipient string, known map[string]*subagent) bool {
	if sub.parent != nil {
		return sub.parent.id == recipient
	}
	return known[recipient] == nil
}

func (sub *subagent) touch() {
	for x := sub; x != nil; x = x.parent {
		x.dirty = true
		x.entry.node = nil
	}
}

func (sub *subagent) upsert(kind, id string) *entry {
	k := key(kind, id)
	if i, ok := sub.index[k]; ok {
		return sub.entries[i]
	}
	en := &entry{Kind: kind, ID: id, Turn: sub.entry.Turn}
	sub.index[k] = len(sub.entries)
	sub.entries = append(sub.entries, en)
	return en
}

func (sub *subagent) message(kind, messageID string) *entry {
	if messageID != "" {
		return sub.upsert(kind, messageID)
	}
	if n := len(sub.entries); n > 0 && sub.entries[n-1].Kind == kind {
		return sub.entries[n-1]
	}
	sub.nextID++
	return sub.upsert(kind, kind+"-"+strconv.Itoa(sub.nextID))
}

func (sub *subagent) tool(create bool, payload json.RawMessage, at time.Time) bool {
	var fields map[string]json.RawMessage
	var head acp.ToolCallHeader
	if json.Unmarshal(payload, &fields) != nil || json.Unmarshal(payload, &head) != nil || head.ToolCallID == "" {
		return false
	}
	dropUnkeptToolFields(fields)
	en := sub.upsert(entryTool, head.ToolCallID)
	if en.At.IsZero() {
		en.At = at
	}
	if create || en.Tool == nil {
		en.Tool = map[string]json.RawMessage{}
	}
	for k, v := range fields {
		en.Tool[k] = v
	}
	var kind string
	return json.Unmarshal(en.Tool["kind"], &kind) == nil && slices.Contains(editKinds, kind)
}

func (sub *subagent) toolFields() map[string]json.RawMessage {
	title := sub.name()
	if activity := sub.activity(); activity != "" {
		title += activitySeparator + activity
	}
	content := []map[string]any{{"type": "content", "content": map[string]string{"type": "text", "text": sub.body()}}}
	input := map[string]string{"description": sub.title}
	if sub.prompt != "" {
		input["prompt"] = sub.prompt
	}
	if sub.role != "" {
		input["subagent_type"] = sub.role
	}
	out := map[string]json.RawMessage{}
	for k, v := range map[string]any{"toolCallId": sub.id, "kind": subagentKind, "title": title, "status": sub.status(), "content": content, "rawInput": input} {
		out[k], _ = json.Marshal(v)
	}
	return out
}

func (sub *subagent) name() string {
	title := cmp.Or(sub.title, sub.role, subagentFallback)
	if sub.role != "" && sub.role != title {
		return sub.role + ": " + title
	}
	return title
}

func (sub *subagent) status() string {
	if sub.state == nil {
		return "completed"
	}
	switch sub.state.State {
	case acp.SubagentRunning:
		return "in_progress"
	case acp.SubagentRequiresAction:
		return "pending"
	case acp.SubagentUnknown:
		return "cancelled"
	}
	switch sub.state.StopReason {
	case acp.StopCancelled:
		return "cancelled"
	case "error", "refusal":
		return "failed"
	}
	return "completed"
}

func (sub *subagent) working() bool {
	return sub.state != nil && (sub.state.State == acp.SubagentRunning || sub.state.State == acp.SubagentRequiresAction)
}

func (sub *subagent) activity() string {
	if !sub.working() {
		if sub.state != nil && sub.state.Error != nil {
			return firstLine(sub.state.Error.Message)
		}
		return ""
	}
	if sub.state.State == acp.SubagentRequiresAction {
		return subagentWaiting
	}
	for i := len(sub.entries) - 1; i >= 0; i-- {
		en := sub.entries[i]
		switch en.Kind {
		case entryTool:
			if en.sub != nil {
				return en.sub.name()
			}
			if title := toolActivityTitle(en.Tool); title != "" {
				return firstLine(title)
			}
		case entryAgent:
			if line := firstLine(strings.TrimSpace(en.Text)); line != "" {
				return line
			}
		case entryThought:
			return subagentThinking
		}
	}
	return ""
}

func (sub *subagent) body() string {
	var b strings.Builder
	line := func(text string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text)
	}
	replied := false
	for _, en := range sub.entries {
		switch en.Kind {
		case entryUser:
			line("→ " + strings.ReplaceAll(capLines(strings.TrimSpace(en.Text), promptLines), "\n", "\n  "))
		case entryAgent:
			text := strings.TrimSpace(en.Text)
			if text == "" {
				continue
			}
			replied = replied || text == strings.TrimSpace(sub.reply)
			line(text)
		case entryTool:
			if en.sub != nil {
				line("↳ " + en.sub.name() + " — " + strings.ReplaceAll(en.sub.status(), "_", " "))
				continue
			}
			var f toolFields
			_ = json.Unmarshal(en.Tool["title"], &f.Title)
			_ = json.Unmarshal(en.Tool["status"], &f.Status)
			mark := "• "
			if f.Status == "failed" {
				mark = "✗ "
			}
			line(mark + firstLine(cmp.Or(f.Title, entryTool)))
		}
	}
	if reply := strings.TrimSpace(sub.reply); reply != "" && !replied {
		line("← " + reply)
	}
	if sub.state != nil && sub.state.Error != nil && sub.state.Error.Message != "" {
		line("Error: " + sub.state.Error.Message)
	}
	return b.String()
}

func capLines(text string, n int) string {
	lines := strings.SplitN(text, "\n", n+1)
	if len(lines) <= n {
		return text
	}
	return strings.Join(lines[:n], "\n") + " …"
}

func toolActivityTitle(tool map[string]json.RawMessage) string {
	var title, kind string
	_ = json.Unmarshal(tool["title"], &title)
	if json.Unmarshal(tool["kind"], &kind) != nil || kind != toolExecute {
		return title
	}
	var input struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(tool["rawInput"], &input)
	return cmp.Or(strings.TrimSpace(input.Description), commandActivity)
}
