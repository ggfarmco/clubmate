package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	archiveSheetID    = "archiveSheet"
	archiveTickNode   = archiveSheetID + "/tick/"
	archiveFolderNode = archiveSheetID + "/folder/"

	actArchive        = "archive"
	actArchiveTick    = "archiveTick"
	actArchiveFolder  = "archiveFolder"
	actArchiveCancel  = "archiveCancel"
	actArchiveConfirm = "archiveConfirm"
	actArchiveClose   = "archiveClose"

	archiveSheetTitle = "Archive “%s”?"
	archiveDoneTitle  = "“%s” is archived"
	archiveLeadMany   = "All %d children are done. Archiving cleans up what the task left in each repo and moves its %s to Closed. Remote branches are never touched."
	archiveLeadOne    = "Its child is done. Archiving cleans up what the task left in its repo and moves its %s to Closed. Remote branches are never touched."
	archiveLeadHeld   = "Archiving cleans up what the task left in each repo and moves its %s to Closed. Remote branches are never touched."
	archiveRule       = "Transcripts are always kept. Restore in Closed brings the sessions back while their worktrees are on disk."
	archiveAnyway     = "Remove it anyway"
	archiveLose       = "Archive · lose %s"
	archiveShort      = "Archive"
	archiveButton     = "Archive task"
	archiveCloseOnly  = "Close sessions only"
	archiveBusy       = "Archiving…"
	archiveAlso       = "Also remove %s from %s"
	archiveAlsoNote   = "It was added for this task and no other session uses it. The files stay on disk."
	archiveNothing    = "Nothing to remove · sessions move to Closed"
	archiveNoLoss     = "nothing is lost"
	archiveLoses      = "%s lost"
	archiveRemoves    = "Removes %s"
	archiveCronRuns   = "%s runs in %s and pauses until you resume it in Cron."
	archiveMenu       = "Archive Task…"
	restoreMenu       = "Restore"
	archiveFooterOpen = "All children done · %s left behind"
	archiveFooterNone = "All children done"
	archiveWordJoin   = " · "
	impactFolderPart  = "alsoRemove/"
	impactRepoPart    = "repo/"
)

type archiveSheet struct {
	plan    archivePlan
	session *session
	ticked  map[string]bool
	folders map[string]bool
	busy    bool
	note    string
	kept    []string
}

func (a *app) archiveSheetCommand(ctx context.Context, cmd pluginsdk.CommandRun, s *session) (any, error) {
	if !a.userWord(cmd) {
		return nil, errArchiveUser
	}
	if err := a.openArchiveSheet(ctx, cmd.WorkspaceID, s); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, cmd.WorkspaceID)
}

func (a *app) openArchiveSheet(ctx context.Context, workspaceID string, s *session) error {
	plan, err := a.archivePlan(ctx, workspaceID, s)
	if err != nil {
		return err
	}
	sheet := &archiveSheet{plan: plan, session: s, ticked: map[string]bool{}, folders: map[string]bool{}}
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.sheetBusy() {
		p.folder, p.archive, p.cleanup = nil, sheet, nil
	}
	return nil
}

func isArchiveAction(action string) bool {
	switch action {
	case actArchiveTick, actArchiveFolder, actArchiveCancel, actArchiveConfirm, actArchiveClose:
		return true
	}
	return false
}

func (p *workspaceUI) sheetBusy() bool {
	return p.folder != nil && p.folder.busy || p.archive != nil && p.archive.busy || p.cleanup != nil && p.cleanup.busy
}

