package sdui

type Node struct {
	Type     string         `json:"type"`
	ID       string         `json:"id,omitempty"`
	Props    map[string]any `json:"props,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

type Item struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Icon   string `json:"icon,omitempty"`
	Badge  string `json:"badge,omitempty"`
}

type TreeItem struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Detail      string     `json:"detail,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Badge       string     `json:"badge,omitempty"`
	Muted       bool       `json:"muted,omitempty"`
	HasChildren bool       `json:"hasChildren,omitempty"`
	Expanded    bool       `json:"expanded,omitempty"`
	Children    []TreeItem `json:"children,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
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
