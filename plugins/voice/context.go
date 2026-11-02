package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/contract"
	"clubmate/plugins/agents/api"
)

const (
	sectionAgents  = "agents"
	sectionBacklog = "backlog"
	statusRunning  = "running"
	statusWaiting  = "waiting"

	maxViewBytes     = 8 << 10
	maxHintBytes     = 800
	maxSessions      = 50
	maxBriefSessions = 10
	recentSession    = 2 * time.Hour
	maxOtherBranches = 10
	resolveTimeout   = 3 * time.Second
	truncatedView    = `{"truncated":true}`
	entryPermission  = "permission"
	entryQuestion    = "elicitation"
)

var fixedHints = []string{"MCP", "worktree", "merge", "squash", "commit", "backlog", "CI", "PR", "Clubmate", "Companion"}

type Context struct {
	Device    string          `json:"device"`
	Section   string          `json:"section"`
	Workspace WorkspaceInfo   `json:"workspace"`
	Session   *SessionInfo    `json:"session,omitempty"`
	Task      json.RawMessage `json:"task,omitempty"`
	File      string          `json:"file,omitempty"`
	Line      int             `json:"line,omitempty"`
	View      json.RawMessage `json:"view,omitempty"`
	NeedsYou  []SessionBrief  `json:"needsYou,omitempty"`
}

type WorkspaceInfo struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Branches []string       `json:"branches,omitempty"`
	Sessions []SessionBrief `json:"sessions,omitempty"`
}

type SessionBrief struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Waiting string `json:"waiting,omitempty"`
}

type SessionInfo struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Status     string      `json:"status"`
	Question   *Question   `json:"question,omitempty"`
	Permission *Permission `json:"permission,omitempty"`
}

type Question struct {
	Request string          `json:"request,omitempty"`
	Text    string          `json:"text"`
	Fields  []QuestionField `json:"fields,omitempty"`
}

type QuestionField struct {
	Key     string           `json:"key"`
	Type    string           `json:"type"`
	Title   string           `json:"title,omitempty"`
	Options []QuestionOption `json:"options,omitempty"`
}

type QuestionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Permission struct {
	Request string             `json:"request"`
	Title   string             `json:"title"`
	Options []PermissionOption `json:"options"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type agentSession struct {
	api.Session
	Asks           bool       `json:"asks"`
	Question       string     `json:"question"`
	NeedsYou       bool       `json:"needsYou"`
	NeedsYouSince  *time.Time `json:"needsYouSince"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	PendingSummary *struct {
		Text string `json:"text"`
	} `json:"pendingSummary"`
}

type transcript struct {
	Entries []transcriptEntry `json:"entries"`
}

type transcriptEntry struct {
	Kind     string             `json:"kind"`
	ID       string             `json:"id"`
	ToolCall json.RawMessage    `json:"toolCall"`
	Options  []PermissionOption `json:"options"`
	Outcome  json.RawMessage    `json:"outcome"`
	Card     *struct {
		Title string `json:"title"`
	} `json:"card"`
	Elicitation *struct {
		Message string          `json:"message"`
		Fields  []QuestionField `json:"fields"`
	} `json:"elicitation"`
	Response   json.RawMessage `json:"response"`
	Completed  bool            `json:"completed"`
	AnsweredAt *time.Time      `json:"answeredAt"`
}

type slotView struct {
	Slot string          `json:"slot"`
	Tree json.RawMessage `json:"tree"`
}

type sduiNode struct {
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []sduiNode      `json:"children"`
}

type worktree struct {
	Branch    string    `json:"branch"`
	Committed time.Time `json:"committed"`
	Main      bool      `json:"main"`
	Current   bool      `json:"current"`
}

