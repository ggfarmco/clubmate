package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"clubmate/internal/bus"
	"clubmate/pkg/pluginsdk"
)

const (
	anyTopic     = "*"
	emitPrefix   = "event."
	payloadField = "payload"
	byField      = "by"
	byAgent      = "agent"
)

var (
	placeholderRe   = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)
	workspacePaths  = []string{"workspace", "projectId", "project.id", "session.projectId"}
	errNotInEvent   = errors.New("not in the event")
	errBadOnPattern = errors.New(`on: give a topic like agents.turn or a prefix like event.*; "*" is not allowed`)
)

func checkPattern(on string) error {
	if on == anyTopic || !bus.ValidPattern(on) {
		return errBadOnPattern
	}
	return nil
}

func parseMatch(text string) (json.RawMessage, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return nil, fmt.Errorf("match: invalid JSON object: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	for path := range fields {
		if path == "" || slices.Contains(strings.Split(path, "."), "") {
			return nil, fmt.Errorf("match: bad field path %q", path)
		}
	}
	return json.Marshal(fields)
}

func decodePayload(raw json.RawMessage) any {
	var v any
	if len(raw) > 0 && json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

func eventWorkspace(payload any) string {
	for _, path := range workspacePaths {
		if v, ok := lookup(payload, path); ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func matches(t *Trigger, topic string, payload any) bool {
	if !bus.Match(t.On, topic) {
		return false
	}
	if eventWorkspace(payload) != t.Workspace {
		return false
	}
	if len(t.Match) == 0 {
		return true
	}
	var want map[string]any
	if json.Unmarshal(t.Match, &want) != nil {
		return false
	}
	for path, expected := range want {
		got, ok := lookup(payload, path)
		if !ok || !equal(got, expected) {
			return false
		}
	}
	return true
}

func equal(got, expected any) bool {
	if options, ok := expected.([]any); ok {
		for _, o := range options {
			if reflect.DeepEqual(got, o) {
				return true
			}
		}
		return false
	}
	return reflect.DeepEqual(got, expected)
}

func lookup(v any, path string) (any, bool) {
	for part := range strings.SplitSeq(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[part]; !ok {
			return nil, false
		}
	}
	return v, true
}

func expand(args json.RawMessage, ev pluginsdk.Event) (json.RawMessage, error) {
	if !bytes.Contains(args, []byte("{{")) {
		return args, nil
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("args: %w", err)
	}
	payload := decodePayload(ev.Payload)
	var failed error
	v = walk(v, func(s string) string {
		return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
			name := placeholderRe.FindStringSubmatch(m)[1]
			out, err := placeholder(name, ev, payload)
			if err != nil && failed == nil {
				failed = fmt.Errorf("args: {{%s}}: %w", name, err)
			}
			return out
		})
	})
	if failed != nil {
		return nil, failed
	}
	return json.Marshal(v)
}

func walk(v any, f func(string) string) any {
	switch x := v.(type) {
	case string:
		return f(x)
	case map[string]any:
		for k, e := range x {
			x[k] = walk(e, f)
		}
	case []any:
		for i, e := range x {
			x[i] = walk(e, f)
		}
	}
	return v
}

func placeholder(name string, ev pluginsdk.Event, payload any) (string, error) {
	switch {
	case name == "topic":
		return ev.Topic, nil
	case name == "seq":
		return strconv.FormatInt(ev.Seq, 10), nil
	case name == payloadField:
		return text(payload)
	}
	path, ok := strings.CutPrefix(name, payloadField+".")
	if !ok {
		return "", fmt.Errorf("unknown placeholder; use topic, seq, payload or payload.<field>")
	}
	v, ok := lookup(payload, path)
	if !ok {
		return "", errNotInEvent
	}
	return text(v)
}

func text(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	raw, err := json.Marshal(v)
	return string(raw), err
}
