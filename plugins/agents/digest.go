package main

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/agents/acp"
)

const (
	digestSessionCap   = 8 << 10
	digestWorkspaceCap = 64 << 10
	digestPieceRunes   = 1000
	digestRepeats      = 3
	digestSeparator    = "\n\n"

	entryCancelled = "cancelled"
	toolFailed     = "failed"
	bashTool       = "Bash"
	rejectPrefix   = "reject"
)

func (a *app) digest(workspaceID string, since time.Time) string {
	recs := slices.DeleteFunc(a.list(workspaceID), func(rec record) bool {
		return rec.restricted() || !rec.UpdatedAt.After(since)
	})
	slices.SortFunc(recs, func(x, y record) int { return y.UpdatedAt.Compare(x.UpdatedAt) })
	var parts []string
	total := 0
	for _, rec := range recs {
		s := a.session(rec.ID)
		if s == nil {
			continue
		}
		part := digestSession(rec, digestEntries(s.viewSince(0, -1)), since)
		if part == "" {
			continue
		}
		total += len(part) + len(digestSeparator)
		if total > digestWorkspaceCap {
			break
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, digestSeparator)
}

func digestSession(rec record, entries []entry, since time.Time) string {
	entries = slices.DeleteFunc(slices.Clone(entries), func(en entry) bool { return en.At.Before(since) })
	body := slices.Concat(digestFacts(entries), digestTurns(entries))
	if len(body) == 0 {
		return ""
	}
	head := "## " + cmp.Or(rec.Title, "New session") + " (" + rec.ID + ")"
	return capBytes(head+digestSeparator+strings.Join(body, digestSeparator), digestSessionCap)
}

func digestEntries(v transcriptView) []entry {
	out := make([]entry, 0, len(v.Entries))
	lastAt := map[int]time.Time{}
	for _, en := range v.Entries {
		out = append(out, *en)
		lastAt[en.Turn] = en.At
	}
	for _, t := range v.Turns {
		if t.StopReason == acp.StopCancelled {
			out = append(out, entry{Kind: entryCancelled, Turn: t.Turn, At: lastAt[t.Turn]})
		}
	}
	return out
}

func digestFacts(entries []entry) []string {
	failed, commands := map[string]int{}, map[string]int{}
	cancelledTurns, deniedPermissions := 0, 0
	for _, en := range entries {
		switch en.Kind {
		case entryCancelled:
			cancelledTurns++
		case entryPermission:
			if rejected(en) {
				deniedPermissions++
			}
		case entryTool:
			name, title := toolField(en, "name"), toolField(en, "title")
			if toolField(en, "status") == toolFailed {
				failed[titleOf(cmp.Or(name, title))]++
			}
			if name == bashTool && title != "" {
				commands[title]++
			}
		}
	}
	maps.DeleteFunc(commands, func(_ string, n int) bool { return n < digestRepeats })
	var facts []string
	if len(failed) > 0 {
		facts = append(facts, "failed tool calls: "+tally(failed))
	}
	if len(commands) > 0 {
		facts = append(facts, "repeated commands: "+tally(commands))
	}
	if cancelledTurns > 0 {
		facts = append(facts, "cancelled turns: "+strconv.Itoa(cancelledTurns))
	}
	if deniedPermissions > 0 {
		facts = append(facts, "denied permissions: "+strconv.Itoa(deniedPermissions))
	}
	return facts
}

func digestTurns(entries []entry) []string {
	lastReply := map[int]int{}
	for i, en := range entries {
		if en.Kind == entryAgent {
			lastReply[en.Turn] = i
		}
	}
	var parts []string
	for i, en := range entries {
		label := ""
		switch {
		case en.Kind == entryUser && en.From == "":
			label = "User"
		case en.Kind == entryAgent && lastReply[en.Turn] == i:
			label = "Agent"
		}
		text := clipRunes(strings.TrimSpace(en.Text), digestPieceRunes)
		if label != "" && text != "" {
			parts = append(parts, label+" (turn "+strconv.Itoa(en.Turn)+"):\n"+text)
		}
	}
	return parts
}

func rejected(en entry) bool {
	if en.Outcome == nil || en.Outcome.Outcome != acp.OutcomeSelected {
		return false
	}
	i := slices.IndexFunc(en.Options, func(o acp.PermissionOption) bool { return o.OptionID == en.Outcome.OptionID })
	return i >= 0 && strings.HasPrefix(en.Options[i].Kind, rejectPrefix)
}

func toolField(en entry, name string) string {
	var value string
	_ = json.Unmarshal(en.Tool[name], &value)
	return value
}

func tally(counts map[string]int) string {
	names := slices.SortedFunc(maps.Keys(counts), func(x, y string) int {
		return cmp.Or(cmp.Compare(counts[y], counts[x]), cmp.Compare(x, y))
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, titleOf(name)+" ×"+strconv.Itoa(counts[name]))
	}
	return strings.Join(parts, ", ")
}

func capBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "")
}
