package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	reposInCmd      = "git.repos-in"
	reposSheetID    = "reposSheet"
	reposTickNode   = reposSheetID + "/tick/"
	reposRead       = 30 * time.Second
	reposAddLimit   = 2 * time.Minute
	reposShown      = 50
	reposRowGap     = 6
	reposNamed      = 3
	reposOfferedKey = "reposOffered/%s/%s"

	actAddReposTick    = "addReposTick"
	actAddReposAll     = "addReposAll"
	actAddReposFilter  = "addReposFilter"
	actAddReposCancel  = "addReposCancel"
	actAddReposConfirm = "addReposConfirm"

	reposAction      = "Add repos from %s…"
	reposTitle       = "Add repos from %s"
	reposLead        = "%s isn't a git repo, but these repos are inside it. The ticked ones are added as folders after it; it stays first."
	reposPlaceholder = "Filter %s"
	reposHead        = "Repos · %d"
	reposOrder       = "newest commit first"
	reposLooking     = "Looking for repos in %s…"
	reposNone        = "No repos left to add: every repo in %s is a folder of %s."
	reposInWorkspace = "in %s"
	reposButton      = "Add %d to %s"
	reposBusy        = "Adding %d of %d…"
	reposNothing     = "Nothing ticked"
	reposAdds        = "Adds %s after %s"
	reposPartial     = "Stopped looking after a few seconds: repos deeper in %s may be missing."
	reposNoDate      = "no commits"
)

var errReposUser = errors.New("only the user adds repos from a folder")

type reposFound struct {
	Path    string      `json:"path"`
	Repo    bool        `json:"repo"`
	Repos   []foundRepo `json:"repos"`
	Partial bool        `json:"partial"`
}

type foundRepo struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	LastCommit time.Time `json:"lastCommit,omitzero"`
}

type reposSheet struct {
	folder    string
	workspace string
	found     reposFound
	loaded    bool
	added     map[string]bool
	ticked    map[string]bool
	filter    string
	busy      bool
	done      int
	total     int
	note      string
}

func (a *app) reposSheetCommand(ctx context.Context, cmd pluginsdk.CommandRun, args pathArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errReposUser
	}
	w, err := a.host.GetWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	folder, ok := folderIn(w, args.Path)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNotFolder, args.Path)
	}
	a.openReposSheet(w, folder, false)
	return nil, a.render(ctx, cmd.WorkspaceID)
}

func (a *app) openReposSheet(w pluginsdk.Workspace, folder string, offer bool) bool {
	sheet := &reposSheet{folder: folder, workspace: w.Name, added: addedPaths(w), ticked: map[string]bool{}}
	p := a.workspaceState(w.ID)
	a.uiMu.Lock()
	if p.sheetBusy() || offer && (p.folder != nil || p.archive != nil || p.cleanup != nil || p.repos != nil) {
		a.uiMu.Unlock()
		return false
	}
	p.folder, p.archive, p.cleanup, p.repos = nil, nil, nil, sheet
	a.uiMu.Unlock()
	go a.readReposSheet(w.ID, sheet)
	return true
}

func (a *app) readReposSheet(workspaceID string, sheet *reposSheet) {
	ctx, cancel := context.WithTimeout(context.Background(), reposRead)
	defer cancel()
	found, err := a.reposIn(ctx, workspaceID, sheet.folder, true)
	a.uiMu.Lock()
	sheet.loaded, sheet.found = true, found
	if err != nil {
		sheet.note = err.Error()
	}
	a.uiMu.Unlock()
	a.kick(workspaceID)
}

func (a *app) reposIn(ctx context.Context, workspaceID, folder string, dates bool) (reposFound, error) {
	var out reposFound
	err := a.host.RunCommand(ctx, reposInCmd, workspaceID, map[string]any{"path": folder, "dates": dates}, &out)
	return out, err
}

func addedPaths(w pluginsdk.Workspace) map[string]bool {
	out := map[string]bool{}
	for _, path := range w.Paths() {
		out[gitrepo.Canonical(path)] = true
	}
	return out
}

func (f reposFound) left(added map[string]bool) int {
	n := 0
	for _, r := range f.Repos {
		if !added[gitrepo.Canonical(r.Path)] {
			n++
		}
	}
	return n
}

func (a *app) addReposActions(ctx context.Context, workspaceID string, folders []worktreeFolder) []pickerAddRepos {
	var out []pickerAddRepos
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil || w.Home {
		return out
	}
	added := addedPaths(w)
	for _, f := range folders {
		if f.Worktrees > 0 {
			continue
		}
		found, err := a.reposIn(ctx, workspaceID, f.Path, false)
		if err == nil && found.left(added) > 0 {
			out = append(out, pickerAddRepos{Folder: f.Path, Title: fmt.Sprintf(reposAction, f.Name)})
		}
	}
	return out
}

