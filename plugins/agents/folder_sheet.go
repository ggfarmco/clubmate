package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	sheetSlot           = "agents.sheet"
	folderSheetID       = "folderSheet"
	folderTickNode      = folderSheetID + "/tick/"
	folderRemoveTimeout = 10 * time.Minute
	folderBranchWidth   = 23

	actFolderTick    = "folderTick"
	actFolderCancel  = "folderCancel"
	actFolderConfirm = "folderConfirm"

	folderTitle      = "Remove %s from %s?"
	folderLead       = "The folder and its files stay at `%s`. What changes in %s:"
	folderKeepsOne   = "A workspace keeps at least one folder."
	folderRefused    = "Refused while a session runs or waits in the folder."
	folderHeld       = "Refused: %s."
	folderChanged    = "A worktree changed. Check what would be lost, then remove again."
	folderCronGone   = "Cron stopped answering, so its jobs wouldn't pause. Nothing was removed."
	folderStayed     = "%s stays on disk: %s."
	folderLeftClose  = "Close"
	folderRemoveWord = "Remove from %s"
	folderLose       = "Remove · lose %s"
	folderBusy       = "Removing…"

	folderNone      = "none"
	folderNoneHere  = "none run here"
	folderClosedOK  = "Stay in Closed, read-only."
	folderOpenOK    = "Open ones stay in Agents and keep working in the folder on disk."
	folderNoSession = "No session worked here."
	folderFree      = "None running or waiting ✓"
	folderStays     = "%s stays on disk; Clubmate stops showing it."
	folderStaysMany = "They stay on disk; Clubmate stops showing them."
	folderMoreTrees = "%d more stay on disk."
	folderAlsoOne   = "Also remove the worktree"
	folderAlsoMany  = "Also remove %s"
	folderUnread    = "couldn't read it, so it stays"
	folderNested    = "holds a repo Clubmate can't read, so it stays"
	gitFatal        = "fatal: "
	folderLoses     = "loses %s"
	lostCommit      = "commit"
	lostDetached    = "detached commit"
	folderGraph     = "%s `%s` leaves Graphs. Its past runs stay readable."
	folderGraphs    = "%s leave Graphs. Their past runs stay readable."
	folderHooks     = "%s stop running."
	folderHook      = "%s stops running."
	folderInstr     = "%s — only ever loaded for sessions in this folder."
	folderKept      = "Trust for %s, per repo: connect %s again and they are trusted, unless they changed."
	folderKeptHook  = "the hook"
	folderKeptHooks = "the hooks"
	folderCronNone  = "One that runs in this folder would pause."
	folderCronPause = "%s pauses until you resume it in Cron."
	folderCronMany  = "%s pause until you resume them in Cron."
	folderCronLost  = "Couldn't read Cron, so nothing pauses."
	folderUntouched = "Backlog and triggers belong to the workspace, not to a folder."
	folderGraphFile = ".clubmate/graphs/%s.json"

	impactSessions = "sessions"
	impactTrees    = "worktrees"
	impactGraphs   = "graphs"
	impactHooks    = "hooks"
	impactInstr    = "instructions"
	impactKept     = "kept"
	impactCron     = "cron"
	impactLeft     = "untouched"

	wordOpen    = "open"
	wordBlocked = "%s is %s"
)

var (
	errCostChanged = errors.New(folderChanged)
	errCronGone    = errors.New(folderCronGone)
)

type folderSheet struct {
	impact folderImpactResult
	dirs   []string
	ticked map[string]bool
	busy   bool
	note   string
	kept   []string
}

type folderHolds struct {
	sessions []folderSession
	blockers []folderSession
	tasks    []string
}

func (a *app) folderSheetCommand(ctx context.Context, cmd pluginsdk.CommandRun, args pathArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errFolderUser
	}
	if err := a.openFolderSheet(ctx, cmd.WorkspaceID, args.Path); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, cmd.WorkspaceID)
}

