package main

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"clubmate/pkg/sdui"
)

const (
	sidebarSlot = "cron.sidebar"
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
		sdui.Text("Owner: "+ownerLabel(j.Owner, titles)),
		historyView(j),
	)
}

func sortJobs(jobs []*Job) {
	slices.SortFunc(jobs, func(x, y *Job) int {
		return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID))
	})
}

func resolveSelection(jobs []*Job, selected string) string {
	if slices.ContainsFunc(jobs, func(j *Job) bool { return j.ID == selected }) {
		return selected
	}
	if len(jobs) > 0 {
		return jobs[0].ID
	}
	return ""
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
	if r, ok := lastRun(j); ok && r.Status == statusError {
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

func ownerLabel(owner string, titles map[string]string) string {
	if owner == "" {
		return ownerNone
	}
	return cmp.Or(titles[owner], owner)
}

func historyView(j *Job) sdui.Node {
	if len(j.History) == 0 {
		return sdui.MutedText("No runs yet")
	}
	runs := newestFirst(j.History)
	loc := jobLocation(j)
	rows := make([]sdui.TableRow, 0, len(runs))
	for i, r := range runs {
		rows = append(rows, sdui.TableRow{ID: "r" + strconv.Itoa(i), Cells: []sdui.Cell{
			{Text: r.At.In(loc).Format(whenLayout)},
			{Text: r.Status, Tone: statusTone(r.Status)},
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

func newestFirst(history []Run) []Run {
	runs := slices.Clone(history)
	slices.SortStableFunc(runs, func(x, y Run) int { return y.At.Compare(x.At) })
	return runs
}

func lastRun(j *Job) (Run, bool) {
	if len(j.History) == 0 {
		return Run{}, false
	}
	return slices.MaxFunc(j.History, func(x, y Run) int { return x.At.Compare(y.At) }), true
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

func jobLocation(j *Job) *time.Location {
	loc, err := loadLocation(j.Schedule.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}
