package sdui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

	maxTableRows      = 1000
	maxTableColumns   = 32
	maxSparkValues    = 900
	minSparkHeight    = 8
	maxSparkHeight    = 200
	maxTreeSparkTotal = 100_000
	minLines          = 2
	maxLines          = 50
)

var actionPattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9.-]*$`)

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
	prepare     func(c *validator, props map[string]any)
}

var (
	anyChildren = childRule{0, -1}
	noChildren  = childRule{0, 0}

	directions = []string{"vertical", "horizontal"}
	textStyles = []string{"body", "muted", "title", "mono"}
	badgeTones = []string{"neutral", "accent", "success", "warning", "danger"}

	itemFields = map[string]propCheck{
		"id": uniqueItemID, "label": isString, "detail": isString, "icon": isString, "badge": isString, "menu": listOf(menuEntryCheck),
	}
	itemRequired = []string{"id", "label"}

	messageRoles     = []string{"user", "agent", "thought"}
	planStatuses     = []string{"pending", "in_progress", "completed"}
	toolKinds        = []string{"read", "edit", "delete", "move", "search", "execute", "think", "fetch", "switch_mode", "other"}
	toolStatuses     = []string{"pending", "in_progress", "completed", "failed", "cancelled"}
	toolFormats      = []string{"diff", "text", "terminal"}
	permissionKinds  = []string{"allow_once", "allow_always", "reject_once", "reject_always"}
	attachmentKinds  = []string{"file", "selection"}
	attachmentFields = map[string]propCheck{"id": uniqueItemID, "kind": oneOf(attachmentKinds), "path": isString, "startLine": countNumber, "endLine": countNumber}
	attachmentsCheck = listOf(objectCheck([]string{"kind", "path"}, attachmentFields))

	columnAligns = []string{"left", "right"}
	cellTones    = []string{"", "ok", "warn", "danger"}

	anyNumber   = number(func(float64) bool { return true }, "a number")
	unitNumber  = number(func(n float64) bool { return n >= 0 && n <= 1 }, "a number within 0…1")
	nonNegative = number(func(n float64) bool { return n >= 0 }, "a non-negative number")
	positive    = number(func(n float64) bool { return n > 0 }, "a positive number")
	linesNumber = number(func(n float64) bool { return n >= minLines && n <= maxLines && n == math.Trunc(n) }, fmt.Sprintf("an integer within %d…%d", minLines, maxLines))
	countNumber = number(func(n float64) bool { return n >= 0 && n == math.Trunc(n) }, "a non-negative integer")
	depthNumber = number(func(n float64) bool { return n >= 0 && n <= maxDepth && n == math.Trunc(n) }, fmt.Sprintf("an integer within 0…%d", maxDepth))

	sparkValues = maxLen(maxSparkValues, sparkBudget(arrayOf(anyNumber)))
	sparkFields = map[string]propCheck{"values": sparkValues, "max": positive}
	sparkHeight = number(func(n float64) bool { return n >= minSparkHeight && n <= maxSparkHeight }, fmt.Sprintf("a number within %d…%d", minSparkHeight, maxSparkHeight))

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
			props:       map[string]propCheck{"items": listOf(treeItemCheck), "selected": isString, "breadcrumb": listOf(itemCheck), "review": reviewCheck, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"table": {
			required: []string{"columns", "rows"},
			props: map[string]propCheck{
				"columns":  maxLen(maxTableColumns, listOf(columnCheck)),
				"rows":     maxLen(maxTableRows, listOf(tableRowCheck)),
				"selected": isString,
				"sort":     sortCheck,
				"action":   actionCheck,
			},
			children:    noChildren,
			interactive: true,
			prepare:     func(c *validator, props map[string]any) { c.columns, _ = props["columns"].([]any) },
		},
		"sparkline": {
			required: []string{"values", "max"},
			props:    map[string]propCheck{"values": sparkValues, "max": positive, "height": sparkHeight},
			children: noChildren,
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
			props:       map[string]propCheck{"value": isString, "placeholder": isString, "lines": linesNumber, "action": actionCheck},
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

		"transcript": {children: anyChildren},
		"message": {
			required: []string{"role", "text"},
			props:    map[string]propCheck{"role": oneOf(messageRoles), "text": isString, "attachments": attachmentsCheck, "from": isString},
			children: noChildren,
		},
		"plan": {required: []string{"entries"}, props: map[string]propCheck{"entries": listOf(planEntryCheck)}, children: noChildren},
		"toolCard": {
			required: []string{"kind", "title"},
			props: map[string]propCheck{
				"kind": oneOf(toolKinds), "title": isString, "status": oneOf(toolStatuses), "body": toolBodyCheck,
				"truncated": isBool, "actions": listOf(cardActionCheck),
			},
			children:    noChildren,
			interactive: true,
		},
		"permissionCard": {
			required:    []string{"request", "text", "options"},
			props:       map[string]propCheck{"request": isString, "text": isString, "options": listOf(permissionOptionCheck), "outcome": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"composer": {
			props: map[string]propCheck{
				"placeholder": isString, "agent": isString, "modes": listOf(optionCheck), "mode": isString, "pickers": listOf(pickerCheck),
				"commands": listOf(commandCheck), "attach": listOf(oneOf(attachmentKinds)), "attachments": attachmentsCheck, "running": isBool, "steers": isBool,
			},
			children:    noChildren,
			interactive: true,
		},
	}
)

type validator struct {
	nodes   int
	depth   int
	sparks  int
	ids     map[string]struct{}
	itemIDs map[string]struct{}
	columns []any
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
	if sp.prepare != nil {
		sp.prepare(v, props)
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

func number(valid func(float64) bool, rule string) propCheck {
	return func(_ *validator, v any) error {
		n, ok := v.(float64)
		if !ok || !valid(n) {
			return fmt.Errorf("must be %s", rule)
		}
		return nil
	}
}

func listOf(item propCheck) propCheck {
	return arrayOf(func(c *validator, v any) error {
		if err := c.countNode(); err != nil {
			return err
		}
		return item(c, v)
	})
}

func arrayOf(item propCheck) propCheck {
	return func(c *validator, v any) error {
		if v == nil {
			return nil
		}
		list, ok := v.([]any)
		if !ok {
			return errors.New("must be an array")
		}
		for i, e := range list {
			if err := item(c, e); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
		return nil
	}
}

func maxLen(limit int, check propCheck) propCheck {
	return func(c *validator, v any) error {
		if list, ok := v.([]any); ok && len(list) > limit {
			return fmt.Errorf("more than %d entries", limit)
		}
		return check(c, v)
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

func menuEntryCheck(c *validator, v any) error {
	return objectCheck([]string{"action", "label"}, map[string]propCheck{"action": actionCheck, "label": isString, "confirm": isString})(c, v)
}

func planEntryCheck(c *validator, v any) error {
	return objectCheck([]string{"content", "status"}, map[string]propCheck{"content": isString, "status": oneOf(planStatuses)})(c, v)
}

func toolBodyCheck(c *validator, v any) error {
	return objectCheck([]string{"format", "text"}, map[string]propCheck{"format": oneOf(toolFormats), "text": isString})(c, v)
}

func cardActionCheck(c *validator, v any) error {
	return objectCheck([]string{"action", "label"}, map[string]propCheck{"action": actionCheck, "label": isString, "icon": isString, "value": payloadCheck})(c, v)
}

func permissionOptionCheck(c *validator, v any) error {
	return objectCheck([]string{"id", "label"}, map[string]propCheck{"id": uniqueItemID, "label": isString, "kind": oneOf(permissionKinds)})(c, v)
}

func pickerCheck(c *validator, v any) error {
	return objectCheck([]string{"id", "value", "options"}, map[string]propCheck{
		"id": isString, "title": isString, "help": isString, "value": isString, "options": listOf(optionCheck),
	})(c, v)
}

func commandCheck(c *validator, v any) error {
	return objectCheck([]string{"name"}, map[string]propCheck{"name": isString, "description": isString, "hint": isString})(c, v)
}

func payloadCheck(_ *validator, v any) error {
	if obj, ok := v.(map[string]any); ok {
		for name, field := range obj {
			if !isScalar(field) {
				return fmt.Errorf("field %s must be a string, number or boolean", name)
			}
		}
		return nil
	}
	if !isScalar(v) {
		return errors.New("must be a string, number, boolean or an object of those")
	}
	return nil
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, float64, bool:
		return true
	}
	return false
}

func treeItemCheck(c *validator, v any) error {
	c.depth++
	defer func() { c.depth-- }()
	if c.depth > maxDepth {
		return fmt.Errorf("depth exceeds %d", maxDepth)
	}
	fields := map[string]propCheck{
		"muted": isBool, "hasChildren": isBool, "expanded": isBool, "children": listOf(treeItemCheck),
		"additions": countNumber, "deletions": countNumber, "reviewed": isBool, "comments": countNumber,
		"tone": oneOf(badgeTones),
	}
	for k, f := range itemFields {
		fields[k] = f
	}
	return objectCheck(itemRequired, fields)(c, v)
}

func reviewCheck(c *validator, v any) error {
	return objectCheck([]string{"base", "head"}, map[string]propCheck{
		"base": isString, "head": isString, "baseRef": isString, "headRef": isString, "readOnly": isBool, "mode": isString,
		"progress": isString, "warning": isString, "error": isString, "chips": listOf(chipCheck), "started": countNumber,
	})(c, v)
}

func chipCheck(c *validator, v any) error {
	return objectCheck([]string{"id", "label"}, map[string]propCheck{"id": isString, "label": isString, "on": isBool})(c, v)
}

func columnCheck(c *validator, v any) error {
	return objectCheck([]string{"id", "title"}, map[string]propCheck{
		"id": uniqueItemID, "title": isString, "align": oneOf(columnAligns), "width": nonNegative,
	})(c, v)
}

func tableRowCheck(c *validator, v any) error {
	return objectCheck([]string{"id", "cells"}, map[string]propCheck{
		"id": uniqueItemID, "depth": depthNumber, "hasChildren": isBool, "expanded": isBool, "muted": isBool, "cells": cellsCheck,
	})(c, v)
}

func cellCheck(c *validator, v any) error {
	return objectCheck(nil, map[string]propCheck{
		"text": isString, "bar": unitNumber, "tone": oneOf(cellTones), "spark": objectCheck([]string{"values", "max"}, sparkFields),
	})(c, v)
}

func cellsCheck(c *validator, v any) error {
	cells, _ := v.([]any)
	if len(cells) != len(c.columns) {
		return fmt.Errorf("%d cells for %d columns", len(cells), len(c.columns))
	}
	return arrayOf(cellCheck)(c, v)
}

func sortCheck(c *validator, v any) error {
	return objectCheck([]string{"column"}, map[string]propCheck{"column": columnID, "desc": isBool})(c, v)
}

func columnID(c *validator, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("must be a string")
	}
	for _, col := range c.columns {
		if fields, _ := col.(map[string]any); fields["id"] == s {
			return nil
		}
	}
	return fmt.Errorf("unknown column %q", s)
}

func sparkBudget(check propCheck) propCheck {
	return func(c *validator, v any) error {
		list, _ := v.([]any)
		c.sparks += len(list)
		if c.sparks > maxTreeSparkTotal {
			return fmt.Errorf("more than %d spark values in the tree", maxTreeSparkTotal)
		}
		return check(c, v)
	}
}
