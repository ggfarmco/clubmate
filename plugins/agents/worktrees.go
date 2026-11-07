package main

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/editorroot"
	"clubmate/plugins/internal/gitrepo"
)

const (
	worktreeIndexCmd = "git.worktree-index"
	codeRootCmd      = "editor.code-root"
	codeRootWait     = time.Second
	staleAfter       = 30 * 24 * time.Hour
	recentWithin     = 7 * 24 * time.Hour

	groupNow    = "now"
	groupLive   = "live"
	groupRecent = "recent"
	groupTask   = "task"
	groupOther  = "other"
	groupHand   = "byHand"
	groupStale  = "stale"

	taskFolderOne     = "1 folder"
	taskFoldersFormat = "%d folders"
	taskWordFormat    = "%d %s"
	taskStateJoin     = " · "
)

var worktreeGroups = []string{groupNow, groupLive, groupRecent, groupTask, groupOther, groupHand, groupStale}

type gitWorktree struct {
	Repo       string    `json:"repo"`
	Path       string    `json:"path"`
	Branch     string    `json:"branch"`
	Head       string    `json:"head"`
	Committed  time.Time `json:"committed"`
	Main       bool      `json:"main"`
	Current    bool      `json:"current"`
	Created    time.Time `json:"created,omitzero"`
	LastOpened time.Time `json:"lastOpened,omitzero"`
	MadeBy     string    `json:"madeBy,omitempty"`
	Merged     bool      `json:"merged"`
	RemoteGone bool      `json:"remoteGone"`
	Locked     bool      `json:"locked,omitempty"`
	LockWhy    string    `json:"lockWhy,omitempty"`
}

type worktreeRow struct {
	gitWorktree
	Now     bool                `json:"now"`
	Live    bool                `json:"live"`
	Active  time.Time           `json:"active,omitzero"`
	Task    *worktreeTask       `json:"task,omitempty"`
	Session *worktreeSessionRef `json:"session,omitempty"`
	ByHand  bool                `json:"byHand"`
	Stale   bool                `json:"stale"`
	Group   string              `json:"group"`
	Folder  string              `json:"folder"`
	Detail  string              `json:"detail"`
	Icon    string              `json:"icon"`
	Tone    string              `json:"tone,omitempty"`
	landing taskRepo
}

type worktreeTask struct {
	Session string `json:"session,omitempty"`
	Title   string `json:"title"`
	State   string `json:"state"`
}

type worktreeSessionRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type worktreeTaskRow struct {
	Session   string   `json:"session"`
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Live      bool     `json:"live"`
	Stale     bool     `json:"stale"`
	Group     string   `json:"group"`
	Icon      string   `json:"icon"`
	Tone      string   `json:"tone,omitempty"`
	Folders   []string `json:"folders"`
	Worktrees []string `json:"worktrees"`
}

type worktreeFolder struct {
	Path      string          `json:"path"`
	Name      string          `json:"name"`
	AddedBy   string          `json:"addedBy,omitempty"`
	AddedAt   time.Time       `json:"addedAt,omitzero"`
	AddedAfk  bool            `json:"addedAfk,omitempty"`
	New       bool            `json:"new"`
	Worktrees int             `json:"worktrees"`
	Live      int             `json:"live"`
	Session   string          `json:"session,omitempty"`
	Caption   string          `json:"caption,omitempty"`
	Byline    string          `json:"byline,omitempty"`
	Summary   string          `json:"summary"`
	Filter    string          `json:"filter"`
	Rows      []int           `json:"rows,omitempty"`
	Sections  []pickerSection `json:"sections,omitempty"`
}

type worktreesResult struct {
	Folders   []worktreeFolder  `json:"folders"`
	Tasks     []worktreeTaskRow `json:"tasks"`
	Worktrees []worktreeRow     `json:"worktrees"`
	Picker    worktreePicker    `json:"picker"`
}

type sessionFact struct {
	rec  record
	live bool
}

type taskLink struct {
	owner record
	look  repoLook
	repo  taskRepo
}

func (a *app) worktreesCommand(ctx context.Context, cmd pluginsdk.CommandRun) (worktreesResult, error) {
	return a.worktreeRows(ctx, cmd.WorkspaceID)
}

