package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	cleanupSheetID    = "cleanupSheet"
	cleanupTickNode   = cleanupSheetID + "/tick/"
	cleanupGroupNode  = cleanupSheetID + "/group/"
	cleanupShowNode   = cleanupSheetID + "/show/"
	cleanupRowsShown  = 25
	cleanupRead       = 2 * time.Minute
	cleanupRowGap     = 6
	cleanupAllFolders = ""
	cleanupCronNamed  = 3

	actCleanupTick    = "cleanupTick"
	actCleanupGroup   = "cleanupGroup"
	actCleanupShow    = "cleanupShow"
	actCleanupFilter  = "cleanupFilter"
	actCleanupFolder  = "cleanupFolder"
	actCleanupCancel  = "cleanupCancel"
	actCleanupConfirm = "cleanupConfirm"

	cleanupLead        = "Grouped by what removing them costs. Live worktrees and any open in Code are never listed."
	cleanupPlaceholder = "Filter by folder, branch or task"
	cleanupAllWord     = "all folders"
	cleanupButton      = "Remove %d"
	cleanupLose        = "Remove %d · lose %s"
	cleanupBusy        = "Removing %d of %d…"
	cleanupReading     = "Reading %d of %d…"
	cleanupRemoves     = "Removes %s"
	cleanupNothing     = "Nothing ticked"
	cleanupDoneTitle   = "Cleaned up %s"
	cleanupKeptTitle   = "%d stayed"
	cleanupMore        = "%d more"
	cleanupShowMore    = "Show %d more · %d left"
	cleanupWillLose    = "%s will be lost"
	cleanupStays       = "%s · branch stays"
	cleanupHeldIgnored = "%s hold ignored files, not ticked"
	cleanupHeldOpen    = "%s with an open session, tick one at a time"
	cleanupUnread      = "reading…"
	cleanupEmpty       = "Nothing to clean up: no merged, idle or hand-made worktree is left."
	cleanupCronOne     = "1 Cron job runs in a worktree on this list and pauses with it: %s"
	cleanupCronMany    = "%d Cron jobs run in worktrees on this list and pause with them: %s"
	cleanupCronMore    = " and %d more"
)

type cleanupSheet struct {
	plan     cleanupPlan
	ticked   map[string]bool
	chosen   map[string]bool
	shown    map[string]int
	queue    []string
	reading  bool
	inflight int
	filter   string
	folder   string
	busy     bool
	done     int
	total    int
	note     string
	kept     []string
	finished int
}

func (a *app) cleanupSheetCommand(ctx context.Context, cmd pluginsdk.CommandRun, args cleanupArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errCleanupUser
	}
	if err := a.openCleanupSheet(ctx, cmd.WorkspaceID, args.Folder); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, cmd.WorkspaceID)
}

func (a *app) openCleanupSheet(ctx context.Context, workspaceID, folder string) error {
	if folder != "" {
		var err error
		if folder, err = a.workspaceFolder(ctx, workspaceID, folder); err != nil {
			return err
		}
	}
	plan, err := a.cleanupPlan(ctx, workspaceID, "")
	if err != nil {
		return err
	}
	sheet := &cleanupSheet{plan: plan, ticked: map[string]bool{}, chosen: map[string]bool{}, shown: map[string]int{}, folder: folder}
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.sheetBusy() {
		return nil
	}
	p.folder, p.archive, p.cleanup = nil, nil, sheet
	a.queueRead(workspaceID, sheet, sheet.wanted())
	return nil
}

func (s *cleanupSheet) wanted() []string {
	out := slices.DeleteFunc(s.plan.preTicked(), func(path string) bool { return !s.visible(s.plan.Rows[s.plan.index[path]]) })
	for _, g := range s.plan.Groups {
		rows := s.visibleRows(g)
		for _, r := range rows[:min(s.shown[g.ID]+s.firstShown(g), len(rows))] {
			out = append(out, r.Path)
		}
	}
	return out
}

func (s *cleanupSheet) visibleRows(g cleanupGroup) []cleanupRow {
	var rows []cleanupRow
	for _, path := range g.Rows {
		if r := s.plan.Rows[s.plan.index[path]]; s.visible(r) {
			rows = append(rows, r)
		}
	}
	return rows
}

