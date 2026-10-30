package sdui

type Node struct {
	Type     string         `json:"type"`
	ID       string         `json:"id,omitempty"`
	Props    map[string]any `json:"props,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

type Item struct {
	ID     string      `json:"id"`
	Label  string      `json:"label"`
	Detail string      `json:"detail,omitempty"`
	Icon   string      `json:"icon,omitempty"`
	Badge  string      `json:"badge,omitempty"`
	Menu   []MenuEntry `json:"menu,omitempty"`
}

type MenuEntry struct {
	Action  string `json:"action"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
}

type TreeItem struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Detail      string      `json:"detail,omitempty"`
	Icon        string      `json:"icon,omitempty"`
	Tone        string      `json:"tone,omitempty"`
	Badge       string      `json:"badge,omitempty"`
	Menu        []MenuEntry `json:"menu,omitempty"`
	Muted       bool        `json:"muted,omitempty"`
	HasChildren bool        `json:"hasChildren,omitempty"`
	Expanded    bool        `json:"expanded,omitempty"`
	Additions   int         `json:"additions,omitempty"`
	Deletions   int         `json:"deletions,omitempty"`
	Reviewed    bool        `json:"reviewed,omitempty"`
	Comments    int         `json:"comments,omitempty"`
	Children    []TreeItem  `json:"children,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Column struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Align string  `json:"align,omitempty"`
	Width float64 `json:"width,omitempty"`
}

type TableRow struct {
	ID          string `json:"id"`
	Depth       int    `json:"depth,omitempty"`
	HasChildren bool   `json:"hasChildren,omitempty"`
	Expanded    bool   `json:"expanded,omitempty"`
	Muted       bool   `json:"muted,omitempty"`
	Cells       []Cell `json:"cells"`
}

type Cell struct {
	Text  string   `json:"text,omitempty"`
	Bar   *float64 `json:"bar,omitempty"`
	Tone  string   `json:"tone,omitempty"`
	Spark *Spark   `json:"spark,omitempty"`
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
	Kind      string
	Title     string
	Status    string
	Body      *ToolBody
	Truncated bool
	Actions   []CardAction
}

type PermissionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
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
}

type ComposerProps struct {
	Placeholder string
	Agent       string
	Modes       []Option
	Mode        string
	Pickers     []Picker
	Commands    []Command
	Attach      []string
	Attachments []Attachment
	Running     bool
	Steers      bool
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

func BreadcrumbTree(id string, items []TreeItem, selected string, breadcrumb []Item) Node {
	n := Tree(id, items, selected)
	if len(breadcrumb) > 0 {
		n.Props["breadcrumb"] = breadcrumb
	}
	return n
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

func Text(text string) Node {
	return Node{Type: "text", Props: map[string]any{"text": text}}
}

func MutedText(text string) Node {
	return Node{Type: "text", Props: map[string]any{"text": text, "style": "muted"}}
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

func Select(id string, options []Option, value string) Node {
	return Node{Type: "select", ID: id, Props: withOptional(map[string]any{"options": nonNil(options)}, "value", value)}
}

func Form(id string, children ...Node) Node {
	return Node{Type: "form", ID: id, Children: children}
}

func Surface(sessionID string) Node {
	return Node{Type: "surface", Props: map[string]any{"sessionId": sessionID}}
}

func Empty(text string) Node {
	return Node{Type: "empty", Props: map[string]any{"text": text}}
}

func Transcript(id string, children ...Node) Node {
	return Node{Type: "transcript", ID: id, Children: children}
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
	if len(p.Actions) > 0 {
		props["actions"] = p.Actions
	}
	return Node{Type: "toolCard", ID: id, Props: props}
}

func PermissionCard(id, request, text string, options []PermissionOption, outcome string) Node {
	props := map[string]any{"request": request, "text": text, "options": nonNil(options)}
	return Node{Type: "permissionCard", ID: id, Props: withOptional(props, "outcome", outcome)}
}

func Composer(id string, p ComposerProps) Node {
	props := withOptional(map[string]any{}, "placeholder", p.Placeholder)
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
	return Node{Type: "composer", ID: id, Props: props}
}

func WithAction(n Node, action string) Node {
	props := make(map[string]any, len(n.Props)+1)
	for k, v := range n.Props {
		props[k] = v
	}
	props["action"] = action
	n.Props = props
	return n
}

func WithConfirm(n Node, text string) Node {
	props := make(map[string]any, len(n.Props)+1)
	for k, v := range n.Props {
		props[k] = v
	}
	props["confirm"] = text
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
