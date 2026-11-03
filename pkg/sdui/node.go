package sdui

import "time"

type Node struct {
	Type     string         `json:"type"`
	ID       string         `json:"id,omitempty"`
	Props    map[string]any `json:"props,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

type Item struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Detail   string      `json:"detail,omitempty"`
	Icon     string      `json:"icon,omitempty"`
	Badge    string      `json:"badge,omitempty"`
	Tone     string      `json:"tone,omitempty"`
	Menu     []MenuEntry `json:"menu,omitempty"`
	Truncate string      `json:"truncate,omitempty"`
}

type MenuEntry struct {
	Action  string `json:"action"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
	Key     string `json:"key,omitempty"`
}

type Changes struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

type TreeItem struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Detail      string        `json:"detail,omitempty"`
	Icon        string        `json:"icon,omitempty"`
	Tone        string        `json:"tone,omitempty"`
	DetailTone  string        `json:"detailTone,omitempty"`
	Badge       string        `json:"badge,omitempty"`
	Menu        []MenuEntry   `json:"menu,omitempty"`
	Muted       bool          `json:"muted,omitempty"`
	Faint       bool          `json:"faint,omitempty"`
	HasChildren bool          `json:"hasChildren,omitempty"`
	Expanded    bool          `json:"expanded,omitempty"`
	Children    []TreeItem    `json:"children,omitempty"`
	Truncate    string        `json:"truncate,omitempty"`
	Badges      []HeaderBadge `json:"badges,omitempty"`
	Changes     *Changes      `json:"changes,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Column struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Align    string  `json:"align,omitempty"`
	Width    float64 `json:"width,omitempty"`
	Truncate string  `json:"truncate,omitempty"`
	Priority int     `json:"priority,omitempty"`
}

type TableRow struct {
	ID          string `json:"id"`
	Depth       int    `json:"depth,omitempty"`
	HasChildren bool   `json:"hasChildren,omitempty"`
	Expanded    bool   `json:"expanded,omitempty"`
	Muted       bool   `json:"muted,omitempty"`
	Group       bool   `json:"group,omitempty"`
	Cells       []Cell `json:"cells"`
}

type Cell struct {
	Text   string   `json:"text,omitempty"`
	Bar    *float64 `json:"bar,omitempty"`
	Tone   string   `json:"tone,omitempty"`
	Spark  *Spark   `json:"spark,omitempty"`
	Since  int64    `json:"since,omitempty"`
	Action string   `json:"action,omitempty"`
	Label  string   `json:"label,omitempty"`
}

type Spark struct {
	Values []float64 `json:"values"`
	Max    float64   `json:"max"`
}

type Sort struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc,omitempty"`
}

type Attachment struct {
	ID        string `json:"id,omitempty"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
}

type DocCard struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Rel       string `json:"rel"`
	Presented bool   `json:"presented,omitempty"`
}

type ImageRef struct {
	Path   string `json:"path"`
	Rel    string `json:"rel"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type PlanEntry struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type ToolBody struct {
	Format string `json:"format"`
	Text   string `json:"text"`
}

type CardAction struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Icon   string `json:"icon,omitempty"`
	Value  any    `json:"value,omitempty"`
}

type ToolCardProps struct {
	Kind       string
	Title      string
	Status     string
	Body       *ToolBody
	Truncated  bool
	Standalone bool
	Actions    []CardAction
}

