package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	repoPrefix   = "repo:"
	finishedKey  = "finished:"
	repoIcon     = "folder"
	mergedTone   = "success"
	siblingPrev  = "sibling/prev"
	siblingNext  = "sibling/next"
	prevKey      = "opt+up"
	nextKey      = "opt+down"
	removeNode   = "folderRemove/"
	afkIcon      = "folder.badge.plus"
	crumbSection = "Agents"
	crumbFormat  = "%d of %d"
	crumbWaits   = "waits for %s"

	wordConnect  = "connect?"
	wordAsking   = "asking"
	wordMerged   = "merged"
	wordOnBranch = "on branch"
	wordFailed   = "failed"
	wordQueued   = "queued"
	wordWaits    = "waits"
	wordRunning  = "running"
	wordPROpen   = "PR open"
	wordLeft     = "left"

	repoNeedsOne  = "1 needs you"
	repoNeedsMany = "%d need you"
	repoOne       = "1 repo"

	afkRowTitle    = "Connected **%s** while you were away"
	afkPROpened    = "opened a PR, didn't merge"
	afkPRAtMost    = "a PR at most, never merges"
	afkRemoved     = "removed from %s"
	afkRemove      = "Remove from %s…"
	removeSessions = "%d sessions work there"
	removeSession  = "1 session works there"
	removeTask     = "task %s lands there"
)

type repoEntry struct {
	repo    taskRepo
	connect bool
	asks    bool
}

type repoLook struct {
	word string
	icon string
	tone string
}

type repoCrumb struct {
	Parent string
	Repo   string
	At     int
	Of     int
	Prev   string
	Next   string
	Waits  string
}

func (r record) pendingRepos() []connectRepo {
	if r.closed() {
		return nil
	}
	var out []connectRepo
	for _, req := range r.ConnectRequests {
		for _, repo := range req.Repos {
			if repo.State == repoPending && !slices.ContainsFunc(out, func(c connectRepo) bool { return c.key() == repo.key() }) {
				out = append(out, repo)
			}
		}
	}
	return out
}

func (r record) repoEntries() []repoEntry {
	pending := r.pendingRepos()
	asks := r.connectWaits()
	var out []repoEntry
	if r.Task != nil {
		for _, repo := range r.Task.Repos {
			i := slices.IndexFunc(pending, func(c connectRepo) bool { return repo.is(c) })
			out = append(out, repoEntry{repo: repo, connect: i >= 0, asks: asks})
			if i >= 0 {
				pending = slices.Delete(pending, i, i+1)
			}
		}
	}
	for _, c := range pending {
		out = append(out, repoEntry{repo: taskRepo{Name: c.Name, Path: c.key()}, connect: true, asks: asks})
	}
	return out
}

func (r taskRepo) is(c connectRepo) bool {
	if c.Path == "" {
		return r.Name == c.Name
	}
	return gitrepo.Canonical(r.Path) == gitrepo.Canonical(c.Path)
}

func (r record) taskFinished() bool {
	return r.Task != nil && !r.closed() && !r.Task.open() && !r.taskWaits() && !turnActive(r)
}

func repoSummary(rec record, kids []record) (string, bool) {
	need := len(slices.DeleteFunc(slices.Clone(kids), func(kid record) bool { return !needsYou(kid) }))
	if rec.Status == statusWaiting && !rec.AsksParent {
		need++
	}
	if rec.connectWaits() {
		need += len(rec.pendingRepos())
	}
	failed := 0
	if rec.Task != nil {
		for _, r := range rec.Task.Repos {
			switch {
			case r.Request != "":
				need++
			case r.State == landFailed:
				failed++
			}
		}
	}
	switch {
	case need == 1 && failed == 0:
		return repoNeedsOne, true
	case need > 0:
		return fmt.Sprintf(repoNeedsMany, need+failed), true
	case failed > 0:
		return fmt.Sprintf(taskFailedFormat, failed), true
	case rec.Task == nil:
		return "", false
	case len(rec.Task.Repos) == 1:
		return repoOne, false
	}
	return fmt.Sprintf(taskReposFormat, len(rec.Task.Repos)), false
}

