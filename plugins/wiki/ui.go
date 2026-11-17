package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"html"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/docext"
	"clubmate/pkg/docrender"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/wikiroot"
)

const (
	sectionID    = "wiki"
	searchLimit  = 50
	pollInterval = 3 * time.Second
	untitled     = "untitled"
)

var (
	errUnknownAction  = errors.New("unknown action")
	errNotForAgents   = errors.New("not for agents: use the wiki.* commands")
	errNotInWorkspace = errors.New("not in workspace")
)

type workspaceUI struct {
	render     sync.Mutex
	selected   string
	query      string
	agentNotes bool
	history    bool
	opening    bool
	find       string
	findSeq    int
	read       *readState
	change     string
	collapsed  map[string]bool
	sent       map[string][32]byte
}

func (w *wiki) workspace(workspaceID string) *workspaceUI {
	w.mu.Lock()
	defer w.mu.Unlock()
	u, ok := w.ui[workspaceID]
	if !ok {
		u = &workspaceUI{collapsed: map[string]bool{}, sent: map[string][32]byte{}}
		w.ui[workspaceID] = u
	}
	return u
}

func (w *wiki) setPresent(workspaceID string, on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if on {
		w.present[workspaceID] = true
		return
	}
	delete(w.present, workspaceID)
	if u, ok := w.ui[workspaceID]; ok {
		u.sent = map[string][32]byte{}
	}
}

func (w *wiki) presentIDs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Sorted(maps.Keys(w.present))
}

func (w *wiki) loadPresence(ctx context.Context) error {
	present, err := w.h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, id := range present {
		w.setPresent(id, true)
	}
	w.renderAll(ctx)
	return nil
}

func (w *wiki) presenceEvent(ctx context.Context, ev pluginsdk.Event) {
	if ev.Topic != "ui.opened" && ev.Topic != "ui.idle" {
		return
	}
	var p contract.UIPresence
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	w.setPresent(p.Workspace, ev.Topic == "ui.opened")
	if ev.Topic == "ui.opened" {
		logError("render "+p.Workspace, w.renderNow(ctx, p.Workspace))
	}
}

func (w *wiki) renderAll(ctx context.Context) {
	for _, id := range w.presentIDs() {
		logError("render "+id, w.renderNow(ctx, id))
	}
}

func (w *wiki) projects(ctx context.Context, ws contract.Workspace) []wikiroot.Project {
	var out []wikiroot.Project
	for _, f := range ws.Folders {
		p, ok, err := wikiroot.Find(ctx, f.Root())
		if err != nil {
			logError("wiki of "+f.Path, err)
			continue
		}
		if ok && !slices.ContainsFunc(out, func(q wikiroot.Project) bool { return q.Dir == p.Dir }) {
			out = append(out, p)
		}
	}
	return out
}

func (w *wiki) renderNow(ctx context.Context, workspaceID string) error {
	u := w.workspace(workspaceID)
	u.render.Lock()
	defer u.render.Unlock()
	ws, err := w.h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if ws.Home || len(ws.Folders) == 0 {
		if err := w.push(ctx, u, workspaceID, sidebarSlot, sidebarView(sidebarData{Home: true})); err != nil {
			return err
		}
		return w.push(ctx, u, workspaceID, mainSlot, mainView(mainData{Home: true}))
	}
	w.mu.Lock()
	selected, query, notes, history, change, collapsed, opening := u.selected, u.query, u.agentNotes, u.history, u.change, maps.Clone(u.collapsed), u.opening
	u.opening = false
	f := find{query: u.find, seq: u.findSeq}
	w.mu.Unlock()
	projects := w.projects(ctx, ws)
	read := w.readState(ctx, workspaceID, u)
	d := sidebarData{Query: query, AgentNotes: notes, Collapsed: collapsed, Now: w.now()}
	stores := map[string]*Store{}
	var all []recentRow
	for _, p := range projects {
		s := Open(p, w.now)
		stores[p.Name] = s
		pages, err := s.List(ctx, true)
		if err != nil {
			logError("list "+p.Dir, err)
			continue
		}
		d.Projects = append(d.Projects, projectView{Name: p.Name, Pages: pages})
		for _, pg := range pages {
			all = append(all, recentRow{Project: p.Name, Page: pg})
		}
		if query != "" {
			for _, h := range s.searchIn(pages, query, true, searchLimit) {
				d.Hits = append(d.Hits, hitRow{Project: p.Name, Hit: h})
			}
		}
	}
	slices.SortStableFunc(all, func(a, b recentRow) int {
		return cmp.Or(b.Page.Updated.Compare(a.Page.Updated), cmp.Compare(pageID(a.Project, a.Page.Path), pageID(b.Project, b.Page.Path)))
	})
	current, ok := findRow(all, selected)
	if !ok {
		current, ok = newest(all)
	}
	m := mainData{}
	if ok {
		d.Selected = pageID(current.Project, current.Page.Path)
		m = w.pageData(ctx, ws, projects, stores[current.Project], current, projectPages(d.Projects, current.Project), history, f)
		if history {
			w.historyData(ctx, stores[current.Project], &m, change, collapsed)
		}
		if opening || w.looking(ctx, workspaceID) {
			w.markOpened(ctx, workspaceID, u, current, all)
			read = w.readState(ctx, workspaceID, u)
		}
	}
	d.New = newToYou(all, read)
	w.settleSelection(u, selected, d.Selected)
	if err := w.push(ctx, u, workspaceID, sidebarSlot, sidebarView(d)); err != nil {
		return err
	}
	return w.push(ctx, u, workspaceID, mainSlot, mainView(m))
}