func (a *app) queueRead(workspaceID string, sheet *cleanupSheet, paths []string) {
	queued := make(map[string]bool, len(sheet.queue))
	for _, path := range sheet.queue {
		queued[path] = true
	}
	for _, path := range paths {
		if r, ok := sheet.plan.row(path); ok && !r.State.Read && !queued[path] {
			sheet.queue, queued[path] = append(sheet.queue, path), true
		}
	}
	if len(sheet.queue) > 0 && !sheet.reading {
		sheet.reading = true
		go a.readSheet(workspaceID, sheet)
	}
}

func (a *app) queueWanted(workspaceID string, sheet *cleanupSheet) {
	a.queueRead(workspaceID, sheet, sheet.wanted())
}

func (a *app) readSheet(workspaceID string, sheet *cleanupSheet) {
	for {
		a.uiMu.Lock()
		batch := sheet.queue[:min(len(sheet.queue), cleanupBatch)]
		sheet.queue = sheet.queue[len(batch):]
		sheet.inflight = len(batch)
		trees := sheet.plan.trees(batch)
		if len(batch) == 0 {
			sheet.reading = false
			a.uiMu.Unlock()
			return
		}
		a.uiMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), cleanupRead)
		a.proveWorktrees(ctx, workspaceID, trees)
		cancel()
		a.uiMu.Lock()
		sheet.apply(trees)
		sheet.inflight = 0
		a.queueWanted(workspaceID, sheet)
		a.uiMu.Unlock()
		a.kick(workspaceID)
	}
}

func (s *cleanupSheet) apply(read []folderWorktree) {
	s.plan.setStates(read)
	s.plan.regroup()
	for _, wt := range read {
		r, ok := s.plan.row(wt.Path)
		if !ok {
			continue
		}
		switch {
		case !r.tickable():
			delete(s.ticked, r.Path)
		case !s.chosen[r.Path] && r.preTick():
			s.ticked[r.Path] = true
		}
	}
}

func isCleanupAction(action string) bool {
	switch action {
	case actCleanupTick, actCleanupGroup, actCleanupShow, actCleanupFilter, actCleanupFolder, actCleanupCancel, actCleanupConfirm:
		return true
	}
	return false
}

func (a *app) cleanupSheetAct(ev pluginsdk.UIEvent) error {
	p := a.workspaceState(ev.WorkspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	sheet := p.cleanup
	if sheet == nil || sheet.busy {
		return nil
	}
	switch ev.Action {
	case actCleanupCancel:
		p.cleanup = nil
	case actCleanupTick:
		return sheet.tickRow(ev)
	case actCleanupGroup:
		return sheet.tickGroup(ev)
	case actCleanupShow:
		id, ok := strings.CutPrefix(ev.NodeID, cleanupShowNode)
		if !ok {
			return fmt.Errorf("%w: %s on %q", errUnknownAction, ev.Action, ev.NodeID)
		}
		sheet.shown[id] += cleanupRowsShown
		a.queueWanted(ev.WorkspaceID, sheet)
	case actCleanupFilter:
		var query string
		if err := decode(ev.Value, &query); err != nil {
			return err
		}
		sheet.filter = query
		a.queueWanted(ev.WorkspaceID, sheet)
	case actCleanupFolder:
		var folder string
		if err := decode(ev.Value, &folder); err != nil {
			return err
		}
		sheet.folder = folder
		a.queueWanted(ev.WorkspaceID, sheet)
	case actCleanupConfirm:
		if sheet.reads() > 0 || len(sheet.kept) > 0 || sheet.finished > 0 {
			return nil
		}
		rows := sheet.chosenRows()
		if len(rows) == 0 {
			return nil
		}
		sheet.busy, sheet.done, sheet.total, sheet.note = true, 0, len(rows), ""
		go a.finishCleanup(ev.WorkspaceID, sheet, rows)
	}
	return nil
}

func (s *cleanupSheet) tickRow(ev pluginsdk.UIEvent) error {
	path, ok := strings.CutPrefix(ev.NodeID, cleanupTickNode)
	if !ok {
		return fmt.Errorf("%w: %s on %q", errUnknownAction, ev.Action, ev.NodeID)
	}
	var on bool
	if err := decode(ev.Value, &on); err != nil {
		return err
	}
	if r, ok := s.plan.row(path); ok && r.tickable() {
		s.ticked[path], s.chosen[path] = on, true
	}
	return nil
}

func (s *cleanupSheet) tickGroup(ev pluginsdk.UIEvent) error {
	id, ok := strings.CutPrefix(ev.NodeID, cleanupGroupNode)
	if !ok {
		return fmt.Errorf("%w: %s on %q", errUnknownAction, ev.Action, ev.NodeID)
	}
	var on bool
	if err := decode(ev.Value, &on); err != nil {
		return err
	}
	for _, g := range s.plan.Groups {
		if g.ID != id || !g.Bulk {
			continue
		}
		for _, path := range g.Rows {
			if r := s.plan.Rows[s.plan.index[path]]; r.tickable() && len(r.Closes) == 0 {
				s.ticked[path], s.chosen[path] = on, true
			}
		}
	}
	return nil
}

func (s *cleanupSheet) chosenRows() []removalRow {
	var out []removalRow
	for _, r := range s.plan.Rows {
		if s.ticked[r.Path] && r.tickable() && s.visible(r) {
			out = append(out, r.removal(s.plan.Cron))
		}
	}
	return out
}

func (s *cleanupSheet) reads() int {
	n := len(s.queue)
	if s.reading {
		n++
	}
	return n
}

func (a *app) finishCleanup(workspaceID string, sheet *cleanupSheet, rows []removalRow) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(rows))*removeRowLimit)
	defer cancel()
	p := a.workspaceState(workspaceID)
	out := newRemovalResult()
	var failed error
	for i, row := range rows {
		if err := a.pauseJobs(ctx, workspaceID, cronReason(row.repo, row.tree), row.jobs); err != nil {
			failed = err
			for _, paused := range rows[:i] {
				failed = errors.Join(failed, a.resumeJobs(ctx, workspaceID, paused.jobs))
			}
			break
		}
	}
	if failed == nil {
		a.removeRows(ctx, workspaceID, rows, &out, func(done int) {
			a.uiMu.Lock()
			sheet.done = done
			a.uiMu.Unlock()
			a.kick(workspaceID)
		})
	}
	a.uiMu.Lock()
	switch {
	case p.cleanup != sheet:
	case failed != nil:
		sheet.busy, sheet.note = false, keptWhy(failed)
	case len(out.Kept) > 0:
		sheet.busy, sheet.kept, sheet.finished = false, out.Kept, len(out.Removed)
	default:
		p.cleanup = nil
	}
	a.uiMu.Unlock()
	a.renderLogged(workspaceID)
}

