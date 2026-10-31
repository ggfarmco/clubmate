package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/internal/claudectx"
)

const (
	actContext  = "context"
	contextNode = "context"
	contextKey  = "cmd+i"

	ctxGroup        = "g:"
	ctxInstructions = "instructions"
	noContextLog    = "Claude has not written a log for this session yet."
	deferredNote    = "Loaded on demand through ToolSearch; the log has no description for it."
	notUsed         = "Not used in this session."
)

type contextScreen struct {
	Data     claudectx.Context
	Problem  string
	Selected string
	Open     map[string]bool
	Worktree string
}

type contextGroup struct {
	id    string
	label string
	icon  string
	items []contextItem
	used  int
}

type contextItem struct {
	id     string
	label  string
	detail string
	icon   string
	tone   string
	muted  bool
	title  string
	meta   string
	body   string
	note   string
}

func contextButton(sc screen) (sdui.Node, bool) {
	rec := sc.View.Session
	if !hasContext(rec) {
		return sdui.Node{}, false
	}
	label, icon := "Context", "list.bullet.rectangle"
	if sc.Context != nil {
		label, icon = "Transcript", "bubble.left"
	}
	return sdui.WithShortcut(sdui.WithAction(sdui.Button(rec.ID+"/"+contextNode+"/toggle", label, icon), actContext), contextKey), true
}

func hasContext(rec record) bool {
	return rec.Agent == agentClaude && rec.AgentSessionID != ""
}

func contextView(sc screen) sdui.Node {
	c := sc.Context
	if c.Problem != "" {
		return sdui.EmptyState(c.Problem, sdui.EmptyProps{Icon: "doc.text.magnifyingglass"})
	}
	groups := contextGroups(*c)
	selected := c.Selected
	if selected == "" && len(groups[0].items) > 0 {
		selected = groups[0].items[0].id
	}
	var items []sdui.TreeItem
	var body sdui.Node
	for _, g := range groups {
		open := c.Open[g.id]
		if _, set := c.Open[g.id]; !set {
			open = g.id == ctxInstructions
		}
		gi := sdui.TreeItem{ID: ctxGroup + g.id, Label: g.label, Icon: g.icon, Detail: groupDetail(g), HasChildren: len(g.items) > 0, Expanded: open}
		for _, it := range g.items {
			if open {
				gi.Children = append(gi.Children, sdui.TreeItem{ID: it.id, Label: it.label, Detail: it.detail, Icon: it.icon, Tone: it.tone, Muted: it.muted})
			}
			if it.id == selected {
				body = itemBody(it)
			}
		}
		if gi.ID == selected {
			body = sdui.MutedText(g.label + ": " + groupDetail(g))
		}
		items = append(items, gi)
	}
	if body.Type == "" {
		body = sdui.MutedText("Pick an item to read what the session sees.")
	}
	tree := sdui.Tree(sc.View.Session.ID+"/"+contextNode, items, selected)
	return sdui.Split("vertical", tree, sdui.Scroll(body))
}

func contextGroups(c contextScreen) []contextGroup {
	d := c.Data
	instructions := contextGroup{id: ctxInstructions, label: "Instructions", icon: "doc.text"}
	for _, f := range d.Instructions {
		kind := strings.ToLower(f.Kind)
		if f.Kind == "AutoMem" {
			kind = "memory"
		}
		if f.Nested {
			kind += " · nested"
		}
		path := displayPath(f.Path, c.Worktree)
		instructions.items = append(instructions.items, contextItem{id: "i:" + f.Path, label: path, detail: kind, title: path, meta: f.Path, body: f.Content})
	}
	mcp := contextGroup{id: "mcp", label: "MCP servers", icon: "server.rack"}
	for _, e := range d.MCP {
		mcp.items = append(mcp.items, contextItem{id: "m:" + e.Name, label: e.Name, title: e.Name, body: e.Text, note: "The server sent no instructions."})
	}
	for _, n := range d.NeedsAuth {
		mcp.items = append(mcp.items, contextItem{id: "mx:" + n, label: n, detail: "needs sign-in", icon: "exclamationmark.triangle", tone: "warning", title: n, note: "Its tools are unavailable until it is authorised."})
	}
	for _, n := range d.Failed {
		mcp.items = append(mcp.items, contextItem{id: "mx:" + n, label: n, detail: "failed", icon: "xmark.octagon", tone: "danger", title: n, note: "The server did not start; its tools are unavailable."})
	}
	hooks := contextGroup{id: "hooks", label: "Hooks", icon: "bolt"}
	for _, e := range d.Hooks {
		hooks.items = append(hooks.items, contextItem{id: "h:" + e.Name, label: e.Name, title: e.Name, meta: "Context added by the " + e.Name + " hook", body: e.Text})
	}
	return []contextGroup{
		instructions,
		usedGroup("skills", "Skills", "sparkles", "s:", d.Skills, ""),
		mcp,
		hooks,
		usedGroup("subagents", "Subagents", "person.2", "a:", d.Subagents, ""),
		usedGroup("tools", "Tools", "wrench.and.screwdriver", "t:", d.Tools, ""),
		usedGroup("deferred", "Deferred tools", "clock", "d:", d.Deferred, deferredNote),
	}
}

