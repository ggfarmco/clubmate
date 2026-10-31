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
	"unicode/utf8"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	windowStep    = 20
	bodyLines     = 200
	bodyBytes     = 64 << 10
	expandedBytes = 1 << 20
	nodeBudget    = 4000
	diffContext   = 3
	fromAnother   = "another agent"
)

var (
	cardKinds    = []string{"read", "edit", "delete", "move", "search", "execute", "think", "fetch", "switch_mode", "other"}
	cardStatuses = []string{"pending", "in_progress", "completed", "failed", "cancelled"}
	oneLineKinds = []string{"read", "search"}
	permVerbs    = map[string]string{"execute": "run", "edit": "edit", "delete": "delete", "move": "move", "read": "read", "fetch": "fetch"}
	titleVerbs   = map[string][]string{"execute": {"Run"}, "edit": {"Write", "Edit"}, "delete": {"Delete"}, "move": {"Move"}, "read": {"Read"}, "fetch": {"Fetch"}}
	selectionRe  = regexp.MustCompile(`^(.+):(\d+)-(\d+)\n`)
	fragmentRe   = regexp.MustCompile(`^L(\d+)-(\d+)$`)
)

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
		return toolCard(sid+"/tool/"+en.ID, root, en, sc.Expanded[en.ID], sha != ""), true
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

func toolCard(id, root string, en *entry, expanded, reviewable bool) sdui.Node {
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
	p := sdui.ToolCardProps{Kind: kind, Title: cmp.Or(shortPaths(f.Title, root), command, kind), Status: status}
	body := toolBody(kind, command, root, f.Content)
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
	var f toolFields
	_ = json.Unmarshal(en.ToolCall, &f)
	text := agentLabel(rec.Agent) + " asks for permission"
	if f.Title != "" {
		verb, title := cmp.Or(permVerbs[f.Kind], "use"), shortPaths(f.Title, rec.Worktree)
		if first, rest, ok := strings.Cut(title, " "); ok && slices.Contains(titleVerbs[f.Kind], first) {
			verb, title = strings.ToLower(first), rest
		}
		text = agentLabel(rec.Agent) + " wants to " + verb + " " + title
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