func (a *app) openFolderSheet(ctx context.Context, workspaceID, path string) error {
	impact, err := a.folderImpact(ctx, workspaceID, path)
	if err != nil {
		return err
	}
	sheet := &folderSheet{impact: impact, dirs: folderDirs(ctx, impact.Path), ticked: map[string]bool{}}
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.sheetBusy() {
		p.folder, p.archive, p.cleanup, p.repos = sheet, nil, nil, nil
	}
	return nil
}

func (a *app) folderSheetAct(ev pluginsdk.UIEvent) error {
	p := a.workspaceState(ev.WorkspaceID)
	sheet := a.openSheet(p)
	if sheet == nil {
		return nil
	}
	var holds folderHolds
	if ev.Action == actFolderConfirm {
		holds = a.folderHoldsOf(ev.WorkspaceID, sheet)
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.folder != sheet || sheet.busy {
		return nil
	}
	switch ev.Action {
	case actFolderCancel:
		p.folder = nil
	case actFolderTick:
		path, ok := strings.CutPrefix(ev.NodeID, folderTickNode)
		if !ok {
			return fmt.Errorf("%w: %s on %q", errUnknownAction, ev.Action, ev.NodeID)
		}
		var on bool
		if err := decode(ev.Value, &on); err != nil {
			return err
		}
		sheet.ticked[path] = on
	case actFolderConfirm:
		if sheet.refused(holds) {
			return nil
		}
		sheet.busy, sheet.note = true, ""
		go a.finishFolderRemove(ev.WorkspaceID, sheet.impact.Path)
	}
	return nil
}

func (a *app) openSheet(p *workspaceUI) *folderSheet {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	return p.folder
}

func (a *app) finishFolderRemove(workspaceID, folder string) {
	ctx, cancel := context.WithTimeout(context.Background(), folderRemoveTimeout)
	defer cancel()
	p := a.workspaceState(workspaceID)
	res, err := a.removeWithWorktrees(ctx, workspaceID, p, folder)
	a.settleFolderSheet(ctx, workspaceID, p, res, err)
	a.renderLogged(workspaceID)
}

func (a *app) settleFolderSheet(ctx context.Context, workspaceID string, p *workspaceUI, res folderRemoveResult, err error) {
	a.uiMu.Lock()
	sheet := p.folder
	switch {
	case res.Removed && len(res.Kept) == 0:
		p.folder = nil
	case res.Removed && sheet != nil:
		sheet.busy, sheet.kept = false, res.Kept
	}
	a.uiMu.Unlock()
	if sheet == nil || res.Removed {
		return
	}
	note := ""
	if err != nil {
		note = err.Error()
	}
	impact, impactErr := a.folderImpact(ctx, workspaceID, sheet.impact.Path)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if impactErr == nil {
		sheet.impact = impact
	}
	sheet.busy, sheet.note = false, note
}

func (a *app) removeWithWorktrees(ctx context.Context, workspaceID string, p *workspaceUI, folder string) (folderRemoveResult, error) {
	a.uiMu.Lock()
	sheet := p.folder
	var shown []folderWorktree
	cronRead := false
	if sheet != nil {
		shown, cronRead = sheet.tickedWorktrees(), !sheet.impact.CronUnread
	}
	a.uiMu.Unlock()
	now := slices.Clone(shown)
	a.readWorktrees(ctx, workspaceID, now)
	if !slices.EqualFunc(now, shown, folderWorktree.same) {
		return folderRemoveResult{}, errCostChanged
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return folderRemoveResult{}, err
	}
	jobs, err := a.folderJobs(ctx, w, folder)
	if !cronRead {
		err = nil
	}
	return a.dropFolder(ctx, w, folder, folderDrop{jobs: jobs, cronErr: err, ticked: shown})
}

func (s *folderSheet) tickedWorktrees() []folderWorktree {
	var out []folderWorktree
	for _, wt := range s.impact.Worktrees {
		if wt.Read && !wt.Unread && wt.Nested == 0 && !wt.Locked && s.ticked[wt.Path] {
			out = append(out, wt)
		}
	}
	return out
}

func (a *app) removeSheetWorktrees(ctx context.Context, workspaceID, folder string, worktrees []folderWorktree) []string {
	var kept []string
	for _, wt := range worktrees {
		if err := a.removeSheetWorktree(ctx, workspaceID, folder, wt); err != nil {
			why := strings.TrimPrefix(strings.TrimRight(err.Error(), ".\n "), gitFatal)
			kept = append(kept, fmt.Sprintf(folderStayed, mdCode(wt.Path), mdPlain(why)))
		}
	}
	return kept
}

func (a *app) removeSheetWorktree(ctx context.Context, workspaceID, folder string, wt folderWorktree) error {
	return a.removeSeen(ctx, workspaceID, wt, map[string]any{"repo": folder, "path": wt.Path, "left": true, "discard": wt.Dirty > 0})
}

func (a *app) removeSeen(ctx context.Context, workspaceID string, wt folderWorktree, args map[string]any) error {
	if err := a.terminalIn(ctx, wt.Path); err != nil {
		return err
	}
	a.worktreeMu.Lock()
	defer a.worktreeMu.Unlock()
	var op worktreeOp
	args["seenDirty"], args["seenSecrets"], args["seenOrphans"] = wt.Dirty, len(wt.Secrets), wt.Orphans
	if err := a.host.RunCommand(ctx, worktreeRemoveCmd, workspaceID, args, &op); err != nil {
		return err
	}
	_, err := a.awaitOp(ctx, workspaceID, op.ID)
	return err
}

func (a *app) folderHoldsOf(workspaceID string, sheet *folderSheet) folderHolds {
	a.uiMu.Lock()
	path := sheet.impact.Path
	ticked := sheet.tickedWorktrees()
	a.uiMu.Unlock()
	holds := folderHolds{blockers: a.folderBlockers(workspaceID, sheet.dirs, ticked), tasks: a.taskHolds(workspaceID, path, ticked)}
	for _, s := range a.sessionsIn(workspaceID, sheet.dirs) {
		holds.sessions = append(holds.sessions, folderSessionOf(s.snapshot()))
	}
	for _, b := range holds.blockers {
		if !slices.ContainsFunc(holds.sessions, func(f folderSession) bool { return f.Session == b.Session }) {
			holds.sessions = append(holds.sessions, b)
		}
	}
	return holds
}

func (a *app) sheetTree(workspaceID string, p *workspaceUI) sdui.Node {
	if node, ok := a.reposSheetTree(p); ok {
		return node
	}
	if node, ok := a.cleanupSheetTree(p); ok {
		return node
	}
	if node, ok := a.archiveSheetTree(p); ok {
		return node
	}
	return a.folderSheetTree(workspaceID, p)
}

func (a *app) folderSheetTree(workspaceID string, p *workspaceUI) sdui.Node {
	sheet := a.openSheet(p)
	if sheet == nil {
		return sdui.Empty(folderSheetID)
	}
	holds := a.folderHoldsOf(workspaceID, sheet)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	return sdui.VStack(sheet.view(holds))
}

func (s *folderSheet) refused(h folderHolds) bool {
	return s.impact.Last || len(s.kept) > 0 || len(h.blockers) > 0 || len(h.tasks) > 0
}

func (s *folderSheet) view(h folderHolds) sdui.Node {
	impact := s.impact
	if len(s.kept) > 0 {
		return s.keptView()
	}
	rows, bodies := s.rows(h)
	note, tone := s.footnote(h)
	children := []sdui.Node{
		sdui.Markdown(fmt.Sprintf(folderLead, tildePath(impact.Path), mdPlain(impact.Workspace))),
		sdui.Impact(folderSheetID+"/impact", rows, bodies...),
		sdui.HStack(s.cancelButton(), s.removeButton(h)),
	}
	return sdui.Sheet(folderSheetID, sdui.SheetProps{
		Title: fmt.Sprintf(folderTitle, impact.Name, impact.Workspace), Icon: repoFolderIcon, Cancel: actFolderCancel, Note: note, NoteTone: tone,
	}, children...)
}

func (s *folderSheet) keptView() sdui.Node {
	closeButton := sdui.WithVariant(sdui.WithAction(sdui.Button(folderSheetID+"/close", folderLeftClose, ""), actFolderCancel), sdui.VariantPrimary)
	return sdui.Sheet(folderSheetID, sdui.SheetProps{
		Title: fmt.Sprintf(cronPausedWhy, s.impact.Name, s.impact.Workspace), Icon: repoFolderIcon, Cancel: actFolderCancel,
	}, sdui.Markdown(strings.Join(s.kept, "\n\n")), sdui.HStack(sdui.WithShortcut(closeButton, "cmd+return")))
}

func (s *folderSheet) footnote(h folderHolds) (string, string) {
	switch {
	case s.note != "":
		return s.note, "danger"
	case s.impact.Last:
		return folderKeepsOne, "danger"
	case len(h.blockers) > 0 || len(h.tasks) > 0:
		return fmt.Sprintf(folderHeld, holdWords(h)), "danger"
	case s.lost() != "":
		return "", ""
	}
	return folderRefused, ""
}

func holdWords(h folderHolds) string {
	var why []string
	if n := len(h.blockers); n > 0 {
		why = append(why, plural(n, removeSession, fmt.Sprintf(removeSessions, n)))
	}
	for _, slug := range h.tasks {
		why = append(why, fmt.Sprintf(removeTask, slug))
	}
	return strings.Join(why, ", ")
}

func (s *folderSheet) cancelButton() sdui.Node {
	n := sdui.WithAction(sdui.Button(folderSheetID+"/cancel", "Cancel", ""), actFolderCancel)
	if s.busy {
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *folderSheet) removeButton(h folderHolds) sdui.Node {
	label := fmt.Sprintf(folderRemoveWord, s.impact.Workspace)
	variant := sdui.VariantPrimary
	if lost := s.lost(); lost != "" {
		label, variant = fmt.Sprintf(folderLose, lost), sdui.VariantDanger
	}
	n := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(folderSheetID+"/remove", label, ""), actFolderConfirm), variant), "cmd+return")
	switch {
	case s.busy:
		return sdui.WithLoading(n, folderBusy)
	case s.refused(h):
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *folderSheet) lost() string {
	var all folderWorktree
	for _, wt := range s.tickedWorktrees() {
		all.Dirty += wt.Dirty
		all.Secrets = append(all.Secrets, wt.Secrets...)
		all.Orphans += wt.Orphans
	}
	return lostWords(all, lostCommit)
}

