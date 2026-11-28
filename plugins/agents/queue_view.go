package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	landLinePrefix  = "lq:"
	queueFilePrefix = "lqf:"
	queueGlyph      = "arrow.down.to.line"
	queueShareIcon  = "doc.on.doc"
	queueFileIcon   = "doc"
	queueWhyIcon    = "info.circle"
	queueInLine     = "%d in line"
	queueNeedYou    = "%d %s you"
	queueAlone      = "Landing into %s · alone in line"
	queueLineText   = "%s of %d in line · %s"
	queueShares     = "Shares %s with %s"
	queueAlso       = "Also changes"
	queueMore       = "+%d more"
	queueUserEdits  = "Edits by the user"
	queueRemoveAsk  = "Remove this landing from the line? The session and its branch stay."
	markCarried     = "carried"
	markKept        = "Review kept"
	markKeptHelp    = "Review kept: %s moved in other files only"
	markAhead       = "ahead"
	markAheadLabel  = "Reviewed ahead"
	markAheadHelp   = "Reviewed ahead on the tree %s holds after the landing before it lands"

	headReviewing  = "reviewing"
	headTesting    = "testing"
	headCommitting = "committing"
	headFailed     = "failed"

	wordReviewing = "reviewing"
	wordTesting   = "testing"
	wordLanding   = "landing"
	wordInBlock   = "blocked"
	wordReady     = "ready"

	glyphRunning = "play.circle"
	glyphFailed  = "xmark.circle"
	glyphAsking  = "exclamationmark.circle"
	glyphBlocked = "minus.circle"
	glyphReady   = "circle.inset.filled"
	glyphWaits   = "circle"

	whyReviewing  = "review runs on the branch as it is now"
	whyTesting    = "tests run before it commits"
	whyCommitting = "every action is off until it is in"
	whyTurn       = "its turn: it calls git_land"
	whyFailed     = "its review or tests failed; the agent fixes it"
	whyAsking     = "waits for your yes"
	whyReady      = "waits for the head to land"
	whyPassed     = "passed once; waits until it calls git_land again"
	whyConflicts  = "conflicts with the tip; rebase before its turn"
	whyWaits      = "waits for its turn"

	queueActUp     = "up"
	queueActDown   = "down"
	queueActSkip   = "skip"
	queueActFiles  = "files"
	queueActOpen   = "open"
	queueActRemove = "remove"

	actQueuePlace  = "queuePlace"
	actQueueUp     = "queueUp"
	actQueueDown   = "queueDown"
	actQueueSkip   = "queueSkip"
	actQueueFiles  = "queueFiles"
	actQueueOpen   = "queueOpen"
	actQueueRemove = "queueRemove"
)

var queueMenuActions = map[string]string{
	queueActUp:     actQueueUp,
	queueActDown:   actQueueDown,
	queueActSkip:   actQueueSkip,
	queueActFiles:  actQueueFiles,
	queueActOpen:   actQueueOpen,
	queueActRemove: actQueueRemove,
}

type queueAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	To      int    `json:"to,omitempty"`
	Confirm string `json:"confirm,omitempty"`
	Divider bool   `json:"divider,omitempty"`
}

type queueMark struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Help  string `json:"help"`
}

type queueFileGroup struct {
	Title  string   `json:"title"`
	Shared bool     `json:"shared,omitempty"`
	Files  []string `json:"files"`
}

type queueGroup struct {
	row  queueRow
	rows map[string]sessionRow
}

func entryLook(state, step string, needs bool) (string, string, string) {
	switch state {
	case queueHead:
		switch step {
		case headReviewing:
			return wordReviewing, glyphRunning, toneAccent
		case headTesting:
			return wordTesting, glyphRunning, toneAccent
		case headFailed:
			return wordFailed, glyphFailed, toneDanger
		}
		return wordLanding, glyphRunning, toneAccent
	case queueAsks:
		return wordAsking, glyphAsking, toneWarning
	case queueBlocked:
		if needs {
			return wordInBlock, glyphBlocked, toneWarning
		}
		return wordInBlock, glyphBlocked, ""
	case queueReady:
		return wordReady, glyphReady, ""
	}
	return wordWaits, glyphWaits, ""
}

