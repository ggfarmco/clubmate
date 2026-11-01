package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	sidebarSlot  = "cron.sidebar"
	mainSlot     = "cron.main"
	jobsList     = "jobs"
	historyID    = "history"
	actDuplicate = "duplicate"
	actRunMissed = "runMissed"
	actMissed    = "missed"
	clockLayout  = "15:04"
	dayLayout    = "Jan 2 15:04"
	nextLayout   = "Mon, Jan 2 · 15:04"
	whenLayout   = "Jan 2 15:04:05"
)

var missedOptions = []sdui.Option{
	{Value: missedOnce, Label: "Run once when Clubmate is back"},
	{Value: missedSkip, Label: "Skip it and wait for the next run"},
}

func sidebarView(jobs []*Job, selected string, now time.Time) sdui.Node {
	if len(jobs) == 0 {
		return sdui.Empty("No cron jobs yet. Agents schedule them for you.")
	}
	items := make([]sdui.Item, 0, len(jobs))
	for _, j := range jobs {
		icon, tone := automation.ListIcon(j.Enabled, j.History, "clock")
		items = append(items, sdui.Item{
			ID:     j.ID,
			Label:  j.Name,
			Detail: jobDetail(j, now),
			Icon:   icon,
			Tone:   tone,
			Menu:   jobMenu(j),
		})
	}
	return sdui.List(jobsList, items, selected)
}

func mainView(j *Job, titles map[string]string, open automation.Open, now time.Time) sdui.Node {
	if j == nil {
		return sdui.Empty("Select a cron job")
	}
	actions := []sdui.Node{
		automation.ActionNode(j.ID, automation.RunAction(sdui.VariantPrimary)),
		automation.ActionNode(j.ID, automation.PauseAction(j.Enabled)),
		automation.ActionNode(j.ID, automation.DetailAction{Action: actDuplicate, Label: "Duplicate", Icon: "plus.square.on.square", Variant: sdui.VariantGhost}),
	}
	if _, ok := titles[j.Owner]; ok {
		actions = append(actions, automation.ActionNode(j.ID, automation.DetailAction{Action: automation.ActReveal, Label: "Show session", Icon: "sparkles", Variant: sdui.VariantGhost}))
	}
	actions = append(actions, automation.ActionNode(j.ID, automation.DeleteAction("Delete cron job “"+j.Name+"”?")))
	badges := automation.StateBadges(j.Enabled, j.History)
	missed, last := missedRuns(j.History)
	if missed > 0 {
		badges = append(badges, sdui.HeaderBadge{Text: plural(missed, "missed 1 run", "missed %d runs"), Tone: "warning"})
	}
	header := sdui.Header(j.Name, sdui.HeaderProps{Icon: headerIcon(j), Badges: badges}, actions...)
	loc := jobLocation(j)
	rows := []sdui.Node{automation.Property("Schedule", automation.Capitalize(j.Schedule.Describe()), j.Schedule.Cron)}
	switch {
	case !j.Enabled:
		rows = append(rows, automation.Property("Paused", cmp.Or(j.LastError, "by you"), ""))
	case !j.NextRun.IsZero():
		rows = append(rows, automation.Property("Next run", j.NextRun.In(loc).Format(nextLayout), automation.Until(j.NextRun, now)))
	}
	if missed > 0 {
		text := last.In(loc).Format(nextLayout) + " — not run: Clubmate was closed or the Mac asleep"
		run := automation.ActionNode(j.ID, automation.DetailAction{Action: actRunMissed, Label: "Run it now", Variant: sdui.VariantSecondary})
		rows = append(rows, sdui.HStack(sdui.MutedText("Missed"), sdui.WithTone(sdui.Text(text), "warning"), run))
	}
	policy := sdui.WithAction(sdui.Select(j.ID+"/"+actMissed, missedOptions, cmp.Or(j.Missed, missedOnce)), actMissed)
	rows = append(rows, sdui.HStack(sdui.MutedText("When missed"), policy))
	if branch, ok := worktree(j); ok {
		rows = append(rows, automation.Property("Worktree", branch, ""))
	}
	rows = append(rows, automation.Property("Owner", automation.OwnerLabel(j.Owner, titles), ""))
	rows = append(rows, automation.Does(j.ID, j.Command, j.Args, titles, open)...)
	if len(j.History) == 0 {
		return sdui.VStack(header, automation.Properties(append(rows, automation.Property("Runs", noRuns(j, now), ""))...))
	}
	return sdui.VStack(header, automation.Properties(append(rows, automation.Property("Runs", "", ""))...), historyView(j, titles))
}

func missedRuns(history []Run) (int, time.Time) {
	runs := automation.NewestFirst(history)
	n := 0
	for n < len(runs) && runs[n].Reason == reasonMissed {
		n++
	}
	if n == 0 {
		return 0, time.Time{}
	}
	return n, runs[0].At
}

func worktree(j *Job) (string, bool) {
	if j.Command != "agents.new" {
		return "", false
	}
	var args struct {
		Branch string `json:"branch"`
	}
	_ = json.Unmarshal(j.Args, &args)
	switch {
	case args.Branch != "":
		return "new worktree " + args.Branch, true
	case j.Owner != "":
		return "the owner's worktree", true
	}
	return "the main checkout", true
}

func noRuns(j *Job, now time.Time) string {
	if !j.Enabled || j.NextRun.IsZero() {
		return "No runs yet."
	}
	return "No runs yet. The first one is " + automation.Until(j.NextRun, now) + "."
}

func jobDetail(j *Job, now time.Time) string {
	if !j.Enabled {
		return "paused"
	}
	if j.NextRun.IsZero() {
		return j.Schedule.Describe()
	}
	next := j.NextRun.In(jobLocation(j))
	if next.Sub(now) < 24*time.Hour {
		return next.Format(clockLayout)
	}
	return next.Format(dayLayout)
}

func headerIcon(j *Job) string {
	if j.Enabled {
		return "clock"
	}
	return "pause.circle"
}

func jobMenu(j *Job) []sdui.MenuEntry {
	toggle := sdui.MenuEntry{Action: automation.ActPause, Label: "Pause"}
	if !j.Enabled {
		toggle = sdui.MenuEntry{Action: automation.ActResume, Label: "Resume"}
	}
	return []sdui.MenuEntry{
		{Action: automation.ActRun, Label: "Run now"},
		toggle,
		{Action: actDuplicate, Label: "Duplicate"},
		{Action: automation.ActDelete, Label: "Delete", Confirm: "Delete cron job “" + j.Name + "”?"},
	}
}

func historyView(j *Job, titles map[string]string) sdui.Node {
	runs := automation.NewestFirst(j.History)
	loc := jobLocation(j)
	rows := make([]sdui.TableRow, 0, len(runs))
	for i, r := range runs {
		rows = append(rows, sdui.TableRow{ID: "r" + strconv.Itoa(i), Cells: []sdui.Cell{
			{Text: r.At.In(loc).Format(whenLayout)},
			{Text: r.Status, Tone: automation.StatusTone(r.Status)},
			{Text: automation.Names(cmp.Or(r.Reason, r.Result), titles)},
		}})
	}
	columns := []sdui.Column{
		{ID: "when", Title: "When", Width: 120},
		{ID: "status", Title: "Status", Width: 72},
		{ID: "detail", Title: "Reason / Result"},
	}
	return sdui.Scroll(sdui.Table(historyID, columns, rows, "", sdui.Sort{}))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}

func jobLocation(j *Job) *time.Location {
	loc, err := loadLocation(j.Schedule.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}