func (a *app) codeFolder(ctx context.Context, workspaceID string) string {
	ctx, cancel := context.WithTimeout(ctx, codeRootWait)
	defer cancel()
	var out editorroot.CodeRoot
	if err := a.host.RunCommand(ctx, codeRootCmd, workspaceID, struct{}{}, &out); err != nil {
		return ""
	}
	return out.Folder
}

func joinWorktrees(index []gitWorktree, facts []sessionFact, code string, now time.Time) ([]worktreeRow, []worktreeTaskRow) {
	rows := make([]worktreeRow, len(index))
	byPath := make(map[string][]int, len(index))
	if code == "" && len(index) > 0 {
		code = index[0].Repo
	}
	for i, w := range index {
		rows[i].gitWorktree = w
		rows[i].Now = w.Current && w.Repo == code
		rows[i].Folder = filepath.Base(w.Repo)
		path := gitrepo.Canonical(w.Path)
		byPath[path] = append(byPath[path], i)
	}
	best := make([]*sessionFact, len(rows))
	links := taskLinks(facts, now)
	for i := range facts {
		fact := &facts[i]
		for _, row := range rowsHolding(byPath, fact.rec.Worktree) {
			rows[row].Live = rows[row].Live || fact.live
			if fact.rec.UpdatedAt.After(rows[row].Active) {
				rows[row].Active = fact.rec.UpdatedAt
			}
			if best[row] == nil || betterSession(fact, best[row]) {
				best[row] = fact
			}
		}
	}
	for i := range rows {
		look := rows[i].attach(best[i], links)
		rows[i].classify(now)
		rows[i].describe(look, now)
	}
	tasks := worktreeTaskRows(facts, rows, links, now)
	slices.SortStableFunc(rows, func(x, y worktreeRow) int {
		return cmp.Or(cmp.Compare(groupRank(x.Group), groupRank(y.Group)), y.lastTouched().Compare(x.lastTouched()))
	})
	return rows, tasks
}

func (w *worktreeRow) attach(fact *sessionFact, links map[string]taskLink) repoLook {
	if fact == nil {
		return repoLook{}
	}
	rec := fact.rec
	w.Session = &worktreeSessionRef{ID: rec.ID, Title: rec.Title, Status: rec.Status}
	if link, ok := links[rec.ID]; ok {
		w.Task = &worktreeTask{Session: link.owner.ID, Title: link.owner.Title, State: link.look.word}
		w.landing = link.repo
		return link.look
	}
	look := w.sessionLook(rec)
	w.Task = &worktreeTask{Title: rec.Title, State: look.word}
	return look
}

func (w *worktreeRow) sessionLook(rec record) repoLook {
	switch {
	case w.Live && (rec.Status == statusRunning || rec.Status == statusWaiting):
		return repoLook{word: rec.Status, icon: statusIcons[rec.Status], tone: statusTones[rec.Status]}
	case w.Live:
		return repoLook{word: wordQueued, icon: statusIcons[statusIdle]}
	case w.Merged:
		return repoLook{word: wordMerged, icon: statusIcons[statusDone]}
	}
	return repoLook{word: wordOnBranch, icon: statusIcons[statusDone]}
}

func (w *worktreeRow) classify(now time.Time) {
	w.ByHand = w.Session == nil && !w.Main && w.MadeBy == ""
	w.Stale = !w.Live && !w.Current && !w.Now && !w.Main && (w.Merged || now.Sub(w.lastTouched()) >= staleAfter)
	recent := !w.LastOpened.IsZero() && now.Sub(w.LastOpened) <= recentWithin
	switch {
	case w.Now:
		w.Group = groupNow
	case w.Live:
		w.Group = groupLive
	case recent:
		w.Group = groupRecent
	case w.ByHand:
		w.Group = groupHand
	case w.Stale:
		w.Group = groupStale
	case w.Task != nil && !w.Main:
		w.Group = groupTask
	default:
		w.Group = groupOther
	}
}

func (w *worktreeRow) lastTouched() time.Time {
	return slices.MaxFunc([]time.Time{w.Committed, w.LastOpened, w.Active, w.Created}, time.Time.Compare)
}