type PermissionOption struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind,omitempty"`
	Shortcut  string `json:"shortcut,omitempty"`
	Menu      bool   `json:"menu,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type PermissionCardProps struct {
	Text    string
	Title   string
	Command string
	Caption string
	Options []PermissionOption
	Outcome string
}

type Command struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

type Picker struct {
	ID      string   `json:"id"`
	Title   string   `json:"title,omitempty"`
	Help    string   `json:"help,omitempty"`
	Value   string   `json:"value"`
	Options []Option `json:"options"`
	Toggle  bool     `json:"toggle,omitempty"`
}

type ComposerProps struct {
	Placeholder string
	Hint        string
	Agent       string
	Modes       []Option
	Mode        string
	Pickers     []Picker
	Commands    []Command
	Attach      []string
	Attachments []Attachment
	Running     bool
	Steers      bool
	Docked      bool
	Disabled    bool
}

func VStack(children ...Node) Node {
	return Node{Type: "stack", Props: map[string]any{"direction": "vertical"}, Children: children}
}

func HStack(children ...Node) Node {
	return Node{Type: "stack", Props: map[string]any{"direction": "horizontal"}, Children: children}
}

func Split(direction string, children ...Node) Node {
	return Node{Type: "split", Props: map[string]any{"direction": direction}, Children: children}
}

func Scroll(child Node) Node {
	return Node{Type: "scroll", Children: []Node{child}}
}

func Tabs(id string, items []Item, selected string) Node {
	return Node{Type: "tabs", ID: id, Props: map[string]any{"items": nonNil(items), "selected": selected}}
}

func List(id string, items []Item, selected string) Node {
	return Node{Type: "list", ID: id, Props: withOptional(map[string]any{"items": nonNil(items)}, "selected", selected)}
}

func Tree(id string, items []TreeItem, selected string) Node {
	return Node{Type: "tree", ID: id, Props: withOptional(map[string]any{"items": nonNil(items)}, "selected", selected)}
}

func Table(id string, columns []Column, rows []TableRow, selected string, sort Sort) Node {
	props := withOptional(map[string]any{"columns": nonNil(columns), "rows": nonNil(rows)}, "selected", selected)
	if sort.Column != "" {
		props["sort"] = sort
	}
	return Node{Type: "table", ID: id, Props: props}
}

func Sparkline(id string, values []float64, maxValue float64) Node {
	return Node{Type: "sparkline", ID: id, Props: map[string]any{"values": clamp(values, maxValue), "max": maxValue}}
}

func NewSpark(values []float64, maxValue float64) *Spark {
	return &Spark{Values: clamp(values, maxValue), Max: maxValue}
}

func SparklineSized(id string, values []float64, maxValue, height float64) Node {
	n := Sparkline(id, values, maxValue)
	n.Props["height"] = height
	return n
}

func SettingRow(label, hint string, control Node) Node {
	text := []Node{Text(label)}
	if hint != "" {
		text = append(text, MutedText(hint))
	}
	return HStack(VStack(text...), control)
}

func Text(text string) Node {
	return Node{Type: "text", Props: map[string]any{"text": text}}
}

func MutedText(text string) Node {
	return Node{Type: "text", Props: map[string]any{"text": text, "style": "muted"}}
}

func MonoText(text string) Node {
	return Node{Type: "text", Props: map[string]any{"text": text, "style": "mono"}}
}

func BlockText(text string) Node {
	return Node{Type: "text", Props: map[string]any{"text": text, "style": "block"}}
}

func Badge(text, tone string) Node {
	return Node{Type: "badge", Props: withOptional(map[string]any{"text": text}, "tone", tone)}
}

func Icon(name string) Node {
	return Node{Type: "icon", Props: map[string]any{"name": name}}
}

func Diff(text string) Node {
	return Node{Type: "diff", Props: map[string]any{"text": text}}
}

func Markdown(text string) Node {
	return Node{Type: "markdown", Props: map[string]any{"text": text}}
}

const (
	SizeFill = "fill"
	SizeFit  = "fit"
)

const (
	DocumentMarkdown = "markdown"
	DocumentArtifact = "artifact"
)

type DocumentProps struct {
	HTML   string
	Path   string
	Rev    int
	Size   string
	Anchor string
	Kind   string
}

func Document(id string, p DocumentProps) Node {
	props := withOptional(withOptional(withOptional(withOptional(map[string]any{"html": p.HTML}, "path", p.Path), "size", p.Size), "anchor", p.Anchor), "kind", p.Kind)
	if p.Rev != 0 {
		props["rev"] = p.Rev
	}
	return Node{Type: "document", ID: id, Props: props}
}

type GraphNode struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	State     string     `json:"state"`
	Tone      string     `json:"tone,omitempty"`
	Icon      string     `json:"icon,omitempty"`
	Attention bool       `json:"attention,omitempty"`
	Pending   bool       `json:"pending,omitempty"`
	Start     bool       `json:"start,omitempty"`
	Tooltip   string     `json:"tooltip,omitempty"`
	Kind      string     `json:"kind,omitempty"`
	Command   string     `json:"command,omitempty"`
	Since     string     `json:"since,omitempty"`
	Timeout   int        `json:"timeout,omitempty"`
	Tag       string     `json:"tag,omitempty"`
	Outs      []GraphOut `json:"outs"`
}

type GraphOut struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count,omitempty"`
	State string `json:"state"`
	End   bool   `json:"end,omitempty"`
}

