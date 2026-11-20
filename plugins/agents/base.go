package main

import (
	"context"
	"slices"
	"strings"
	"sync"
)

const worktreeBaseCmd = "git.worktree-base"

type worktreeBase struct {
	Base string `json:"base"`
	Live bool   `json:"live"`
}

type baseCache struct {
	mu      sync.Mutex
	bases   map[string]map[string]string
	protect map[string][]string
}

func (a *app) worktreeBase(ctx context.Context, workspace, path, branch string) (worktreeBase, error) {
	var out worktreeBase
	err := a.host.RunCommand(ctx, worktreeBaseCmd, workspace, map[string]string{"path": path, "branch": branch}, &out)
	return out, err
}

func (a *app) baseOf(ctx context.Context, rec record) string {
	branch := a.branches.name(ctx, rec.Workspace, rec.Worktree)
	if branch == "" || detachedBranch(branch) {
		return ""
	}
	key := rec.Worktree + "\x00" + branch
	c := &a.baseCache
	c.mu.Lock()
	base, ok := c.bases[rec.Workspace][key]
	c.mu.Unlock()
	if ok {
		return base
	}
	b, err := a.worktreeBase(ctx, rec.Workspace, rec.Worktree, branch)
	if err != nil {
		return ""
	}
	if b.Live && b.Base != branch {
		base = b.Base
	}
	c.mu.Lock()
	if c.bases == nil {
		c.bases = map[string]map[string]string{}
	}
	if c.bases[rec.Workspace] == nil {
		c.bases[rec.Workspace] = map[string]string{}
	}
	c.bases[rec.Workspace][key] = base
	c.mu.Unlock()
	return base
}

func (c *baseCache) reset(workspace string) {
	c.mu.Lock()
	delete(c.bases, workspace)
	c.mu.Unlock()
}

func (a *app) sessionBases(ctx context.Context, rec record) []string {
	b, err := a.worktreeBase(ctx, rec.Workspace, rec.Worktree, "")
	if err != nil || b.Base == "" {
		return a.lineageBases(rec)
	}
	a.keepBase(rec.ID, b.Base)
	return appendNew(a.lineageBases(rec), b.Base)
}

func (a *app) lineageBases(rec record) []string {
	if s := a.session(rec.ID); s != nil {
		rec = s.snapshot()
	}
	var out []string
	for r := range a.ancestry(rec) {
		for _, base := range r.Bases {
			out = appendNew(out, base)
		}
	}
	return out
}

func (a *app) protectList(rec record, protect, bases []string) []string {
	c := &a.baseCache
	c.mu.Lock()
	defer c.mu.Unlock()
	out := slices.Clone(c.protect[rec.ID])
	for _, ref := range protect {
		out = appendNew(out, ref)
	}
	for _, base := range bases {
		out = appendNew(out, "refs/heads/"+base)
	}
	if c.protect == nil {
		c.protect = map[string][]string{}
	}
	c.protect[rec.ID] = out
	return out
}

func appendNew(list []string, ref string) []string {
	if slices.Contains(list, ref) {
		return list
	}
	return append(list, ref)
}

func (a *app) keepBase(id, base string) {
	s := a.session(id)
	if s == nil {
		return
	}
	s.mu.Lock()
	added := !slices.Contains(s.rec.Bases, base)
	if added {
		s.rec.Bases = append(s.rec.Bases, base)
	}
	s.mu.Unlock()
	if added {
		a.notes.mark(s, changeUpdate, true)
	}
}

func (a *app) sessionBase(id, into string) bool {
	return slices.ContainsFunc(a.lineageBases(record{ID: id}), func(b string) bool { return strings.EqualFold(b, into) })
}