func resolve(ctx context.Context, h host, workspace string, p Pointer) (Context, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	c := Context{Device: p.Device, Section: p.Section, File: p.Selection.File, Line: p.Selection.Line, Workspace: WorkspaceInfo{ID: workspace}}
	var errs []error
	var ws contract.Workspace
	if err := h.Call(ctx, "workspace.get", map[string]string{"id": workspace}, &ws); err != nil {
		errs = append(errs, fmt.Errorf("workspace: %w", err))
	}
	c.Workspace.Name = ws.Name
	var worktrees []worktree
	if err := h.RunCommand(ctx, "git.worktrees", workspace, struct{}{}, &worktrees); err != nil {
		errs = append(errs, fmt.Errorf("worktrees: %w", err))
	}
	c.Workspace.Branches = branches(worktrees)
	var sessions []agentSession
	if err := h.RunCommand(ctx, "agents.sessions", workspace, struct{}{}, &sessions); err != nil {
		errs = append(errs, fmt.Errorf("agent sessions: %w", err))
	}
	for _, s := range sessions {
		if s.NeedsYou && !s.Closed() && len(c.NeedsYou) < maxSessions {
			c.NeedsYou = append(c.NeedsYou, brief(s))
		}
	}
	var slots []slotView
	if err := h.Call(ctx, "ui.get", map[string]string{"workspaceId": workspace}, &slots); err != nil {
		errs = append(errs, fmt.Errorf("ui: %w", err))
	}
	c.View = mainView(slots, p.Section)
	sel := p.Selection
	if sel == (Selection{}) {
		sel = fallbackSelection(slots, p.Section, sessions)
	}
	c.Workspace.Sessions = briefSessions(sessions, sel.Session, time.Now())
	if sel.Session != "" {
		info, err := sessionInfo(ctx, h, workspace, sessions, sel.Session)
		if err != nil {
			errs = append(errs, err)
		}
		c.Session = info
	}
	if sel.Task != 0 {
		var task json.RawMessage
		if err := h.RunCommand(ctx, "backlog.get", workspace, map[string]int{"id": sel.Task}, &task); err != nil {
			errs = append(errs, fmt.Errorf("task %d: %w", sel.Task, err))
		}
		c.Task = cutTask(task)
	}
	return c, errors.Join(errs...)
}

