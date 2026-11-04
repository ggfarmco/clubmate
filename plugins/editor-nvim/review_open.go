package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	defaultReviewBase = "main"
	reviewStorePrefix = "review/"
)

var (
	errReviewCwd    = errors.New("review: cwd must be absolute")
	errReviewRev    = errors.New("review: base and head must be branch names or commits")
	errNoReviewTree = errors.New("review: no worktree to review; pass cwd")
	errNoEditor     = errors.New("review: no Neovim for this workspace")
	revPattern      = regexp.MustCompile(`^[A-Za-z0-9._/@^~][A-Za-z0-9._/@^~-]*$`)
)

type reviewArgs struct {
	Base string `json:"base"`
	Head string `json:"head"`
	Cwd  string `json:"cwd"`
}

type reviewResult struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	Base     string `json:"base"`
	Head     string `json:"head"`
}

type openReviewReq struct {
	Key         reviewKey
	BaseRev     string
	Left        string
	Opener      string
	OpenerTitle string
}

type reviewState struct {
	Opener         string `json:"opener,omitempty"`
	OpenerTitle    string `json:"openerTitle,omitempty"`
	Recipient      string `json:"recipient,omitempty"`
	RecipientTitle string `json:"recipientTitle,omitempty"`
	Sent           int    `json:"sent,omitempty"`
}

type agentSession struct {
	ID        string    `json:"id"`
	Worktree  string    `json:"worktree"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Service   bool      `json:"service"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a *app) reviewCommand(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (reviewResult, error) {
	var args reviewArgs
	if len(cmd.Args) > 0 {
		dec := json.NewDecoder(bytes.NewReader(cmd.Args))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			return reviewResult{}, fmt.Errorf("review args: %w", err)
		}
	}
	base := cmp.Or(args.Base, defaultReviewBase)
	if !revPattern.MatchString(base) || (args.Head != "" && !revPattern.MatchString(args.Head)) {
		return reviewResult{}, errReviewRev
	}
	if args.Cwd != "" && !filepath.IsAbs(args.Cwd) {
		return reviewResult{}, errReviewCwd
	}
	caller, title := a.callerSession(ctx, h, cmd)
	from := cmp.Or(args.Cwd, caller.Worktree)
	if from == "" {
		if e, err := a.active(ctx, h, cmd.WorkspaceID); err == nil && e != nil {
			from = e.root
		}
	}
	if from == "" {
		return reviewResult{}, errNoReviewTree
	}
	worktree, head, err := resolveReviewHead(ctx, from, args.Head)
	if err != nil {
		return reviewResult{}, err
	}
	baseRev, left, err := reviewBase(ctx, worktree, base, head)
	if err != nil {
		return reviewResult{}, err
	}
	r, err := a.openReview(ctx, h, cmd.WorkspaceID, openReviewReq{
		Key: reviewKey{Worktree: worktree, Base: base, Head: head}, BaseRev: baseRev, Left: left, Opener: caller.ID, OpenerTitle: title,
	})
	if err != nil {
		return reviewResult{}, err
	}
	return reviewResult{ID: r.ID, Worktree: worktree, Base: base, Head: head}, nil
}