func (a *app) cleanupSheetTree(p *workspaceUI) (sdui.Node, bool) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.cleanup == nil {
		return sdui.Node{}, false
	}
	return sdui.VStack(p.cleanup.view()), true
}

func (s *cleanupSheet) view() sdui.Node {
	if len(s.kept) > 0 {
		return s.keptView()
	}
	children := []sdui.Node{s.filterRow(), sdui.MutedText(s.plan.countWords(s.folder) + ". " + cleanupLead)}
	children = append(children, s.groupNodes()...)
	if line := s.cronLine(); line != "" {
		children = append(children, sdui.Markdown(line))
	}
	children = append(children, sdui.HStack(s.cancelButton(), s.removeButton()))
	note, tone := s.footnote()
	return sdui.Sheet(cleanupSheetID, sdui.SheetProps{
		Title: s.title(), Icon: "trash", Cancel: actCleanupCancel, Note: note, NoteTone: tone,
	}, children...)
}

func (s *cleanupSheet) title() string {
	if s.folder == cleanupAllFolders {
		return s.plan.Title
	}
	return fmt.Sprintf(cleanupTitleAll, filepath.Base(s.folder))
}

func (s *cleanupSheet) footnote() (string, string) {
	switch {
	case s.note != "":
		return s.note, "danger"
	case s.lost() != "":
		return s.summary() + " and " + s.lost(), "danger"
	}
	return s.summary(), ""
}

func (s *cleanupSheet) keptView() sdui.Node {
	closeButton := sdui.WithVariant(sdui.WithAction(sdui.Button(cleanupSheetID+"/close", folderLeftClose, ""), actCleanupCancel), sdui.VariantPrimary)
	title := fmt.Sprintf(cleanupDoneTitle, counted(s.finished, "worktree"))
	return sdui.Sheet(cleanupSheetID, sdui.SheetProps{Title: title, Icon: "trash", Cancel: actCleanupCancel, Note: fmt.Sprintf(cleanupKeptTitle, len(s.kept))},
		sdui.Markdown(strings.Join(s.kept, "\n\n")), sdui.HStack(sdui.WithShortcut(closeButton, "cmd+return")))
}

