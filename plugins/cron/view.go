package main

import (
	"cmp"
	"strconv"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	sidebarSlot = "cron.sidebar"
	ownerNode   = "/owner"
	actReveal   = "reveal"
	mainSlot    = "cron.main"
	jobsList    = "jobs"
	historyID   = "history"
	badgeFailed = "failed"
	clockLayout = "15:04"
	dayLayout   = "Jan 2 15:04"
	whenLayout  = "Jan 2 15:04:05"
)

func sidebarView(jobs []*Job, titles map[string]string, selected string, now time.Time) sdui.Node {
	if len(jobs) == 0 {
		return sdui.Empty("No cron jobs. Agents create them with cron.add.")
	}
	items := make([]sdui.Item, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, sdui.Item{
			ID:     j.ID,
			Label:  j.Name,
			Detail: jobDetail(j, now),
			Icon:   jobIcon(j),
			Badge:  jobBadge(j),
			Menu:   jobMenu(j),
		})
	}
	return sdui.List(jobsList, items, selected)
}

func mainView(j *Job, titles map[string]string) sdui.Node {
	if j == nil {
		return sdui.Empty("Select a cron job")
	}
	return sdui.VStack(
		sdui.Text(j.Name),
		sdui.MutedText(j.Command+" "+string(j.Args)),
		sdui.Text("Schedule: "+j.Schedule.Describe()),
		sdui.Text(stateLine(j)),
		sdui.Text("Missed runs: "+j.Missed),
		ownerRow(j, titles),
		historyView(j),
	)
}

func ownerRow(j *Job, titles map[string]string) sdui.Node {
	owner := sdui.Text("Owner: " + automation.OwnerLabel(j.Owner, titles))
	if _, ok := titles[j.Owner]; !ok {
		return owner
	}
	return sdui.HStack(owner, sdui.WithAction(sdui.Button(j.ID+ownerNode, "Show Session", "sparkles"), actReveal))
}

func jobDetail(j *Job, now time.Time) string {
	if !j.Enabled {
		if j.LastError == "" {
			return "paused"
		}
		return "paused · " + j.LastError
	}
	if j.NextRun.IsZero() {
		return j.Schedule.Describe()
	}
	loc := jobLocation(j)
	next, today := j.NextRun.In(loc), now.In(loc)
	layout := dayLayout
	if next.YearDay() == today.YearDay() && next.Year() == today.Year() {
		layout = clockLayout
	}
	return j.Schedule.Describe() + " · next " + next.Format(layout)
}

func jobIcon(j *Job) string {
	if j.Enabled {
		return "clock"
	}
	return "pause.circle"
}

func jobBadge(j *Job) string {
	if r, ok := automation.LastRun(j.History); ok && r.Status == automation.StatusError {
		return badgeFailed
	}
	return ""
}

func jobMenu(j *Job) []sdui.MenuEntry {
	toggle := sdui.MenuEntry{Action: "pause", Label: "Pause"}
	if !j.Enabled {
		toggle = sdui.MenuEntry{Action: "resume", Label: "Resume"}
	}
	return []sdui.MenuEntry{
		toggle,
		{Action: "run", Label: "Run Now"},
		{Action: "delete", Label: "Delete", Confirm: "Delete cron job “" + j.Name + "”?"},
	}
}

func stateLine(j *Job) string {
	if j.Enabled {
		return "Next run: " + j.NextRun.In(jobLocation(j)).Format(dayLayout)
	}
	if j.LastError == "" {
		return "Paused"
	}
	return "Paused: " + j.LastError
}

func historyView(j *Job) sdui.Node {
	if len(j.History) == 0 {
		return sdui.MutedText("No runs yet")
	}
	runs := automation.NewestFirst(j.History)
	loc := jobLocation(j)
	rows := make([]sdui.TableRow, 0, len(runs))
	for i, r := range runs {
		rows = append(rows, sdui.TableRow{ID: "r" + strconv.Itoa(i), Cells: []sdui.Cell{
			{Text: r.At.In(loc).Format(whenLayout)},
			{Text: r.Status, Tone: automation.StatusTone(r.Status)},
			{Text: cmp.Or(r.Reason, r.Result)},
		}})
	}
	columns := []sdui.Column{
		{ID: "when", Title: "When", Width: 120},
		{ID: "status", Title: "Status", Width: 72},
		{ID: "detail", Title: "Reason / Result"},
	}
	return sdui.Scroll(sdui.Table(historyID, columns, rows, "", sdui.Sort{}))
}

func jobLocation(j *Job) *time.Location {
	loc, err := loadLocation(j.Schedule.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}