func lookOf(e repoEntry, kid *sessionRow, now time.Time) repoLook {
	r := e.repo
	asking := repoLook{word: wordAsking, icon: statusIcons[statusAsks], tone: statusTones[statusAsks]}
	switch {
	case e.connect && e.asks:
		return repoLook{word: wordConnect, icon: statusIcons[statusWaiting], tone: statusTones[statusWaiting]}
	case e.connect:
		return repoLook{word: wordConnect, icon: statusIcons[statusIdle]}
	case r.Request != "":
		return asking
	}
	switch r.State {
	case landMerged:
		return repoLook{word: wordMerged, icon: statusIcons[statusDone], tone: mergedTone}
	case landOnBranch:
		return repoLook{word: wordOnBranch, icon: statusIcons[statusDone]}
	case landPROpen:
		return repoLook{word: wordPROpen, icon: "arrow.triangle.pull"}
	case landLeft:
		return repoLook{word: wordLeft, icon: "minus.circle"}
	case landFailed:
		return repoLook{word: wordFailed, icon: statusIcons[statusError], tone: statusTones[statusError]}
	case landAsking:
		if kid != nil && !needsYou(kid.Rec) {
			return repoLook{word: wordAsking, icon: statusIcons[statusAsksParent], tone: statusTones[statusAsksParent]}
		}
		return asking
	case landReady:
		return repoLook{word: wordQueued, icon: statusIcons[statusIdle]}
	case landRunning:
		look := repoLook{word: wordRunning, icon: statusIcons[statusRunning], tone: statusTones[statusRunning]}
		if kid != nil && !kid.Started.IsZero() {
			look.word = shortElapsed(now.Sub(kid.Started))
		}
		return look
	}
	return repoLook{word: wordWaits, icon: statusIcons[statusIdle]}
}

func shortElapsed(d time.Duration) string {
	if d < time.Hour {
		return elapsed(d)
	}
	return strconv.Itoa(int(d/time.Hour)) + " h"
}

func repoOwner(id string) (string, bool) {
	rest, ok := strings.CutPrefix(id, repoPrefix)
	if !ok {
		return "", false
	}
	owner, _, _ := strings.Cut(rest, ":")
	return owner, true
}

func taskParent(item sdui.TreeItem, row sessionRow, entries []repoEntry, children []sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) sdui.TreeItem {
	rec := row.Rec
	folded, ok := collapsed[expandPrefix+rec.ID]
	item.HasChildren = true
	item.Expanded = ok && !folded || !ok && !rec.closed()
	item.Children = repoItems(rec, entries, children, kids, collapsed, now, path, oldest)
	if !item.Expanded {
		item.Children = slices.DeleteFunc(item.Children, func(kid sdui.TreeItem) bool { return !kid.Pinned })
	}
	if rec.closed() {
		return item
	}
	recs := make([]record, 0, len(children))
	for _, kid := range children {
		recs = append(recs, kid.Rec)
	}
	summary, urgent := repoSummary(rec, recs)
	if summary != "" {
		item.Detail, item.DetailFaint = strings.Join(nonEmpty(summary, afkDetail(rec), notifyLabels[row.Notify]), " · "), false
	}
	if urgent {
		item.DetailTone = statusTones[statusWaiting]
		if rec.Status != statusError {
			item.Icon, item.Tone = statusIcons[statusWaiting], statusTones[statusWaiting]
		}
	}
	return item
}

func repoItems(rec record, entries []repoEntry, children []sessionRow, kids map[string][]sessionRow, collapsed map[string]bool, now time.Time, path map[string]bool, oldest map[string]time.Time) []sdui.TreeItem {
	byID := make(map[string]sessionRow, len(children))
	for _, kid := range children {
		byID[kid.Rec.ID] = kid
	}
	items := make([]sdui.TreeItem, 0, len(entries)+len(children))
	for _, e := range entries {
		var kid *sessionRow
		item := sdui.TreeItem{ID: repoPrefix + rec.ID + ":" + e.repo.Path, Action: true}
		if found, ok := byID[e.repo.Session]; ok {
			kid = &found
			item = nestedItem(found, kids, collapsed, now, path, oldest)
			item.Pinned = !oldest[found.Rec.ID].IsZero()
			delete(byID, found.Rec.ID)
		}
		look := lookOf(e, kid, now)
		item.Label, item.LabelIcon, item.Icon, item.Tone = e.repo.Name, repoIcon, look.icon, look.tone
		item.Detail, item.DetailTone, item.DetailFaint = look.word, "", false
		item.Pinned = item.Pinned || look.tone == statusTones[statusWaiting] || look.tone == statusTones[statusError]
		items = append(items, item)
	}
	rest := slices.DeleteFunc(slices.Clone(children), func(kid sessionRow) bool {
		_, ok := byID[kid.Rec.ID]
		return !ok
	})
	others := runItems(rest, rec.ID, kids, collapsed, now, path, oldest)
	for i := range others {
		others[i].Pinned = !oldest[others[i].ID].IsZero()
	}
	return append(items, others...)
}