func taskLinks(facts []sessionFact, now time.Time) map[string]taskLink {
	links := map[string]taskLink{}
	for _, fact := range facts {
		if !ownsTask(fact.rec) {
			continue
		}
		for _, e := range fact.rec.repoEntries() {
			if e.repo.Session != "" {
				links[e.repo.Session] = taskLink{owner: fact.rec, look: lookOf(e, nil, now), repo: e.repo}
			}
		}
	}
	return links
}

func worktreeTaskRows(facts []sessionFact, rows []worktreeRow, links map[string]taskLink, now time.Time) []worktreeTaskRow {
	out := []worktreeTaskRow{}
	for _, fact := range facts {
		rec := fact.rec
		if !ownsTask(rec) {
			continue
		}
		row := worktreeTaskRow{Session: rec.ID, Slug: rec.Task.Slug, Title: rec.Title, State: taskState(rec, now), Group: groupTask, Stale: true, Folders: make([]string, 0, len(rec.Task.Repos))}
		row.Icon, row.Tone = taskGlyph(rec, now)
		for _, r := range rec.Task.Repos {
			row.Folders = append(row.Folders, r.Path)
		}
		for _, w := range rows {
			if w.Task == nil || w.Task.Session != rec.ID {
				continue
			}
			row.Worktrees = append(row.Worktrees, w.Path)
			row.Live = row.Live || w.Live
			row.Stale = row.Stale && w.Stale
			if groupRank(w.Group) < groupRank(row.Group) {
				row.Group = w.Group
			}
		}
		row.Stale = row.Stale && len(row.Worktrees) > 0
		if len(row.Worktrees) > 0 || !rec.closed() {
			out = append(out, row)
		}
	}
	slices.SortStableFunc(out, func(x, y worktreeTaskRow) int { return cmp.Compare(groupRank(x.Group), groupRank(y.Group)) })
	return out
}

func taskState(rec record, now time.Time) string {
	var order []string
	counts := map[string]int{}
	for _, e := range rec.repoEntries() {
		word := lookOf(e, nil, now).word
		if counts[word] == 0 {
			order = append(order, word)
		}
		counts[word]++
	}
	parts := make([]string, 0, len(order)+1)
	if len(rec.Task.Repos) == 1 {
		parts = append(parts, taskFolderOne)
	} else {
		parts = append(parts, fmt.Sprintf(taskFoldersFormat, len(rec.Task.Repos)))
	}
	for _, word := range order {
		parts = append(parts, fmt.Sprintf(taskWordFormat, counts[word], word))
	}
	return strings.Join(parts, taskStateJoin)
}

func worktreeFolders(w pluginsdk.Workspace, rows []worktreeRow, facts []sessionFact, now time.Time) []worktreeFolder {
	out := make([]worktreeFolder, 0, len(w.Folders))
	for i, f := range w.Folders {
		row := worktreeFolder{Path: f.Path, Name: filepath.Base(f.Path), AddedBy: f.AddedBy, AddedAt: f.AddedAt, AddedAfk: f.AddedAfk, New: !f.Seen}
		row.Session = openSessionOf(row, rows, facts)
		row.Caption, row.Byline = folderWords(row, i == 0, now)
		for _, r := range rows {
			if r.Repo == f.Path {
				row.Worktrees++
				if r.Live {
					row.Live++
				}
			}
		}
		out = append(out, row)
	}
	return out
}

func rowsHolding(byPath map[string][]int, worktree string) []int {
	if worktree == "" {
		return nil
	}
	for dir := gitrepo.Canonical(worktree); ; dir = filepath.Dir(dir) {
		if rows, ok := byPath[dir]; ok {
			return rows
		}
		if dir == filepath.Dir(dir) {
			return nil
		}
	}
}

func betterSession(x, y *sessionFact) bool {
	if x.live != y.live {
		return x.live
	}
	return x.rec.UpdatedAt.After(y.rec.UpdatedAt)
}

func ownsTask(rec record) bool {
	return rec.Task != nil && rec.HandoffTo == ""
}

func groupRank(group string) int {
	return slices.Index(worktreeGroups, group)
}
