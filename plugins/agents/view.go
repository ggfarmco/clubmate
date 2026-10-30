package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	sidebarSlot = "agents.sidebar"
	mainSlot    = "agents.main"
	sectionID   = "agents"

	windowStep    = 20
	bodyLines     = 200
	bodyBytes     = 64 << 10
	expandedBytes = 1 << 20
	nodeBudget    = 4000
	diffContext   = 3

	actNew             = "new"
	actSelect          = "select"
	actExpand          = "expand"
	actCollapse        = "collapse"
	actSend            = "send"
	actStop            = "stop"
	actPermission      = "permission"
	actMode            = "mode"
	actConfig          = "config"
	actAttachFile      = "attachFile"
	actAttachSelection = "attachSelection"
	actDetach          = "detach"
	actOpenLocation    = "openLocation"
	actReview          = "review"
	actShowEarlier     = "showEarlier"
	actShowAll         = "showAll"
	actClose           = "close"
	actDelete          = "delete"
	actRetry           = "retry"
	actSignIn          = "signIn"
	actContinue        = "continue"
	actUnqueue         = "unqueue"
	actSendQueued      = "sendQueued"

	groupPrefix    = "wt:"
	deleteConfirm  = "Delete this agent session, its transcript and its checkpoints?"
	noSessions     = "No agent sessions"
	pickSession    = "Start an agent session with + New"
	installingNote = "Installing %s adapter…"
	queuedNote     = "Queued, sent when the turn ends"
	heldNote       = "Queued, not sent"
	queuePrefix    = "queue/"
	advisorHelp    = "Advisor model for new sessions in this worktree"
	finishHelp     = "What the agent does once the task is done"

	finishAskText   = "[Clubmate finish mode: ask] When the task is done, don't merge into main or remove worktrees and branches on your own; ask the user first unless they already asked for it."
	finishMergeText = "[Clubmate finish mode: merge] When the task is done, run the project's tests. If they all pass, commit, merge the task branch into main without asking, then remove the worktree created for the task and delete the merged branch; never remove the primary checkout, and on main itself just commit. Never remove a worktree that has uncommitted changes or a running process. If tests fail or cannot run, don't merge; report why."
)

var (
	statusIcons = map[string]string{
		statusRunning: "circle.dotted",
		statusWaiting: "exclamationmark.circle.fill",
		statusDone:    "checkmark.circle",
		statusIdle:    "circle",
		statusError:   "xmark.octagon",
		statusClosed:  "archivebox",
	}
	statusLabels = map[string]string{
		statusRunning: "Running",
		statusWaiting: "Waiting for permission",
		statusDone:    "Done",
		statusIdle:    "Idle",
		statusError:   "Error",
		statusClosed:  "Closed",
	}
	agentLabels    = map[string]string{agentClaude: "Claude"}
	cardKinds      = []string{"read", "edit", "delete", "move", "search", "execute", "think", "fetch", "switch_mode", "other"}
	cardStatuses   = []string{"pending", "in_progress", "completed", "failed", "cancelled"}
	oneLineKinds   = []string{"read", "search"}
	permVerbs      = map[string]string{"execute": "run", "edit": "edit", "delete": "delete", "move": "move", "read": "read", "fetch": "fetch"}
	selectionRe    = regexp.MustCompile(`^(.+):(\d+)-(\d+)\n`)
	fragmentRe     = regexp.MustCompile(`^L(\d+)-(\d+)$`)
	advisorChoices = []advisorChoice{
		{label: "Default"},
		{value: "fable", label: "Fable"},
		{value: "opus", label: "Opus"},
		{value: "sonnet", label: "Sonnet"},
	}
	finishChoices = []advisorChoice{
		{value: finishAsk, label: "Ask"},
		{value: finishMerge, label: "Merge to main"},
	}
)

type advisorChoice struct {
	value string
	label string
}

type sessionRow struct {
	Rec      record
	Activity string
}

type pendingAttachment struct {
	ID string
	attachment
}