func entryWhy(e queueEntry, step string) string {
	switch e.State {
	case queueHead:
		switch step {
		case headReviewing:
			return whyReviewing
		case headTesting:
			return whyTesting
		case headCommitting:
			return whyCommitting
		case headFailed:
			return whyFailed
		}
		return whyTurn
	case queueAsks:
		return whyAsking
	case queueReady:
		return whyReady
	case queueWaiting:
		switch {
		case e.Passed:
			return whyPassed
		case !e.Clean && e.Mode == queueModeBranch:
			return whyConflicts
		}
		return whyWaits
	}
	return ""
}

func (a *app) headStep(q landQueue, e queueEntry) string {
	s := a.session(e.Session)
	if s == nil || e.State != queueHead {
		return ""
	}
	st := s.landReview(q.Into, e.Source)
	if _, ok := a.reviewing(e.Session, st); ok {
		return headReviewing
	}
	if a.checking(st) {
		return headTesting
	}
	if st.Tree == "" || st.Tree != e.Tree {
		return ""
	}
	switch st.Last {
	case gateApproved:
		return headCommitting
	case gateChanges:
		return headFailed
	}
	return ""
}

func (a *app) entryMarks(q landQueue, e queueEntry) []queueMark {
	s := a.session(e.Session)
	if s == nil || e.Mode != queueModeBranch {
		return []queueMark{}
	}
	if h := q.head(); h >= 0 && q.entry(e.ID) == h+1 && a.aheadShown() && aheadValid(q.Entries[h], e) {
		return []queueMark{{ID: markAhead, Label: markAheadLabel, Help: fmt.Sprintf(markAheadHelp, q.Into)}}
	}
	st := s.landReview(q.Into, e.Source)
	if st.Carried == "" || st.Last != gateApproved || st.Tree == "" || st.Tree != e.Tree {
		return []queueMark{}
	}
	return []queueMark{{ID: markCarried, Label: markKept, Help: fmt.Sprintf(markKeptHelp, q.Into)}}
}

func ordinal(n int) string {
	suffix := "th"
	switch n % 10 {
	case 1:
		suffix = "st"
	case 2:
		suffix = "nd"
	case 3:
		suffix = "rd"
	}
	if n%100 >= 11 && n%100 <= 13 {
		suffix = "th"
	}
	return strconv.Itoa(n) + suffix
}

func dressQueue(row *queueRow, steps map[string]string) {
	n := len(row.Entries)
	needs := 0
	for i := range row.Entries {
		e := &row.Entries[i]
		e.Word, e.Glyph, e.Tone = entryLook(e.State, steps[e.ID], e.NeedsYou)
		e.Line = fmt.Sprintf(queueLineText, ordinal(e.Place), n, e.Word)
		e.Actions = entryActions(row.Entries, i, steps)
		e.FileGroups = fileGroups(row.Entries, i)
		if e.Marks == nil {
			e.Marks = []queueMark{}
		}
		if e.NeedsYou {
			needs++
		}
	}
	row.Title = fmt.Sprintf(landingInto, row.Into)
	row.Glyph = queueGlyph
	row.Detail = fmt.Sprintf(queueInLine, n)
	row.Grouped = n > 1
	if needs > 0 {
		row.Detail, row.DetailTone = fmt.Sprintf(queueNeedYou, needs, plural(needs, "needs", "need")), toneWarning
	}
	if n == 1 {
		row.Help = fmt.Sprintf(queueAlone, row.Into)
	}
}