func lostWords(wt folderWorktree, commit string) string {
	var parts []string
	if wt.Dirty > 0 {
		parts = append(parts, counted(wt.Dirty, "file"))
	}
	if words := secretWords(wt.Secrets); words != "" {
		parts = append(parts, words)
	}
	if wt.Orphans > 0 {
		parts = append(parts, counted(wt.Orphans, commit))
	}
	return listWords(parts, func(part string) string { return part })
}

func secretWords(secrets []string) string {
	switch len(secrets) {
	case 0:
		return ""
	case 1:
		return path.Base(secrets[0])
	}
	return counted(len(secrets), "secret file")
}

func (s *folderSheet) rows(h folderHolds) ([]sdui.ImpactRow, []sdui.Node) {
	var rows []sdui.ImpactRow
	var bodies []sdui.Node
	add := func(row sdui.ImpactRow, body ...sdui.Node) {
		rows = append(rows, row)
		bodies = append(bodies, sdui.WithID(sdui.WithGap(sdui.VStack(body...), 4), row.ID))
	}
	impact := s.impact
	count, body := sessionsRow(h)
	add(sdui.ImpactRow{ID: impactSessions, Icon: "sparkles", Title: "Sessions", Count: count}, body...)
	if n := len(impact.Worktrees); n > 0 {
		add(sdui.ImpactRow{ID: impactTrees, Icon: "arrow.triangle.branch", Title: "Worktrees", Count: strconv.Itoa(n)}, s.worktreesRow()...)
	}
	if n := len(impact.Graphs); n > 0 {
		add(sdui.ImpactRow{ID: impactGraphs, Icon: "point.3.connected.trianglepath.dotted", Title: "Graphs", Count: strconv.Itoa(n)}, sdui.Markdown(graphsText(impact.Graphs)))
	}
	if n := len(impact.Hooks); n > 0 {
		add(sdui.ImpactRow{ID: impactHooks, Icon: "terminal", Title: "Worktree hooks", Count: strconv.Itoa(n)}, sdui.Markdown(hooksText(impact.Hooks)))
	}
	if text := instructionsText(impact); text != "" {
		add(sdui.ImpactRow{ID: impactInstr, Icon: "doc", Title: "Agent instructions"}, sdui.Markdown(text))
	}
	if text := keptText(impact); text != "" {
		add(sdui.ImpactRow{ID: impactKept, Icon: "checkmark", Tone: "success", Title: "Kept"}, sdui.Markdown(text))
	}
	count, body = cronRow(impact)
	add(sdui.ImpactRow{ID: impactCron, Icon: "clock", Title: "Cron jobs", Count: count}, body...)
	add(sdui.ImpactRow{ID: impactLeft, Icon: "xmark", Title: "Untouched"}, sdui.Text(folderUntouched))
	return rows, bodies
}