func (s *cleanupSheet) filterRow() sdui.Node {
	field := sdui.WithAction(sdui.WithLive(sdui.TextField(cleanupSheetID+"/filter", s.filter, cleanupPlaceholder)), actCleanupFilter)
	if len(s.plan.Folders) < 2 {
		return field
	}
	options := []sdui.Option{{Value: cleanupAllFolders, Label: cleanupAllWord}}
	for _, folder := range s.plan.Folders {
		options = append(options, sdui.Option{Value: folder, Label: filepath.Base(folder)})
	}
	return sdui.WithGap(sdui.HStack(field, sdui.WithAction(sdui.Select(cleanupSheetID+"/folder", options, s.folder), actCleanupFolder)), cleanupRowGap)
}

func (s *cleanupSheet) visible(r cleanupRow) bool {
	return (s.folder == cleanupAllFolders || r.Repo == s.folder) && (s.filter == "" || r.matches(s.filter))
}

func (s *cleanupSheet) groupNodes() []sdui.Node {
	if len(s.plan.Rows) == 0 {
		return []sdui.Node{sdui.MutedText(cleanupEmpty)}
	}
	var out []sdui.Node
	for _, g := range s.plan.Groups {
		if rows := s.visibleRows(g); len(rows) > 0 {
			out = append(out, s.groupNode(g, rows))
		}
	}
	return out
}

func (s *cleanupSheet) groupNode(g cleanupGroup, rows []cleanupRow) sdui.Node {
	title := sdui.Markdown("**" + mdPlain(g.Title) + "** · " + strconv.Itoa(len(rows)))
	note := sdui.MutedText(g.Note)
	if g.Tone != "" {
		note = sdui.WithTone(sdui.Text(g.Note), g.Tone)
	}
	head := []sdui.Node{title, sdui.Spacer(), note}
	if g.Bulk {
		box := sdui.WithAction(sdui.Checkbox(cleanupGroupNode+g.ID, "", s.allTicked(rows)), actCleanupGroup)
		head = append([]sdui.Node{box}, head...)
	}
	lines := []sdui.Node{sdui.WithGap(sdui.HStack(head...), cleanupRowGap)}
	if held := s.heldBack(rows); held > 0 {
		lines = append(lines, sdui.MutedText(fmt.Sprintf(cleanupHeldIgnored, counted(held, "worktree"))))
	}
	if open := countOpen(rows); open > 0 {
		lines = append(lines, sdui.WithTone(sdui.Text(fmt.Sprintf(cleanupHeldOpen, counted(open, "worktree"))), "warning"))
	}
	shown := min(s.shown[g.ID]+s.firstShown(g), len(rows))
	for _, r := range rows[:shown] {
		lines = append(lines, s.rowNode(r))
	}
	if left := len(rows) - shown; left > 0 {
		lines = append(lines, s.moreButton(g, left))
	}
	return sdui.WithGap(sdui.VStack(lines...), cleanupRowGap)
}

func (s *cleanupSheet) firstShown(g cleanupGroup) int {
	if g.ID == cleanupRisky {
		return 2
	}
	return 1
}

func (s *cleanupSheet) moreButton(g cleanupGroup, left int) sdui.Node {
	label := fmt.Sprintf(cleanupMore, left)
	if s.shown[g.ID] > 0 {
		label = fmt.Sprintf(cleanupShowMore, min(left, cleanupRowsShown), left)
	}
	return sdui.WithVariant(sdui.WithAction(sdui.Button(cleanupShowNode+g.ID, label, "chevron.right"), actCleanupShow), sdui.VariantGhost)
}

func countOpen(rows []cleanupRow) int {
	n := 0
	for _, r := range rows {
		if len(r.Closes) > 0 {
			n++
		}
	}
	return n
}

func (s *cleanupSheet) allTicked(rows []cleanupRow) bool {
	bulk := slices.DeleteFunc(slices.Clone(rows), func(r cleanupRow) bool { return !r.tickable() || len(r.Closes) > 0 })
	return len(bulk) > 0 && !slices.ContainsFunc(bulk, func(r cleanupRow) bool { return !s.ticked[r.Path] })
}

func (s *cleanupSheet) heldBack(rows []cleanupRow) int {
	n := 0
	for _, r := range rows {
		if r.tickable() && !s.ticked[r.Path] && !s.chosen[r.Path] && r.State.Ignored > 0 {
			n++
		}
	}
	return n
}

func (s *cleanupSheet) rowNode(r cleanupRow) sdui.Node {
	var parts []sdui.Node
	if r.tickable() {
		parts = append(parts, sdui.WithAction(sdui.Checkbox(cleanupTickNode+r.Path, "", s.ticked[r.Path]), actCleanupTick))
	}
	parts = append(parts, sdui.Text(r.label()))
	if word, tone := s.rowWord(r); word != "" {
		note := sdui.MutedText(word)
		if tone != "" {
			note = sdui.WithTone(sdui.Text(word), tone)
		}
		parts = append(parts, note)
	}
	return sdui.WithGap(sdui.HStack(parts...), cleanupRowGap)
}