func entryActions(entries []queueEntryRow, i int, steps map[string]string) []queueAction {
	e := entries[i]
	n := len(entries)
	committing := func(j int) bool { return steps[entries[j].ID] == headCommitting }
	movable := e.Mode != queueModeDirty && !committing(i)
	up := movable && i > 0 && entries[i-1].Mode != queueModeDirty && !committing(i-1) && (e.Mode != queueModeBranch || entries[i-1].Mode == queueModeBranch)
	down := movable && i < n-1 && (e.Mode == queueModeBranch || entries[i+1].Mode != queueModeBranch)
	return []queueAction{
		{ID: queueActUp, Label: "Move up", Enabled: up, To: max(i, 1)},
		{ID: queueActDown, Label: "Move down", Enabled: down, To: min(i+2, n)},
		{ID: queueActSkip, Label: "Skip once", Enabled: down},
		{ID: queueActFiles, Label: "Show changed files", Enabled: len(e.Files) > 0 && !committing(i), Divider: true},
		{ID: queueActOpen, Label: "Open session", Enabled: e.Session != "" && !committing(i)},
		{ID: queueActRemove, Label: "Remove from line", Enabled: movable, Confirm: queueRemoveAsk, Divider: true},
	}
}

func fileGroups(entries []queueEntryRow, i int) []queueFileGroup {
	e := entries[i]
	var out []queueFileGroup
	shared := map[string]bool{}
	for _, oid := range e.Overlaps {
		j := slices.IndexFunc(entries, func(o queueEntryRow) bool { return o.ID == oid })
		if j < 0 {
			continue
		}
		var files []string
		for _, f := range e.Files {
			if slices.Contains(entries[j].Files, f) {
				files = append(files, f)
				shared[f] = true
			}
		}
		if len(files) > 0 {
			out = append(out, queueFileGroup{Title: fmt.Sprintf(queueShares, counted(len(files), "file"), entries[j].Title), Shared: true, Files: files})
		}
	}
	rest := slices.DeleteFunc(slices.Clone(e.Files), func(f string) bool { return shared[f] })
	if len(rest) > 0 {
		out = append(out, queueFileGroup{Title: queueAlso, Files: rest})
	}
	if out == nil {
		return []queueFileGroup{}
	}
	return out
}

func (a *app) queueOwner(q landQueue, recs []record) string {
	if q.Target == "" || q.Target == q.Repo {
		return ""
	}
	inLine := map[string]bool{}
	for _, e := range q.Entries {
		if e.Mode != queueModeDirty {
			inLine[e.Session] = true
		}
	}
	owner := ""
	var best record
	for _, rec := range recs {
		if rec.Worktree != q.Target || rec.closed() || rec.restricted() || inLine[rec.ID] || rec.GateFor != nil {
			continue
		}
		if owner == "" || startedFirst(rec, best) < 0 {
			owner, best = rec.ID, rec
		}
	}
	return owner
}

func (a *app) queueTrees(workspaceID string, recs []record) []queueRow {
	out := []queueRow{}
	for _, q := range a.queueList() {
		if !a.queueInWorkspace(q, workspaceID) {
			continue
		}
		row := a.queueRowOf(q, time.Now().UTC())
		row.Owner = a.queueOwner(q, recs)
		withSessionState(&row, recs)
		out = append(out, row)
	}
	return out
}

func withSessionState(row *queueRow, recs []record) {
	byID := make(map[string]record, len(recs))
	for _, rec := range recs {
		byID[rec.ID] = rec
	}
	for i := range row.Entries {
		e := &row.Entries[i]
		rec, ok := byID[e.Session]
		if !ok || e.Mode == queueModeDirty || !needsYou(rec) && rec.Status != statusError {
			continue
		}
		d := displayStatus(rec, "")
		e.Word, e.Glyph, e.Tone = statusLabels[d], statusIcons[d], statusTones[d]
		e.Line = fmt.Sprintf(queueLineText, ordinal(e.Place), len(row.Entries), e.Word)
	}
}