type GraphEdge struct {
	From    string `json:"from"`
	Out     string `json:"out"`
	To      string `json:"to"`
	State   string `json:"state"`
	Ref     string `json:"ref,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Label   string `json:"label,omitempty"`
}

type GraphMove struct {
	Node    string `json:"node"`
	Cause   int    `json:"cause"`
	Label   string `json:"label,omitempty"`
	Tooltip string `json:"tooltip"`
	Out     string `json:"out,omitempty"`
	Current bool   `json:"current,omitempty"`
	User    bool   `json:"user,omitempty"`
	Last    bool   `json:"last,omitempty"`
	End     string `json:"end,omitempty"`
	Bar     string `json:"bar,omitempty"`
	Tone    string `json:"tone,omitempty"`
	Round   int    `json:"round,omitempty"`
}

type GraphProps struct {
	Nodes   []GraphNode
	Edges   []GraphEdge
	Moves   []GraphMove
	Reached bool
}

func Graph(id string, p GraphProps, children ...Node) Node {
	nodes := make([]GraphNode, len(p.Nodes))
	for i, n := range p.Nodes {
		n.Outs = nonNil(n.Outs)
		nodes[i] = n
	}
	props := map[string]any{"nodes": nodes, "edges": nonNil(p.Edges)}
	if len(p.Moves) > 0 {
		props["moves"] = p.Moves
	}
	if p.Reached {
		props["reached"] = true
	}
	return Node{Type: "graph", ID: id, Props: props, Children: children}
}

const (
	EdgeTaken    = "taken"
	EdgeLast     = "last"
	EdgeNever    = "never"
	EdgePlain    = "plain"
	EdgeSelected = "selected"
	EdgeDim      = "dim"
	EdgeGhost    = "ghost"
	EdgeHeld     = "held"
)

const (
	KindAgent  = "agent"
	KindCheck  = "check"
	KindAction = "action"
)

const (
	MarkerStart    = "start"
	MarkerEnd      = "end"
	MarkerSchedule = "schedule"
	MarkerInbox    = "inbox"
	MarkerSession  = "session"
	MarkerRuns     = "runs"
)

type FlowCard struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	State     string    `json:"state,omitempty"`
	Tone      string    `json:"tone,omitempty"`
	Icon      string    `json:"icon,omitempty"`
	Attention bool      `json:"attention,omitempty"`
	Pending   bool      `json:"pending,omitempty"`
	Ghost     bool      `json:"ghost,omitempty"`
	Tooltip   string    `json:"tooltip,omitempty"`
	Outs      []FlowOut `json:"outs"`
}

type FlowOut struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count,omitempty"`
	State string `json:"state"`
}

type FlowMarker struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Reached bool   `json:"reached,omitempty"`
}

type FlowEdge struct {
	From    string `json:"from"`
	Out     string `json:"out,omitempty"`
	To      string `json:"to"`
	State   string `json:"state"`
	Ref     string `json:"ref,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Label   string `json:"label,omitempty"`
}

type FlowProps struct {
	Cards   []FlowCard
	Markers []FlowMarker
	Edges   []FlowEdge
}

func Flow(id string, p FlowProps) Node {
	cards := make([]FlowCard, len(p.Cards))
	for i, c := range p.Cards {
		c.Outs = nonNil(c.Outs)
		cards[i] = c
	}
	props := map[string]any{"cards": cards, "edges": nonNil(p.Edges)}
	if len(p.Markers) > 0 {
		props["markers"] = p.Markers
	}
	return Node{Type: "flow", ID: id, Props: props}
}

type LaneActor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

type LaneMove struct {
	Actor   string `json:"actor"`
	Cause   int    `json:"cause"`
	Label   string `json:"label,omitempty"`
	Tooltip string `json:"tooltip"`
	Current bool   `json:"current,omitempty"`
	Last    bool   `json:"last,omitempty"`
	User    bool   `json:"user,omitempty"`
	Tone    string `json:"tone,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

type LanesProps struct {
	Actors    []LaneActor
	Moves     []LaneMove
	Title     string
	Collapsed bool
	Count     int
}

func Lanes(id string, p LanesProps) Node {
	props := withOptional(map[string]any{"actors": nonNil(p.Actors), "moves": nonNil(p.Moves)}, "title", p.Title)
	if p.Collapsed {
		props["collapsed"] = true
	}
	if p.Count > 0 {
		props["count"] = p.Count
	}
	return Node{Type: "lanes", ID: id, Props: props}
}

const (
	RunOK      = "ok"
	RunFailed  = "failed"
	RunMissed  = "missed"
	RunRunning = "running"
	RunIgnored = "ignored"
)

type StripRun struct {
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Tooltip string    `json:"tooltip,omitempty"`
	Ref     string    `json:"ref,omitempty"`
}

type PausedSpan struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to,omitzero"`
}

type RunstripProps struct {
	Runs     []StripRun
	Now      time.Time
	Next     time.Time
	Paused   []PausedSpan
	Selected string
	Title    string
	Count    int
}