func (s *cleanupSheet) rowWord(r cleanupRow) (string, string) {
	wt := r.State
	lost := r.lost()
	switch {
	case r.Locked || wt.Locked:
		return lockedWords(folderWorktree{Locked: true, LockWhy: cmp.Or(r.Why, wt.LockWhy)}), "warning"
	case !wt.Read:
		return cleanupUnread, ""
	case wt.Unread:
		return folderUnread, "warning"
	case wt.Nested > 0:
		return folderNested, "warning"
	case r.Words != "" && lost != "" && s.ticked[r.Path]:
		return fmt.Sprintf(cleanupWillLose, lost) + " · " + r.Words, "danger"
	case r.Words != "":
		return r.Words, "warning"
	case lost != "" && s.ticked[r.Path]:
		return fmt.Sprintf(cleanupWillLose, lost), "danger"
	case lost != "":
		return lost, ""
	case wt.Unpushed > 0 && wt.Branch != "":
		return counted(wt.Unpushed, "unpushed commit"), ""
	case !r.branchGoes() && (r.Group == cleanupMerged || r.Group == cleanupPRMerged):
		return fmt.Sprintf(cleanupStays, r.Land.branchStep(wt, true).Why), ""
	}
	return "", ""
}

func (s *cleanupSheet) cronLine() string {
	if s.plan.CronUnread {
		return folderCronLost
	}
	jobs := s.plan.Cron
	if len(jobs) == 0 {
		return ""
	}
	names := make([]string, 0, cleanupCronNamed)
	for _, job := range jobs[:min(len(jobs), cleanupCronNamed)] {
		names = append(names, "**"+mdPlain(job.Name)+"** in "+mdPlain(s.jobPlace(job)))
	}
	text := strings.Join(names, ", ")
	if more := len(jobs) - len(names); more > 0 {
		text += fmt.Sprintf(cleanupCronMore, more)
	}
	if len(jobs) == 1 {
		return fmt.Sprintf(cleanupCronOne, text)
	}
	return fmt.Sprintf(cleanupCronMany, len(jobs), text)
}

func (s *cleanupSheet) jobPlace(job folderJob) string {
	if r, ok := s.plan.row(job.In); ok {
		return r.label()
	}
	return job.In
}

func (s *cleanupSheet) summary() string {
	rows := s.chosenRows()
	if len(rows) == 0 {
		return cleanupNothing
	}
	branches := 0
	for _, r := range rows {
		if r.branch {
			branches++
		}
	}
	parts := []string{counted(len(rows), "worktree")}
	if branches > 0 {
		parts = append(parts, "deletes "+mergedBranches(branches))
	}
	return fmt.Sprintf(cleanupRemoves, strings.Join(parts, ", "))
}

func (s *cleanupSheet) lost() string {
	var all folderWorktree
	for _, r := range s.chosenRows() {
		all.Dirty += r.tree.Dirty
		all.Ignored += r.tree.Ignored
		all.Orphans += r.tree.Orphans
	}
	return lostWords(all, lostCommit)
}

func (s *cleanupSheet) cancelButton() sdui.Node {
	n := sdui.WithAction(sdui.Button(cleanupSheetID+"/cancel", "Cancel", ""), actCleanupCancel)
	if s.busy {
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *cleanupSheet) removeButton() sdui.Node {
	count := len(s.chosenRows())
	label, variant := fmt.Sprintf(cleanupButton, count), sdui.VariantPrimary
	if lost := s.lost(); lost != "" {
		label, variant = fmt.Sprintf(cleanupLose, count, lost), sdui.VariantDanger
	}
	reading := s.readingWord()
	if reading != "" {
		label = reading
	}
	n := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(cleanupSheetID+"/remove", label, ""), actCleanupConfirm), variant), "cmd+return")
	switch {
	case s.busy:
		return sdui.WithLoading(n, fmt.Sprintf(cleanupBusy, s.done, s.total))
	case reading != "" || count == 0:
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *cleanupSheet) readingWord() string {
	if s.reads() == 0 {
		return ""
	}
	read := 0
	for _, r := range s.plan.Rows {
		if r.State.Read {
			read++
		}
	}
	return fmt.Sprintf(cleanupReading, read, read+s.inflight+len(s.queue))
}
