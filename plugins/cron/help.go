package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/automation"
)

const (
	kindsID   = "kinds"
	askTitle  = "New cron job"
	helpMeta  = "a cron job does something on a clock · triggers react to events · a job can start an agent, prompt a session, run a graph or emit an event a trigger listens to"
	helpKinds = "Can run on · ↩ asks an agent for a job on the selected schedule"
	askBase   = "Help me add a cron job in this workspace."
	askKind   = "Help me add a cron job in this workspace that runs %s (%s)."
	askRest   = " Ask me what it should do and when, and anything else you need to know (which session, worktree), then add it with cron_add and owner none so it outlives this session, and tell me its schedule and what it does in one line each."
)

type scheduleKind struct {
	id      string
	label   string
	example string
	phrase  string
	field   string
}

var kinds = []scheduleKind{
	{"cron", "on a calendar", "weekdays 9:00 · 0 9 * * 1-5", "on a calendar, in a time zone", "cron"},
	{"every", "on an interval", "every 15 minutes", "on an interval of a minute or more", "every"},
	{"at", "once", "Nov 5 09:00 · in 30 minutes", "once, at a time or after a delay", "at or in"},
}

var helpFacts = [][2]string{
	{"Can do", "start an agent · prompt a session (skipped while it is busy) · run a graph · emit an event · any command an agent may run; a destructive one asks you first"},
	{"Missed runs", "while Clubmate was closed or the Mac asleep, the job's When missed decides: run once when back, or skip it"},
	{"Owner", "an agent's job is deleted with its session unless it was added with owner none; failures go to the owner, else to the inbox"},
	{"Worktree", "a job that starts an agent can give it a new worktree, else it works in the owner's worktree or the main checkout"},
	{"Overlap", "a run never starts while the previous one runs; that run is skipped, not queued"},
	{"Failures", "3 errors in a row pause the job and notify you"},
}

func kindOf(s Schedule) string {
	switch {
	case s.Cron != "":
		return "cron"
	case s.Every != "":
		return "every"
	case !s.At.IsZero():
		return "at"
	}
	return ""
}

func helpView(jobs []*Job) sdui.Node {
	counts := map[string]int{}
	for _, j := range jobs {
		counts[kindOf(j.Schedule)]++
	}
	rows := make([]sdui.TableRow, 0, len(kinds))
	for _, k := range kinds {
		rows = append(rows, sdui.TableRow{ID: k.id, Cells: []sdui.Cell{
			{Text: k.label}, {Text: k.example}, {Text: automation.Yours(counts[k.id], "job")}, {Action: automation.ActAsk, Label: "Ask an agent"},
		}})
	}
	columns := []sdui.Column{
		{ID: "kind", Title: "Schedule", Width: 140},
		{ID: "example", Title: "Example"},
		{ID: "yours", Title: "Yours", Width: 80, Priority: 1},
		{ID: "ask", Title: "", Width: 110},
	}
	body := sdui.VStack(
		ghostFlow(),
		sdui.MutedText(helpKinds),
		sdui.Table(kindsID, columns, rows, "", sdui.Sort{}),
		automation.Facts(helpFacts),
	)
	return sdui.VStack(automation.HelpHeader("How cron works", helpMeta, "Ask an agent to add one"), sdui.Scroll(body))
}

func askPrompt(kind string) string {
	i := slices.IndexFunc(kinds, func(k scheduleKind) bool { return k.id == kind })
	if i < 0 {
		return askBase + askRest
	}
	return fmt.Sprintf(askKind, kinds[i].phrase, kinds[i].field) + askRest
}

func (a *app) askAgent(ctx context.Context, workspace, kind string) error {
	return agentcall.Ask(ctx, a.host, workspace, askTitle, askPrompt(kind))
}

func (a *app) askFromRow(ctx context.Context, ev pluginsdk.UIEvent) error {
	if ev.Action != automation.ActAsk {
		return nil
	}
	var kind string
	if err := json.Unmarshal(ev.Value, &kind); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return a.askAgent(ctx, ev.WorkspaceID, kind)
}