func (a *app) repoCrumb(s *session) *repoCrumb {
	rec := s.snapshot()
	if rec.RepoPath == "" {
		return nil
	}
	owner := a.topLevel(s).snapshot()
	if owner.Task == nil || len(owner.Task.Repos) < 2 {
		return nil
	}
	i := owner.Task.repoIndex(rec.RepoPath)
	if i < 0 {
		return nil
	}
	crumb := &repoCrumb{
		Parent: cmp.Or(owner.Title, "New session"),
		Repo:   owner.Task.Repos[i].Name,
		At:     i + 1,
		Of:     len(owner.Task.Repos),
		Prev:   a.sibling(owner.Task, i, -1),
		Next:   a.sibling(owner.Task, i, 1),
	}
	if r := owner.Task.Repos[i]; r.State == landWaits && r.For != "" {
		crumb.Waits = fmt.Sprintf(crumbWaits, r.For)
	}
	return crumb
}

func (a *app) sibling(t *taskRecord, i, by int) string {
	for j := i + by; j >= 0 && j < len(t.Repos); j += by {
		if id := t.Repos[j].Session; id != "" && a.session(id) != nil {
			return id
		}
	}
	return ""
}

func crumbNode(sid string, c repoCrumb) sdui.Node {
	return sdui.Crumb(sdui.CrumbProps{
		Parts: []string{crumbSection, c.Parent},
		Leaf:  c.Repo,
		Icon:  repoIcon,
		Note:  fmt.Sprintf(crumbFormat, c.At, c.Of),
	}, siblingButton(sid+"/"+siblingPrev, "Previous repo", "chevron.up", prevKey, c.Prev), siblingButton(sid+"/"+siblingNext, "Next repo", "chevron.down", nextKey, c.Next))
}

func siblingButton(id, label, icon, key, target string) sdui.Node {
	b := sdui.WithShortcut(sdui.WithIconOnly(sdui.WithVariant(sdui.WithAction(sdui.Button(id, label, icon), actSibling), sdui.VariantGhost)), key)
	if target == "" {
		return sdui.WithDisabled(b)
	}
	return b
}

func afkRow(sc screen, en *entry, n noticeUpdate) sdui.Node {
	rec := sc.View.Session
	card := afkContent(sc, en, n)
	title := sdui.HStack(sdui.Markdown(fmt.Sprintf(afkRowTitle, n.Repo)), sdui.Badge(afkLabel, "neutral"))
	row := []sdui.Node{sdui.Icon(afkIcon), sdui.VStack(title, sdui.MutedText(card.Detail)), sdui.Spacer()}
	if card.Remove != "" {
		button := sdui.WithAction(sdui.Button(rec.ID+"/"+removeNode+n.Path, card.Remove, ""), actFolderRemove)
		row = append(row, sdui.WithVariant(button, sdui.VariantGhost))
	}
	return sdui.Card("", "", sdui.HStack(row...))
}

func afkContent(sc screen, en *entry, n noticeUpdate) afkCard {
	rec := sc.View.Session
	_, present := folderIn(sc.Folders, n.Path)
	outcome := afkPRAtMost
	if rec.Task != nil {
		if i := rec.Task.repoIndex(n.Path); i >= 0 && rec.Task.Repos[i].State == landPROpen {
			outcome = afkPROpened
		}
	}
	if !present && sc.Workspace != "" {
		outcome = fmt.Sprintf(afkRemoved, sc.Workspace)
	}
	card := afkCard{Detail: strings.Join(nonEmpty(clockOf(en.At), outcome), " · ")}
	if present && sc.Workspace != "" {
		card.Remove = fmt.Sprintf(afkRemove, sc.Workspace)
	}
	return card
}

func clockOf(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Local().Format("15:04")
}