func sessionsRow(h folderHolds) (string, []sdui.Node) {
	counts := map[string]int{}
	closed := 0
	for _, f := range h.sessions {
		if f.Status == statusClosed {
			closed++
			continue
		}
		counts[sessionWord(f.Status)]++
	}
	var parts []string
	for _, word := range []string{wordRunning, statusWaiting, wordOpen} {
		if n := counts[word]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+word)
		}
	}
	if closed > 0 {
		parts = append(parts, strconv.Itoa(closed)+" closed")
	}
	count := folderNone
	if len(parts) > 0 {
		count = strings.Join(parts, ", ")
	}
	var lines []string
	if closed > 0 {
		lines = append(lines, folderClosedOK)
	}
	if counts[wordOpen] > 0 {
		lines = append(lines, folderOpenOK)
	}
	if len(lines) == 0 && len(h.blockers) == 0 {
		lines = append(lines, folderNoSession)
	}
	body := []sdui.Node{}
	if len(lines) > 0 {
		body = append(body, sdui.Text(strings.Join(lines, " ")))
	}
	if len(h.blockers) == 0 && len(h.tasks) == 0 {
		return count, append(body, sdui.WithTone(sdui.Text(folderFree), "success"))
	}
	for _, b := range h.blockers {
		body = append(body, sdui.WithTone(sdui.Text(blockerText(b)), "danger"))
	}
	for _, slug := range h.tasks {
		body = append(body, sdui.WithTone(sdui.Text(fmt.Sprintf(removeTask, slug)), "danger"))
	}
	return count, body
}