func Runstrip(id string, p RunstripProps) Node {
	props := withOptional(map[string]any{"runs": nonNil(p.Runs)}, "selected", p.Selected)
	for key, t := range map[string]time.Time{"now": p.Now, "next": p.Next} {
		if !t.IsZero() {
			props[key] = t.UTC().Format(time.RFC3339)
		}
	}
	if len(p.Paused) > 0 {
		props["paused"] = p.Paused
	}
	props = withOptional(props, "title", p.Title)
	if p.Count > 0 {
		props["count"] = p.Count
	}
	return Node{Type: "runstrip", ID: id, Props: props}
}

func Button(id, label, icon string) Node {
	return Node{Type: "button", ID: id, Props: withOptional(map[string]any{"label": label}, "icon", icon)}
}

func TextField(id, value, placeholder string) Node {
	props := withOptional(map[string]any{}, "value", value)
	return Node{Type: "textField", ID: id, Props: withOptional(props, "placeholder", placeholder)}
}

func FolderField(id, label string, values []string) Node {
	return Node{Type: "folderField", ID: id, Props: map[string]any{"label": label, "values": nonNil(values)}}
}

func Toggle(id string, value bool) Node {
	return Node{Type: "toggle", ID: id, Props: map[string]any{"value": value}}
}

func Checkbox(id, label string, value bool) Node {
	return Node{Type: "toggle", ID: id, Props: map[string]any{"value": value, "label": label, "style": "checkbox"}}
}

func Progress(id string, value, total int, tone string) Node {
	return Node{Type: "progress", ID: id, Props: withOptional(map[string]any{"value": value, "total": total}, "tone", tone)}
}

func Select(id string, options []Option, value string) Node {
	return Node{Type: "select", ID: id, Props: withOptional(map[string]any{"options": nonNil(options)}, "value", value)}
}

func Form(id string, children ...Node) Node {
	return Node{Type: "form", ID: id, Children: children}
}

func Surface(sessionID string) Node {
	return Node{Type: "surface", Props: map[string]any{"sessionId": sessionID}}
}

func WebView(key, url string, rev int) Node {
	props := map[string]any{"key": key, "url": url}
	if rev != 0 {
		props["rev"] = rev
	}
	return Node{Type: "webview", Props: props}
}

func Empty(text string) Node {
	return Node{Type: "empty", Props: map[string]any{"text": text}}
}

type HeaderBadge struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

type HeaderProps struct {
	Icon   string
	Meta   string
	Badges []HeaderBadge
	Status string
	Agent  string
	Branch string
	Mode   string
	Field  string
	Error  string
}

func Header(title string, p HeaderProps, actions ...Node) Node {
	props := withOptional(withOptional(map[string]any{"title": title}, "icon", p.Icon), "meta", p.Meta)
	for _, kv := range [][2]string{{"status", p.Status}, {"agent", p.Agent}, {"branch", p.Branch}, {"mode", p.Mode}, {"field", p.Field}, {"error", p.Error}} {
		props = withOptional(props, kv[0], kv[1])
	}
	if len(p.Badges) > 0 {
		props["badges"] = p.Badges
	}
	return Node{Type: "header", Props: props, Children: actions}
}

type EmptyProps struct {
	Title  string
	Icon   string
	Hint   string
	Action *Node
	Center bool
}

func EmptyState(text string, p EmptyProps) Node {
	props := withOptional(withOptional(withOptional(map[string]any{"text": text}, "title", p.Title), "icon", p.Icon), "hint", p.Hint)
	if p.Center {
		props["center"] = true
	}
	n := Node{Type: "empty", Props: props}
	if p.Action != nil {
		n.Children = []Node{*p.Action}
	}
	return n
}

func Transcript(id string, children ...Node) Node {
	return Node{Type: "transcript", ID: id, Children: children}
}

func CenteredTranscript(id string, children ...Node) Node {
	return Node{Type: "transcript", ID: id, Props: map[string]any{"center": true}, Children: children}
}

func Message(role, text string, attachments []Attachment) Node {
	props := map[string]any{"role": role, "text": text}
	if len(attachments) > 0 {
		props["attachments"] = attachments
	}
	return Node{Type: "message", Props: props}
}

func Plan(entries []PlanEntry) Node {
	return Node{Type: "plan", Props: map[string]any{"entries": nonNil(entries)}}
}

func ToolCard(id string, p ToolCardProps) Node {
	props := withOptional(map[string]any{"kind": p.Kind, "title": p.Title}, "status", p.Status)
	if p.Body != nil {
		props["body"] = *p.Body
	}
	if p.Truncated {
		props["truncated"] = true
	}
	if p.Standalone {
		props["standalone"] = true
	}
	if len(p.Actions) > 0 {
		props["actions"] = p.Actions
	}
	return Node{Type: "toolCard", ID: id, Props: props}
}

