package sdui

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"clubmate/internal/id"
	"clubmate/pkg/sdui"
)

const (
	maxNodes     = 5000
	maxDepth     = 32
	maxActionLen = 64
)

var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]*$`)

var ErrInvalidTree = errors.New("invalid sdui tree")

type propCheck func(c *validator, v any) error

type childRule struct {
	min, max int
}

type spec struct {
	required    []string
	props       map[string]propCheck
	children    childRule
	interactive bool
}

var (
	anyChildren = childRule{0, -1}
	noChildren  = childRule{0, 0}

	directions = []string{"vertical", "horizontal"}
	textStyles = []string{"body", "muted", "title", "mono"}
	badgeTones = []string{"neutral", "accent", "success", "warning", "danger"}

	itemFields = map[string]propCheck{
		"id": uniqueItemID, "label": isString, "detail": isString, "icon": isString, "badge": isString,
	}
	itemRequired = []string{"id", "label"}

	catalog = map[string]spec{
		"stack":  {required: []string{"direction"}, props: map[string]propCheck{"direction": oneOf(directions)}, children: anyChildren},
		"split":  {required: []string{"direction"}, props: map[string]propCheck{"direction": oneOf(directions)}, children: childRule{2, 4}},
		"scroll": {children: childRule{1, 1}},
		"tabs": {
			required:    []string{"items", "selected"},
			props:       map[string]propCheck{"items": listOf(itemCheck), "selected": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"list": {
			required:    []string{"items"},
			props:       map[string]propCheck{"items": listOf(itemCheck), "selected": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"tree": {
			required:    []string{"items"},
			props:       map[string]propCheck{"items": listOf(treeItemCheck), "selected": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"text":     {required: []string{"text"}, props: map[string]propCheck{"text": isString, "style": oneOf(textStyles)}, children: noChildren},
		"badge":    {required: []string{"text"}, props: map[string]propCheck{"text": isString, "tone": oneOf(badgeTones)}, children: noChildren},
		"icon":     {required: []string{"name"}, props: map[string]propCheck{"name": isString}, children: noChildren},
		"diff":     {required: []string{"text"}, props: map[string]propCheck{"text": isString}, children: noChildren},
		"markdown": {required: []string{"text"}, props: map[string]propCheck{"text": isString}, children: noChildren},
		"button": {
			required:    []string{"label"},
			props:       map[string]propCheck{"label": isString, "icon": isString, "confirm": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"textField": {
			props:       map[string]propCheck{"value": isString, "placeholder": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"folderField": {
			required:    []string{"label"},
			props:       map[string]propCheck{"label": isString, "values": listOf(isString), "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"toggle": {required: []string{"value"}, props: map[string]propCheck{"value": isBool, "action": actionCheck}, children: noChildren, interactive: true},
		"select": {
			required:    []string{"options"},
			props:       map[string]propCheck{"options": listOf(optionCheck), "value": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"form":    {props: map[string]propCheck{"action": actionCheck}, children: anyChildren, interactive: true},
		"surface": {required: []string{"sessionId"}, props: map[string]propCheck{"sessionId": sessionIDCheck}, children: noChildren},
		"empty":   {required: []string{"text"}, props: map[string]propCheck{"text": isString}, children: noChildren},
	}
)

type validator struct {
	nodes   int
	depth   int
	ids     map[string]struct{}
	itemIDs map[string]struct{}
}

func Validate(n sdui.Node) error {
	v := &validator{ids: map[string]struct{}{}}
	return v.node(n, n.Type, 1)
}

func (v *validator) countNode() error {
	v.nodes++
	if v.nodes > maxNodes {
		return fmt.Errorf("more than %d nodes", maxNodes)
	}
	return nil
}

func (v *validator) node(n sdui.Node, path string, depth int) error {
	if depth > maxDepth {
		return invalid(path, "depth exceeds %d", maxDepth)
	}
	if err := v.countNode(); err != nil {
		return invalid(path, "%v", err)
	}
	v.depth = depth
	sp, ok := catalog[n.Type]
	if !ok {
		return invalid(path, "unknown type %q", n.Type)
	}
	if err := v.identity(n, sp, path); err != nil {
		return err
	}
	if err := v.checkProps(n, sp, path); err != nil {
		return err
	}
	if err := checkChildCount(n, sp, path); err != nil {
		return err
	}
	for i, c := range n.Children {
		if err := v.node(c, path+"["+strconv.Itoa(i)+"]."+c.Type, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (v *validator) identity(n sdui.Node, sp spec, path string) error {
	if n.ID == "" {
		if sp.interactive {
			return invalid(path, "missing id")
		}
		return nil
	}
	if _, dup := v.ids[n.ID]; dup {
		return invalid(path, "duplicate id %q", n.ID)
	}
	v.ids[n.ID] = struct{}{}
	return nil
}

func (v *validator) checkProps(n sdui.Node, sp spec, path string) error {
	props, err := normalize(n.Props)
	if err != nil {
		return invalid(path, "props: %v", err)
	}
	for _, name := range sp.required {
		if _, ok := props[name]; !ok {
			return invalid(path, "missing prop %s", name)
		}
	}
	for name, value := range props {
		check, ok := sp.props[name]
		if !ok {
			return invalid(path, "unknown prop %s", name)
		}
		v.itemIDs = map[string]struct{}{}
		if err := check(v, value); err != nil {
			return invalid(path, "prop %s: %v", name, err)
		}
	}
	return nil
}

func checkChildCount(n sdui.Node, sp spec, path string) error {
	count := len(n.Children)
	if count < sp.children.min || (sp.children.max >= 0 && count > sp.children.max) {
		return invalid(path, "invalid child count %d", count)
	}
	return nil
}

func normalize(props map[string]any) (map[string]any, error) {
	if len(props) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func invalid(path, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidTree, path, fmt.Sprintf(format, args...))
}

func isString(_ *validator, v any) error {
	if _, ok := v.(string); !ok {
		return errors.New("must be a string")
	}
	return nil
}

func isBool(_ *validator, v any) error {
	if _, ok := v.(bool); !ok {
		return errors.New("must be a boolean")
	}
	return nil
}

func actionCheck(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok || len(s) == 0 || len(s) > maxActionLen || !actionPattern.MatchString(s) {
		return fmt.Errorf("must match %s and be 1-%d characters", actionPattern, maxActionLen)
	}
	return nil
}

func oneOf(allowed []string) propCheck {
	return func(_ *validator, v any) error {
		s, ok := v.(string)
		if !ok || !slices.Contains(allowed, s) {
			return fmt.Errorf("must be one of %v", allowed)
		}
		return nil
	}
}

func sessionIDCheck(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok || !id.Valid("ses", s) {
		return errors.New("must be a valid session id")
	}
	return nil
}

func listOf(item propCheck) propCheck {
	return func(c *validator, v any) error {
		if v == nil {
			return nil
		}
		list, ok := v.([]any)
		if !ok {
			return errors.New("must be an array")
		}
		for i, e := range list {
			if err := c.countNode(); err != nil {
				return err
			}
			if err := item(c, e); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	}
}

func objectCheck(required []string, fields map[string]propCheck) propCheck {
	return func(c *validator, v any) error {
		obj, ok := v.(map[string]any)
		if !ok {
			return errors.New("must be an object")
		}
		for _, name := range required {
			if _, ok := obj[name]; !ok {
				return fmt.Errorf("missing field %s", name)
			}
		}
		for name, value := range obj {
			check, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown field %s", name)
			}
			if err := check(c, value); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	}
}

func uniqueItemID(c *validator, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("must be a string")
	}
	if s == "" {
		return errors.New("must not be empty")
	}
	if _, dup := c.itemIDs[s]; dup {
		return fmt.Errorf("duplicate item id %q", s)
	}
	c.itemIDs[s] = struct{}{}
	return nil
}

func itemCheck(c *validator, v any) error {
	return objectCheck(itemRequired, itemFields)(c, v)
}

func optionCheck(c *validator, v any) error {
	return objectCheck([]string{"value", "label"}, map[string]propCheck{"value": isString, "label": isString})(c, v)
}

func treeItemCheck(c *validator, v any) error {
	c.depth++
	defer func() { c.depth-- }()
	if c.depth > maxDepth {
		return fmt.Errorf("depth exceeds %d", maxDepth)
	}
	fields := map[string]propCheck{"muted": isBool, "hasChildren": isBool, "expanded": isBool, "children": listOf(treeItemCheck)}
	for k, f := range itemFields {
		fields[k] = f
	}
	return objectCheck(itemRequired, fields)(c, v)
}