func usedGroup(id, label, icon, prefix string, entries []claudectx.Entry, note string) contextGroup {
	g := contextGroup{id: id, label: label, icon: icon}
	for _, e := range entries {
		it := contextItem{id: prefix + e.Name, label: e.Name, title: e.Name, body: e.Text, note: note, muted: e.Uses == 0, meta: notUsed}
		if e.Uses > 0 {
			g.used++
			it.detail = "×" + strconv.Itoa(e.Uses)
			it.meta = "Used " + times(e.Uses) + " in this session."
		}
		g.items = append(g.items, it)
	}
	return g
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return strconv.Itoa(n) + " times"
}

func groupDetail(g contextGroup) string {
	n := strconv.Itoa(len(g.items))
	if g.used > 0 {
		return strconv.Itoa(g.used) + " of " + n + " used"
	}
	return n
}

func itemBody(it contextItem) sdui.Node {
	children := []sdui.Node{{Type: "text", Props: map[string]any{"text": it.title, "style": "title"}}}
	if it.meta != "" && it.meta != it.title {
		children = append(children, sdui.MutedText(it.meta))
	}
	switch {
	case it.body != "":
		children = append(children, sdui.Markdown(it.body))
	case it.note != "":
		children = append(children, sdui.MutedText(it.note))
	}
	return sdui.VStack(children...)
}

func displayPath(path, worktree string) string {
	if rel, err := filepath.Rel(worktree, path); err == nil && worktree != "" && !strings.HasPrefix(rel, "..") {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = "~/" + rest
		}
	}
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) <= 4 {
		return path
	}
	return strings.Join([]string{parts[0], "…", parts[len(parts)-2], parts[len(parts)-1]}, string(filepath.Separator))
}

func (v *sessionUI) contextEvent(action, id string) {
	switch action {
	case actContext:
		v.context = !v.context
	case actSelect:
		v.contextSelected = id
	case actExpand, actCollapse:
		if v.contextOpen == nil {
			v.contextOpen = map[string]bool{}
		}
		v.contextOpen[strings.TrimPrefix(id, ctxGroup)] = action == actExpand
	}
}

func isContextEvent(ev pluginsdk.UIEvent) (string, bool) {
	sid, rest, ok := strings.Cut(ev.NodeID, "/")
	if !ok || (rest != contextNode && !strings.HasPrefix(rest, contextNode+"/")) {
		return "", false
	}
	return sid, true
}

func (a *app) contextScreen(st *sessionUI, rec record) *contextScreen {
	a.uiMu.Lock()
	open, selected, reader := st.context, st.contextSelected, st.contextReader
	expanded := maps.Clone(st.contextOpen)
	a.uiMu.Unlock()
	if !open || !hasContext(rec) {
		return nil
	}
	c := &contextScreen{Selected: selected, Open: expanded, Worktree: rec.Worktree}
	if reader == nil {
		path, err := claudectx.Find(claudectx.ConfigDir(), rec.AgentSessionID)
		if err != nil {
			c.Problem = problemText(err)
			return c
		}
		reader = claudectx.NewReader(path)
		a.uiMu.Lock()
		st.contextReader = reader
		a.uiMu.Unlock()
	}
	data, err := reader.Read()
	if err != nil {
		c.Problem = problemText(err)
		return c
	}
	c.Data = data
	return c
}

func problemText(err error) string {
	if errors.Is(err, claudectx.ErrNoLog) || errors.Is(err, os.ErrNotExist) {
		return noContextLog
	}
	return err.Error()
}

func (a *app) contextAct(ev pluginsdk.UIEvent, sid string) error {
	if _, err := a.lookup(ev.WorkspaceID, sid); err != nil {
		return err
	}
	id := ""
	if ev.Action != actContext {
		var err error
		if id, err = itemID(ev); err != nil {
			return err
		}
	}
	st := a.sessionState(ev.WorkspaceID, sid)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.contextEvent(ev.Action, id)
	return nil
}
