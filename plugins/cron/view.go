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
	actions := []sdui.Node{
		automation.ActionNode(j.ID, automation.DetailAction{Action: automation.ActRun, Label: "Run now", Icon: "play.fill", Variant: sdui.VariantPrimary, Shortcut: "cmd+r"}),
		automation.ActionNode(j.ID, automation.PauseAction(j.Enabled)),
	}
	if _, ok := titles[j.Owner]; ok {
		actions = append(actions, automation.ActionNode(j.ID, automation.DetailAction{Action: automation.ActReveal, Label: "Show session", Icon: "sparkles", Variant: sdui.VariantGhost}))
	}
	actions = append(actions, automation.ActionNode(j.ID, automation.DeleteAction("Delete cron job “"+j.Name+"”?")))
	header := sdui.Header(j.Name, sdui.HeaderProps{Icon: jobIcon(j), Badges: automation.StateBadges(j.Enabled, j.History)}, actions...)
	rows := []sdui.Node{
		automation.Property("Schedule", j.Schedule.Describe(), j.Schedule.Cron),
		automation.Property(stateKey(j), stateValue(j), ""),
		automation.Property("Missed runs", j.Missed, ""),
		automation.Property("Owner", automation.OwnerLabel(j.Owner, titles), ""),
	}
	rows = append(rows, automation.Does(j.Command, j.Args)...)
	if len(j.History) == 0 {
		return sdui.VStack(header, automation.Properties(append(rows, automation.Property("Runs", "No runs yet. Run now with ⌘R.", ""))...))
	}
	return sdui.VStack(header, automation.Properties(append(rows, automation.Property("Runs", "", ""))...), historyView(j))
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

func stateKey(j *Job) string {
	if j.Enabled {
		return "Next run"
	}
	return "Paused"
}

func stateValue(j *Job) string {
	if j.Enabled {
		return j.NextRun.In(jobLocation(j)).Format(dayLayout)
	}
	return cmp.Or(j.LastError, "by the user")
}

func historyView(j *Job) sdui.Node {
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
