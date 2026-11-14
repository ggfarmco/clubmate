package main

import (
	"cmp"
	"encoding/json"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/docext"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const cursorSep = "."

var transcriptEpoch = strconv.FormatInt(time.Now().UnixNano(), 36)

const (
	entryUser       = "user"
	entryAgent      = "agent"
	entryThought    = "thought"
	entryTool       = "tool"
	entryPlan       = "plan"
	entryPermission = "permission"
	entryNotice     = "notice"
	entryCompaction = "compaction"
)

var (
	chunkKinds       = map[string]string{acp.UpdateUserMessageChunk: entryUser, acp.UpdateAgentMessageChunk: entryAgent, acp.UpdateAgentThoughtChunk: entryThought}
	editKinds        = []string{"edit", "delete", "move"}
	openToolStatuses = []string{"", "pending", "in_progress"}
	unkeptToolFields = []string{"sessionUpdate", "rawOutput", "_meta"}
)

type transcriptState struct {
	entries  []*entry
	index    map[string]int
	commands []acp.AvailableCommand
	modes    []acp.SessionMode
	configs  []acp.SessionConfigOption
	usage    *acp.UsageUpdate
	turns    []turnInfo
	turn     int
	nextID   int
	rev      int64
	edited   bool
	prompted bool
	senders  []string
	at       time.Time

	presented    map[int][]presentedDoc
	ended        map[int]bool
	reviewStates map[string]map[int64]string

	subagents map[string]*subagent

	openState
}

type transcriptView struct {
	Session  record                    `json:"session"`
	Modes    []acp.SessionMode         `json:"modes"`
	Configs  []acp.SessionConfigOption `json:"configOptions"`
	Advisor  string                    `json:"advisor,omitempty"`
	Usage    *acp.UsageUpdate          `json:"usage,omitempty"`
	Commands []acp.AvailableCommand    `json:"commands"`
	Turns    []turnInfo                `json:"turns"`
	Entries  []*entry                  `json:"entries"`
	Cursor   string                    `json:"cursor,omitempty"`
	Delta    bool                      `json:"delta,omitempty"`
	Request  *sdui.Node                `json:"request,omitempty"`
	Repos    *sdui.Node                `json:"repos,omitempty"`

	ReviewStates map[string]map[int64]string `json:"-"`
}

type entry struct {
	Kind      string                     `json:"kind"`
	ID        string                     `json:"id"`
	Turn      int                        `json:"turn"`
	Text      string                     `json:"text,omitempty"`
	Content   []json.RawMessage          `json:"content,omitempty"`
	Tool      map[string]json.RawMessage `json:"tool,omitempty"`
	Plan      json.RawMessage            `json:"plan,omitempty"`
	ToolCall  json.RawMessage            `json:"toolCall,omitempty"`
	Options   []acp.PermissionOption     `json:"options,omitempty"`
	Outcome   *acp.PermissionOutcome     `json:"outcome,omitempty"`
	Notice    json.RawMessage            `json:"notice,omitempty"`
	From      string                     `json:"from,omitempty"`
	Subagent  string                     `json:"subagent,omitempty"`
	Stoppable bool                       `json:"stoppable,omitempty"`
	Card      *permissionContent         `json:"card,omitempty"`
	AFK       *afkCard                   `json:"afk,omitempty"`

	Elicitation *elicitation             `json:"elicitation,omitempty"`
	Response    *acp.ElicitationResponse `json:"response,omitempty"`
	Completed   bool                     `json:"completed,omitempty"`
	At          time.Time                `json:"at,omitzero"`
	AnsweredAt  time.Time                `json:"answeredAt,omitzero"`

	Compaction *compaction       `json:"compaction,omitempty"`
	Doc        string            `json:"doc,omitempty"`
	DocLinks   map[string]string `json:"docLinks,omitempty"`
	DocCards   []sdui.DocCard    `json:"docCards,omitempty"`
	Images     []sdui.ImageRef   `json:"images,omitempty"`
	ImageLinks map[string]string `json:"imageLinks,omitempty"`
	Hidden     bool              `json:"hidden,omitempty"`
	Voice      *voicePrompt      `json:"voice,omitempty"`
	Review     *reviewSent       `json:"review,omitempty"`

	Items    []openItem `json:"items,omitempty"`
	Overflow []int      `json:"overflow,omitempty"`
	Later    bool       `json:"later,omitempty"`

	node    *nodeCache
	sub     *subagent
	rev     int64
	closed  bool
	marked  string
	numbers []int
}

type voicePrompt struct {
	Heard     string `json:"heard"`
	Utterance string `json:"utterance"`
}

type permissionContent struct {
	Text    string                  `json:"text"`
	Title   string                  `json:"title"`
	Command string                  `json:"command,omitempty"`
	Caption string                  `json:"caption,omitempty"`
	Options []sdui.PermissionOption `json:"options"`
}

type turnInfo struct {
	Turn          int      `json:"turn"`
	CheckpointSha string   `json:"checkpointSha"`
	StopReason    string   `json:"stopReason,omitempty"`
	Commits       []string `json:"commits,omitempty"`
}

type promptPayload struct {
	acp.PromptRequest
	From      string      `json:"from,omitempty"`
	FromTitle string      `json:"fromTitle,omitempty"`
	Steered   bool        `json:"steered,omitempty"`
	Voice     bool        `json:"voice,omitempty"`
	Review    *reviewSent `json:"review,omitempty"`
	Sent      string      `json:"sent,omitempty"`
	Queued    []string    `json:"queued,omitempty"`
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
	Repo          string `json:"repo,omitempty"`
	Path          string `json:"path,omitempty"`
}

func (s *session) view(fromTurn int) transcriptView {
	return s.viewSince(fromTurn, -1)
}

func (s *session) viewSince(fromTurn int, since int64) transcriptView {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	v := transcriptView{Session: s.rec, Modes: slices.Clone(s.modes), Configs: slices.Clone(s.configs), Usage: s.usage, Commands: slices.Clone(s.commands), Turns: slices.Clone(s.turns), ReviewStates: maps.Clone(s.reviewStates)}
	marked := s.markedLocked()
	numbers := openNumbers(s.opens)
	for _, en := range s.entries {
		if since < 0 && en.Turn < fromTurn {
			continue
		}
		s.refreshLocked(en)
		if marked[en] != en.marked {
			en.marked, en.node = marked[en], nil
		}
		if !slices.Equal(numbers[en], en.numbers) {
			en.numbers, en.node = numbers[en], nil
		}
		if en.node == nil {
			en.node = new(nodeCache)
			s.bumpLocked(en)
		}
		c := *en
		c.sub = nil
		c.Text = cmp.Or(en.marked, en.Text)
		c.Items = withNumbers(en.Items, en.numbers)
		closed := false
		if en.Tool != nil && (en.sub == nil && !s.workingLocked() || en.sub != nil && s.proc == nil) {
			if tool, ok := closeTool(en.Tool); ok {
				c.Tool, c.node, c.Stoppable, closed = tool, nil, false, true
			}
		}
		if !s.workingLocked() && c.Compaction != nil && c.Compaction.Status == compactionRunning {
			c.Compaction, c.node, closed = &compaction{Status: compactionCancelled}, nil, true
		}
		if closed != en.closed {
			en.closed = closed
			s.bumpLocked(en)
			c.rev = en.rev
		}
		if c.rev > since {
			v.Entries = append(v.Entries, &c)
		}
	}
	v.Cursor = transcriptEpoch + cursorSep + strconv.FormatInt(s.rev, 10)
	return v
}

func (s *session) bumpLocked(en *entry) {
	s.rev++
	en.rev = s.rev
}

func parseCursor(cursor string) (int64, bool) {
	epoch, rev, ok := strings.Cut(cursor, cursorSep)
	if !ok || epoch != transcriptEpoch {
		return 0, false
	}
	n, err := strconv.ParseInt(rev, 10, 64)
	return n, err == nil && n >= 0
}

func (v transcriptView) lastTurn() transcriptView {
	last := 0
	for _, en := range v.Entries {
		last = max(last, en.Turn)
	}
	for _, t := range v.Turns {
		last = max(last, t.Turn)
	}
	v.Modes, v.Configs, v.Commands = nil, nil, nil
	v.Turns = slices.DeleteFunc(v.Turns, func(t turnInfo) bool { return t.Turn != last })
	v.Entries = slices.DeleteFunc(v.Entries, func(en *entry) bool { return en.Turn != last })
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
		en.Content, en.node = content, nil
	}
}