type screen struct {
	View        transcriptView
	Pending     map[string]bool
	Window      int
	Expanded    map[string]bool
	Attachments []pendingAttachment
	Queue       []queuedPrompt
	Composer    string
	NeedsAuth   bool
	Continue    bool
	SignIn      string
	NodeProblem string
	Installing  bool
	Changes     bool
	Advisor     string
}

type toolFields struct {
	Title     string         `json:"title"`
	Kind      string         `json:"kind"`
	Status    string         `json:"status"`
	Content   []toolContent  `json:"content"`
	Locations []toolLocation `json:"locations"`
	RawInput  map[string]any `json:"rawInput"`
}

type toolContent struct {
	Type    string           `json:"type"`
	Content acp.ContentBlock `json:"content"`
	Path    string           `json:"path"`
	OldText *string          `json:"oldText"`
	NewText string           `json:"newText"`
}

type toolLocation struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type planUpdate struct {
	Entries []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"entries"`
}

type noticeUpdate struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func agentLabel(agent string) string {
	if label, ok := agentLabels[agent]; ok {
		return label
	}
	return agent
}

func sidebarView(rows []sessionRow, selected string, collapsed map[string]bool, now time.Time) sdui.Node {
	newButton := sdui.WithAction(sdui.Button("new", "New", "plus"), actNew)
	if len(rows) == 0 {
		return sdui.VStack(newButton, sdui.Empty(noSessions))
	}
	var groups []sdui.TreeItem
	index := map[string]int{}
	for _, row := range rows {
		wt := row.Rec.Worktree
		i, ok := index[wt]
		if !ok {
			i = len(groups)
			index[wt] = i
			groups = append(groups, sdui.TreeItem{ID: groupPrefix + wt, Label: filepath.Base(wt), Detail: wt, Icon: "arrow.triangle.branch", HasChildren: true, Expanded: !collapsed[wt]})
		}
		groups[i].Children = append(groups[i].Children, sessionItem(row, now))
	}
	for i := range groups {
		if !groups[i].Expanded {
			groups[i].Children = nil
		}
	}
	return sdui.VStack(newButton, sdui.Tree("sessions", groups, selected))
}

func sessionItem(row sessionRow, now time.Time) sdui.TreeItem {
	rec := row.Rec
	menu := []sdui.MenuEntry{{Action: actDelete, Label: "Delete Session", Confirm: deleteConfirm}}
	if !rec.Closed {
		menu = slices.Insert(menu, 0, sdui.MenuEntry{Action: actClose, Label: "Close Session"})
	}
	return sdui.TreeItem{
		ID:     rec.ID,
		Label:  cmp.Or(rec.Title, "New session"),
		Detail: agentLabel(rec.Agent) + " · " + activity(row, now),
		Icon:   statusIcons[rec.Status],
		Menu:   menu,
	}
}

func activity(row sessionRow, now time.Time) string {
	rec := row.Rec
	label := cmp.Or(statusLabels[rec.Status], rec.Status)
	switch rec.Status {
	case statusRunning, statusWaiting:
		if row.Activity != "" {
			return label + " · " + row.Activity
		}
		return label
	case statusError:
		if line := firstLine(rec.Error); line != "" {
			return label + " · " + line
		}
		return label
	}
	return label + " · " + ago(rec.UpdatedAt, now)
}

func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	case d < 48*time.Hour:
		return "yesterday"
	}
	return t.Local().Format("Jan 2")
}

func mainView(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	children := []sdui.Node{header(sc)}
	if sc.Installing {
		children = append(children, sdui.MutedText(fmt.Sprintf(installingNote, agentLabel(rec.Agent))))
	}
	if sc.NodeProblem != "" && rec.Status == statusError {
		return sdui.VStack(append(children, sdui.Empty(sc.NodeProblem))...)
	}
	if sc.SignIn != "" {
		children = append(children, sdui.MutedText("Sign in to "+agentLabel(rec.Agent)+" in the terminal below; the session retries when it exits."), sdui.Surface(sc.SignIn))
		return sdui.VStack(children...)
	}
	children = append(children, transcript(sc))
	if rec.Closed {
		reopen := sdui.WithAction(sdui.Button(sid+"/reopen", "Reopen", "arrow.clockwise"), actRetry)
		if sc.Continue {
			reopen = continueButton(sid)
		}
		children = append(children, sdui.HStack(sdui.MutedText("The session is closed; its transcript stays readable."), reopen))
		return sdui.VStack(children...)
	}
	if len(sc.Queue) > 0 {
		children = append(children, queueView(sc))
	}
	return sdui.VStack(append(children, composer(sc))...)
}

func queueView(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	head := []sdui.Node{sdui.MutedText(queuedNote)}
	if !turnActive(rec) {
		head = []sdui.Node{sdui.MutedText(heldNote), sdui.WithAction(sdui.Button(sid+"/"+queuePrefix+"send", "Send queued", "paperplane"), actSendQueued)}
	}
	rows := []sdui.Node{sdui.HStack(head...)}
	for _, q := range sc.Queue {
		text := strings.TrimSpace(q.Text)
		if n := len(q.Attachments); n > 0 {
			text = strings.Join(nonEmpty(text, strconv.Itoa(n)+" attached"), " · ")
		}
		rows = append(rows, sdui.HStack(sdui.MutedText(text), sdui.WithAction(sdui.Button(sid+"/"+queuePrefix+q.ID, "Remove", "xmark"), actUnqueue)))
	}
	return sdui.VStack(rows...)
}

func turnActive(rec record) bool {
	return rec.Status == statusRunning || rec.Status == statusWaiting
}

func header(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	meta := []string{agentLabel(rec.Agent), "⑂ " + filepath.Base(rec.Worktree)}
	if name := modeName(sc.View.Modes, rec.Mode); name != "" {
		meta = append(meta, name)
	}
	if name := modelName(sc.View.Configs); name != "" {
		meta = append(meta, name)
	}
	row := []sdui.Node{
		{Type: "text", Props: map[string]any{"text": cmp.Or(rec.Title, "New session"), "style": "title"}},
		sdui.MutedText(strings.Join(meta, " · ")),
	}
	if sc.Changes {
		row = append(row, sdui.WithAction(sdui.Button(sid+"/review", "Review changes", "doc.text.magnifyingglass"), actReview))
	}
	if turnActive(rec) {
		row = append(row, sdui.WithAction(sdui.Button(sid+"/stop", "Stop", "stop.fill"), actStop))
	}
	children := []sdui.Node{sdui.HStack(row...)}
	if rec.Status != statusError {
		return sdui.VStack(children...)
	}
	problem := []sdui.Node{sdui.MutedText(cmp.Or(firstLine(rec.Error), "The agent failed"))}
	switch {
	case sc.NeedsAuth:
		problem = append(problem, sdui.WithAction(sdui.Button(sid+"/signin", "Sign in", "person.badge.key"), actSignIn))
	case sc.Continue:
		problem = append(problem, continueButton(sid))
	default:
		problem = append(problem, sdui.WithAction(sdui.Button(sid+"/retry", "Retry", "arrow.clockwise"), actRetry))
	}
	return sdui.VStack(append(children, sdui.HStack(problem...))...)
}

func continueButton(sid string) sdui.Node {
	return sdui.WithAction(sdui.Button(sid+"/continue", "Continue in a new session", "arrow.turn.down.right"), actContinue)
}

func transcript(sc screen) sdui.Node {
	v := sc.View
	sid := v.Session.ID
	window := max(sc.Window, windowStep)
	turns := turnNumbers(v)
	cutoff := 0
	if len(turns) > window {
		cutoff = turns[len(turns)-window]
	}
	stops := map[int]string{}
	shas := map[int]string{}
	for _, t := range v.Turns {
		stops[t.Turn] = t.StopReason
		shas[t.Turn] = t.CheckpointSha
	}
	var shown []*entry
	for _, en := range v.Entries {
		if en.Turn >= cutoff {
			shown = append(shown, en)
		}
	}
	hidden := len(v.Entries) - len(shown)
	var nodes []sdui.Node
	budget := nodeBudget
	for i := len(shown) - 1; i >= 0; i-- {
		en := shown[i]
		var group []sdui.Node
		if i == len(shown)-1 || shown[i+1].Turn != en.Turn {
			if note := stopNote(stops[en.Turn]); note != "" {
				group = append(group, sdui.MutedText(note))
			}
		}
		n, ok := entryNode(sc, en, shas[en.Turn])
		if ok {
			group = slices.Insert(group, 0, n)
		}
		cost := 0
		for _, g := range group {
			cost += nodeCost(g)
		}
		if cost > budget {
			hidden += i + 1
			break
		}
		budget -= cost
		nodes = append(group, nodes...)
	}
	if hidden > 0 {
		label := "Show earlier"
		if len(turns) > window {
			label += " (" + strconv.Itoa(len(turns)-window) + " more turns)"
		}
		nodes = slices.Insert(nodes, 0, sdui.WithAction(sdui.Button(sid+"/earlier", label, "chevron.up"), actShowEarlier))
	}
	return sdui.Transcript(sid+"/transcript", nodes...)
}

func turnNumbers(v transcriptView) []int {
	seen := map[int]bool{}
	var turns []int
	for _, en := range v.Entries {
		if !seen[en.Turn] {
			seen[en.Turn] = true
			turns = append(turns, en.Turn)
		}
	}
	slices.Sort(turns)
	return turns
}

func stopNote(reason string) string {
	switch reason {
	case "", acp.StopEndTurn:
		return ""
	case acp.StopCancelled:
		return "Stopped"
	}
	return "Stopped: " + strings.ReplaceAll(reason, "_", " ")
}

func nodeCost(n sdui.Node) int {
	cost := 1
	for _, v := range n.Props {
		switch list := v.(type) {
		case []sdui.Attachment:
			cost += len(list)
		case []sdui.PlanEntry:
			cost += len(list)
		case []sdui.CardAction:
			cost += len(list)
		case []sdui.PermissionOption:
			cost += len(list)
		}
	}
	for _, c := range n.Children {
		cost += nodeCost(c)
	}
	return cost
}

func entryNode(sc screen, en *entry, sha string) (sdui.Node, bool) {
	sid := sc.View.Session.ID
	root := sc.View.Session.Worktree
	switch en.Kind {
	case entryUser:
		n := sdui.Message("user", en.Text, promptAttachments(en.Content, root))
		n.ID = sid + "/msg/" + en.Kind + "/" + en.ID
		return n, true
	case entryAgent, entryThought:
		if strings.TrimSpace(en.Text) == "" {
			return sdui.Node{}, false
		}
		n := sdui.Message(en.Kind, en.Text, nil)
		n.ID = sid + "/msg/" + en.Kind + "/" + en.ID
		return n, true
	case entryPlan:
		var p planUpdate
		if json.Unmarshal(en.Plan, &p) != nil {
			return sdui.Node{}, false
		}
		entries := make([]sdui.PlanEntry, 0, len(p.Entries))
		for _, e := range p.Entries {
			status := e.Status
			if !slices.Contains([]string{"pending", "in_progress", "completed"}, status) {
				status = "pending"
			}
			entries = append(entries, sdui.PlanEntry{Content: e.Content, Status: status})
		}
		n := sdui.Plan(entries)
		n.ID = sid + "/plan/" + strconv.Itoa(en.Turn)
		return n, true
	case entryTool:
		return toolCard(sid+"/tool/"+en.ID, en, sc.Expanded[en.ID], sha != ""), true
	case entryPermission:
		return permissionCard(sc, en), true
	case entryNotice:
		var n noticeUpdate
		if json.Unmarshal(en.Notice, &n) != nil || n.Title == "" && n.Description == "" {
			return sdui.Node{}, false
		}
		return sdui.MutedText(strings.TrimSpace(strings.Join(nonEmpty(n.Title, n.Description), " — "))), true
	}
	return sdui.Node{}, false
}

func toolCard(id string, en *entry, expanded, reviewable bool) sdui.Node {
	var f toolFields
	for name, dst := range map[string]any{"title": &f.Title, "kind": &f.Kind, "status": &f.Status, "content": &f.Content, "locations": &f.Locations, "rawInput": &f.RawInput} {
		if raw, ok := en.Tool[name]; ok {
			_ = json.Unmarshal(raw, dst)
		}
	}
	kind := f.Kind
	if !slices.Contains(cardKinds, kind) {
		kind = "other"
	}
	status := f.Status
	if status == "" {
		status = "pending"
	}
	if !slices.Contains(cardStatuses, status) {
		status = ""
	}
	command, _ := f.RawInput["command"].(string)
	p := sdui.ToolCardProps{Kind: kind, Title: cmp.Or(f.Title, command, kind), Status: status}
	body := toolBody(kind, command, f.Content)
	if body != nil && !slices.Contains(oneLineKinds, kind) {
		text, cut := truncateBody(body.Text, expanded)
		body.Text = text
		p.Body, p.Truncated = body, cut
	}
	if path, line := location(f); path != "" && kind != "execute" {
		value := map[string]any{"path": path}
		if line > 0 {
			value["line"] = line
		}
		p.Actions = append(p.Actions, sdui.CardAction{Action: actOpenLocation, Label: "Open in nvim", Icon: "arrow.up.forward.square", Value: value})
	}
	if reviewable && slices.Contains(editKinds, kind) {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actReview, Label: "Diff", Icon: "plus.forwardslash.minus", Value: map[string]any{"turn": en.Turn}})
	}
	if p.Truncated && !expanded {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actShowAll, Label: "Show all", Icon: "arrow.down.right.and.arrow.up.left"})
	}
	return sdui.ToolCard(id, p)
}

func toolBody(kind, command string, content []toolContent) *sdui.ToolBody {
	var diffs, texts []string
	for _, c := range content {
		switch c.Type {
		case "diff":
			diffs = append(diffs, unifiedDiff(c.Path, c.OldText, c.NewText))
		case "content":
			if c.Content.Type == "text" && c.Content.Text != "" {
				texts = append(texts, c.Content.Text)
			}
		}
	}
	switch {
	case len(diffs) > 0:
		return &sdui.ToolBody{Format: "diff", Text: strings.Join(diffs, "")}
	case kind == "execute":
		text := strings.Join(texts, "\n")
		if command != "" {
			text = strings.TrimRight("$ "+command+"\n"+text, "\n")
		}
		if text == "" {
			return nil
		}
		return &sdui.ToolBody{Format: "terminal", Text: text}
	case len(texts) > 0:
		return &sdui.ToolBody{Format: "text", Text: strings.Join(texts, "\n")}
	}
	return nil
}

func location(f toolFields) (string, int) {
	for _, l := range f.Locations {
		if l.Path != "" {
			return l.Path, l.Line
		}
	}
	for _, c := range f.Content {
		if c.Type == "diff" && c.Path != "" {
			return c.Path, firstChangedLine(c.OldText, c.NewText)
		}
	}
	return "", 0
}

func firstChangedLine(oldText *string, newText string) int {
	old, cur := splitLines(oldText), splitLines(&newText)
	prefix, _ := commonEnds(old, cur)
	return prefix + 1
}

func truncateBody(text string, expanded bool) (string, bool) {
	limitBytes, limitLines := bodyBytes, bodyLines
	if expanded {
		limitBytes, limitLines = expandedBytes, -1
	}
	cut := false
	if limitLines >= 0 {
		if i := nthNewline(text, limitLines); i >= 0 && i < len(text)-1 {
			text, cut = text[:i], true
		}
	}
	if len(text) > limitBytes {
		end := limitBytes
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		text, cut = text[:end], true
	}
	return text, cut
}

func nthNewline(text string, n int) int {
	pos := -1
	for range n {
		i := strings.IndexByte(text[pos+1:], '\n')
		if i < 0 {
			return -1
		}
		pos += i + 1
	}
	return pos
}

func unifiedDiff(path string, oldText *string, newText string) string {
	old, cur := splitLines(oldText), splitLines(&newText)
	prefix, suffix := commonEnds(old, cur)
	if prefix == len(old) && prefix == len(cur) {
		return ""
	}
	start := max(prefix-diffContext, 0)
	oldEnd := min(len(old)-suffix+diffContext, len(old))
	curEnd := min(len(cur)-suffix+diffContext, len(cur))
	from := "a/" + strings.TrimPrefix(path, "/")
	if oldText == nil {
		from = "/dev/null"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", from, strings.TrimPrefix(path, "/"), start+1, oldEnd-start, start+1, curEnd-start)
	for _, l := range old[start:prefix] {
		b.WriteString(" " + l + "\n")
	}
	for _, l := range old[prefix : len(old)-suffix] {
		b.WriteString("-" + l + "\n")
	}
	for _, l := range cur[prefix : len(cur)-suffix] {
		b.WriteString("+" + l + "\n")
	}
	for _, l := range old[len(old)-suffix : oldEnd] {
		b.WriteString(" " + l + "\n")
	}
	return b.String()
}

func splitLines(text *string) []string {
	if text == nil || *text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(*text, "\n"), "\n")
}

func commonEnds(a, b []string) (int, int) {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	return prefix, suffix
}

func permissionCard(sc screen, en *entry) sdui.Node {
	rec := sc.View.Session
	var f toolFields
	_ = json.Unmarshal(en.ToolCall, &f)
	text := agentLabel(rec.Agent) + " asks for permission"
	if f.Title != "" {
		verb := cmp.Or(permVerbs[f.Kind], "use")
		text = agentLabel(rec.Agent) + " wants to " + verb + " " + f.Title
	}
	options := make([]sdui.PermissionOption, 0, len(en.Options))
	seen := map[string]bool{}
	for _, o := range en.Options {
		if o.OptionID == "" || seen[o.OptionID] {
			continue
		}
		seen[o.OptionID] = true
		kind := o.Kind
		if !slices.Contains([]string{"allow_once", "allow_always", "reject_once", "reject_always"}, kind) {
			kind = ""
		}
		options = append(options, sdui.PermissionOption{ID: o.OptionID, Label: cmp.Or(o.Name, o.OptionID), Kind: kind})
	}
	outcome := ""
	switch {
	case en.Outcome != nil && en.Outcome.Outcome == acp.OutcomeSelected:
		outcome = en.Outcome.OptionID
	case en.Outcome != nil || !sc.Pending[en.ID]:
		outcome = acp.OutcomeCancelled
	}
	return sdui.PermissionCard(rec.ID+"/permission/"+en.ID, en.ID, text, options, outcome)
}

func promptAttachments(content []json.RawMessage, root string) []sdui.Attachment {
	var out []sdui.Attachment
	textSeen := false
	for _, raw := range content {
		var b acp.ContentBlock
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		switch b.Type {
		case "text":
			if !textSeen {
				textSeen = true
				continue
			}
			if m := selectionRe.FindStringSubmatch(b.Text); m != nil {
				start, _ := strconv.Atoi(m[2])
				end, _ := strconv.Atoi(m[3])
				out = append(out, sdui.Attachment{Kind: "selection", Path: relPath(m[1], root), StartLine: start, EndLine: end})
			}
		case "resource_link":
			if path, _, _ := fileURI(b.URI); path != "" {
				out = append(out, sdui.Attachment{Kind: "file", Path: relPath(path, root)})
			}
		case "resource":
			if b.Resource == nil {
				continue
			}
			path, start, end := fileURI(b.Resource.URI)
			if path == "" {
				continue
			}
			a := sdui.Attachment{Kind: "file", Path: relPath(path, root)}
			if start > 0 {
				a.Kind, a.StartLine, a.EndLine = "selection", start, end
			}
			out = append(out, a)
		}
	}
	return out
}

func fileURI(uri string) (string, int, int) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", 0, 0
	}
	if m := fragmentRe.FindStringSubmatch(u.Fragment); m != nil {
		start, _ := strconv.Atoi(m[1])
		end, _ := strconv.Atoi(m[2])
		return u.Path, start, end
	}
	return u.Path, 0, 0
}

func relPath(path, root string) string {
	if rel, err := filepath.Rel(root, path); err == nil && filepath.IsAbs(path) && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

func composer(sc screen) sdui.Node {
	v := sc.View
	rec := v.Session
	modes := make([]sdui.Option, 0, len(v.Modes))
	for _, m := range v.Modes {
		modes = append(modes, sdui.Option{Value: m.ID, Label: cmp.Or(m.Name, m.ID)})
	}
	commands := make([]sdui.Command, 0, len(v.Commands))
	for _, c := range v.Commands {
		var input struct {
			Hint string `json:"hint"`
		}
		_ = json.Unmarshal(c.Input, &input)
		commands = append(commands, sdui.Command{Name: c.Name, Description: c.Description, Hint: input.Hint})
	}
	attachments := make([]sdui.Attachment, 0, len(sc.Attachments))
	for _, a := range sc.Attachments {
		attachments = append(attachments, sdui.Attachment{ID: a.ID, Kind: a.Kind, Path: relPath(a.Path, rec.Worktree), StartLine: a.StartLine, EndLine: a.EndLine})
	}
	placeholder := "Ask " + agentLabel(rec.Agent) + ", @ to attach a file, / for commands…"
	if len(v.Turns) > 0 {
		placeholder = "Ask a follow-up, or type / for commands…"
	}
	return sdui.Composer(sc.Composer, sdui.ComposerProps{
		Placeholder: placeholder,
		Agent:       agentLabel(rec.Agent),
		Modes:       modes,
		Mode:        rec.Mode,
		Pickers:     pickers(sc),
		Commands:    commands,
		Attach:      []string{"file", "selection"},
		Attachments: attachments,
		Running:     turnActive(rec),
	})
}

func pickers(sc screen) []sdui.Picker {
	var out []sdui.Picker
	for _, o := range sc.View.Configs {
		if !pickable(o) {
			continue
		}
		choices := o.Choices()
		options := make([]sdui.Option, 0, len(choices))
		for _, c := range choices {
			options = append(options, sdui.Option{Value: c.Value, Label: cmp.Or(c.Name, c.Value)})
		}
		p := sdui.Picker{ID: o.ID, Help: cmp.Or(o.Description, o.Name), Value: o.Current(), Options: options}
		if o.Category != acp.CategoryModel {
			p.Title = o.Name
		}
		out = append(out, p)
	}
	if sc.View.Session.Agent != agentClaude {
		return out
	}
	options := make([]sdui.Option, 0, len(advisorChoices))
	for _, c := range advisorChoices {
		options = append(options, sdui.Option{Value: c.value, Label: c.label})
	}
	out = append(out, sdui.Picker{ID: advisorID, Title: "Advisor", Help: advisorHelp, Value: sc.Advisor, Options: options})
	finish := make([]sdui.Option, 0, len(finishChoices))
	for _, c := range finishChoices {
		finish = append(finish, sdui.Option{Value: c.value, Label: c.label})
	}
	return append(out, sdui.Picker{ID: finishID, Title: "Finish", Help: finishHelp, Value: cmp.Or(sc.View.Session.Finish, finishAsk), Options: finish})
}

func modelName(configs []acp.SessionConfigOption) string {
	for _, o := range configs {
		if o.Category != acp.CategoryModel {
			continue
		}
		if c, ok := o.Choice(o.Current()); ok {
			return cmp.Or(c.Name, c.Value)
		}
		return o.Current()
	}
	return ""
}

func modeName(modes []acp.SessionMode, id string) string {
	for _, m := range modes {
		if m.ID == id {
			return cmp.Or(m.Name, m.ID)
		}
	}
	return id
}

func firstCheckpoint(turns []turnInfo) turnInfo {
	var best turnInfo
	for _, t := range turns {
		if t.CheckpointSha != "" && (best.Turn == 0 || t.Turn < best.Turn) {
			best = t
		}
	}
	return best
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
