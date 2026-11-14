package main

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	taskRowConnect  = "not connected yet"
	landCommitsMax  = "50"
	landCommitsSep  = "\t"
	landCommitsLine = "%h%x09%s"
)

type taskRow struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Session string `json:"session,omitempty"`
	Word    string `json:"word,omitempty"`
	Tone    string `json:"tone,omitempty"`
	Connect bool   `json:"connect,omitempty"`
}

type afkCard struct {
	Detail string `json:"detail"`
	Remove string `json:"remove,omitempty"`
}

type landCommit struct {
	Sha   string `json:"sha"`
	Title string `json:"title"`
}

type landCommitsResult struct {
	Commits []landCommit `json:"commits"`
	More    int          `json:"more,omitempty"`
	Tip     string       `json:"tip,omitempty"`
	Detail  string       `json:"detail"`
}

func (a *app) withTaskContent(v *transcriptView, w pluginsdk.Workspace) {
	rec := v.Session
	sc := screen{View: *v, Workspace: w.Name, Folders: w, Now: time.Now(), Repos: reposScreenData{Kids: a.repoKids(rec), Phone: true}}
	if card, ok := requestCard(sc); ok && !rec.closed() {
		v.Request = &card
	}
	if rec.Task != nil && len(rec.Task.Repos) > 0 {
		p, bars := reposData(sc)
		card := sdui.Repos(reposCardID(rec.ID), p, bars...)
		v.Repos = &card
	}
	for _, en := range v.Entries {
		var n noticeUpdate
		if en.Kind == entryNotice && json.Unmarshal(en.Notice, &n) == nil && n.Repo != "" {
			card := afkContent(sc, en, n)
			en.AFK = &card
		}
	}
}

func (i *sessionInfo) withTask(kids []record) {
	rec := i.record
	i.Requests = rec.openRequests()
	i.TaskRows = taskRowsOf(rec, kids)
	if rec.Task == nil {
		return
	}
	i.TaskBadge = taskBadge(rec)
	i.TaskFinished = rec.taskFinished()
	i.Archivable = archivableNow(rec) && !slices.ContainsFunc(kids, turnActive)
}

func (r record) openRequests() []string {
	if r.closed() {
		return nil
	}
	var out []string
	for _, req := range r.ConnectRequests {
		if req.waits() {
			out = append(out, req.ID)
		}
	}
	if r.Task != nil {
		out = append(out, r.Task.requests()...)
	}
	return out
}

func taskRowsOf(rec record, kids []record) []taskRow {
	entries := rec.repoEntries()
	out := make([]taskRow, 0, len(entries))
	for _, e := range entries {
		var kid *sessionRow
		if i := slices.IndexFunc(kids, func(k record) bool { return k.ID == e.repo.Session }); i >= 0 && e.repo.Session != "" {
			kid = &sessionRow{Rec: kids[i]}
		}
		look := lookOf(e, kid, time.Time{})
		row := taskRow{Name: e.repo.Name, Path: e.repo.Path, Session: e.repo.Session, Word: look.word, Tone: look.tone, Connect: e.connect}
		switch {
		case e.connect:
			row.Word = taskRowConnect
		case kid != nil && turnActive(kid.Rec):
			row.Word, row.Tone = "", ""
		}
		out = append(out, row)
	}
	return out
}

func taskBadge(rec record) string {
	if badge := landBadge(rec); badge != "" {
		return badge
	}
	prs := 0
	for _, r := range rec.Task.Repos {
		if r.State == landPROpen {
			prs++
		}
	}
	if prs == 0 {
		return ""
	}
	return counted(prs, "PR") + " open"
}

func (a *app) landCommitsCommand(ctx context.Context, cmd pluginsdk.CommandRun, args landAnswerArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errLandUser
	}
	if out, ok, err := a.gateCommits(ctx, cmd.WorkspaceID, args.Request); ok || err != nil {
		return out, err
	}
	a.landMu.Lock()
	_, task, i, ok := a.findLand(cmd.WorkspaceID, args.Request)
	a.landMu.Unlock()
	if !ok {
		return nil, errLandStale
	}
	r := task.Repos[i]
	tip := a.liveTip(ctx, r)
	out := landCommitsResult{Commits: []landCommit{}, Tip: tip, Detail: r.landLine()}
	if r.Base == "" || tip == "" {
		return out, nil
	}
	revs := r.Base + ".." + tip
	top := repoTop(r.Path)
	log, err := gitrepo.Run(ctx, top, "log", "--format="+landCommitsLine, "-n", landCommitsMax, revs)
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(string(log)) {
		sha, title, _ := strings.Cut(strings.TrimRight(line, "\n"), landCommitsSep)
		out.Commits = append(out.Commits, landCommit{Sha: sha, Title: title})
	}
	if count, err := gitrepo.Run(ctx, top, "rev-list", "--count", revs); err == nil {
		r.Commits, _ = strconv.Atoi(strings.TrimSpace(string(count)))
		out.Detail = r.landLine()
	}
	out.More = max(r.Commits-len(out.Commits), 0)
	return out, nil
}

func (a *app) liveTip(ctx context.Context, r taskRepo) string {
	if kid := a.session(r.Session); kid != nil {
		if tip := headOf(ctx, kid.snapshot().Worktree); tip != "" {
			return tip
		}
	}
	return r.Tip
}