func Card(tone, icon string, children ...Node) Node {
	props := withOptional(map[string]any{}, "tone", tone)
	return Node{Type: "card", Props: withOptional(props, "icon", icon), Children: children}
}

type SheetProps struct {
	Title  string
	Icon   string
	Tone   string
	Cancel string
}

func Sheet(id string, p SheetProps, children ...Node) Node {
	props := withOptional(withOptional(map[string]any{"title": p.Title, "cancel": p.Cancel}, "icon", p.Icon), "tone", p.Tone)
	return Node{Type: "sheet", ID: id, Props: props, Children: children}
}

func PermissionCard(id, request string, p PermissionCardProps) Node {
	props := map[string]any{"request": request, "text": p.Text, "options": nonNil(p.Options)}
	for _, kv := range [][2]string{{"title", p.Title}, {"command", p.Command}, {"caption", p.Caption}, {"outcome", p.Outcome}} {
		props = withOptional(props, kv[0], kv[1])
	}
	return Node{Type: "permissionCard", ID: id, Props: props}
}

func Composer(id string, p ComposerProps) Node {
	props := withOptional(map[string]any{}, "placeholder", p.Placeholder)
	props = withOptional(props, "hint", p.Hint)
	props = withOptional(props, "agent", p.Agent)
	props = withOptional(props, "mode", p.Mode)
	if len(p.Modes) > 0 {
		props["modes"] = p.Modes
	}
	if len(p.Pickers) > 0 {
		props["pickers"] = p.Pickers
	}
	if len(p.Commands) > 0 {
		props["commands"] = p.Commands
	}
	if len(p.Attach) > 0 {
		props["attach"] = p.Attach
	}
	if len(p.Attachments) > 0 {
		props["attachments"] = p.Attachments
	}
	if p.Running {
		props["running"] = true
	}
	if p.Steers {
		props["steers"] = true
	}
	if p.Docked {
		props["docked"] = true
	}
	if p.Disabled {
		props["disabled"] = true
	}
	return Node{Type: "composer", ID: id, Props: props}
}

func WithLines(n Node, lines int) Node {
	props := make(map[string]any, len(n.Props)+1)
	for k, v := range n.Props {
		props[k] = v
	}
	props["lines"] = lines
	n.Props = props
	return n
}

func WithAction(n Node, action string) Node {
	return withProp(n, "action", action)
}

func WithConfirm(n Node, text string) Node {
	return withProp(n, "confirm", text)
}

const (
	VariantPrimary   = "primary"
	VariantSecondary = "secondary"
	VariantGhost     = "ghost"
	VariantDanger    = "danger"
)

func WithVariant(n Node, variant string) Node {
	return withProp(n, "variant", variant)
}

func WithShortcut(n Node, combo string) Node {
	return withProp(n, "shortcut", combo)
}

func WithOverflow(n Node) Node {
	return withProp(n, "overflow", true)
}

func WithTone(n Node, tone string) Node {
	return withProp(n, "tone", tone)
}

func WithIconOnly(n Node) Node {
	return withProp(n, "iconOnly", true)
}

func WithDisabled(n Node) Node {
	return withProp(n, "disabled", true)
}

func WithLoading(n Node, verb string) Node {
	return withProp(n, "loading", verb)
}

func WithError(n Node, caption string) Node {
	return withProp(n, "error", caption)
}

func WithAutosave(n Node) Node {
	return withProp(n, "autosave", true)
}

func WithLive(n Node) Node {
	return withProp(n, "live", true)
}

func WithCopy(n Node, text string) Node {
	return withProp(n, "copy", text)
}

func WithPopover(n Node, form Node) Node {
	n.Children = []Node{form}
	return n
}

func WithDrop(n Node) Node {
	return withProp(n, "drop", true)
}

func WithAnchor(n Node, id string) Node {
	return withProp(n, "anchor", id)
}

func WithAnchorRev(n Node, rev int) Node {
	return withProp(n, "anchorRev", rev)
}

func withProp(n Node, key string, value any) Node {
	props := make(map[string]any, len(n.Props)+1)
	for k, v := range n.Props {
		props[k] = v
	}
	props[key] = value
	n.Props = props
	return n
}
func clamp(values []float64, maxValue float64) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = min(max(v, 0), maxValue)
	}
	return out
}

func withOptional(props map[string]any, key, value string) map[string]any {
	if value != "" {
		props[key] = value
	}
	return props
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