func (a *app) offerRepos(workspaceID string) {
	a.uiMu.Lock()
	present := a.present[workspaceID]
	a.uiMu.Unlock()
	if !present {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reposRead)
	defer cancel()
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil || w.Home {
		return
	}
	added := addedPaths(w)
	for _, f := range w.Folders {
		key := fmt.Sprintf(reposOfferedKey, workspaceID, f.Path)
		if !a.checkRepos(key) {
			continue
		}
		var offered bool
		if _, err := a.host.StorageGet(ctx, key, &offered); err != nil || offered {
			continue
		}
		found, err := a.reposIn(ctx, workspaceID, f.Path, false)
		if err != nil || found.left(added) == 0 {
			continue
		}
		if !a.openReposSheet(w, f.Path, true) {
			a.uncheckRepos(key)
			return
		}
		if err := a.host.StorageSet(ctx, key, true); err != nil {
			fmt.Fprintln(os.Stderr, "agents: repos offered:", err)
		}
		a.renderLogged(workspaceID)
		return
	}
}

func (a *app) checkRepos(key string) bool {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if a.reposChecked[key] {
		return false
	}
	a.reposChecked[key] = true
	return true
}

func (a *app) uncheckRepos(key string) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	delete(a.reposChecked, key)
}

func isAddReposAction(action string) bool {
	switch action {
	case actAddReposTick, actAddReposAll, actAddReposFilter, actAddReposCancel, actAddReposConfirm:
		return true
	}
	return false
}

func (a *app) reposSheetAct(ev pluginsdk.UIEvent) error {
	p := a.workspaceState(ev.WorkspaceID)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	sheet := p.repos
	if sheet == nil || sheet.busy {
		return nil
	}
	switch ev.Action {
	case actAddReposCancel:
		p.repos = nil
	case actAddReposTick:
		path, ok := strings.CutPrefix(ev.NodeID, reposTickNode)
		if !ok {
			return fmt.Errorf("%w: %s on %q", errUnknownAction, ev.Action, ev.NodeID)
		}
		var on bool
		if err := decode(ev.Value, &on); err != nil {
			return err
		}
		if sheet.listed(path) {
			sheet.ticked[path] = on
		}
	case actAddReposAll:
		var on bool
		if err := decode(ev.Value, &on); err != nil {
			return err
		}
		for _, r := range sheet.visible() {
			if !sheet.isAdded(r) {
				sheet.ticked[r.Path] = on
			}
		}
	case actAddReposFilter:
		return decode(ev.Value, &sheet.filter)
	case actAddReposConfirm:
		rows := sheet.chosen()
		if !sheet.loaded || len(rows) == 0 {
			return nil
		}
		sheet.busy, sheet.done, sheet.total, sheet.note = true, 0, len(rows), ""
		go a.addRepos(ev.WorkspaceID, sheet, rows)
	}
	return nil
}

func (a *app) addRepos(workspaceID string, sheet *reposSheet, rows []foundRepo) {
	ctx, cancel := context.WithTimeout(context.Background(), reposAddLimit)
	defer cancel()
	p := a.workspaceState(workspaceID)
	var failed error
	for i, r := range rows {
		w, err := a.host.GetWorkspace(ctx, workspaceID)
		if err == nil {
			if _, in := folderIn(w, r.Path); !in {
				_, err = a.host.AddFolder(ctx, workspaceID, r.Path, pluginsdk.FolderMeta{})
			}
		}
		if err != nil {
			failed = fmt.Errorf("%s: %w", r.Name, err)
			break
		}
		a.uiMu.Lock()
		sheet.done, sheet.added[gitrepo.Canonical(r.Path)] = i+1, true
		delete(sheet.ticked, r.Path)
		a.uiMu.Unlock()
		a.kick(workspaceID)
	}
	a.uiMu.Lock()
	switch {
	case p.repos != sheet:
	case failed != nil:
		sheet.busy, sheet.note = false, failed.Error()
	default:
		p.repos = nil
	}
	a.uiMu.Unlock()
	a.renderLogged(workspaceID)
}

func (a *app) reposSheetTree(p *workspaceUI) (sdui.Node, bool) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.repos == nil {
		return sdui.Node{}, false
	}
	return sdui.VStack(p.repos.view()), true
}

func (s *reposSheet) name() string {
	return filepath.Base(s.folder)
}

func (s *reposSheet) isAdded(r foundRepo) bool {
	return s.added[gitrepo.Canonical(r.Path)]
}

func (s *reposSheet) listed(path string) bool {
	return slices.ContainsFunc(s.found.Repos, func(r foundRepo) bool { return r.Path == path && !s.isAdded(r) })
}