func (a *app) callerSession(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (agentSession, string) {
	if cmd.Caller == "" {
		return agentSession{}, ""
	}
	s := a.agentSession(ctx, h, cmd.WorkspaceID, cmd.Caller)
	s.ID = cmd.Caller
	return s, s.Title
}

func (a *app) agentSession(ctx context.Context, h *pluginsdk.Host, workspaceID, id string) agentSession {
	var list []agentSession
	if err := h.RunCommand(ctx, "agents.sessions", workspaceID, map[string]string{"session": id}, &list); err != nil || len(list) == 0 {
		return agentSession{ID: id}
	}
	return list[0]
}

func resolveReviewHead(ctx context.Context, from, head string) (string, string, error) {
	top, ok := gitrepo.RepoRoot(from)
	if !ok {
		return "", "", fmt.Errorf("%w: %s", gitrepo.ErrNotRepo, from)
	}
	trees, err := gitrepo.Worktrees(ctx, top)
	if err != nil {
		return "", "", err
	}
	caller := top
	if t := gitrepo.Longest(trees, gitrepo.Canonical(from)); t != nil {
		caller = t.Real
	}
	if head == "" {
		return caller, "", nil
	}
	for _, t := range trees {
		if !t.Bare && t.Branch == head {
			return t.Real, "", nil
		}
	}
	if _, err := gitrepo.ResolveCommit(ctx, caller, head); err != nil {
		return "", "", fmt.Errorf("review: unknown head %q: %w", head, err)
	}
	return caller, head, nil
}

func reviewBase(ctx context.Context, worktree, base, head string) (string, string, error) {
	if _, err := gitrepo.ResolveCommit(ctx, worktree, base); err != nil {
		return "", "", fmt.Errorf("review: unknown base %q: %w", base, err)
	}
	if head != "" || shaPattern.MatchString(base) {
		return base, "", nil
	}
	out, err := gitrepo.Run(ctx, worktree, "merge-base", base, "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("review: merge-base of %s and HEAD: %w", base, err)
	}
	return strings.TrimSpace(string(out)), base, nil
}

func (a *app) openReview(ctx context.Context, h *pluginsdk.Host, workspaceID string, req openReviewReq) (*review, error) {
	e, c, err := a.attach(ctx, h, workspaceID, req.Key.Worktree)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoEditor
	}
	r, err := a.startReview(ctx, h, workspaceID, e.root, c, req)
	if err != nil {
		e.setOpenError(shortError(err.Error()))
		a.scheduleStatus(h, e)
		return nil, err
	}
	focusMain(ctx, h, workspaceID)
	return r, nil
}

func (a *app) startReview(ctx context.Context, h reviewHost, ws, root string, nv reviewer, req openReviewReq) (*review, error) {
	a.reviewMu.Lock()
	r, err := a.openReviewLocked(ctx, h, ws, root, nv, req)
	a.reviewMu.Unlock()
	if err != nil {
		return nil, err
	}
	a.refreshMeta(ctx, h, r.ID)
	return r, nil
}

func (a *app) openReviewLocked(ctx context.Context, h reviewHost, ws, root string, nv reviewer, req openReviewReq) (*review, error) {
	if r, ok := a.reviews[reviewID(req.Key)]; ok && r.Workspace == ws {
		if err := r.nv.reviewFocus(r.Tab); err == nil {
			r.setOpener(req)
			a.reviewSel[ws] = r.ID
			a.persistReview(ctx, h, r)
			a.reviewChangedLocked(ctx, h, r)
			return r, nil
		}
		a.removeReviewLocked(ctx, h, r)
	} else if ok {
		if err := r.nv.reviewClose(r.Tab); err != nil {
			logError("review move", err)
		}
		a.removeReviewLocked(ctx, h, r)
		a.renderReviewsLocked(ctx, h, r.Workspace)
	}
	if r := a.adoptOpenLocked(ctx, h, ws, root, nv, reviewID(req.Key)); r != nil {
		if err := nv.reviewFocus(r.Tab); err != nil {
			logError("review focus", err)
		}
		r.setOpener(req)
		a.persistReview(ctx, h, r)
		a.reviewChangedLocked(ctx, h, r)
		return r, nil
	}
	tab, err := nv.reviewOpen(reviewOpenArgs{
		Cwd: req.Key.Worktree, Base: req.BaseRev, Head: req.Key.Head, Left: req.Left,
		Host: reviewTabHost{ID: reviewID(req.Key), Worktree: req.Key.Worktree, Base: req.Key.Base, Head: req.Key.Head},
	})
	if err != nil {
		return nil, err
	}
	r := a.trackReviewLocked(ctx, h, ws, root, req.Key, tab, nv)
	r.setOpener(req)
	if layout := a.reviewLayouts[ws]; layout != "" {
		if err := nv.reviewLayout(tab, layout); err != nil {
			logError("review layout", err)
		}
	}
	a.persistReview(ctx, h, r)
	a.reviewChangedLocked(ctx, h, r)
	return r, nil
}

