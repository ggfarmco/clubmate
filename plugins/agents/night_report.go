package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/plugins/agents/acp"
)

const (
	nightTitle = "Night review"

	reportDir          = "reports"
	reportExt          = ".md"
	reportMessageID    = "night-report"
	reportQuiet        = "Nothing to change tonight."
	reportNone         = "Nothing to change"
	reportNoLanding    = "Nothing landed, see the report"
	reportCut          = "\n… %d more bytes cut"
	reportMinFence     = 3
	reportRefused      = "- Refused: %d"
	reportStopped      = "Stopped: "
	reportEarly        = ", stopped early"
	reportFromChat     = ". Undo from any chat"
	reportMaxLines     = 10
	reportWhyRunes     = 200
	reportDiffCap      = 4 << 10
	reportMore         = "- +%d more"
	reportMoreProblems = "- +%d more dropped or skipped"
	reportProblems     = 3
	reportMemory       = "Undo: tonight's memory cleanup"
	reportFold         = "Undo: %d changes"
	reportUndo         = "Undo: "

	selfKindReport = "report"
)

type nightResult struct {
	Applied []applied
	Memory  []selfChange
	Stuck   []string
	Skipped []string
	Diffs   map[string]string
	Refused int
}

type reportUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	acp.ContentChunk
}

func renderReport(date string, res nightResult) string {
	problems, landed := problemLines(res), landedLines(res)
	if len(problems)+len(landed) == 0 {
		return reportQuiet
	}
	if len(problems) > reportProblems {
		problems = append(slices.Clip(problems[:reportProblems-1]), fmt.Sprintf(reportMoreProblems, len(problems)-reportProblems+1))
	}
	if room := reportMaxLines - 2 - len(problems); len(landed) > room {
		landed = append(slices.Clip(landed[:room-1]), fmt.Sprintf(reportMore, len(landed)-room+1))
	}
	parts := []string{"**" + nightTitle + ", " + date + "**", strings.Join(slices.Concat(problems, landed), "\n")}
	for _, c := range res.changes() {
		if diff := res.Diffs[c.ID]; diff != "" {
			parts = append(parts, diffBlock(diff))
		}
	}
	return strings.Join(parts, "\n\n")
}

func diffBlock(diff string) string {
	shown := capBytes(diff, reportDiffCap)
	if cut := len(diff) - len(shown); cut > 0 {
		shown += fmt.Sprintf(reportCut, cut)
	}
	fence := strings.Repeat("`", longestRun(shown)+1)
	return fence + "diff\n" + shown + "\n" + fence
}

func longestRun(text string) int {
	longest := reportMinFence - 1
	for _, run := range strings.FieldsFunc(text, func(r rune) bool { return r != '`' }) {
		longest = max(longest, len(run))
	}
	return longest
}

func reportItems(res nightResult) []openItem {
	var edits, reverts []selfChange
	for _, c := range res.changes() {
		if c.Kind == selfKindRevert {
			reverts = append(reverts, c)
		} else {
			edits = append(edits, c)
		}
	}
	items := slices.Concat(undoItems(edits, maxEditsPerNight), undoItems(reverts, 1))
	if len(res.Memory) == 0 {
		return items
	}
	return append(items, openItem{Kind: itemOffer, Label: reportMemory, Quote: res.Memory[0].Summary, Changes: changeIDs(res.Memory)})
}

func (a *app) reportNight(ctx context.Context, date, workspace string, host *session, res nightResult, stop error) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	a.refreshSettings(ctx)
	note := notifyPost{WorkspaceID: workspace, Title: nightTitle, Message: res.message(stop), Kind: notifyDone}
	if stop != nil && !res.landed() {
		a.notes.post(ctx, note)
		return
	}
	text := renderReport(date, res)
	if err := a.self.saveReport(ctx, date, workspace, text); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night report:", err)
	}
	if host != nil {
		host.notice(nightTitle, date)
		host.recordReport(text)
		if items := reportItems(res); len(items) > 0 {
			host.postOpen(items, nil, itemOpen)
		}
		note.Session = host.snapshot().ID
	}
	a.notes.post(ctx, note)
}

func (a *app) reportRestart(ctx context.Context, ledger nightLedger, refused int) {
	workspaces, err := a.nightWorkspaces(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: night restart report:", err)
		return
	}
	if len(workspaces) == 0 {
		return
	}
	res, err := a.landedSince(ctx, ledger.Base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: night restart report:", err)
	}
	if !res.landed() {
		a.notes.post(ctx, notifyPost{WorkspaceID: workspaces[0].ID, Title: nightTitle, Message: reportStopped + errNightRestart.Error(), Kind: notifyDone})
		return
	}
	res.Refused = refused
	res.Skipped = append(res.Skipped, nightEndedEarly+errNightRestart.Error()+reportFromChat)
	a.reportNight(ctx, ledger.Date, workspaces[0].ID, nil, res, errNightRestart)
}

