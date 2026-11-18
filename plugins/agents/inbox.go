package main

import (
	"cmp"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/sdui"
)

const (
	inboxPrefix    = "inbox:"
	inboxNeedsKey  = inboxPrefix + "needs"
	inboxOpenKey   = inboxPrefix + "open"
	mirrorPrefix   = "mirror:"
	inboxNeedsIcon = "tray"
	inboxOpenIcon  = "tray.full"
	inboxNeedsWord = "Needs you"
	inboxOpenWord  = "Open"
	inboxNeeds     = "0"
	inboxOpen      = "1"
	mirrorWhere    = 8
)

type inboxEntry struct {
	ID    string
	Run   bool
	Gate  bool
	Label string
	Words string
	Open  bool
	Since time.Time
	Key   string
}

type inboxMemo struct {
	mu    sync.Mutex
	since map[string]inboxSeen
}

type inboxSeen struct {
	workspace string
	open      bool
	at        time.Time
}

func inboxParent(info sessionInfo) string {
	return cmp.Or(info.NestUnder, info.RowUnder, statusParent(info.record))
}

func inboxEntries(infos []sessionInfo) []inboxEntry {
	index := make(map[string]int, len(infos))
	for i, info := range infos {
		index[info.ID] = i
	}
	kids := map[string][]int{}
	for i, info := range infos {
		if p := inboxParent(info); p != "" && p != info.ID {
			if _, ok := index[p]; ok {
				kids[p] = append(kids[p], i)
			}
		}
	}
	var earliest func(i int, at func(sessionInfo) time.Time, seen map[int]bool) time.Time
	earliest = func(i int, at func(sessionInfo) time.Time, seen map[int]bool) time.Time {
		seen[i] = true
		best := at(infos[i])
		for _, k := range kids[infos[i].ID] {
			if seen[k] {
				continue
			}
			if t := earliest(k, at, seen); !t.IsZero() && (best.IsZero() || t.Before(best)) {
				best = t
			}
		}
		return best
	}
	var out []inboxEntry
	for i, info := range infos {
		if info.closed() || info.Hidden || info.isAside() {
			continue
		}
		if _, ok := index[inboxParent(info)]; ok {
			continue
		}
		var entry inboxEntry
		switch info.Group {
		case groupNeedsYou:
			entry.Since = earliest(i, needSince, map[int]bool{})
		case groupOpen:
			entry.Open = true
			entry.Since = earliest(i, openSince, map[int]bool{})
		default:
			continue
		}
		entry.ID, entry.Run, entry.Gate = info.ID, info.Kind == kindRun, info.Kind == kindGate
		if entry.Gate {
			entry.Label, entry.Words = info.Title, info.RowDetail
		}
		entry.Since = cmp.Or(entry.Since, info.UpdatedAt)
		out = append(out, entry)
	}
	return out
}

func needSince(info sessionInfo) time.Time {
	if !info.NeedsYou || info.NeedsYouSince == nil {
		return time.Time{}
	}
	return *info.NeedsYouSince
}

func openSince(info sessionInfo) time.Time {
	if info.OpenCount == 0 || info.isAside() {
		return time.Time{}
	}
	return info.OpenSince
}

func inboxKey(entry inboxEntry) string {
	section := inboxNeeds
	if entry.Open {
		section = inboxOpen
	}
	return section + entry.Since.UTC().Format(startLayout) + " " + entry.ID
}

func (m *inboxMemo) apply(workspace string, infos []sessionInfo) {
	entries := inboxEntries(infos)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.since == nil {
		m.since = map[string]inboxSeen{}
	}
	members := make(map[string]int, len(entries))
	for i, entry := range entries {
		if seen, ok := m.since[entry.ID]; ok && seen.open == entry.Open {
			entry.Since = seen.at
		}
		m.since[entry.ID] = inboxSeen{workspace: workspace, open: entry.Open, at: entry.Since}
		entry.Key = inboxKey(entry)
		entries[i] = entry
		members[entry.ID] = i
	}
	for id, seen := range m.since {
		if _, ok := members[id]; !ok && seen.workspace == workspace {
			delete(m.since, id)
		}
	}
	for i := range infos {
		at, ok := members[infos[i].ID]
		if !ok {
			continue
		}
		infos[i].InboxKey = entries[at].Key
		infos[i].inbox = entries[at]
	}
}