func (a *app) trackReviewLocked(ctx context.Context, h reviewHost, ws, root string, key reviewKey, tab int, nv reviewer) *review {
	r := newReview(ws, root, key, tab, nv)
	var err error
	if r.Info, err = nv.reviewInfo(tab); err != nil {
		logError("review info", err)
	}
	a.restoreReview(ctx, h, r)
	if err := r.refresh(true); err != nil {
		logError("review entries", err)
	}
	a.addReviewLocked(r)
	a.watchReviewer(h, nv)
	return r
}

func (a *app) adoptReviews(ctx context.Context, h reviewHost, ws, root string, nv reviewer) {
	a.reviewMu.Lock()
	adopted := a.adoptLocked(ctx, h, ws, root, nv, "")
	a.reviewMu.Unlock()
	for _, r := range adopted {
		a.refreshMeta(ctx, h, r.ID)
	}
}

func (a *app) adoptOpenLocked(ctx context.Context, h reviewHost, ws, root string, nv reviewer, id string) *review {
	if adopted := a.adoptLocked(ctx, h, ws, root, nv, id); len(adopted) > 0 {
		return adopted[0]
	}
	return nil
}

func (a *app) adoptLocked(ctx context.Context, h reviewHost, ws, root string, nv reviewer, only string) []*review {
	tabs, err := nv.reviewList()
	if err != nil {
		logError("review list", err)
		return nil
	}
	var adopted []*review
	for _, t := range tabs {
		key := reviewKey{Worktree: t.Host.Worktree, Base: t.Host.Base, Head: t.Host.Head}
		if t.Host.ID == "" || t.Host.ID != reviewID(key) || (only != "" && t.Host.ID != only) {
			continue
		}
		if r, ok := a.reviews[t.Host.ID]; ok && (only == "" || r.nv == nv) {
			continue
		}
		if _, ok := a.reviewLayouts[ws]; !ok && (t.Layout == layoutInline || t.Layout == layoutSplit) {
			a.reviewLayouts[ws] = t.Layout
		}
		r := a.trackReviewLocked(ctx, h, ws, root, key, t.Tab, nv)
		a.reviewChangedLocked(ctx, h, r)
		adopted = append(adopted, r)
	}
	return adopted
}

func (r *review) setOpener(req openReviewReq) {
	if req.Opener != "" {
		r.Opener, r.OpenerTitle = req.Opener, req.OpenerTitle
	}
}

func (a *app) restoreReview(ctx context.Context, h reviewHost, r *review) {
	var st reviewState
	ok, err := h.StorageGet(ctx, reviewStorePrefix+r.ID, &st)
	if err != nil {
		logError("restore review", err)
	}
	if !ok {
		return
	}
	r.Opener, r.OpenerTitle, r.Recipient, r.RecipientTitle, r.Sent = st.Opener, st.OpenerTitle, st.Recipient, st.RecipientTitle, st.Sent
}

func (a *app) persistReview(ctx context.Context, h reviewHost, r *review) {
	st := reviewState{Opener: r.Opener, OpenerTitle: r.OpenerTitle, Recipient: r.Recipient, RecipientTitle: r.RecipientTitle, Sent: r.Sent}
	if err := h.StorageSet(ctx, reviewStorePrefix+r.ID, st); err != nil {
		logError("persist review", err)
	}
}

func (a *app) watchReviewer(h reviewHost, nv reviewer) {
	c, ok := nv.(*client)
	if !ok || a.reviewWatched[c] {
		return
	}
	a.reviewWatched[c] = true
	go func() {
		<-c.done
		a.dropReviewsOf(context.Background(), h, nv)
		a.reviewMu.Lock()
		delete(a.reviewWatched, c)
		a.reviewMu.Unlock()
	}()
}