func (w *wiki) looking(ctx context.Context, workspaceID string) bool {
	section, err := w.h.SelectedSection(ctx, workspaceID)
	return err == nil && section == sectionID
}

func newest(rows []recentRow) (recentRow, bool) {
	for _, r := range rows {
		if !r.Page.Shadow {
			return r, true
		}
	}
	return recentRow{}, false
}

func findRow(rows []recentRow, id string) (recentRow, bool) {
	for _, r := range rows {
		if pageID(r.Project, r.Page.Path) == id {
			return r, true
		}
	}
	return recentRow{}, false
}

type find struct {
	query string
	seq   int
}

func projectPages(projects []projectView, name string) []Page {
	for _, p := range projects {
		if p.Name == name {
			return p.Pages
		}
	}
	return nil
}

func (w *wiki) pageData(ctx context.Context, ws contract.Workspace, projects []wikiroot.Project, s *Store, row recentRow, pages []Page, history bool, f find) mainData {
	page := row.Page
	folder, isIndex := indexFolder(page.Path, pages)
	m := mainData{Project: row.Project, Page: &page, ShowHistory: history, Now: w.now(), Path: filepath.Join(s.p.Dir, filepath.FromSlash(page.Path)), Index: isIndex}
	if history {
		return m
	}
	src, err := s.readFile(page.Path)
	if err != nil {
		logError("read "+page.Path, err)
		m.HTML = "<p>" + html.EscapeString(err.Error()) + "</p>"
		m.Kind = sdui.DocumentMarkdown
		return m
	}
	if docext.IsArtifact(page.Path) {
		m.HTML, m.Kind, m.Rev = docrender.Artifact(src, m.Path).HTML, sdui.DocumentArtifact, rev(src)
		return m
	}
	body := dropTitle(docrender.Render(src, docrender.Options{Path: m.Path, Allowed: scope(ws, projects)}).HTML, page.Title)
	if f.query != "" {
		anchor := matchAnchor + strconv.Itoa(f.seq)
		if marked, ok := markMatches(body, f.query, anchor); ok {
			body, m.Anchor = marked, anchor
		}
	}
	m.HTML = body
	if docext.Kind(page.Path) == docext.KindMarkdown {
		var here []folderEntry
		if isIndex {
			here = folderEntries(s.p.Dir, folder, page.Path, pages)
		}
		m.HTML = w.margin(ctx, s, m.Path, page, pages, here, body) + body
	}
	m.Kind, m.Rev = sdui.DocumentMarkdown, rev([]byte(m.HTML))
	return m
}

func rev(b []byte) int {
	return int(crc32.ChecksumIEEE(b) & 0x7fffffff)
}