func (s *reposSheet) visible() []foundRepo {
	var out []foundRepo
	query := strings.ToLower(s.filter)
	for _, r := range s.found.Repos {
		if query == "" || strings.Contains(strings.ToLower(r.Name), query) {
			out = append(out, r)
		}
	}
	return out
}

func (s *reposSheet) chosen() []foundRepo {
	var out []foundRepo
	for _, r := range s.found.Repos {
		if s.ticked[r.Path] && !s.isAdded(r) {
			out = append(out, r)
		}
	}
	return out
}

func (s *reposSheet) view() sdui.Node {
	var children []sdui.Node
	switch {
	case !s.loaded:
		children = append(children, sdui.MutedText(fmt.Sprintf(reposLooking, s.name())))
	case s.found.left(s.added) == 0 && !s.busy:
		children = append(children, sdui.MutedText(fmt.Sprintf(reposNone, s.name(), s.workspace)))
	default:
		children = append(children,
			sdui.MutedText(fmt.Sprintf(reposLead, s.name())),
			sdui.WithAction(sdui.WithLive(sdui.TextField(reposSheetID+"/filter", s.filter, fmt.Sprintf(reposPlaceholder, counted(len(s.found.Repos), "repo")))), actAddReposFilter),
			s.listNode(),
		)
	}
	children = append(children, sdui.HStack(s.cancelButton(), s.addButton()))
	note, tone := s.footnote()
	return sdui.Sheet(reposSheetID, sdui.SheetProps{
		Title: fmt.Sprintf(reposTitle, s.name()), Icon: "folder.badge.plus", Cancel: actAddReposCancel, Note: note, NoteTone: tone,
	}, children...)
}

func (s *reposSheet) listNode() sdui.Node {
	rows := s.visible()
	open := slices.DeleteFunc(slices.Clone(rows), s.isAdded)
	all := len(open) > 0 && !slices.ContainsFunc(open, func(r foundRepo) bool { return !s.ticked[r.Path] })
	box := sdui.WithAction(sdui.Checkbox(reposSheetID+"/all", fmt.Sprintf(reposHead, len(rows)), all), actAddReposAll)
	head := sdui.WithGap(sdui.HStack(box, sdui.Spacer(), sdui.MutedText(reposOrder)), reposRowGap)
	lines := []sdui.Node{head}
	now := time.Now()
	for _, r := range rows[:min(len(rows), reposShown)] {
		lines = append(lines, s.rowNode(r, now))
	}
	if more := len(rows) - reposShown; more > 0 {
		lines = append(lines, sdui.MutedText(fmt.Sprintf(pickerMore, more)))
	}
	return sdui.WithGap(sdui.VStack(lines...), reposRowGap)
}

func (s *reposSheet) rowNode(r foundRepo, now time.Time) sdui.Node {
	if s.isAdded(r) {
		return sdui.WithGap(sdui.HStack(sdui.MutedText(r.Name), sdui.Spacer(), sdui.MutedText(fmt.Sprintf(reposInWorkspace, s.workspace))), reposRowGap)
	}
	when := reposNoDate
	if !r.LastCommit.IsZero() {
		when = ageWord(now.Sub(r.LastCommit))
	}
	box := sdui.WithAction(sdui.Checkbox(reposTickNode+r.Path, r.Name, s.ticked[r.Path]), actAddReposTick)
	return sdui.WithGap(sdui.HStack(box, sdui.Spacer(), sdui.MutedText(when)), reposRowGap)
}

func (s *reposSheet) footnote() (string, string) {
	switch {
	case s.note != "":
		return s.note, "danger"
	case s.found.Partial:
		return fmt.Sprintf(reposPartial, s.name()), "warning"
	}
	rows := s.chosen()
	if len(rows) == 0 {
		return reposNothing, ""
	}
	names := make([]string, 0, reposNamed+1)
	for _, r := range rows[:min(len(rows), reposNamed)] {
		names = append(names, r.Name)
	}
	if more := len(rows) - len(names); more > 0 {
		names = append(names, fmt.Sprintf(cleanupMore, more))
	}
	return fmt.Sprintf(reposAdds, listWords(names, func(n string) string { return n }), s.name()), ""
}

func (s *reposSheet) cancelButton() sdui.Node {
	n := sdui.WithAction(sdui.Button(reposSheetID+"/cancel", "Cancel", ""), actAddReposCancel)
	if s.busy {
		return sdui.WithDisabled(n)
	}
	return n
}

func (s *reposSheet) addButton() sdui.Node {
	count := len(s.chosen())
	n := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(reposSheetID+"/add", fmt.Sprintf(reposButton, count, s.workspace), ""), actAddReposConfirm), sdui.VariantPrimary), "cmd+return")
	switch {
	case s.busy:
		return sdui.WithLoading(n, fmt.Sprintf(reposBusy, s.done, s.total))
	case !s.loaded || count == 0:
		return sdui.WithDisabled(n)
	}
	return n
}