func (a *app) landedSince(ctx context.Context, base string) (nightResult, error) {
	var res nightResult
	if base == "" {
		return res, nil
	}
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	list, err := a.self.historyOf(ctx, base+"..HEAD")
	if err != nil {
		return res, err
	}
	var changes []selfChange
	for _, c := range slices.Backward(list) {
		if c.Kind == selfKindMemory {
			res.Memory = append(res.Memory, c)
		} else {
			changes = append(changes, c)
		}
	}
	res.Applied = []applied{{Changes: changes}}
	a.addDiffs(ctx, changes, &res)
	return res, nil
}

func (x *selfStore) saveReport(ctx context.Context, date, workspace, text string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.settle(ctx); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(x.dir, reportDir, date+reportExt), []byte(text+"\n")); err != nil {
		return err
	}
	return x.record(ctx, changeMessage(nightTitle+" "+date, "", selfKindReport, "", workspace))
}

func (x *selfStore) changeDiff(ctx context.Context, id string) (string, error) {
	return x.git(ctx, "show", "--format=", "--no-color", "--no-ext-diff", "--no-textconv", id, "--", selfPrompt, selfSkillsDir)
}

func (s *session) recordReport(text string) {
	s.mu.Lock()
	s.loadLocked()
	s.recordLocked(actorClubmate, kindUpdate, reportUpdate{
		SessionUpdate: acp.UpdateAgentMessageChunk,
		ContentChunk:  acp.ContentChunk{Content: acp.ContentBlock{Type: "text", Text: text}, MessageID: reportMessageID},
	})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func (r nightResult) changes() []selfChange {
	var out []selfChange
	for _, got := range r.Applied {
		out = append(out, got.Changes...)
	}
	return out
}

func (r nightResult) dropped() []string {
	var out []string
	for _, got := range r.Applied {
		out = append(out, got.Dropped...)
	}
	return out
}

func (r nightResult) landed() bool {
	return r.count() > 0
}

func (r nightResult) count() int {
	return len(r.Memory) + len(r.Stuck) + len(r.changes())
}

func (r nightResult) message(stop error) string {
	n := r.count()
	switch {
	case n == 0 && stop != nil:
		return reportStopped + titleOf(stop.Error())
	case n == 0 && len(r.Skipped)+len(r.dropped())+r.Refused > 0:
		return reportNoLanding
	case n == 0:
		return reportNone
	case stop != nil:
		return counted(n, "change") + reportEarly
	}
	return counted(n, "change")
}

func problemLines(res nightResult) []string {
	var lines []string
	if res.Refused > 0 {
		lines = append(lines, fmt.Sprintf(reportRefused, res.Refused))
	}
	for _, dropped := range res.dropped() {
		lines = append(lines, "- Dropped: "+dropped)
	}
	for _, skipped := range res.Skipped {
		lines = append(lines, "- Skipped: "+skipped)
	}
	return lines
}

func landedLines(res nightResult) []string {
	var lines []string
	for _, c := range res.changes() {
		lines = append(lines, changeLine(c))
	}
	for _, c := range res.Memory {
		lines = append(lines, "- Memory: "+c.Summary)
	}
	for _, stuck := range res.Stuck {
		lines = append(lines, "- Memory: "+stuck)
	}
	return lines
}

func changeLine(c selfChange) string {
	if c.Kind == selfKindRevert {
		return "- Reverted: " + c.Summary
	}
	line := "- Changed: " + c.Summary
	if why := oneLine(c.Why); why != "" {
		line += ". Why: " + clipRunes(why, reportWhyRunes)
	}
	return line
}

func undoItems(changes []selfChange, room int) []openItem {
	var items []openItem
	for i, c := range changes {
		if i == room-1 && len(changes) > room {
			rest := changes[i:]
			return append(items, openItem{Kind: itemOffer, Label: fmt.Sprintf(reportFold, len(rest)), Quote: c.Summary, Changes: changeIDs(rest)})
		}
		items = append(items, openItem{Kind: itemOffer, Label: reportUndo + c.Summary, Description: oneLine(c.Why), Quote: c.Summary, Changes: []string{c.ID}})
	}
	return items
}

func changeIDs(changes []selfChange) []string {
	ids := make([]string, 0, len(changes))
	for _, c := range changes {
		ids = append(ids, c.ID)
	}
	return ids
}
