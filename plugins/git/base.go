package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	basePrefix      = "worktree-base/"
	baseTreePrefix  = "worktree-base-tree/"
	baseImportedKey = basePrefix + "imported"
	baseConfigKey   = ".clubmate-base"
	baseImportLimit = 5 * time.Minute
	baseRetryEvery  = time.Minute
)

var errBaseRefused = errors.New("git.worktree-base is for the agents plugin only")

type baseResult struct {
	Base string `json:"base,omitempty"`
	Live bool   `json:"live,omitempty"`
}

type baseImport struct {
	mu     sync.Mutex
	done   bool
	walked bool
	failed map[string]string
	tried  time.Time
}

func baseRecordKey(primary, branch string) string {
	return basePrefix + primary + "/" + branch
}

func baseTreeKey(tree string) string {
	return baseTreePrefix + tree
}

func (b *baseImport) ensure(ctx context.Context, h *pluginsdk.Host, primary string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return nil
	}
	if !b.walked {
		var done bool
		found, err := h.StorageGet(ctx, baseImportedKey, &done)
		if err != nil {
			return fmt.Errorf("import worktree bases: %w", err)
		}
		if found && done {
			b.done = true
			return nil
		}
		folders, err := baseFolders(ctx, h)
		if err != nil {
			return fmt.Errorf("import worktree bases: %w", err)
		}
		b.failed, b.walked = map[string]string{}, true
		b.retry(ctx, h, folders, map[string]bool{})
	} else if path, ok := b.failed[primary]; ok {
		b.retry(ctx, h, []string{path}, map[string]bool{})
	} else if time.Since(b.tried) >= baseRetryEvery {
		b.retry(ctx, h, slices.Collect(maps.Values(b.failed)), map[string]bool{})
	}
	if len(b.failed) > 0 {
		if _, ok := b.failed[primary]; ok {
			return fmt.Errorf("import worktree bases of %s failed; retried on the next read", primary)
		}
		return nil
	}
	if err := h.StorageSet(ctx, baseImportedKey, true); err != nil {
		return fmt.Errorf("import worktree bases: %w", err)
	}
	b.done = true
	return nil
}

func (b *baseImport) retry(ctx context.Context, h *pluginsdk.Host, folders []string, seen map[string]bool) {
	b.tried = time.Now()
	for _, path := range folders {
		key, err := importFolder(ctx, h, path, seen)
		for k, p := range b.failed {
			if p == path {
				delete(b.failed, k)
			}
		}
		if err != nil {
			logError("import bases of "+path, err)
			b.failed[key] = path
		}
	}
}

func (b *baseImport) start(h *pluginsdk.Host) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), baseImportLimit)
		defer cancel()
		logError("import bases", b.ensure(ctx, h, ""))
	}()
}

func baseFolders(ctx context.Context, h *pluginsdk.Host) ([]string, error) {
	list, err := h.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	var folders []string
	for _, w := range list {
		for _, r := range w.Folders {
			folders = append(folders, r.Path)
		}
	}
	return folders, nil
}

func importFolder(ctx context.Context, h *pluginsdk.Host, path string, seen map[string]bool) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	f, err := gitrepo.OpenFolder(ctx, path)
	switch {
	case errors.Is(err, gitrepo.ErrNotRepo):
		return "", nil
	case err != nil:
		return gitrepo.Canonical(path), err
	case len(f.Trees) == 0 || seen[f.Trees[0].Real]:
		return "", nil
	}
	seen[f.Trees[0].Real] = true
	return f.Trees[0].Real, importRepoBases(ctx, h, f.Trees[0])
}

func importRepoBases(ctx context.Context, h *pluginsdk.Host, primary gitrepo.Worktree) error {
	out, err := gitrepo.Run(ctx, primary.Path, "config", "--null", "--get-regexp", `^branch\..*\.clubmate-base$`)
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read bases of %s: %w", primary.Path, err)
	}
	for entry := range strings.SplitSeq(string(out), "\x00") {
		key, base, _ := strings.Cut(entry, "\n")
		branch := strings.TrimSuffix(strings.TrimPrefix(key, "branch."), baseConfigKey)
		if base == "" || branch == "" || branch == key {
			continue
		}
		var stored string
		found, err := h.StorageGet(ctx, baseRecordKey(primary.Real, branch), &stored)
		if err != nil {
			return err
		}
		if !found {
			if err := h.StorageSet(ctx, baseRecordKey(primary.Real, branch), base); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *app) base(ctx context.Context, h *pluginsdk.Host, f *gitrepo.Folder, branch string) (baseResult, error) {
	if err := a.bases.ensure(ctx, h, f.Trees[0].Real); err != nil {
		return baseResult{}, err
	}
	var base string
	found, err := h.StorageGet(ctx, baseRecordKey(f.Trees[0].Real, branch), &base)
	if err != nil {
		return baseResult{}, err
	}
	if i := slices.IndexFunc(f.Trees, func(t gitrepo.Worktree) bool { return t.Branch == branch && !t.Bare }); !found && i >= 0 {
		if _, err := h.StorageGet(ctx, baseTreeKey(f.Trees[i].Real), &base); err != nil {
			return baseResult{}, err
		}
	}
	if base == "" || base == branch {
		return baseResult{}, nil
	}
	return baseResult{Base: base, Live: slices.ContainsFunc(f.Trees, func(t gitrepo.Worktree) bool {
		if t.Branch != base || t.Bare {
			return false
		}
		_, err := os.Stat(t.Path)
		return err == nil
	})}, nil
}

func (a *app) recordBase(ctx context.Context, h *pluginsdk.Host, primary, branch, base string) error {
	if err := a.bases.ensure(ctx, h, primary); err != nil {
		return err
	}
	return storeBase(ctx, h, baseRecordKey(primary, branch), base)
}

func recordTreeBase(ctx context.Context, h *pluginsdk.Host, tree, base string) error {
	return storeBase(ctx, h, baseTreeKey(gitrepo.Canonical(tree)), base)
}

func storeBase(ctx context.Context, h *pluginsdk.Host, key, base string) error {
	if base == "" {
		return h.StorageDelete(ctx, key)
	}
	return h.StorageSet(ctx, key, base)
}

func (a *app) dropBaseIfGone(ctx context.Context, h *pluginsdk.Host, primary gitrepo.Worktree, branch string) {
	if !hasRef(ctx, primary.Path, headsPrefix+branch) {
		logError("drop base of "+branch, a.recordBase(ctx, h, primary.Real, branch, ""))
	}
}

func worktreeBaseCommand(ctx context.Context, a *app, c call) (any, error) {
	if c.via != discardPlugin || c.agentCall {
		return nil, errBaseRefused
	}
	if !filepath.IsAbs(c.args.Path) {
		return nil, errRelativePath
	}
	f, err := gitrepo.OpenFolder(ctx, c.args.Path)
	switch {
	case errors.Is(err, gitrepo.ErrNotRepo):
		return baseResult{}, nil
	case err != nil:
		return nil, gitError(nil, err)
	}
	branch := c.args.Branch
	if branch == "" {
		branch = f.Checkout.Branch
	}
	if branch == "" {
		return baseResult{}, nil
	}
	return a.base(ctx, c.h, f, branch)
}