func compactTools(entries []*entry) {
	for _, en := range entries {
		if en.Tool == nil {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(en.Tool["content"], &items) != nil {
			continue
		}
		kept := make([]json.RawMessage, 0, len(items))
		for _, raw := range items {
			var item struct {
				Content acp.ContentBlock `json:"content"`
			}
			if json.Unmarshal(raw, &item) == nil && item.Content.Type == "image" {
				continue
			}
			kept = append(kept, raw)
		}
		if len(kept) < len(items) {
			en.Tool, en.node = maps.Clone(en.Tool), nil
			en.Tool["content"], _ = json.Marshal(kept)
		}
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
	if n.SessionID != "" && s.rec.AgentSessionID != "" && n.SessionID != s.rec.AgentSessionID {
		e.AgentSession = n.SessionID
	}
	var chunk acp.ContentChunk
	var summary acp.CompactionSummaryChunk
	_, chunked := chunkKinds[head.SessionUpdate]
	switch {
	case chunked && json.Unmarshal(n.Update, &chunk) == nil && chunk.Content.Type == "text":
		s.log.chunk(e, head.SessionUpdate, chunk.MessageID, chunk.Content.Text)
	case head.SessionUpdate == acp.UpdateCompactionChunk && json.Unmarshal(n.Update, &summary) == nil && summary.Content.Type == "text":
		s.log.chunk(e, head.SessionUpdate, summary.CompactionID, summary.Content.Text)
	case head.SessionUpdate == acp.UpdateUsage:
		s.log.latest(e)
	default:
		s.log.write(e)
	}
	title, mode := s.rec.Title, s.rec.Mode
	s.apply(e)
	var started, ended bool
	if e.AgentSession == "" {
		started, ended = s.trackLocked(head.SessionUpdate, n.Update)
	}
	meta := title != s.rec.Title || mode != s.rec.Mode
	s.mu.Unlock()
	s.changed(changeUpdate, meta)
	if started {
		s.changed(changeStatus, true)
	}
	if ended && s.idled != nil {
		go s.idled()
	}
}

func (s *session) notice(title, description string) {
	s.noticeUpdate(noticeUpdate{SessionUpdate: acp.UpdateNotice, Title: title, Description: description})
}

func (s *session) setStartNotice(text string) {
	s.mu.Lock()
	s.rec.StartNotice = text
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func (s *session) noticeUpdate(n noticeUpdate) {
	s.mu.Lock()
	s.recordLocked(actorClubmate, kindUpdate, n)
	s.mu.Unlock()
	s.changed(changeUpdate, false)
}

func (s *session) apply(e envelope) {
	s.at = e.TS
	if e.Turn > s.turn {
		s.turn = e.Turn
	}
	switch e.Kind {
	case kindUpdate:
		if e.AgentSession != "" {
			s.applySubagent(e.Turn, e.AgentSession, e.Payload)
			return
		}
		s.applyUpdate(e.Turn, e.Payload)
	case kindNew, kindResume:
		s.loseSubagents()
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
		if s.ended == nil {
			s.ended = map[int]bool{}
		}
		s.ended[e.Turn] = true
		s.linkDocs(e.Turn)
		var p turnEndPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			s.turnInfo(e.Turn).StopReason = p.StopReason
		}
	case kindPresented:
		var p presentedDoc
		if json.Unmarshal(e.Payload, &p) == nil && p.Path != "" {
			s.present(e.Turn, p)
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
			if sub := s.subagents[req.SessionID]; sub != nil {
				en.Subagent = sub.name()
			}
		}
	case kindAnswer:
		var p answerPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			if i, ok := s.index[key(entryPermission, p.Request)]; ok {
				s.entries[i].Outcome, s.entries[i].AnsweredAt, s.entries[i].node = &p.Outcome, e.TS, nil
			}
		}
	case kindElicitation:
		s.nextID++
		s.applyElicitation(e)
	case kindElicitationAnswer:
		s.applyElicitationAnswer(e)
	case kindElicitationComplete:
		s.applyElicitationComplete(e)
	case kindOpenItems:
		s.applyOpenItems(e)
	case kindOpenChanged:
		s.applyOpenChanged(e)
	case kindReviewClosed:
		s.applyReviewClosed(e)
	}
}

func (s *session) applyConfig(p configPayload) {
	if p.ConfigOptions != nil {
		s.configs = p.ConfigOptions
		return
	}
	for i, o := range s.configs {
		if o.ID == p.ConfigID {
			s.configs[i].CurrentValue = o.Encode(p.Value)
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
	if req.FromTitle != "" && !slices.Contains(s.senders, req.FromTitle) {
		s.senders = append(s.senders, req.FromTitle)
	}
	for _, b := range req.Prompt {
		raw, err := json.Marshal(b)
		if err == nil {
			en.Content = append(en.Content, raw)
		}
		if b.Type == "text" && en.Text == "" {
			en.Text = b.Text
		}
	}
	en.Voice = nil
	en.Review = req.Review
	if v := (voicePrompt{}); req.Voice && json.Unmarshal([]byte(en.Text), &v) == nil && v.Heard != "" {
		en.Voice = &v
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
		if json.Unmarshal(payload, &u) == nil && !s.rec.TitleSet && u.Title != nil && *u.Title != "" && !slices.Contains(s.senders, titleOf(*u.Title)) {
			s.rec.Title = titleOf(*u.Title)
		}
	case acp.UpdateSubagent:
		s.announceSubagent(nil, turn, payload)
	case acp.UpdateSessionMessage:
		s.sessionMessage(payload)
	case acp.UpdateNotice:
		s.nextID++
		s.upsert(entryNotice, "notice-"+strconv.Itoa(s.nextID), turn).Notice = payload
	case acp.UpdateUsage:
		s.applyUsage(payload)
	case acp.UpdateCompaction, acp.UpdateCompactionChunk:
		s.applyCompaction(head.SessionUpdate, turn, payload)
	}
}

func (s *session) applyTool(create bool, turn int, payload json.RawMessage) {
	var fields map[string]json.RawMessage
	var head acp.ToolCallHeader
	if json.Unmarshal(payload, &fields) != nil || json.Unmarshal(payload, &head) != nil || head.ToolCallID == "" {
		return
	}
	dropUnkeptToolFields(fields)
	en := s.upsert(entryTool, head.ToolCallID, turn)
	tool := make(map[string]json.RawMessage, len(en.Tool)+len(fields))
	if !create {
		maps.Copy(tool, en.Tool)
	}
	maps.Copy(tool, fields)
	en.Tool = tool
	var kind string
	if json.Unmarshal(en.Tool["kind"], &kind) == nil && slices.Contains(editKinds, kind) {
		s.edited = true
	}
	en.Doc = s.docOf(en.Tool)
	en.Hidden = hiddenTool(en.Tool)
}

func hiddenTool(tool map[string]json.RawMessage) bool {
	var name, kind, status string
	_ = json.Unmarshal(tool["name"], &name)
	_ = json.Unmarshal(tool["kind"], &kind)
	_ = json.Unmarshal(tool["status"], &status)
	return name == openItemsTool && kind == "other" && status == "completed"
}

func (s *session) docOf(tool map[string]json.RawMessage) string {
	var status string
	if json.Unmarshal(tool["status"], &status) != nil || status != "completed" {
		return ""
	}
	f, _, _ := parseTool(tool)
	var paths []string
	if f.Kind == "edit" {
		for _, l := range f.Locations {
			paths = append(paths, l.Path)
		}
		for _, c := range f.Content {
			if c.Type == "diff" {
				paths = append(paths, c.Path)
			}
		}
	}
	for _, c := range f.Content {
		if c.Type != "content" || c.Content.Type != "resource_link" {
			continue
		}
		if u, err := url.Parse(c.Content.URI); err == nil && u.Scheme == "file" {
			paths = append(paths, u.Path)
		}
	}
	for _, p := range paths {
		if p != "" && (docext.Is(p) || docext.IsArtifact(p)) {
			if !filepath.IsAbs(p) {
				p = filepath.Join(s.rec.Worktree, p)
			}
			return p
		}
	}
	return ""
}

func dropUnkeptToolFields(fields map[string]json.RawMessage) {
	for _, k := range unkeptToolFields {
		delete(fields, k)
	}
}

func (s *session) cancelTools(turn int) {
	for _, en := range s.entries {
		if en.Kind != entryTool || en.Turn != turn || en.sub != nil {
			continue
		}
		if closed, ok := closeTool(en.Tool); ok {
			en.Tool, en.node = closed, nil
		}
	}
}

func closeTool(tool map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	var status string
	_ = json.Unmarshal(tool["status"], &status)
	if !slices.Contains(openToolStatuses, status) {
		return tool, false
	}
	closed := maps.Clone(tool)
	closed["status"] = json.RawMessage(`"cancelled"`)
	return closed, true
}

func (s *session) messageEntry(kind, messageID string, turn int) *entry {
	if messageID != "" {
		return s.upsert(kind, messageID, turn)
	}
	if n := len(s.entries); n > 0 {
		if last := s.entries[n-1]; last.Kind == kind && last.Turn == turn {
			last.node = nil
			return last
		}
	}
	s.nextID++
	return s.upsert(kind, kind+"-"+strconv.Itoa(s.nextID), turn)
}

func (s *session) upsert(kind, id string, turn int) *entry {
	k := key(kind, id)
	if i, ok := s.index[k]; ok {
		s.entries[i].node = nil
		return s.entries[i]
	}
	en := &entry{Kind: kind, ID: id, Turn: turn, At: s.at}
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

func (s *session) tool(id string) (map[string]json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	i, ok := s.index[key(entryTool, id)]
	if !ok {
		return nil, false
	}
	s.refreshLocked(s.entries[i])
	return maps.Clone(s.entries[i].Tool), true
}