func sessionWord(status string) string {
	switch status {
	case statusRunning:
		return wordRunning
	case statusWaiting:
		return statusWaiting
	}
	return wordOpen
}

func blockerText(b folderSession) string {
	word := sessionWord(b.Status)
	if word == wordOpen {
		word = wordQueued
	}
	return fmt.Sprintf(wordBlocked, b.Title, word)
}

func (s *folderSheet) worktreesRow() []sdui.Node {
	trees := s.impact.Worktrees
	read := slices.DeleteFunc(slices.Clone(trees), func(wt folderWorktree) bool { return !wt.Read })
	var body []sdui.Node
	if len(trees) == 1 {
		body = append(body, sdui.Markdown(fmt.Sprintf(folderStays, mdCode(branchOr(trees[0])))))
	} else {
		body = append(body, sdui.Text(folderStaysMany))
	}
	for _, wt := range read {
		body = append(body, s.tickRow(wt, len(trees) == 1))
	}
	if more := len(trees) - len(read); more > 0 {
		body = append(body, sdui.MutedText(fmt.Sprintf(folderMoreTrees, more)))
	}
	return body
}

func branchOr(wt folderWorktree) string {
	if wt.Branch != "" {
		return wt.Branch
	}
	return tildePath(wt.Path)
}

func (s *folderSheet) tickRow(wt folderWorktree, only bool) sdui.Node {
	label := folderAlsoOne
	if !only {
		label = fmt.Sprintf(folderAlsoMany, midOf(branchOr(wt), folderBranchWidth))
	}
	if wt.Unread {
		return sdui.WithTone(sdui.Text(label+" · "+folderUnread), "warning")
	}
	if wt.Nested > 0 {
		return sdui.WithTone(sdui.Text(label+" · "+folderNested), "warning")
	}
	if wt.Locked {
		return sdui.WithTone(sdui.Text(label+" · "+lockedWords(wt)), "warning")
	}
	box := sdui.WithAction(sdui.Checkbox(folderTickNode+wt.Path, label, s.ticked[wt.Path]), actFolderTick)
	lines := []sdui.Node{box}
	lost := lostWords(wt, lostDetached)
	loses := fmt.Sprintf(folderLoses, lost)
	switch {
	case lost != "" && only:
		lines[0] = sdui.WithGap(sdui.HStack(box, sdui.WithTone(sdui.Text("· "+loses), "warning")), 4)
	case lost != "":
		lines = append(lines, sdui.WithTone(sdui.Text(loses), "warning"))
	}
	if extra := worktreeCost(wt); extra != "" {
		lines = append(lines, sdui.MutedText(extra))
	}
	return sdui.VStack(lines...)
}