func (w *wiki) margin(ctx context.Context, s *Store, abs string, page Page, pages []Page, here []folderEntry, body string) string {
	changes, err := s.History(ctx, page.Path, marginChanges)
	logError("history "+page.Path, err)
	total, err := s.Count(ctx, page.Path)
	logError("count "+page.Path, err)
	d := marginData{Path: abs, Page: page, Outline: headings(body), Changes: changes, Total: max(total, len(changes)), Folder: here, Now: w.now()}
	for _, l := range w.linkedFrom(s, pages, page.Path) {
		d.Linked = append(d.Linked, linkedPage{Title: l.Title, Path: filepath.Join(s.p.Dir, filepath.FromSlash(l.Path)), Shadow: l.Shadow})
	}
	return margin(d)
}

func (w *wiki) historyData(ctx context.Context, s *Store, m *mainData, change string, folded map[string]bool) {
	changes, err := s.History(ctx, m.Page.Path, historyLimit)
	logError("history "+m.Page.Path, err)
	m.History, m.Folded = changes, folded
	total, err := s.Count(ctx, m.Page.Path)
	logError("count "+m.Page.Path, err)
	m.Total = total
	if len(changes) == 0 {
		return
	}
	i := max(slices.IndexFunc(changes, func(c Change) bool { return c.Rev == change }), 0)
	c := changes[i]
	m.Change = c.Rev
	diff, err := s.Diff(ctx, m.Page.Path, c.Rev)
	logError("diff "+m.Page.Path, err)
	var after []string
	if src, err := s.Version(ctx, m.Page.Path, c.Rev); err == nil {
		after = strings.Split(string(src), "\n")
		m.CanRestore = i > 0
	}
	m.Diff = diffHTML(c, c.At.Local().Format(timeLayout), diff, after)
}

func scope(ws contract.Workspace, projects []wikiroot.Project) func(string) bool {
	var roots []string
	for _, p := range projects {
		roots = append(roots, gitrepo.Canonical(p.Dir))
	}
	for _, f := range ws.Folders {
		roots = append(roots, gitrepo.Canonical(f.Path), gitrepo.Canonical(f.Root()))
	}
	return func(path string) bool {
		path = gitrepo.Canonical(filepath.Clean(path))
		return slices.ContainsFunc(roots, func(root string) bool { return gitrepo.Within(path, root) })
	}
}

func (w *wiki) push(ctx context.Context, u *workspaceUI, workspaceID, slot string, n sdui.Node) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	w.mu.Lock()
	same := u.sent[slot] == sum
	w.mu.Unlock()
	if same {
		return nil
	}
	if _, err := w.h.Render(ctx, workspaceID, slot, n); err != nil {
		return err
	}
	w.mu.Lock()
	u.sent[slot] = sum
	w.mu.Unlock()
	return nil
}

func (w *wiki) uiEvent(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	err := w.handle(ctx, ev)
	if rerr := w.renderNow(ctx, ev.WorkspaceID); err == nil {
		err = rerr
	}
	return nil, err
}

