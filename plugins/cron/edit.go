package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	agentNewCommand = "agents.new"
	actSave         = "save"
	fieldName       = "name"
	fieldWhen       = "schedule"
	fieldZone       = "timezone"
	fieldPrompt     = "prompt"
	fieldBranch     = "branch"
	fieldArgs       = "args"
	fieldMissed     = "missed"
	promptLines     = 6
	argsLines       = 4
)

var errNoName = errors.New("give the job a name")

type draft struct {
	job    string
	values map[string]string
	err    string
}

func editView(j *Job, d draft) sdui.Node {
	values := editValues(j)
	if d.job == j.ID {
		values = d.values
	}
	save := sdui.WithShortcut(sdui.WithVariant(sdui.Button(j.ID+"/submit", "Save", "checkmark"), sdui.VariantPrimary), "cmd+s")
	cancel := automation.ActionNode(j.ID, automation.DetailAction{Action: automation.ActEdit, Label: "Cancel", Variant: sdui.VariantSecondary, Shortcut: "e"})
	header := sdui.Header(j.Name, sdui.HeaderProps{Icon: "pencil"}, save, cancel)
	field := func(key, id, placeholder string) sdui.Node {
		return sdui.HStack(sdui.MutedText(key), sdui.TextField(id, values[id], placeholder))
	}
	rows := []sdui.Node{
		field("Name", fieldName, "What the job does"),
		field("Schedule", fieldWhen, "0 9 * * 1-5, 15m or 2026-11-02T09:00:00+01:00"),
		field("Time zone", fieldZone, "Local time; used by cron expressions"),
	}
	switch key := promptKey(j); {
	case key != "":
		rows = append(rows, sdui.HStack(sdui.MutedText("Prompt"), sdui.WithLines(sdui.TextField(fieldPrompt, values[fieldPrompt], "What the agent should do"), promptLines)))
	default:
		rows = append(rows, sdui.HStack(sdui.MutedText("Arguments"), sdui.WithLines(sdui.TextField(fieldArgs, values[fieldArgs], "{}"), argsLines)))
	}
	if j.Command == agentNewCommand {
		empty := "the main checkout"
		if j.Owner != "" {
			empty = "the owner's worktree"
		}
		rows = append(rows, field("Worktree", fieldBranch, "New worktree branch; empty for "+empty))
	}
	rows = append(rows, sdui.HStack(sdui.MutedText("When missed"), sdui.Select(fieldMissed, missedOptions, cmp.Or(values[fieldMissed], missedOnce))))
	if d.job == j.ID && d.err != "" {
		rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.WithTone(sdui.Text(d.err), "danger")))
	}
	form := sdui.WithAction(sdui.Form(j.ID+"/"+actSave, append([]sdui.Node{header}, automation.Properties(rows...))...), actSave)
	return sdui.Scroll(form)
}

func editValues(j *Job) map[string]string {
	fields := argFields(j)
	values := map[string]string{
		fieldName:   j.Name,
		fieldWhen:   scheduleText(j),
		fieldZone:   j.Schedule.Timezone,
		fieldMissed: cmp.Or(j.Missed, missedOnce),
	}
	if key := promptKey(j); key != "" {
		values[fieldPrompt], _ = fields[key].(string)
	} else {
		values[fieldArgs] = argsText(j.Args)
	}
	values[fieldBranch], _ = fields["branch"].(string)
	return values
}

func scheduleText(j *Job) string {
	switch s := j.Schedule; {
	case s.Cron != "":
		return s.Cron
	case s.Every != "":
		return s.Every
	case !s.At.IsZero():
		return s.At.In(jobLocation(j)).Format(time.RFC3339)
	}
	return ""
}

func argsText(raw json.RawMessage) string {
	var out bytes.Buffer
	if json.Indent(&out, orEmpty(raw), "", "  ") != nil {
		return string(raw)
	}
	return out.String()
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

func argFields(j *Job) map[string]any {
	fields := map[string]any{}
	_ = json.Unmarshal(j.Args, &fields)
	if fields == nil {
		fields = map[string]any{}
	}
	return fields
}

func promptKey(j *Job) string {
	switch j.Command {
	case agentNewCommand:
		return "prompt"
	case automation.PromptCommand:
		return "text"
	}
	fields := argFields(j)
	for _, key := range []string{"prompt", "text"} {
		if _, ok := fields[key].(string); ok {
			return key
		}
	}
	return ""
}

func editArgs(j *Job, values map[string]string) (jobArgs, error) {
	args := jobArgs{ID: j.ID, Missed: values[fieldMissed]}
	args.Name = strings.TrimSpace(values[fieldName])
	if args.Name == "" {
		return jobArgs{}, errNoName
	}
	when := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(values[fieldWhen]), "every "))
	zone := strings.TrimSpace(values[fieldZone])
	if when != scheduleText(j) || zone != j.Schedule.Timezone {
		if _, err := time.ParseDuration(when); err == nil {
			args.Every = when
		} else if _, err := time.Parse(time.RFC3339, when); err == nil {
			args.At = when
		} else {
			args.Cron = when
		}
		if args.Cron != "" {
			args.Timezone = zone
		}
	}
	raw, err := editedArgs(j, values)
	if err != nil {
		return jobArgs{}, err
	}
	if raw != nil && !sameJSON(raw, j.Args) {
		args.Args = string(raw)
	}
	return args, nil
}

func editedArgs(j *Job, values map[string]string) (json.RawMessage, error) {
	key := promptKey(j)
	if key == "" {
		text := strings.TrimSpace(values[fieldArgs])
		var fields map[string]any
		if err := json.Unmarshal([]byte(cmp.Or(text, "{}")), &fields); err != nil {
			return nil, fmt.Errorf("arguments: not a JSON object: %w", err)
		}
		return json.Marshal(fields)
	}
	fields := argFields(j)
	fields[key] = values[fieldPrompt]
	if j.Command == agentNewCommand {
		if branch := strings.TrimSpace(values[fieldBranch]); branch != "" {
			fields["branch"] = branch
		} else {
			delete(fields, "branch")
		}
	}
	return json.Marshal(fields)
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(orEmpty(b), &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}

func (a *app) save(ctx context.Context, ev pluginsdk.UIEvent, id string) error {
	var values map[string]string
	if err := json.Unmarshal(ev.Value, &values); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	a.mu.Lock()
	j, ok := a.jobs[id]
	if ok {
		j = j.Clone()
	}
	a.mu.Unlock()
	if !ok || j.Workspace != ev.WorkspaceID {
		return fmt.Errorf("cron job %q not found", id)
	}
	args, err := editArgs(j, values)
	if err == nil {
		raw, _ := json.Marshal(args)
		_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: "cron.update", WorkspaceID: ev.WorkspaceID, Args: raw})
	}
	a.mu.Lock()
	if err != nil {
		a.drafts[ev.WorkspaceID] = draft{job: id, values: values, err: automation.Capitalize(err.Error()) + "."}
	} else {
		delete(a.drafts, ev.WorkspaceID)
	}
	a.mu.Unlock()
	if err == nil {
		if err := automation.Close(ctx, a.host, automation.ActEdit, ev.WorkspaceID); err != nil {
			return err
		}
	}
	a.registry.Invalidate(ev.WorkspaceID)
	a.registry.Flush(ctx)
	return nil
}