func placeQueues(rows []sessionRow, queues []queueRow) []*queueGroup {
	at := make(map[string]int, len(rows))
	for i, row := range rows {
		at[row.Rec.ID] = i
	}
	var roots []*queueGroup
	seen := map[string]bool{}
	for _, q := range queues {
		if !q.Grouped {
			if len(q.Entries) == 1 {
				if i, ok := at[q.Entries[0].Session]; ok && q.Entries[0].Mode != queueModeDirty && !seen[q.Entries[0].Session] {
					e := q.Entries[0]
					rows[i].Alone, rows[i].AloneHelp = &e, q.Help
					seen[e.Session] = true
				}
			}
			continue
		}
		g := &queueGroup{row: q, rows: map[string]sessionRow{}}
		owner, owned := at[q.Owner]
		if owned && rows[owner].Rec.closed() {
			owned = false
		}
		for _, e := range q.Entries {
			if i, ok := at[e.Session]; ok && e.Session != "" && e.Mode != queueModeDirty && !seen[e.Session] {
				seen[e.Session] = true
				rows[i].InLine = landLinePrefix + q.ID
				if owned {
					rows[i].InLine = q.Owner
				}
				g.rows[e.Session] = rows[i]
			}
		}
		if owned {
			rows[owner].Queues = append(rows[owner].Queues, g)
			continue
		}
		roots = append(roots, g)
	}
	return roots
}

func queueNeeds(groups []*queueGroup, oldest map[string]time.Time) bool {
	return slices.ContainsFunc(groups, func(g *queueGroup) bool {
		if g.row.NeedsYou {
			return true
		}
		for id := range g.rows {
			if !oldest[id].IsZero() {
				return true
			}
		}
		return false
	})
}

func queueItems(groups []*queueGroup, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) []sdui.TreeItem {
	out := make([]sdui.TreeItem, 0, len(groups))
	for _, g := range groups {
		out = append(out, queueGroupItem(g, kids, collapsed, now, path, oldest))
	}
	return out
}

func queueGroupItem(g *queueGroup, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) sdui.TreeItem {
	q := g.row
	children := make([]sdui.TreeItem, 0, len(q.Entries))
	shown := map[string]bool{}
	for _, e := range q.Entries {
		if _, ok := g.rows[e.Session]; ok && shown[e.Session] && e.Mode != queueModeDirty {
			children = append(children, repeatEntryItem(e))
			continue
		}
		shown[e.Session] = true
		children = append(children, queueEntryItem(q, e, g.rows, kids, collapsed, now, path, oldest))
	}
	key := landLinePrefix + q.ID
	group := groupItem(key, q.Title, q.Glyph, children, !collapsed[key] || q.NeedsYou)
	group.Detail, group.DetailTone = q.Detail, q.DetailTone
	group.Pinned = q.NeedsYou
	return group
}

func queueEntryItem(q queueRow, e queueEntryRow, rows map[string]sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) sdui.TreeItem {
	row, ok := rows[e.Session]
	if !ok || e.Mode == queueModeDirty {
		item := sdui.TreeItem{ID: landLinePrefix + q.ID + ":" + e.ID, Label: e.Title, Action: true, Pinned: e.NeedsYou}
		dressEntry(&item, e)
		return item
	}
	item := nestedItem(row, kids, collapsed, now, path, oldest)
	dressEntry(&item, e)
	item.Menu = append(queueMenu(e), item.Menu...)
	if len(item.Menu) > len(e.Actions)+1 {
		item.Menu[len(e.Actions)+1].Divider = true
	}
	item.Pinned = e.NeedsYou || !oldest[row.Rec.ID].IsZero()
	files := entryFileItems(e)
	if len(files) == 0 {
		return item
	}
	folded, set := collapsed[expandPrefix+row.Rec.ID]
	item.HasChildren = true
	if item.Expanded || set && !folded {
		item.Expanded = true
		item.Children = append(files, item.Children...)
	}
	return item
}

func repeatEntryItem(e queueEntryRow) sdui.TreeItem {
	item := sdui.TreeItem{ID: queueFilePrefix + e.Session + ":" + e.ID, Label: cmp.Or(e.Source, e.Title), Action: true, Pinned: e.NeedsYou}
	dressEntry(&item, e)
	return item
}

func dressEntry(item *sdui.TreeItem, e queueEntryRow) {
	item.Icon, item.Tone = e.Glyph, e.Tone
	item.Detail, item.DetailTone, item.DetailFaint = e.Word, "", false
	help := []string{item.Label, e.Line}
	for _, m := range e.Marks {
		item.Badges = append(item.Badges, sdui.HeaderBadge{Text: m.Label, Outline: true})
		help = append(help, m.Help)
	}
	item.Help = strings.Join(nonEmpty(append(help, e.Why)...), " · ")
}