func (w *wiki) handle(ctx context.Context, ev pluginsdk.UIEvent) error {
	u := w.workspace(ev.WorkspaceID)
	if (ev.NodeID == newNode || ev.NodeID == deleteNode || ev.NodeID == editNode || ev.NodeID == revealNode || ev.NodeID == restoreNode) && (ev.Agent || w.fromSession(ctx, ev.WorkspaceID, ev.Caller)) {
		return errNotForAgents
	}
	switch {
	case ev.NodeID == searchNode:
		var q string
		if err := json.Unmarshal(ev.Value, &q); err != nil {
			return fmt.Errorf("search value: %w", err)
		}
		w.mu.Lock()
		u.query = strings.TrimSpace(q)
		if u.query == "" {
			u.find = ""
		}
		w.mu.Unlock()
		return nil
	case ev.NodeID == pagesNode:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if strings.HasPrefix(id, dirPrefix) || strings.HasPrefix(id, projectPrefix) || strings.HasPrefix(id, sectionPrefix) {
			if ev.Action != "expand" && ev.Action != "collapse" {
				return nil
			}
			if _, dir, _ := splitID(strings.TrimPrefix(id, dirPrefix)); strings.HasPrefix(id, dirPrefix) && dir == shadowDir {
				u.agentNotes = ev.Action == "expand"
				return nil
			}
			u.collapsed[id] = ev.Action == "collapse"
			return nil
		}
		if ev.Action == "select" {
			u.selected, u.history, u.opening, u.change = strings.TrimPrefix(id, newPrefix), false, true, ""
			u.find = ""
			if u.query != "" {
				u.find, u.findSeq = u.query, u.findSeq+1
			}
		}
		return nil
	case ev.NodeID == newNode:
		return w.newPage(ctx, ev.WorkspaceID, u)
	case ev.NodeID == editNode:
		path, err := w.selectedPath(ctx, ev.WorkspaceID)
		if err != nil {
			return err
		}
		return w.h.RunCommand(ctx, "editor.open", ev.WorkspaceID, map[string]any{"path": path, "focus": true}, nil)
	case ev.NodeID == revealNode:
		path, err := w.selectedPath(ctx, ev.WorkspaceID)
		if err != nil {
			return err
		}
		return exec.CommandContext(ctx, "/usr/bin/open", "-R", path).Run()
	case ev.NodeID == historyNode:
		w.mu.Lock()
		u.history, u.change = !u.history, ""
		w.mu.Unlock()
		return nil
	case ev.NodeID == changesNode:
		var id string
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		switch {
		case strings.HasPrefix(id, dayPrefix) && (ev.Action == "expand" || ev.Action == "collapse"):
			u.collapsed[id] = ev.Action == "collapse"
		case ev.Action == "select" && !strings.HasPrefix(id, dayPrefix):
			u.change = id
		}
		return nil
	case ev.NodeID == restoreNode:
		return w.restoreSelected(ctx, ev.WorkspaceID, u)
	case ev.NodeID == deleteNode:
		return w.deleteSelected(ctx, ev.WorkspaceID, u)
	case ev.NodeID == docNode && (ev.Action == "openDoc" || ev.Action == "openFile"):
		var target string
		if err := json.Unmarshal(ev.Value, &target); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		return w.openLink(ctx, ev.WorkspaceID, u, ev.Action, target)
	}
	return fmt.Errorf("%w: %s %q", errUnknownAction, ev.NodeID, ev.Action)
}

func (w *wiki) fromSession(ctx context.Context, workspaceID, caller string) bool {
	if caller == "" {
		return false
	}
	sessions, err := agentcall.Sessions(ctx, w.h, workspaceID)
	if err != nil {
		return true
	}
	_, ok := agentcall.Find(sessions, caller)
	return ok
}

func (w *wiki) current(ctx context.Context, workspaceID string) (*Store, string, error) {
	ws, err := w.h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, "", err
	}
	u := w.workspace(workspaceID)
	w.mu.Lock()
	id := u.selected
	w.mu.Unlock()
	if id == "" {
		if err := w.renderNow(ctx, workspaceID); err != nil {
			return nil, "", err
		}
		w.mu.Lock()
		id = u.selected
		w.mu.Unlock()
	}
	projects := w.projects(ctx, ws)
	name, page, ok := splitID(id)
	if !ok {
		return nil, "", errNotFound
	}
	for _, p := range projects {
		if p.Name == name {
			return Open(p, w.now), page, nil
		}
	}
	return nil, "", errNotFound
}

func (w *wiki) selectedPath(ctx context.Context, workspaceID string) (string, error) {
	s, page, err := w.current(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.p.Dir, filepath.FromSlash(page)), nil
}

func (w *wiki) deleteSelected(ctx context.Context, workspaceID string, u *workspaceUI) error {
	s, page, err := w.current(ctx, workspaceID)
	if err != nil {
		return err
	}
	if err := s.Delete(ctx, page, userName); err != nil {
		return err
	}
	w.mu.Lock()
	u.selected, u.history, u.find, u.change = "", false, "", ""
	w.mu.Unlock()
	w.changed(ctx, workspaceID, page)
	w.renderOthers(ctx, workspaceID)
	return nil
}

func (w *wiki) restoreSelected(ctx context.Context, workspaceID string, u *workspaceUI) error {
	s, page, err := w.current(ctx, workspaceID)
	if err != nil {
		return err
	}
	w.mu.Lock()
	rev := u.change
	w.mu.Unlock()
	changes, err := s.History(ctx, page, historyLimit)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(changes, func(c Change) bool { return c.Rev == rev })
	if i <= 0 {
		return errNotFound
	}
	if _, err := s.Restore(ctx, page, rev, userName, "restored the version of "+changes[i].At.Local().Format(timeLayout)); err != nil {
		return err
	}
	w.mu.Lock()
	u.change = ""
	w.mu.Unlock()
	w.changed(ctx, workspaceID, page)
	w.renderOthers(ctx, workspaceID)
	return nil
}

