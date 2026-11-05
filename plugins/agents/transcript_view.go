package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	windowStep    = 20
	bodyLines     = 200
	scrollLines   = 24
	bodyBytes     = 64 << 10
	expandedBytes = 1 << 20
	nodeBudget    = 4000
	diffContext   = 3
	fromAnother   = "another agent"
)

var (
	cardKinds        = []string{"read", "edit", "delete", "move", "search", "execute", "think", "agent", "fetch", "switch_mode", "other"}
	cardStatuses     = []string{"pending", "in_progress", "completed", "failed", "cancelled"}
	oneLineKinds     = []string{"read", "search"}
	permVerbs        = map[string]string{"execute": "run", "edit": "edit", "delete": "delete", "move": "move", "read": "read", "fetch": "fetch"}
	permTitles       = map[string]string{"execute": "Run a command?"}
	optionLabels     = map[string]string{"allow_once": "Allow once", "allow_always": "Always allow…", "reject_once": "Reject", "reject_always": "Always reject"}
	permissionKinds  = []string{"allow_once", "allow_always", "reject_once", "reject_always"}
	idRefRe          = regexp.MustCompile(` \((?:agt|gr)_[0-9A-Za-z]+\)`)
	transcriptHintRe = regexp.MustCompile(` Read its full transcript with agents\.transcript session=\S+ when this summary is not enough\.`)
	allowCommandsRe  = regexp.MustCompile(`don't ask again for (.+) commands$`)
	listSplitRe      = regexp.MustCompile(`,? and |, `)
	titleVerbs       = map[string][]string{"execute": {"Run"}, "edit": {"Write", "Edit"}, "delete": {"Delete"}, "move": {"Move"}, "read": {"Read"}, "fetch": {"Fetch"}}
	selectionRe      = regexp.MustCompile(`^(.+):(\d+)-(\d+)\n`)
	fragmentRe       = regexp.MustCompile(`^L(\d+)-(\d+)$`)
	planHeadRe       = regexp.MustCompile(`(?i)^(#{1,6}\s+)?[*_]{0,2}plan[*_]{0,2}:?[*_]{0,2}$`)
	planStepRe       = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+\S`)
)

type nodeCache struct {
	mu    sync.Mutex
	key   nodeKey
	built builtNode
	ok    bool
}

type nodeKey struct {
	sid        string
	root       string
	reviewable bool
	pending    bool
	from       string
	review     string
}

type builtNode struct {
	node  sdui.Node
	shown bool
	cost  int
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
	shown = openItemsLast(shown)
	var nodes []sdui.Node
	budget := nodeBudget
	for i := len(shown) - 1; i >= 0; i-- {
		en := shown[i]
		note := ""
		if i == len(shown)-1 || shown[i+1].Turn != en.Turn {
			note = stopNote(stops[en.Turn])
		}
		b := entryNode(sc, en, shas[en.Turn])
		b.shown = b.shown && !pending(sc, en) && !foldsIntoPlan(shown, i)
		cost := 0
		if note != "" {
			cost++
		}
		if b.shown {
			cost += b.cost
		}
		if cost > budget {
			hidden += i + 1
			break
		}
		budget -= cost
		if note != "" {
			nodes = append(nodes, sdui.MutedText(note))
		}
		if b.shown {
			nodes = append(nodes, thoughtDuration(b.node, shown, i))
		}
	}
	slices.Reverse(nodes)
	if hidden > 0 {
		label := "Show earlier"
		if len(turns) > window {
			label += " (" + strconv.Itoa(len(turns)-window) + " more turns)"
		}
		nodes = slices.Insert(nodes, 0, sdui.WithAction(sdui.Button(sid+"/earlier", label, "chevron.up"), actShowEarlier))
	}
	n := sdui.Transcript(sid+"/transcript", nodes...)
	if sc.Anchor != "" {
		n = sdui.WithAnchorRev(sdui.WithAnchor(n, sc.Anchor), sc.AnchorRev)
	}
	return n
}

func openItemsLast(shown []*entry) []*entry {
	out := make([]*entry, 0, len(shown))
	var held []*entry
	for i, en := range shown {
		if en.Kind == entryOpenItems {
			held = append(held, en)
		} else {
			out = append(out, en)
		}
		if i == len(shown)-1 || shown[i+1].Turn != en.Turn {
			out = append(out, held...)
			held = nil
		}
	}
	return out
}

func shownPrompt(text string) string {
	return idRefRe.ReplaceAllString(transcriptHintRe.ReplaceAllString(text, ""), "")
}

func thoughtDuration(n sdui.Node, shown []*entry, i int) sdui.Node {
	if shown[i].Kind != entryThought || i+1 >= len(shown) || shown[i].At.IsZero() || shown[i+1].At.IsZero() {
		return n
	}
	n.Props = maps.Clone(n.Props)
	n.Props["seconds"] = max(1, int(shown[i+1].At.Sub(shown[i].At).Round(time.Second)/time.Second))
	return n
}

func foldsIntoPlan(shown []*entry, i int) bool {
	if i+1 >= len(shown) || shown[i].Kind != entryAgent || shown[i+1].Kind != entryPlan {
		return false
	}
	var p planUpdate
	return json.Unmarshal(shown[i+1].Plan, &p) == nil && len(p.Entries) > 0 && planOnly(shown[i].Text)
}

func planOnly(text string) bool {
	for i, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimRight(line, " \t")
		switch {
		case line == "", planStepRe.MatchString(line):
		case i == 0 && planHeadRe.MatchString(line):
		default:
			return false
		}
	}
	return true
}

func pending(sc screen, en *entry) bool {
	return (en.Kind == entryPermission && en.Outcome == nil || en.Kind == entryElicitation && en.Response == nil) && sc.Pending[en.ID]
}

func dockedPermission(sc screen) (sdui.Node, bool) {
	for _, en := range sc.View.Entries {
		if pending(sc, en) && en.Kind == entryElicitation {
			return elicitationCard(sc, en), true
		}
		if pending(sc, en) {
			return permissionCard(sc, en), true
		}
	}
	return sdui.Node{}, false
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

func entryNode(sc screen, en *entry, sha string) builtNode {
	build := func() builtNode {
		n, ok := buildEntryNode(sc, en, sha)
		if !ok {
			return builtNode{}
		}
		return builtNode{node: n, shown: true, cost: nodeCost(n)}
	}
	if en.node == nil || en.Kind == entryTool && sc.Expanded[en.ID] || en.Kind == entryOpenItems {
		return build()
	}
	key := nodeKey{sid: sc.View.Session.ID, root: sc.View.Session.Worktree, reviewable: sha != "", pending: sc.Pending[en.ID], from: sc.FromTitles[en.From], review: reviewCardKey(sc, en)}
	return en.node.get(key, build)
}

func (c *nodeCache) get(key nodeKey, build func() builtNode) builtNode {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ok || c.key != key {
		c.key, c.built, c.ok = key, build(), true
	}
	return c.built
}

func buildEntryNode(sc screen, en *entry, sha string) (sdui.Node, bool) {
	sid := sc.View.Session.ID
	root := sc.View.Session.Worktree
	switch en.Kind {
	case entryUser:
		if en.Review != nil {
			return reviewCard(sc, en), true
		}
		n := sdui.Message("user", shownPrompt(en.Text), promptAttachments(en.Content, root))
		if en.Voice != nil {
			n = sdui.Message("user", en.Text, nil)
			n.Props["heard"] = en.Voice.Heard
		}
		if en.From != "" {
			n.Props["from"] = cmp.Or(sc.FromTitles[en.From], fromAnother)
		}
		n.ID = sid + "/msg/" + en.Kind + "/" + en.ID
		return n, true
	case entryAgent, entryThought:
		if strings.TrimSpace(en.Text) == "" {
			return sdui.Node{}, false
		}
		n := sdui.Message(en.Kind, en.Text, nil)
		if en.Kind == entryAgent && len(en.DocLinks) > 0 {
			n.Props["docLinks"] = en.DocLinks
		}
		if en.Kind == entryAgent && len(en.DocCards) > 0 {
			n.Props["docCards"] = en.DocCards
		}
		if en.Kind == entryAgent && len(en.Images) > 0 {
			n.Props["images"] = en.Images
		}
		if en.Kind == entryAgent && len(en.ImageLinks) > 0 {
			n.Props["imageLinks"] = en.ImageLinks
		}
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
		return toolCard(sid+"/tool/"+en.ID, root, en, sc.Expanded[en.ID], sha != ""), !en.Hidden
	case entryPermission:
		return permissionCard(sc, en), true
	case entryElicitation:
		return elicitationCard(sc, en), en.Elicitation != nil
	case entryOpenItems:
		return openEntryNode(sc, en)
	case entryCompaction:
		return compactionCard(sid+"/compaction/"+en.ID, en), true
	case entryNotice:
		var n noticeUpdate
		if json.Unmarshal(en.Notice, &n) != nil || n.Title == "" && n.Description == "" {
			return sdui.Node{}, false
		}
		return sdui.MutedText(strings.TrimSpace(strings.Join(nonEmpty(n.Title, n.Description), " — "))), true
	}
	return sdui.Node{}, false
}

func parseTool(tool map[string]json.RawMessage) (toolFields, string, string) {
	var f toolFields
	for name, dst := range map[string]any{"title": &f.Title, "kind": &f.Kind, "status": &f.Status, "content": &f.Content, "locations": &f.Locations, "rawInput": &f.RawInput} {
		if raw, ok := tool[name]; ok {
			_ = json.Unmarshal(raw, dst)
		}
	}
	kind := f.Kind
	if !slices.Contains(cardKinds, kind) {
		kind = "other"
	}
	command, _ := f.RawInput["command"].(string)
	return f, kind, command
}

func toolCard(id, root string, en *entry, expanded, reviewable bool) sdui.Node {
	f, kind, command := parseTool(en.Tool)
	status := f.Status
	if status == "" {
		status = "pending"
	}
	if !slices.Contains(cardStatuses, status) {
		status = ""
	}
	p := sdui.ToolCardProps{Kind: kind, Title: cmp.Or(shortPaths(f.Title, root), command, kind), Status: status}
	body := toolBody(kind, command, root, f.Content)
	scrolls := false
	if body != nil && !slices.Contains(oneLineKinds, kind) {
		scrolls = strings.Count(strings.TrimRight(body.Text, "\n"), "\n") >= scrollLines
		text, cut := truncateBody(body.Text, expanded)
		body.Text = text
		p.Body, p.Truncated = body, cut
	}
	if en.Stoppable {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actStopSubagent, Label: "Stop", Icon: "stop.circle"})
	}
	if en.Doc != "" {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actOpenDoc, Label: "Open", Icon: "doc.richtext", Value: en.Doc})
	}
	if path, line := location(f); path != "" && kind != "execute" {
		value := map[string]any{"path": path}
		if line > 0 {
			value["line"] = line
		}
		p.Actions = append(p.Actions, sdui.CardAction{Action: actOpenLocation, Label: "Open in nvim", Icon: "arrow.up.forward.square", Value: value})
	}
	if reviewable && slices.Contains(editKinds, kind) {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actReview, Label: "Full diff", Icon: "plus.forwardslash.minus", Value: map[string]any{"turn": en.Turn}})
	}
	if _, prompt := f.RawInput["prompt"].(string); scrolls || p.Truncated || prompt {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actOpenOutput, Label: "Open output", Icon: "doc.text"})
	}
	if p.Truncated && !expanded {
		p.Actions = append(p.Actions, sdui.CardAction{Action: actShowAll, Label: "Show all", Icon: "arrow.down.right.and.arrow.up.left"})
	}
	return sdui.ToolCard(id, p)
}

func toolBody(kind, command, root string, content []toolContent) *sdui.ToolBody {
	var diffs, texts []string
	for _, c := range content {
		switch c.Type {
		case "diff":
			diffs = append(diffs, unifiedDiff(relPath(c.Path, root), c.OldText, c.NewText))
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
	c := permissionContentOf(rec, en, sc.Workspace)
	p := sdui.PermissionCardProps{Text: c.Text, Title: c.Title, Command: c.Command, Caption: c.Caption, Options: c.Options}
	switch {
	case en.Outcome != nil && en.Outcome.Outcome == acp.OutcomeSelected:
		p.Outcome = en.Outcome.OptionID
	case en.Outcome != nil || !sc.Pending[en.ID]:
		p.Outcome = acp.OutcomeCancelled
	}
	return sdui.PermissionCard(rec.ID+"/permission/"+en.ID, en.ID, p)
}

func permissionContentOf(rec record, en *entry, workspace string) *permissionContent {
	var f toolFields
	_ = json.Unmarshal(en.ToolCall, &f)
	label := agentLabel(rec.Agent)
	if en.Subagent != "" {
		label += " (" + en.Subagent + ")"
	}
	text := label + " asks for permission"
	if f.Title != "" {
		verb, title := cmp.Or(permVerbs[f.Kind], "use"), shortPaths(f.Title, rec.Worktree)
		if first, rest, ok := strings.Cut(title, " "); ok && slices.Contains(titleVerbs[f.Kind], first) {
			verb, title = strings.ToLower(first), rest
		}
		text = label + " wants to " + verb + " " + title
	}
	c := &permissionContent{Text: text, Title: cmp.Or(permTitles[f.Kind], text), Options: permissionOptions(workspace, en, f)}
	if command, ok := f.RawInput["command"].(string); ok && strings.TrimSpace(command) != "" {
		c.Command = strings.TrimSpace(command)
	}
	if desc, ok := f.RawInput["description"].(string); ok && strings.TrimSpace(desc) != "" {
		c.Caption = label + " says: " + strings.TrimSpace(desc)
	}
	return c
}

func withPermissionContent(rec record, entries []*entry, workspace string) {
	for _, en := range entries {
		if en.Kind == entryPermission {
			en.Card = permissionContentOf(rec, en, workspace)
		}
	}
}

func permissionOptions(workspace string, en *entry, f toolFields) []sdui.PermissionOption {
	options := make([]sdui.PermissionOption, 0, len(en.Options))
	seen := map[string]bool{}
	kinds := map[string]int{}
	for _, o := range en.Options {
		if o.OptionID == "" || seen[o.OptionID] {
			continue
		}
		seen[o.OptionID] = true
		kind := o.Kind
		if !slices.Contains(permissionKinds, kind) {
			kind = ""
		}
		kinds[kind]++
		options = append(options, sdui.PermissionOption{ID: o.OptionID, Label: cmp.Or(o.Name, o.OptionID), Kind: kind})
	}
	slices.SortStableFunc(options, func(a, b sdui.PermissionOption) int { return cmp.Compare(optionRank(a.Kind), optionRank(b.Kind)) })
	standard := kinds[""] == 0 && !slices.ContainsFunc(permissionKinds, func(k string) bool { return kinds[k] > 1 })
	keys := 0
	for i, o := range options {
		if standard {
			if o.Kind == "allow_always" {
				o.Rule = allowRule(f, en.ToolCall, o.Label)
				o.Scope = allowScope(workspace)
				o.Workspace = workspace
			}
			o.Label = optionLabels[o.Kind]
			o.Menu = o.Kind == "reject_always"
		}
		if !o.Menu && keys < 3 {
			keys++
			o.Shortcut = "opt+" + strconv.Itoa(keys)
		}
		options[i] = o
	}
	return options
}

func allowRule(f toolFields, call json.RawMessage, name string) string {
	m := allowCommandsRe.FindStringSubmatch(name)
	if f.Kind != "execute" || m == nil || m[1] == "similar" {
		return ""
	}
	var meta struct {
		Meta struct {
			ClaudeCode struct {
				ToolName string `json:"toolName"`
			} `json:"claudeCode"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(call, &meta)
	tool := cmp.Or(meta.Meta.ClaudeCode.ToolName, "Bash")
	var rules []string
	for _, c := range listSplitRe.Split(m[1], -1) {
		if c = strings.TrimSpace(c); c != "" {
			rules = append(rules, tool+"("+c+":*)")
		}
	}
	return strings.Join(rules, " ")
}

func allowScope(workspace string) string {
	if workspace == "" {
		return "on this Mac"
	}
	return "in " + workspace + " on this Mac"
}

func optionRank(kind string) int {
	if i := slices.Index(permissionKinds, kind); i >= 0 {
		return i
	}
	return 4
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
		case "image":
			out = append(out, sdui.Attachment{Kind: "image", Path: "image"})
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

func shortPaths(text, root string) string {
	if root == "" {
		return text
	}
	return strings.ReplaceAll(text, strings.TrimSuffix(root, "/")+"/", "")
}