func (a *app) archiveSheetAct(ev pluginsdk.UIEvent) error {
	p := a.workspaceState(ev.WorkspaceID)
	a.uiMu.Lock()
	sheet := p.archive
	a.uiMu.Unlock()
	if sheet == nil {
		return nil
	}
	var blockers []folderSession
	var holds []string
	if ev.Action == actArchiveConfirm || ev.Action == actArchiveClose {
		blockers, holds = a.archiveHolds(sheet.session.snapshot(), a.subtree(sheet.session))
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.archive != sheet || sheet.busy {
		return nil
	}
	switch ev.Action {
	case actArchiveCancel:
		p.archive = nil
	case actArchiveTick:
		return sheet.tick(ev, archiveTickNode, sheet.ticked)
	case actArchiveFolder:
		return sheet.tick(ev, archiveFolderNode, sheet.folders)
	case actArchiveConfirm, actArchiveClose:
		if len(blockers) > 0 || len(holds) > 0 || len(sheet.kept) > 0 {
			return nil
		}
		choices := sheet.choices()
		if ev.Action == actArchiveClose {
			choices = nil
		}
		sheet.busy, sheet.note = true, ""
		go a.finishArchive(ev.WorkspaceID, sheet, choices)
	}
	return nil
}

func (s *archiveSheet) tick(ev pluginsdk.UIEvent, prefix string, into map[string]bool) error {
	path, ok := strings.CutPrefix(ev.NodeID, prefix)
	if !ok {
		return fmt.Errorf("%w: %s on %q", errUnknownAction, ev.Action, ev.NodeID)
	}
	var on bool
	if err := decode(ev.Value, &on); err != nil {
		return err
	}
	into[path] = on
	return nil
}

func (s *archiveSheet) choices() []archiveChoice {
	var out []archiveChoice
	for _, r := range s.plan.Repos {
		if s.removes(r) {
			out = append(out, archiveChoice{Path: r.State.Path, Seen: r.Seen, Anyway: r.Worktree.Action != stepRemove})
		}
	}
	for _, f := range s.plan.Folders {
		if s.folders[f.Path] {
			out = append(out, archiveChoice{Path: f.Path, Folder: true})
		}
	}
	return out
}

func (s *archiveSheet) removes(r archiveRepo) bool {
	return r.Worktree.Action == stepRemove || r.Anyway && s.ticked[r.State.Path]
}

func (s *archiveSheet) branchStep(r archiveRepo) archiveStep {
	if r.Worktree.Action != stepRemove && s.removes(r) {
		return r.IfRemoved
	}
	return r.Branch
}

func (a *app) finishArchive(workspaceID string, sheet *archiveSheet, choices []archiveChoice) {
	ctx, cancel := context.WithTimeout(context.Background(), folderRemoveTimeout)
	defer cancel()
	res, err := a.archive(ctx, workspaceID, sheet.session, choices, nil)
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	switch {
	case p.archive != sheet:
	case err != nil:
		sheet.busy, sheet.note = false, keptWhy(err)
	case len(res.Kept) > 0:
		sheet.busy, sheet.kept = false, res.Kept
	default:
		p.archive = nil
	}
	a.uiMu.Unlock()
	if err != nil {
		var fresh archivePlan
		if fresh, err = a.archivePlan(ctx, workspaceID, sheet.session); err == nil {
			a.uiMu.Lock()
			if p.archive == sheet {
				sheet.plan = fresh
			}
			a.uiMu.Unlock()
		}
	}
	a.renderLogged(workspaceID)
}

func canArchive(row sessionRow) bool {
	rec := row.Rec
	return ownsTask(rec) && rec.Task.archivable() && !rec.closed() && !turnActive(rec) && row.Children == ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (a *app) archiveSheetTree(p *workspaceUI) (sdui.Node, bool) {
	a.uiMu.Lock()
	sheet := p.archive
	a.uiMu.Unlock()
	if sheet == nil {
		return sdui.Node{}, false
	}
	blockers, holds := a.archiveHolds(sheet.session.snapshot(), a.subtree(sheet.session))
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	return sdui.VStack(sheet.view(blockers, holds)), true
}

func (s *archiveSheet) view(blockers []folderSession, holds []string) sdui.Node {
	if len(s.kept) > 0 {
		return s.keptView()
	}
	rows, bodies := s.rows()
	note, tone := s.footnote(blockers, holds)
	held := len(blockers) > 0 || len(holds) > 0
	children := []sdui.Node{
		sdui.Markdown(s.lead(held)),
		sdui.Text(s.summary()),
		sdui.Impact(archiveSheetID+"/impact", rows, bodies...),
		sdui.MutedText(archiveRule),
		sdui.HStack(s.cancelButton(), s.closeButton(held), s.archiveButton(held)),
	}
	return sdui.Sheet(archiveSheetID, sdui.SheetProps{
		Title: fmt.Sprintf(archiveSheetTitle, s.plan.Title), Icon: "trash", Cancel: actArchiveCancel, Note: note, NoteTone: tone,
	}, children...)
}

func (s *archiveSheet) keptView() sdui.Node {
	closeButton := sdui.WithVariant(sdui.WithAction(sdui.Button(archiveSheetID+"/close", folderLeftClose, ""), actArchiveCancel), sdui.VariantPrimary)
	return sdui.Sheet(archiveSheetID, sdui.SheetProps{
		Title: fmt.Sprintf(archiveDoneTitle, s.plan.Title), Icon: "trash", Cancel: actArchiveCancel,
	}, sdui.Markdown(strings.Join(s.kept, "\n\n")), sdui.HStack(sdui.WithShortcut(closeButton, "cmd+return")))
}

func (s *archiveSheet) lead(held bool) string {
	sessions := counted(s.plan.Sessions, "session")
	switch {
	case held:
		return fmt.Sprintf(archiveLeadHeld, sessions)
	case s.plan.Children == 1:
		return fmt.Sprintf(archiveLeadOne, sessions)
	}
	return fmt.Sprintf(archiveLeadMany, s.plan.Children, sessions)
}

func (s *archiveSheet) footnote(blockers []folderSession, holds []string) (string, string) {
	var why []string
	for _, b := range blockers {
		why = append(why, blockerText(b))
	}
	why = append(why, holds...)
	switch {
	case s.note != "":
		return s.note, "danger"
	case len(why) > 0:
		return fmt.Sprintf(folderHeld, strings.Join(why, ", ")), "danger"
	}
	return "", ""
}

func (s *archiveSheet) summary() string {
	trees, branches := 0, 0
	for _, r := range s.plan.Repos {
		if s.removes(r) {
			trees++
			if s.branchStep(r).Action == stepDelete {
				branches++
			}
		}
	}
	if trees == 0 {
		return archiveNothing
	}
	parts := []string{counted(trees, "worktree")}
	if branches > 0 {
		parts = append(parts, "deletes "+mergedBranches(branches))
	}
	cost := archiveNoLoss
	if lost := s.lost(); lost != "" {
		cost = fmt.Sprintf(archiveLoses, lost)
	}
	if closes := s.closes(); closes != "" {
		cost = strings.Join(nonEmpty(strings.TrimPrefix(cost, archiveNoLoss), closes), archiveWordJoin)
	}
	return fmt.Sprintf(archiveRemoves, strings.Join(parts, ", ")) + archiveWordJoin + cost
}

func (s *archiveSheet) closes() string {
	var words []string
	for _, r := range s.plan.Repos {
		if r.Words != "" && r.Anyway && s.ticked[r.State.Path] {
			words = append(words, r.Words)
		}
	}
	return strings.Join(words, ", ")
}

func mergedBranches(n int) string {
	return strconv.Itoa(n) + " " + plural(n, "merged branch", "merged branches")
}

func (s *archiveSheet) lost() string {
	var all folderWorktree
	for _, r := range s.plan.Repos {
		if s.removes(r) {
			all.Dirty += r.State.Dirty
			all.Ignored += r.State.Ignored
			all.Orphans += r.State.Orphans
		}
	}
	return lostWords(all, lostCommit)
}

func (s *archiveSheet) cancelButton() sdui.Node {
	n := sdui.WithAction(sdui.Button(archiveSheetID+"/cancel", "Cancel", ""), actArchiveCancel)
	if s.busy {
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *archiveSheet) closeButton(held bool) sdui.Node {
	n := sdui.WithVariant(sdui.WithAction(sdui.Button(archiveSheetID+"/closeOnly", archiveCloseOnly, ""), actArchiveClose), sdui.VariantGhost)
	if s.busy || held {
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *archiveSheet) archiveButton(held bool) sdui.Node {
	label, variant := archiveButton, sdui.VariantPrimary
	lost, closes := s.lost(), s.closes()
	switch {
	case lost != "" && closes != "":
		label, variant = fmt.Sprintf(archiveLose, lost)+archiveWordJoin+closes, sdui.VariantDanger
	case lost != "":
		label, variant = fmt.Sprintf(archiveLose, lost), sdui.VariantDanger
	case closes != "":
		label, variant = archiveShort+archiveWordJoin+closes, sdui.VariantDanger
	}
	n := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(archiveSheetID+"/archive", label, ""), actArchiveConfirm), variant), "cmd+return")
	switch {
	case s.busy:
		return sdui.WithLoading(n, archiveBusy)
	case held:
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *archiveSheet) rows() ([]sdui.ImpactRow, []sdui.Node) {
	var rows []sdui.ImpactRow
	var bodies []sdui.Node
	add := func(row sdui.ImpactRow, body ...sdui.Node) {
		rows = append(rows, row)
		bodies = append(bodies, sdui.WithID(sdui.WithGap(sdui.VStack(body...), 4), row.ID))
	}
	for i, r := range s.plan.Repos {
		add(sdui.ImpactRow{ID: impactRepoPart + strconv.Itoa(i), Icon: repoFolderIcon, Title: r.Name}, s.repoBody(r)...)
	}
	if jobs := s.namedJobs(); len(jobs) > 0 || s.plan.CronUnread {
		add(sdui.ImpactRow{ID: impactCron, Icon: "clock", Title: "Cron jobs", Count: strconv.Itoa(len(jobs))}, s.cronBody(jobs)...)
	}
	for _, f := range s.plan.Folders {
		box := sdui.WithAction(sdui.Checkbox(archiveFolderNode+f.Path, fmt.Sprintf(archiveAlso, f.Name, s.plan.Workspace), s.folders[f.Path]), actArchiveFolder)
		add(sdui.ImpactRow{ID: impactFolderPart + f.Name, Icon: repoFolderIcon, Title: "Folder"}, box, sdui.MutedText(archiveAlsoNote))
	}
	return rows, bodies
}

func (s *archiveSheet) namedJobs() []folderJob {
	var out []folderJob
	for _, r := range s.plan.Repos {
		if s.removes(r) {
			out = append(out, jobsOf(s.plan.Cron, r.State.Path)...)
		}
	}
	return out
}

func (s *archiveSheet) cronBody(jobs []folderJob) []sdui.Node {
	if s.plan.CronUnread {
		return []sdui.Node{sdui.WithTone(sdui.Text(folderCronLost), "warning")}
	}
	var body []sdui.Node
	for _, job := range jobs {
		body = append(body, sdui.Markdown(fmt.Sprintf(archiveCronRuns, "**"+mdPlain(job.Name)+"**", mdPlain(s.jobPlace(job)))))
	}
	return body
}

func (s *archiveSheet) jobPlace(job folderJob) string {
	for _, r := range s.plan.Repos {
		if r.State.Path == job.In {
			return worktreeLabel(r.Path, r.State)
		}
	}
	return job.In
}

func (s *archiveSheet) repoBody(r archiveRepo) []sdui.Node {
	tree := r.Worktree
	ticked := r.Anyway && s.ticked[r.State.Path]
	if ticked {
		tree.Why = ""
	}
	body := stepLines(tree)
	if r.Anyway {
		box := sdui.WithAction(sdui.Checkbox(archiveTickNode+r.State.Path, archiveAnyway, s.ticked[r.State.Path]), actArchiveTick)
		body = append(body, box)
	}
	if ticked && r.Lost != "" {
		body = append(body, sdui.WithTone(sdui.Text(fmt.Sprintf(folderLoses, r.Lost)), "danger"))
	}
	if ticked && r.Words != "" {
		body = append(body, sdui.WithTone(sdui.Text(r.Words), "danger"))
	}
	return append(body, stepLines(s.branchStep(r))...)
}

func stepLines(step archiveStep) []sdui.Node {
	title := sdui.Markdown("**" + mdPlain(step.Title) + "**")
	if step.Why == "" {
		return []sdui.Node{title}
	}
	why := sdui.MutedText(step.Why)
	if step.Tone != "" {
		why = sdui.WithTone(sdui.Text(step.Why), step.Tone)
	}
	return []sdui.Node{title, why}
}

func reposArchiveFooter(rec record, kids map[string]record) (string, sdui.Node, bool) {
	if !rec.Task.archivable() || rec.closed() || turnActive(rec) || !ownsTask(rec) {
		return "", sdui.Node{}, false
	}
	left := 0
	for _, r := range rec.Task.Repos {
		kid, ok := kids[r.Session]
		if !ok {
			continue
		}
		if turnActive(kid) {
			return "", sdui.Node{}, false
		}
		if kid.Worktree != "" && kid.Worktree != r.Path && pathExists(kid.Worktree) {
			left++
		}
	}
	text := archiveFooterNone
	if left > 0 {
		text = fmt.Sprintf(archiveFooterOpen, counted(left, "worktree"))
	}
	button := sdui.WithVariant(sdui.WithAction(sdui.Button(reposCardID(rec.ID)+"/archive", "Archive task…", ""), actArchive), sdui.VariantSecondary)
	bar := sdui.WithGap(sdui.HStack(button), reposGap)
	bar.ID = reposCardID(rec.ID) + "/footer"
	return text, bar, true
}