func hintWords(c Context) string {
	var words []string
	add := func(w string) {
		if w = strings.TrimSpace(w); w != "" && !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	add(c.Section)
	if c.Session != nil {
		add(c.Session.Title)
	}
	for _, s := range c.Workspace.Sessions {
		add(s.Title)
	}
	for _, b := range c.Workspace.Branches {
		add(b)
	}
	for _, w := range fixedHints {
		add(w)
	}
	hint := strings.Join(words, " ")
	if len(hint) <= maxHintBytes {
		return hint
	}
	cut := strings.LastIndexByte(hint[:maxHintBytes], ' ')
	return hint[:max(cut, 0)]
}

func sessionInfo(ctx context.Context, h host, workspace string, sessions []agentSession, sid string) (*SessionInfo, error) {
	i := slices.IndexFunc(sessions, func(s agentSession) bool { return s.ID == sid })
	if i < 0 {
		return nil, nil
	}
	s := sessions[i]
	info := &SessionInfo{ID: s.ID, Title: s.Title, Status: s.Status}
	if s.Asks && s.Question != "" {
		info.Question = &Question{Text: s.Question}
	}
	var tr transcript
	if err := h.RunCommand(ctx, "agents.transcript", workspace, map[string]any{"session": sid, "last": true, "compact": true}, &tr); err != nil {
		return info, fmt.Errorf("transcript %s: %w", sid, err)
	}
	for _, e := range tr.Entries {
		if e.AnsweredAt != nil {
			continue
		}
		switch {
		case e.Kind == entryPermission && isNull(e.Outcome):
			info.Permission = &Permission{Request: e.ID, Title: permissionTitle(e), Options: e.Options}
		case e.Kind == entryQuestion && e.Elicitation != nil && isNull(e.Response) && !e.Completed:
			info.Question = &Question{Request: e.ID, Text: e.Elicitation.Message, Fields: e.Elicitation.Fields}
		}
	}
	return info, nil
}

func fallbackSelection(slots []slotView, section string, sessions []agentSession) Selection {
	if section != sectionAgents && section != sectionBacklog {
		return Selection{}
	}
	selected := sidebarSelection(slots, section)
	if section == sectionBacklog {
		task, _ := strconv.Atoi(selected)
		return Selection{Task: task}
	}
	if api.Valid(selected) {
		return Selection{Session: selected}
	}
	var oldest *agentSession
	for i, s := range sessions {
		if !s.NeedsYou || s.NeedsYouSince == nil {
			continue
		}
		if oldest == nil || s.NeedsYouSince.Before(*oldest.NeedsYouSince) {
			oldest = &sessions[i]
		}
	}
	if oldest == nil {
		return Selection{}
	}
	return Selection{Session: oldest.ID}
}

func sidebarSelection(slots []slotView, section string) string {
	i := slices.IndexFunc(slots, func(s slotView) bool { return s.Slot == section+".sidebar" })
	if i < 0 {
		return ""
	}
	var root sduiNode
	if json.Unmarshal(slots[i].Tree, &root) != nil {
		return ""
	}
	return treeSelection(root)
}

func treeSelection(n sduiNode) string {
	if n.Type == "tree" {
		var props struct {
			Selected string `json:"selected"`
		}
		if json.Unmarshal(n.Props, &props) == nil && props.Selected != "" {
			return props.Selected
		}
	}
	for _, child := range n.Children {
		if sel := treeSelection(child); sel != "" {
			return sel
		}
	}
	return ""
}

func mainView(slots []slotView, section string) json.RawMessage {
	i := slices.IndexFunc(slots, func(s slotView) bool { return s.Slot == section+".main" })
	if i < 0 || len(slots[i].Tree) == 0 {
		return nil
	}
	if len(slots[i].Tree) > maxViewBytes {
		return json.RawMessage(truncatedView)
	}
	return slots[i].Tree
}

func cutTask(raw json.RawMessage) json.RawMessage {
	if len(raw) <= maxViewBytes {
		return raw
	}
	var t struct {
		ID        int    `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return json.RawMessage(truncatedView)
	}
	t.Truncated = true
	out, err := json.Marshal(t)
	if err != nil {
		return json.RawMessage(truncatedView)
	}
	return out
}

func branches(worktrees []worktree) []string {
	var current []string
	var others []worktree
	for _, wt := range worktrees {
		switch {
		case wt.Branch == "":
		case wt.Main || wt.Current:
			if !slices.Contains(current, wt.Branch) {
				current = append(current, wt.Branch)
			}
		default:
			others = append(others, wt)
		}
	}
	slices.SortStableFunc(others, func(a, b worktree) int { return b.Committed.Compare(a.Committed) })
	list := current
	for _, wt := range others {
		if len(list) == len(current)+maxOtherBranches {
			break
		}
		if !slices.Contains(list, wt.Branch) {
			list = append(list, wt.Branch)
		}
	}
	return list
}

func briefSessions(sessions []agentSession, selected string, now time.Time) []SessionBrief {
	var active []agentSession
	var picked *agentSession
	for i, s := range sessions {
		switch {
		case s.ID == selected:
			picked = &sessions[i]
		case s.Closed():
		case s.busy(), now.Sub(s.UpdatedAt) < recentSession:
			active = append(active, s)
		}
	}
	slices.SortStableFunc(active, func(a, b agentSession) int {
		if a.busy() != b.busy() {
			if a.busy() {
				return -1
			}
			return 1
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	var list []SessionBrief
	if picked != nil {
		list = append(list, brief(*picked))
	}
	for _, s := range active[:min(len(active), maxBriefSessions)] {
		list = append(list, SessionBrief{ID: s.ID, Title: s.Title, Status: s.Status})
	}
	return list
}

func (s agentSession) busy() bool {
	return s.Status == statusRunning || s.Status == statusWaiting || s.NeedsYou
}

func brief(s agentSession) SessionBrief {
	b := SessionBrief{ID: s.ID, Title: s.Title, Status: s.Status}
	if s.PendingSummary != nil {
		b.Waiting = s.PendingSummary.Text
	}
	return b
}

func permissionTitle(e transcriptEntry) string {
	if e.Card != nil && e.Card.Title != "" {
		return e.Card.Title
	}
	var tc struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(e.ToolCall, &tc)
	return cmp.Or(tc.Title, "permission")
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}