func midOf(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	half := (width - 1) / 2
	return string(runes[:half]) + "…" + string(runes[len(runes)-half:])
}

func worktreeCost(wt folderWorktree) string {
	var parts []string
	if wt.Unpushed > 0 && wt.Branch != "" {
		parts = append(parts, counted(wt.Unpushed, "unpushed commit")+" stay on its branch")
	}
	if wt.Sessions > 0 {
		parts = append(parts, "closes "+counted(wt.Sessions, "session"))
	}
	return strings.Join(parts, " · ")
}

func graphsText(graphs []string) string {
	if len(graphs) == 1 {
		return fmt.Sprintf(folderGraph, "**"+mdPlain(graphs[0])+"**", fmt.Sprintf(folderGraphFile, graphs[0]))
	}
	return fmt.Sprintf(folderGraphs, listWords(graphs, func(g string) string { return "**" + mdPlain(g) + "**" }))
}

func hooksText(hooks []string) string {
	if len(hooks) == 1 {
		return fmt.Sprintf(folderHook, mdCode(hooks[0]))
	}
	return fmt.Sprintf(folderHooks, listWords(hooks, mdCode))
}

func instructionsText(impact folderImpactResult) string {
	var parts []string
	parts = append(parts, impact.Instructions...)
	if n := len(impact.Skills); n > 0 {
		parts = append(parts, counted(n, "skill"))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(folderInstr, listWords(parts, mdPlain))
}

func keptText(impact folderImpactResult) string {
	parts := slices.Clone(impact.TrustedGraphs)
	if impact.TrustedHooks {
		parts = append(parts, plural(len(impact.Hooks), folderKeptHook, folderKeptHooks))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(folderKept, listWords(parts, mdPlain), mdPlain(impact.Name))
}

func cronRow(impact folderImpactResult) (string, []sdui.Node) {
	switch n := len(impact.CronJobs); {
	case impact.CronUnread:
		return "", []sdui.Node{sdui.WithTone(sdui.Text(folderCronLost), "warning")}
	case n == 0:
		return folderNoneHere, []sdui.Node{sdui.Text(folderCronNone)}
	case n == 1:
		return "1", []sdui.Node{sdui.Markdown(fmt.Sprintf(folderCronPause, "**"+mdPlain(impact.CronJobs[0].Name)+"**"))}
	default:
		names := make([]string, 0, n)
		for _, j := range impact.CronJobs {
			names = append(names, j.Name)
		}
		return strconv.Itoa(n), []sdui.Node{sdui.Markdown(fmt.Sprintf(folderCronMany, listWords(names, func(s string) string { return "**" + mdPlain(s) + "**" })))}
	}
}

func listWords(items []string, render func(string) string) string {
	words := make([]string, 0, len(items))
	for _, item := range items {
		words = append(words, render(item))
	}
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

var mdEscaper = strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "~", `\~`, "#", `\#`, "!", `\!`, "\n", " ")

func mdPlain(s string) string {
	return mdEscaper.Replace(s)
}

func mdCode(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}
