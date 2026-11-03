package sdui

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/id"
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

	maxGraphNodes = 16
	maxFlowCards  = 8
	maxGraphEdges = 32
	maxGraphMoves = 1000

	maxFlowMarkers = 8
	maxLaneActors  = 8
	maxStripRuns   = 200
)

var (
	actionPattern   = regexp.MustCompile(`^[a-z][a-zA-Z0-9.-]*$`)
	menuKeyPattern  = regexp.MustCompile("^[a-z0-9]$")
	shortcutPattern = regexp.MustCompile("^((cmd|ctrl|opt|shift)\\+){0,4}(return|tab|space|delete|esc|left|right|down|up|[a-z0-9=\\-\\[\\]';\\\\,/.`])$")
)

var ErrInvalidTree = errors.New("invalid sdui tree")

type propCheck func(c *validator, v any) error

type childRule struct {
	min, max int
	only     []string
	once     []string
}

type spec struct {
	required    []string
	props       map[string]propCheck
	children    childRule
	interactive bool
	prepare     func(c *validator, props map[string]any)
	check       func(c *validator, n sdui.Node) error
}

var (
	anyChildren = childRule{min: 0, max: -1}
	noChildren  = childRule{min: 0, max: 0}

	directions   = []string{"vertical", "horizontal"}
	textStyles   = []string{"body", "muted", "title", "mono", "block"}
	variants     = []string{"primary", "secondary", "ghost", "danger"}
	toggleStyles = []string{"switch", "checkbox"}

	reservedKeys = []string{"esc", "return", "tab"}
	badgeTones   = []string{"neutral", "accent", "success", "warning", "danger"}

	itemFields = map[string]propCheck{
		"id": uniqueItemID, "label": isString, "detail": isString, "icon": isString, "badge": isString, "menu": listOf(menuEntryCheck),
		"truncate": oneOf(truncates), "tone": oneOf(badgeTones),
	}
	itemRequired = []string{"id", "label"}

	messageRoles     = []string{"user", "agent", "thought"}
	sessionStatuses  = []string{"running", "waiting", "asks", "review", "done", "idle", "error", "closed", "childrenRunning", "childrenWaiting"}
	planStatuses     = []string{"pending", "in_progress", "completed"}
	toolKinds        = []string{"read", "edit", "delete", "move", "search", "execute", "think", "agent", "fetch", "switch_mode", "other"}
	toolStatuses     = []string{"pending", "in_progress", "completed", "failed", "cancelled"}
	toolFormats      = []string{"diff", "text", "terminal"}
	permissionKinds  = []string{"allow_once", "allow_always", "reject_once", "reject_always"}
	attachSources    = []string{"file", "selection"}
	attachmentKinds  = []string{"file", "selection", "image"}
	attachmentFields = map[string]propCheck{"id": uniqueItemID, "kind": oneOf(attachmentKinds), "path": isString, "startLine": countNumber, "endLine": countNumber}
	attachmentsCheck = listOf(objectCheck([]string{"kind", "path"}, attachmentFields))

	docCardKinds  = []string{"markdown", "mermaid", "artifact"}
	docCardFields = map[string]propCheck{"path": nonEmptyString, "kind": oneOf(docCardKinds), "title": isString, "rel": isString, "presented": isBool}
	docCardsCheck = listOf(objectCheck([]string{"path", "kind", "title", "rel"}, docCardFields))

	imageRefFields = map[string]propCheck{"path": nonEmptyString, "rel": isString, "width": countNumber, "height": countNumber}
	imageRefsCheck = listOf(objectCheck([]string{"path", "rel"}, imageRefFields))

	documentSizes = []string{"fill", "fit"}
	documentKinds = []string{"markdown", "artifact"}

	columnAligns = []string{"left", "right"}
	truncates    = []string{"tail", "middle", "head"}
	cellTones    = []string{"", "ok", "warn", "danger"}

	anyNumber      = number(func(float64) bool { return true }, "a number")
	unitNumber     = number(func(n float64) bool { return n >= 0 && n <= 1 }, "a number within 0…1")
	nonNegative    = number(func(n float64) bool { return n >= 0 }, "a non-negative number")
	positive       = number(func(n float64) bool { return n > 0 }, "a positive number")
	linesNumber    = number(func(n float64) bool { return n >= minLines && n <= maxLines && n == math.Trunc(n) }, fmt.Sprintf("an integer within %d…%d", minLines, maxLines))
	countNumber    = number(func(n float64) bool { return n >= 0 && n == math.Trunc(n) }, "a non-negative integer")
	priorityNumber = number(func(n float64) bool { return n >= 0 && n <= maxTableColumns && n == math.Trunc(n) }, fmt.Sprintf("an integer within 0…%d", maxTableColumns))
	depthNumber    = number(func(n float64) bool { return n >= 0 && n <= maxDepth && n == math.Trunc(n) }, fmt.Sprintf("an integer within 0…%d", maxDepth))
	causeNumber    = number(func(n float64) bool { return n >= -1 && n == math.Trunc(n) }, "an integer ≥ -1")

	graphStates = []string{"taken", "last", "never", "plain"}
	graphKinds  = []string{"agent", "check", "action"}
	edgeStates  = []string{"taken", "last", "never", "plain", "held"}
	graphOut    = objectCheck([]string{"id", "label", "state"}, map[string]propCheck{
		"id": nonEmptyString, "label": isString, "count": countNumber, "state": oneOf(graphStates), "end": isBool,
	})
	graphNode = objectCheck([]string{"id", "label", "state", "outs"}, map[string]propCheck{
		"id": uniqueItemID, "label": isString, "state": isString, "tone": oneOf(badgeTones), "icon": isString,
		"attention": isBool, "pending": isBool, "start": isBool, "tooltip": isString, "outs": listOf(graphOut),
		"kind": oneOf(graphKinds), "command": isString, "since": timeString, "timeout": countNumber, "tag": isString,
	})
	graphEdge = objectCheck([]string{"from", "out", "to", "state"}, map[string]propCheck{
		"from": nonEmptyString, "out": nonEmptyString, "to": nonEmptyString, "state": oneOf(edgeStates), "ref": nonEmptyString, "tooltip": isString, "label": isString,
	})

	flowStates = []string{"taken", "last", "never", "plain", "selected", "dim", "ghost"}
	flowOut    = objectCheck([]string{"id", "label", "state"}, map[string]propCheck{
		"id": nonEmptyString, "label": isString, "count": countNumber, "state": oneOf(flowStates),
	})
	flowCard = objectCheck([]string{"id", "label", "outs"}, map[string]propCheck{
		"id": uniqueItemID, "label": isString, "state": isString, "tone": oneOf(badgeTones), "icon": isString,
		"attention": isBool, "pending": isBool, "ghost": isBool, "tooltip": isString, "outs": listOf(flowOut),
	})
	markerKinds = []string{"start", "end", "schedule", "inbox", "session", "runs"}
	flowMarker  = objectCheck([]string{"id", "kind"}, map[string]propCheck{
		"id": uniqueItemID, "kind": oneOf(markerKinds), "label": isString, "reached": isBool,
	})
	flowEdge = objectCheck([]string{"from", "to", "state"}, map[string]propCheck{
		"from": nonEmptyString, "out": nonEmptyString, "to": nonEmptyString, "state": oneOf(flowStates), "ref": nonEmptyString, "tooltip": isString, "label": isString,
	})

	laneActor = objectCheck([]string{"id", "label"}, map[string]propCheck{
		"id": uniqueItemID, "label": isString, "icon": isString, "tone": oneOf(badgeTones),
	})
	laneMove = objectCheck([]string{"actor", "cause", "tooltip"}, map[string]propCheck{
		"actor": nonEmptyString, "cause": causeNumber, "label": isString, "tooltip": isString,
		"current": isBool, "last": isBool, "user": isBool, "tone": oneOf(badgeTones), "ref": nonEmptyString,
	})

	runStatuses = []string{"ok", "failed", "missed", "running", "ignored"}
	stripRun    = objectCheck([]string{"at", "status"}, map[string]propCheck{
		"at": timeString, "status": oneOf(runStatuses), "tooltip": isString, "ref": nonEmptyString,
	})
	pausedSpan = objectCheck([]string{"from"}, map[string]propCheck{"from": timeString, "to": timeString})
	graphMove  = objectCheck([]string{"node", "cause", "tooltip"}, map[string]propCheck{
		"node": nonEmptyString, "cause": causeNumber, "label": isString, "tooltip": isString, "out": isString,
		"current": isBool, "user": isBool, "last": isBool, "end": nonEmptyString,
		"bar": nonEmptyString, "tone": oneOf(badgeTones), "round": countNumber,
	})

	sparkValues = maxLen(maxSparkValues, sparkBudget(arrayOf(anyNumber)))
	sparkFields = map[string]propCheck{"values": sparkValues, "max": positive}
	sparkHeight = number(func(n float64) bool { return n >= minSparkHeight && n <= maxSparkHeight }, fmt.Sprintf("a number within %d…%d", minSparkHeight, maxSparkHeight))

	catalog = map[string]spec{
		"stack":  {required: []string{"direction"}, props: map[string]propCheck{"direction": oneOf(directions)}, children: anyChildren},
		"split":  {required: []string{"direction"}, props: map[string]propCheck{"direction": oneOf(directions)}, children: childRule{min: 2, max: 4}},
		"scroll": {children: childRule{min: 1, max: 1}},
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
		"text":     {required: []string{"text"}, props: map[string]propCheck{"text": isString, "style": oneOf(textStyles), "lines": linesNumber, "tone": oneOf(badgeTones)}, children: noChildren},
		"badge":    {required: []string{"text"}, props: map[string]propCheck{"text": isString, "tone": oneOf(badgeTones)}, children: noChildren},
		"icon":     {required: []string{"name"}, props: map[string]propCheck{"name": isString, "tone": oneOf(badgeTones)}, children: noChildren},
		"diff":     {required: []string{"text"}, props: map[string]propCheck{"text": isString}, children: noChildren},
		"markdown": {required: []string{"text"}, props: map[string]propCheck{"text": isString}, children: noChildren},
		"document": {
			required:    []string{"html"},
			props:       map[string]propCheck{"html": isString, "path": isString, "rev": countNumber, "size": oneOf(documentSizes), "anchor": isString, "kind": oneOf(documentKinds)},
			children:    noChildren,
			interactive: true,
		},
		"button": {
			required:    []string{"label"},
			props:       map[string]propCheck{"label": isString, "icon": isString, "confirm": isString, "action": actionCheck, "variant": oneOf(variants), "disabled": isBool, "shortcut": shortcutCheck, "loading": isString, "iconOnly": isBool, "copy": isString, "overflow": isBool},
			children:    childRule{min: 0, max: 1, only: []string{"form"}},
			interactive: true,
		},
		"textField": {
			props:       map[string]propCheck{"value": isString, "placeholder": isString, "lines": linesNumber, "error": isString, "action": actionCheck, "live": isBool, "drop": isBool},
			children:    noChildren,
			interactive: true,
		},
		"folderField": {
			required:    []string{"label"},
			props:       map[string]propCheck{"label": isString, "values": listOf(isString), "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"toggle": {required: []string{"value"}, props: map[string]propCheck{"value": isBool, "action": actionCheck, "label": isString, "style": oneOf(toggleStyles)}, children: noChildren, interactive: true},
		"progress": {
			required: []string{"value", "total"},
			props:    map[string]propCheck{"value": countNumber, "total": countNumber, "tone": oneOf(badgeTones)},
			children: noChildren,
		},
		"select": {
			required:    []string{"options"},
			props:       map[string]propCheck{"options": listOf(optionCheck), "value": isString, "error": isString, "action": actionCheck},
			children:    noChildren,
			interactive: true,
		},
		"form":    {props: map[string]propCheck{"action": actionCheck, "autosave": isBool, "sheet": isBool}, children: anyChildren, interactive: true},
		"surface": {required: []string{"sessionId"}, props: map[string]propCheck{"sessionId": sessionIDCheck}, children: noChildren},
		"webview": {required: []string{"key", "url"}, props: map[string]propCheck{"key": isString, "url": loopbackURL, "rev": countNumber}, children: noChildren},
		"header": {
			required: []string{"title"},
			props: map[string]propCheck{
				"title": isString, "icon": isString, "meta": isString, "badges": listOf(headerBadgeCheck),
				"status": oneOf(sessionStatuses), "agent": isString, "branch": isString, "mode": isString,
				"field": nonEmptyString, "error": isString,
			},
			children: childRule{min: 0, max: 9, only: []string{"button", "textField"}, once: []string{"textField"}},
		},
		"empty": {
			required: []string{"text"},
			props:    map[string]propCheck{"text": isString, "title": isString, "icon": isString, "hint": isString, "center": isBool},
			children: childRule{min: 0, max: 3, only: []string{"button", "flow", "form", "stack"}, once: []string{"button", "flow", "form", "stack"}},
		},

		"transcript": {props: map[string]propCheck{"center": isBool, "anchor": isString, "anchorRev": countNumber}, children: anyChildren},
		"message": {
			required: []string{"role", "text"},
			props:    map[string]propCheck{"role": oneOf(messageRoles), "text": isString, "attachments": attachmentsCheck, "from": isString, "seconds": countNumber, "heard": isString, "docLinks": stringMap, "docCards": docCardsCheck, "images": imageRefsCheck},
			children: noChildren,
		},
		"plan": {required: []string{"entries"}, props: map[string]propCheck{"entries": listOf(planEntryCheck)}, children: noChildren},
		"toolCard": {
			required: []string{"kind", "title"},
			props: map[string]propCheck{
				"kind": oneOf(toolKinds), "title": isString, "status": oneOf(toolStatuses), "body": toolBodyCheck,
				"truncated": isBool, "standalone": isBool, "actions": listOf(cardActionCheck),
			},
			children:    noChildren,
			interactive: true,
		},
		"card": {props: map[string]propCheck{"tone": oneOf(badgeTones), "icon": isString}, children: childRule{min: 1, max: -1}},
		"sheet": {
			required:    []string{"title", "cancel"},
			props:       map[string]propCheck{"title": isString, "icon": isString, "tone": oneOf(badgeTones), "cancel": actionCheck},
			children:    childRule{min: 1, max: -1},
			interactive: true,
			check:       sheetCheck,
		},
		"permissionCard": {
			required: []string{"request", "text", "options"},
			props: map[string]propCheck{
				"request": isString, "text": isString, "title": isString, "command": isString, "caption": isString,
				"options": listOf(permissionOptionCheck), "outcome": isString, "action": actionCheck,
			},
			children:    noChildren,
			interactive: true,
		},
		"graph": {
			required: []string{"nodes", "edges"},
			props: map[string]propCheck{
				"nodes":   maxLen(maxGraphNodes, listOf(graphNode)),
				"edges":   maxLen(maxGraphEdges, listOf(graphEdge)),
				"moves":   maxLen(maxGraphMoves, listOf(graphMove)),
				"reached": isBool,
			},
			children:    childRule{min: 0, max: 3},
			interactive: true,
		},
		"flow": {
			required: []string{"cards", "edges"},
			props: map[string]propCheck{
				"cards":   maxLen(maxFlowCards, listOf(flowCard)),
				"markers": maxLen(maxFlowMarkers, listOf(flowMarker)),
				"edges":   maxLen(maxGraphEdges, listOf(flowEdge)),
			},
			children:    noChildren,
			interactive: true,
		},
		"lanes": {
			required: []string{"actors", "moves"},
			props: map[string]propCheck{
				"actors":    maxLen(maxLaneActors, listOf(laneActor)),
				"moves":     maxLen(maxGraphMoves, listOf(laneMove)),
				"title":     isString,
				"collapsed": isBool,
				"count":     countNumber,
			},
			children:    noChildren,
			interactive: true,
		},
		"runstrip": {
			required: []string{"runs"},
			props: map[string]propCheck{
				"runs":     maxLen(maxStripRuns, listOf(stripRun)),
				"now":      timeString,
				"next":     timeString,
				"paused":   maxLen(maxStripRuns, listOf(pausedSpan)),
				"selected": isString,
				"title":    isString,
				"count":    countNumber,
			},
			children:    noChildren,
			interactive: true,
		},
		"composer": {
			props: map[string]propCheck{
				"placeholder": isString, "hint": isString, "agent": isString, "modes": listOf(optionCheck), "mode": isString, "pickers": listOf(pickerCheck),
				"commands": listOf(commandCheck), "attach": listOf(oneOf(attachSources)), "attachments": attachmentsCheck, "running": isBool, "steers": isBool, "docked": isBool, "disabled": isBool,
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

func CheckShortcut(combo string) error {
	return shortcutCheck(nil, combo)
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
	if sp.check != nil {
		if err := sp.check(v, n); err != nil {
			return invalid(path, "%v", err)
		}
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
	seen := map[string]bool{}
	for _, c := range n.Children {
		if len(sp.children.only) > 0 && !slices.Contains(sp.children.only, c.Type) {
			return invalid(path, "child %s not allowed; only %v", c.Type, sp.children.only)
		}
		if slices.Contains(sp.children.once, c.Type) {
			if seen[c.Type] {
				return invalid(path, "more than one %s child", c.Type)
			}
			seen[c.Type] = true
		}
	}
	return nil
}

func normalize(props map[string]any) (map[string]any, error) {
	if len(props) == 0 {
		return nil, nil
	}
	if jsonNative(props) {
		return props, nil
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

func jsonNative(v any) bool {
	switch v := v.(type) {
	case nil, string, bool:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case []any:
		for _, e := range v {
			if !jsonNative(e) {
				return false
			}
		}
		return true
	case map[string]any:
		for _, e := range v {
			if !jsonNative(e) {
				return false
			}
		}
		return true
	}
	return false
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

func stringMap(_ *validator, v any) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return errors.New("must be an object")
	}
	for name, value := range obj {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: must be a string", name)
		}
	}
	return nil
}

func nonEmptyString(_ *validator, v any) error {
	if s, ok := v.(string); !ok || s == "" {
		return errors.New("must be a non-empty string")
	}
	return nil
}

func timeString(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("must be an RFC 3339 time")
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return errors.New("must be an RFC 3339 time")
	}
	return nil
}

func isBool(_ *validator, v any) error {
	if _, ok := v.(bool); !ok {
		return errors.New("must be a boolean")
	}
	return nil
}

func sheetCheck(c *validator, n sdui.Node) error {
	if c.depth != 2 {
		return errors.New("a sheet must be a child of the slot's root node")
	}
	cancel, _ := n.Props["cancel"].(string)
	if !hasButton(n.Children, cancel) {
		return fmt.Errorf("cancel %q is not the action of a button in the sheet", cancel)
	}
	return nil
}

func hasButton(nodes []sdui.Node, action string) bool {
	return slices.ContainsFunc(nodes, func(c sdui.Node) bool {
		return c.Type == "button" && c.Props["action"] == action || hasButton(c.Children, action)
	})
}

func actionCheck(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok || len(s) == 0 || len(s) > maxActionLen || !actionPattern.MatchString(s) {
		return fmt.Errorf("must match %s and be 1-%d characters", actionPattern, maxActionLen)
	}
	return nil
}

func shortcutCheck(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok || !shortcutPattern.MatchString(s) {
		return fmt.Errorf("must be a key combo like cmd+r, opt+1 or p")
	}
	bare := !strings.Contains(s, "cmd+") && !strings.Contains(s, "ctrl+") && !strings.Contains(s, "opt+")
	if key := s[strings.LastIndex(s, "+")+1:]; bare && slices.Contains(reservedKeys, key) {
		return fmt.Errorf("%s without cmd, ctrl or opt is reserved for the app", key)
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

func loopbackURL(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("must be a string")
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
		return errors.New("must be an http://127.0.0.1:<port> URL")
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

func headerBadgeCheck(c *validator, v any) error {
	return objectCheck([]string{"text"}, map[string]propCheck{"text": isString, "tone": oneOf(badgeTones)})(c, v)
}

func optionCheck(c *validator, v any) error {
	return objectCheck([]string{"value", "label"}, map[string]propCheck{"value": isString, "label": isString})(c, v)
}

func menuEntryCheck(c *validator, v any) error {
	return objectCheck([]string{"action", "label"}, map[string]propCheck{"action": actionCheck, "label": isString, "confirm": isString, "key": menuKeyCheck})(c, v)
}

func menuKeyCheck(_ *validator, v any) error {
	s, ok := v.(string)
	if !ok || !menuKeyPattern.MatchString(s) {
		return fmt.Errorf("must be one lowercase letter or digit")
	}
	return nil
}

func changesCheck(c *validator, v any) error {
	return objectCheck([]string{"added", "removed"}, map[string]propCheck{"added": countNumber, "removed": countNumber})(c, v)
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
	return objectCheck([]string{"id", "label"}, map[string]propCheck{
		"id": uniqueItemID, "label": isString, "kind": oneOf(permissionKinds), "shortcut": shortcutCheck, "menu": isBool, "rule": isString, "scope": isString, "workspace": isString,
	})(c, v)
}

func pickerCheck(c *validator, v any) error {
	return objectCheck([]string{"id", "value", "options"}, map[string]propCheck{
		"id": isString, "title": isString, "help": isString, "value": isString, "options": listOf(optionCheck), "toggle": isBool,
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
		"muted": isBool, "faint": isBool, "hasChildren": isBool, "expanded": isBool, "children": listOf(treeItemCheck),
		"badges": listOf(headerBadgeCheck), "changes": changesCheck, "detailTone": oneOf(badgeTones),
	}
	for k, f := range itemFields {
		fields[k] = f
	}
	return objectCheck(itemRequired, fields)(c, v)
}

var (
	columnCheck = objectCheck([]string{"id", "title"}, map[string]propCheck{
		"id": uniqueItemID, "title": isString, "align": oneOf(columnAligns), "width": nonNegative,
		"truncate": oneOf(truncates), "priority": priorityNumber,
	})
	tableRowCheck = objectCheck([]string{"id", "cells"}, map[string]propCheck{
		"id": uniqueItemID, "depth": depthNumber, "hasChildren": isBool, "expanded": isBool, "muted": isBool, "group": isBool, "cells": cellsCheck,
	})
	cellCheck = objectCheck(nil, map[string]propCheck{
		"text": isString, "bar": unitNumber, "tone": oneOf(cellTones), "spark": objectCheck([]string{"values", "max"}, sparkFields), "since": nonNegative,
		"action": actionCheck, "label": isString,
	})
	sortCheck  = objectCheck([]string{"column"}, map[string]propCheck{"column": columnID, "desc": isBool})
	cellsArray = arrayOf(cellCheck)
)

func cellsCheck(c *validator, v any) error {
	cells, _ := v.([]any)
	if len(cells) != len(c.columns) {
		return fmt.Errorf("%d cells for %d columns", len(cells), len(c.columns))
	}
	return cellsArray(c, v)
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