func inboxOf(infos []sessionInfo) []inboxEntry {
	var out []inboxEntry
	for _, info := range infos {
		if info.InboxKey != "" {
			out = append(out, info.inbox)
		}
	}
	slices.SortFunc(out, func(x, y inboxEntry) int { return strings.Compare(x.Key, y.Key) })
	return out
}

func inboxGroups(entries []inboxEntry, roots []sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, oldest map[string]time.Time) []sdui.TreeItem {
	byID := make(map[string]sessionRow, len(roots))
	byRun := map[string][]sessionRow{}
	for _, row := range roots {
		byID[row.Rec.ID] = row
		if r := row.Rec.Run; r != nil {
			byRun[r.ID] = append(byRun[r.ID], row)
		}
	}
	var needs, open []sdui.TreeItem
	copied := map[string]bool{}
	for _, entry := range entries {
		if entry.Gate {
			needs = append(needs, gateInboxItem(entry, now))
			continue
		}
		rows := byRun[entry.ID]
		if row, ok := byID[entry.ID]; ok && !entry.Run {
			rows = []sessionRow{row}
		}
		if len(rows) == 0 {
			continue
		}
		items := runItems(rows, inboxPrefix, kids, collapsed, now, map[string]bool{}, oldest)
		if len(items) == 0 || copied[items[0].ID] {
			continue
		}
		copied[items[0].ID] = true
		item := mirrorItem(items[0], rows[0], entry, now)
		if entry.Open {
			open = append(open, item)
		} else {
			needs = append(needs, item)
		}
	}
	var out []sdui.TreeItem
	for _, section := range []struct {
		key, label, icon string
		items            []sdui.TreeItem
	}{{inboxNeedsKey, inboxNeedsWord, inboxNeedsIcon, needs}, {inboxOpenKey, inboxOpenWord, inboxOpenIcon, open}} {
		if len(section.items) == 0 {
			continue
		}
		group := groupItem(section.key, section.label, section.icon, section.items, !collapsed[section.key])
		group.Detail = strconv.Itoa(len(section.items))
		out = append(out, group)
	}
	return out
}

func mirrorItem(item sdui.TreeItem, row sessionRow, entry inboxEntry, now time.Time) sdui.TreeItem {
	item.MirrorOf = item.ID
	item.ID = mirrorPrefix + item.ID
	item.Children, item.HasChildren, item.Expanded, item.Pinned = nil, false, false, false
	where := row.Branch
	if where == "" && row.Rec.Worktree != "" {
		where = path.Base(row.Rec.Worktree)
	}
	where, _, _ = strings.Cut(where, "/")
	where = clipRunes(where, mirrorWhere)
	item.Detail = strings.Join(nonEmpty(where, waited(now.Sub(entry.Since))), " · ")
	item.DetailFaint, item.DetailTone = false, ""
	if !entry.Open {
		item.DetailTone = statusTones[statusWaiting]
	}
	return item
}

func gateInboxItem(entry inboxEntry, now time.Time) sdui.TreeItem {
	return sdui.TreeItem{
		ID:         mirrorPrefix + gatePrefix + entry.ID,
		MirrorOf:   gatePrefix + entry.ID,
		Label:      entry.Label,
		Icon:       gatesIcon,
		Detail:     strings.Join(nonEmpty(entry.Words, waited(now.Sub(entry.Since))), " · "),
		DetailTone: statusTones[statusWaiting],
	}
}

func waited(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " d"
}
