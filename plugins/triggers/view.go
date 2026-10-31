package main

import (
	"cmp"
	"slices"
	"strconv"

	"clubmate/pkg/sdui"
)

const (
	sidebarSlot = "triggers.sidebar"
	mainSlot    = "triggers.main"
	listID      = "triggers"
	historyID   = "history"
	badgeFailed = "failed"
	whenLayout  = "Jan 2 15:04:05"
)

func sidebarView(triggers []*Trigger, selected string) sdui.Node {
	if len(triggers) == 0 {
		return sdui.Empty("No triggers. Agents create them with triggers.add.")
	}
	items := make([]sdui.Item, 0, len(triggers))
	for _, t := range triggers {
		items = append(items, sdui.Item{
			ID:     t.ID,
			Label:  t.Name,
			Detail: triggerDetail(t),
			Icon:   triggerIcon(t),
			Badge:  triggerBadge(t),
			Menu:   triggerMenu(t),
		})
	}
	return sdui.List(listID, items, selected)
}

func mainView(t *Trigger, titles map[string]string) sdui.Node {
	if t == nil {
		return sdui.Empty("Select a trigger")
	}
	return sdui.VStack(
		sdui.Text(t.Name),
		sdui.Text("On: "+eventLine(t)),
		sdui.MutedText(t.Command+" "+string(t.Args)),
		sdui.Text(stateLine(t)),
		sdui.Text("Owner: "+ownerLabel(t.Owner, titles)),
		historyView(t),
	)
}

func resolveSelection(triggers []*Trigger, selected string) string {
	if slices.ContainsFunc(triggers, func(t *Trigger) bool { return t.ID == selected }) {
		return selected
	}
	if len(triggers) > 0 {
		return triggers[0].ID
	}
	return ""
}

func eventLine(t *Trigger) string {
	if len(t.Match) == 0 {
		return t.On
	}
	return t.On + " " + string(t.Match)
}

func triggerDetail(t *Trigger) string {
	if t.Enabled {
		return "on " + eventLine(t)
	}
	if t.LastError == "" {
		return "paused"
	}
	return "paused · " + t.LastError
}

func triggerIcon(t *Trigger) string {
	if t.Enabled {
		return "bolt"
	}
	return "pause.circle"
}

func triggerBadge(t *Trigger) string {
	if r, ok := lastRun(t); ok && r.Status == statusError {
		return badgeFailed
	}
	return ""
}

func triggerMenu(t *Trigger) []sdui.MenuEntry {
	toggle := sdui.MenuEntry{Action: "pause", Label: "Pause"}
	if !t.Enabled {
		toggle = sdui.MenuEntry{Action: "resume", Label: "Resume"}
	}
	return []sdui.MenuEntry{
		toggle,
		{Action: "delete", Label: "Delete", Confirm: "Delete trigger “" + t.Name + "”?"},
	}
}

func stateLine(t *Trigger) string {
	if t.Enabled {
		return "Active"
	}
	if t.LastError == "" {
		return "Paused"
	}
	return "Paused: " + t.LastError
}

func ownerLabel(owner string, titles map[string]string) string {
	if owner == "" {
		return ownerNone
	}
	return cmp.Or(titles[owner], owner)
}

func historyView(t *Trigger) sdui.Node {
	if len(t.History) == 0 {
		return sdui.MutedText("No runs yet")
	}
	runs := slices.Clone(t.History)
	slices.SortStableFunc(runs, func(x, y Run) int { return y.At.Compare(x.At) })
	rows := make([]sdui.TableRow, 0, len(runs))
	for i, r := range runs {
		rows = append(rows, sdui.TableRow{ID: "r" + strconv.Itoa(i), Cells: []sdui.Cell{
			{Text: r.At.Local().Format(whenLayout)},
			{Text: r.Topic},
			{Text: r.Status, Tone: statusTone(r.Status)},
			{Text: cmp.Or(r.Reason, r.Result)},
		}})
	}
	columns := []sdui.Column{
		{ID: "when", Title: "When", Width: 120},
		{ID: "event", Title: "Event", Width: 140},
		{ID: "status", Title: "Status", Width: 72},
		{ID: "detail", Title: "Reason / Result"},
	}
	return sdui.Scroll(sdui.Table(historyID, columns, rows, "", sdui.Sort{}))
}

func lastRun(t *Trigger) (Run, bool) {
	if len(t.History) == 0 {
		return Run{}, false
	}
	return slices.MaxFunc(t.History, func(x, y Run) int { return x.At.Compare(y.At) }), true
}

func statusTone(status string) string {
	switch status {
	case statusOK:
		return "ok"
	case statusError:
		return "danger"
	}
	return ""
}
