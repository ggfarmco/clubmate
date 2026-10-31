package main

import (
	"cmp"
	"strconv"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
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
		sdui.Text("Owner: "+automation.OwnerLabel(t.Owner, titles)),
		historyView(t),
	)
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
	if r, ok := automation.LastRun(t.History); ok && r.Status == automation.StatusError {
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

func historyView(t *Trigger) sdui.Node {
	if len(t.History) == 0 {
		return sdui.MutedText("No runs yet")
	}
	runs := automation.NewestFirst(t.History)
	rows := make([]sdui.TableRow, 0, len(runs))
	for i, r := range runs {
		rows = append(rows, sdui.TableRow{ID: "r" + strconv.Itoa(i), Cells: []sdui.Cell{
			{Text: r.At.Local().Format(whenLayout)},
			{Text: r.Topic},
			{Text: r.Status, Tone: automation.StatusTone(r.Status)},
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