func (w *wiki) newPage(ctx context.Context, workspaceID string, u *workspaceUI) error {
	c, err := w.who(ctx, pluginsdk.CommandRun{WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	p, err := wikiroot.Ensure(ctx, c.folder)
	if err != nil {
		return err
	}
	page := untitled + ".md"
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(p.Dir, page)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		page = untitled + "-" + strconv.Itoa(i) + ".md"
	}
	if _, err := Open(p, w.now).Write(ctx, WriteReq{Page: page, Content: "# Untitled\n", Summary: "created", By: userName, Worktree: c.folder}); err != nil {
		return err
	}
	w.mu.Lock()
	u.selected, u.history, u.find, u.change = pageID(p.Name, page), false, "", ""
	w.mu.Unlock()
	w.changed(ctx, workspaceID, page)
	w.renderOthers(ctx, workspaceID)
	return w.h.RunCommand(ctx, "editor.open", workspaceID, map[string]any{"path": filepath.Join(p.Dir, page), "focus": true}, nil)
}

func (w *wiki) openLink(ctx context.Context, workspaceID string, u *workspaceUI, action, target string) error {
	path, anchor, _ := strings.Cut(target, "#")
	ws, err := w.h.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	projects := w.projects(ctx, ws)
	abs := gitrepo.Canonical(filepath.Clean(path))
	if !filepath.IsAbs(path) || !scope(ws, projects)(abs) {
		return errNotInWorkspace
	}
	if action == "openFile" {
		return w.h.RunCommand(ctx, "editor.open", workspaceID, map[string]string{"path": abs}, nil)
	}
	for _, p := range projects {
		dir := gitrepo.Canonical(p.Dir)
		if rel, err := filepath.Rel(dir, abs); err == nil && gitrepo.Within(abs, dir) {
			if page, err := cleanPage(filepath.ToSlash(rel)); err == nil {
				w.mu.Lock()
				u.selected, u.history, u.opening, u.find, u.change = pageID(p.Name, page), anchor == historyAnchor, true, "", ""
				w.mu.Unlock()
				return nil
			}
		}
	}
	return w.h.RunCommand(ctx, "docs.open", workspaceID, map[string]string{"path": target}, nil)
}

func (w *wiki) renderOthers(ctx context.Context, workspaceID string) {
	for _, id := range w.presentIDs() {
		if id != workspaceID {
			logError("render "+id, w.renderNow(ctx, id))
		}
	}
}

func (w *wiki) poll(ctx context.Context) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.pollOnce(ctx)
		}
	}
}

func (w *wiki) pollOnce(ctx context.Context) {
	for _, id := range w.presentIDs() {
		if section, err := w.h.SelectedSection(ctx, id); err != nil || section != sectionID {
			continue
		}
		ws, err := w.h.GetWorkspace(ctx, id)
		if err != nil || ws.Home {
			continue
		}
		dirty := false
		for _, p := range w.projects(ctx, ws) {
			sig := signature(p.Dir)
			w.mu.Lock()
			prev, seen := w.sigs[p.Dir]
			w.sigs[p.Dir] = sig
			w.mu.Unlock()
			if !seen || prev == sig {
				continue
			}
			swept, err := Open(p, w.now).Sweep(ctx)
			logError("sweep "+p.Dir, err)
			for _, page := range swept {
				w.changed(ctx, id, page)
			}
			w.mu.Lock()
			w.sigs[p.Dir] = signature(p.Dir)
			w.mu.Unlock()
			dirty = true
		}
		if dirty {
			w.renderOthers(ctx, id)
		}
		logError("render "+id, w.renderNow(ctx, id))
	}
}

func signature(dir string) string {
	h := sha256.New()
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			fmt.Fprintf(h, "%s\x00%d\x00%d\n", path, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return string(h.Sum(nil))
}

func (w *wiki) settleSelection(u *workspaceUI, before, resolved string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if u.selected == before {
		u.selected = resolved
	}
}
