package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	maxDiffCells = 250_000
	toolCallCmd  = "agents.tool-call"
)

var (
	errNoToolCall  = errors.New("no such tool call")
	slimInputKeys  = []string{"command", "description"}
	slimToolFields = []string{"content", "rawInput"}
)

type toolSummary struct {
	Diffs  []diffSummary `json:"diffs,omitempty"`
	Output bool          `json:"output,omitempty"`
}

type diffSummary struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

type toolCallArgs struct {
	Session string `json:"session"`
	Tool    string `json:"tool"`
}

type toolCallResult struct {
	Tool map[string]json.RawMessage `json:"tool"`
}

func (a *app) toolCallCommand(_ context.Context, cmd pluginsdk.CommandRun, args toolCallArgs) (any, error) {
	s, err := a.lookupFor(cmd, args.Session)
	if err != nil {
		return nil, err
	}
	tool, ok := s.tool(args.Tool)
	if !ok {
		return nil, fmt.Errorf("%w: %q", errNoToolCall, args.Tool)
	}
	en := []*entry{{Tool: tool}}
	compactTools(en)
	return toolCallResult{Tool: en[0].Tool}, nil
}

func slimTools(entries []*entry) {
	for _, en := range entries {
		if en.Tool == nil || !slimmable(en.Tool) {
			continue
		}
		sum := en.node.summary(func() toolSummary { return summarize(en.Tool) })
		tool := maps.Clone(en.Tool)
		for _, k := range slimToolFields {
			delete(tool, k)
		}
		if input := slimInput(en.Tool["rawInput"]); input != nil {
			tool["rawInput"] = input
		}
		en.Tool, en.Summary, en.node = tool, &sum, nil
	}
}

func slimmable(tool map[string]json.RawMessage) bool {
	_, content := tool["content"]
	_, input := tool["rawInput"]
	return content || input
}

func slimInput(raw json.RawMessage) json.RawMessage {
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil {
		return nil
	}
	kept := map[string]json.RawMessage{}
	for _, k := range slimInputKeys {
		if v, ok := input[k]; ok {
			kept[k] = v
		}
	}
	if len(kept) == 0 {
		return nil
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return nil
	}
	return out
}

func summarize(tool map[string]json.RawMessage) toolSummary {
	var content []toolContent
	_ = json.Unmarshal(tool["content"], &content)
	var sum toolSummary
	var texts []string
	for _, c := range content {
		switch c.Type {
		case "diff":
			if c.Path == "" {
				continue
			}
			added, removed := lineChanges(deref(c.OldText), c.NewText)
			sum.Diffs = append(sum.Diffs, diffSummary{Path: c.Path, Added: added, Removed: removed})
		case "content":
			if c.Content.Type == "text" {
				texts = append(texts, c.Content.Text)
			}
		}
	}
	sum.Output = strings.Join(texts, "\n") != ""
	return sum
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func lineChanges(old, new string) (added, removed int) {
	a, b := diffLines(old), diffLines(new)
	if len(a)*len(b) > maxDiffCells {
		return len(b), len(a)
	}
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				cur[j] = prev[j+1] + 1
			} else {
				cur[j] = max(prev[j], cur[j+1])
			}
		}
		prev, cur = cur, prev
	}
	return len(b) - prev[0], len(a) - prev[0]
}

func diffLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func decorationKey(rec record, w pluginsdk.Workspace, calls ...string) string {
	name, offset := time.Now().Zone()
	parts := append([]string{w.Name, name, fmt.Sprint(offset)}, w.Paths()...)
	if rec.Task != nil {
		for _, r := range rec.Task.Repos {
			parts = append(parts, r.Path, r.State)
		}
	}
	return partsHash(parts) + callKeySep + partsHash(calls)
}

func partsHash(parts []string) string {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func redecorated(seen, key string) func(*entry) bool {
	if seen == key {
		return nil
	}
	base, _, _ := strings.Cut(key, callKeySep)
	waited := slotCallsOf(seen, key)
	slot := func(en *entry) bool { return en.Kind == entryTool && waited[callHash(en.ID)] }
	if strings.HasPrefix(seen, base+callKeySep) {
		return func(en *entry) bool { return gateDecorated(en) || slot(en) }
	}
	return func(en *entry) bool { return decorated(en) || gateDecorated(en) || slot(en) }
}

func slotCallsKey(w *slotWaitView) string {
	if w == nil {
		return ""
	}
	var out strings.Builder
	for _, call := range slices.Sorted(slices.Values(w.Calls)) {
		out.WriteString(callKeySep + callHash(call))
	}
	return out.String()
}

func slotCallsOf(keys ...string) map[string]bool {
	out := map[string]bool{}
	for _, key := range keys {
		parts := strings.Split(key, callKeySep)
		for _, p := range parts[min(2, len(parts)):] {
			out[p] = true
		}
	}
	return out
}

func callHash(id string) string {
	return partsHash([]string{id})
}

func decorated(en *entry) bool {
	if en.Kind == entryPermission {
		return true
	}
	var n noticeUpdate
	return en.Kind == entryNotice && json.Unmarshal(en.Notice, &n) == nil && n.Repo != ""
}