func dressAlone(item *sdui.TreeItem, row sessionRow) {
	e := row.Alone
	if e == nil {
		return
	}
	dressEntry(item, *e)
	item.Help = strings.Join(nonEmpty(item.Label, row.AloneHelp, e.Why), " · ")
}

func queueMenu(e queueEntryRow) []sdui.MenuEntry {
	menu := []sdui.MenuEntry{{Action: actQueuePlace, Label: e.Line, Disabled: true}}
	for _, act := range e.Actions {
		menu = append(menu, sdui.MenuEntry{Action: queueMenuActions[act.ID], Label: act.Label, Disabled: !act.Enabled, Confirm: act.Confirm, Divider: act.Divider})
	}
	return menu
}

func entryFileItems(e queueEntryRow) []sdui.TreeItem {
	if len(e.Files) == 0 {
		return nil
	}
	base := queueFilePrefix + e.Session + ":" + e.ID + ":"
	var out []sdui.TreeItem
	if e.Why != "" {
		out = append(out, sdui.TreeItem{ID: base + "why", Label: e.Why, Icon: queueWhyIcon, Muted: true, Action: true})
	}
	for gi, g := range e.FileGroups {
		icon := queueFileIcon
		if g.Shared {
			icon = queueShareIcon
		}
		out = append(out, sdui.TreeItem{ID: base + "g" + strconv.Itoa(gi), Label: g.Title, Muted: true, Action: true})
		for fi, f := range g.Files {
			out = append(out, sdui.TreeItem{ID: base + strconv.Itoa(gi) + "/" + strconv.Itoa(fi), Label: f, Icon: icon, Truncate: "middle", Action: true})
		}
	}
	if e.More > 0 {
		out = append(out, sdui.TreeItem{ID: base + "more", Label: fmt.Sprintf(queueMore, e.More), Muted: true, Action: true})
	}
	return out
}

func queueRowTarget(id string) (string, bool) {
	if rest, ok := strings.CutPrefix(id, queueFilePrefix); ok {
		session, _, _ := strings.Cut(rest, ":")
		return session, true
	}
	if strings.HasPrefix(id, landLinePrefix) {
		return "", true
	}
	return "", false
}

func (a *app) queueAct(ctx context.Context, ev pluginsdk.UIEvent) error {
	id, err := itemID(ev)
	if err != nil {
		return err
	}
	if ev.Action == actQueuePlace {
		return nil
	}
	qid, e, ok := a.entryOfSession(id)
	if !ok {
		return errQueueUnknown
	}
	act, ok := entryAction(e, ev.Action)
	if !ok || !act.Enabled {
		return nil
	}
	args := queueEntryArgs{Queue: qid, Entry: e.ID, To: act.To}
	switch ev.Action {
	case actQueueUp, actQueueDown:
		return a.moveEntry(ctx, args)
	case actQueueSkip:
		return a.skipEntry(ctx, args)
	case actQueueRemove:
		return a.removeEntry(ctx, args)
	case actQueueFiles:
		return a.setCollapsed(ctx, ev.WorkspaceID, map[string]bool{expandPrefix + id: false})
	}
	return a.selectSession(ctx, ev.WorkspaceID, id)
}

func entryAction(e queueEntryRow, action string) (queueAction, bool) {
	for _, act := range e.Actions {
		if queueMenuActions[act.ID] == action {
			return act, true
		}
	}
	return queueAction{}, false
}

func (a *app) entryOfSession(session string) (string, queueEntryRow, bool) {
	for _, q := range a.queueList() {
		i := slices.IndexFunc(q.Entries, func(e queueEntry) bool { return e.Session == session && e.Mode != queueModeDirty })
		if i < 0 {
			continue
		}
		row := a.queueRowOf(q, time.Now().UTC())
		return q.ID, row.Entries[i], true
	}
	return "", queueEntryRow{}, false
}
