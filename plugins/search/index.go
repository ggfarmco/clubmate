package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/plugins/internal/gitrepo"
)

const (
	maxFiles     = 200_000
	lsFilesLimit = 64 << 20
	gitlinkMode  = "160000 "
)

type index struct {
	home string
}

type root struct {
	folder   string
	resolved string
	label    string
}

func newIndex() index {
	home, err := os.UserHomeDir()
	if err != nil {
		return index{}
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	return index{home: home}
}

func (x index) Files(ctx context.Context, folders []string) ([]string, error) {
	var out []string
	for _, r := range newRoots(folders) {
		root, label := r.resolved, r.label
		var err error
		if x.isRepo(root) {
			err = x.listRepo(ctx, root, label, &out)
		} else {
			err = x.walk(ctx, root, label, &out)
		}
		if err != nil {
			return nil, err
		}
		if len(out) >= maxFiles {
			return out[:maxFiles], nil
		}
	}
	return out, nil
}

func (x index) listRepo(ctx context.Context, repo, prefix string, out *[]string) error {
	cached, err := lsFiles(ctx, repo, "--cached", "--stage")
	if err != nil {
		return err
	}
	others, err := lsFiles(ctx, repo, "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	prev := ""
	for entry := range bytes.SplitSeq(cached, []byte{0}) {
		meta, rel, ok := strings.Cut(string(entry), "\t")
		if !ok || rel == prev {
			continue
		}
		prev = rel
		if strings.HasPrefix(meta, gitlinkMode) {
			err = x.nested(ctx, repo, rel, join(prefix, rel), out)
		} else {
			*out = append(*out, join(prefix, rel))
		}
		if err != nil || len(*out) >= maxFiles {
			return err
		}
	}
	for entry := range bytes.SplitSeq(others, []byte{0}) {
		rel := string(entry)
		if rel == "" {
			continue
		}
		if dir, ok := strings.CutSuffix(rel, "/"); ok {
			err = x.nested(ctx, repo, dir, join(prefix, dir), out)
		} else {
			*out = append(*out, join(prefix, rel))
		}
		if err != nil || len(*out) >= maxFiles {
			return err
		}
	}
	return nil
}

func (x index) nested(ctx context.Context, repo, rel, prefix string, out *[]string) error {
	dir, err := filepath.EvalSymlinks(filepath.Join(repo, rel))
	if err != nil || dir == repo || !within(repo, dir) {
		return nil
	}
	if !x.isRepo(dir) {
		return x.walk(ctx, dir, prefix, out)
	}
	err = x.listRepo(ctx, dir, prefix, out)
	if err == nil || ctx.Err() != nil {
		return err
	}
	logError("index nested repo", err)
	return x.walk(ctx, dir, prefix, out)
}

func (x index) walk(ctx context.Context, root, prefix string, out *[]string) error {
	guarded := x.guarded(root)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return skip(d)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(*out) >= maxFiles {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.Name() == ".git" && path != root {
			return skip(d)
		}
		if !d.IsDir() {
			if d.Type().IsRegular() || d.Type()&fs.ModeSymlink != 0 {
				*out = append(*out, join(prefix, filepath.ToSlash(rel)))
			}
			return nil
		}
		if path == root {
			return nil
		}
		if name := d.Name(); name == "node_modules" || strings.HasPrefix(name, ".") || guarded && filepath.Dir(path) == x.home {
			return filepath.SkipDir
		}
		if !x.isRepo(path) {
			return nil
		}
		err = x.listRepo(ctx, path, join(prefix, filepath.ToSlash(rel)), out)
		switch {
		case err == nil:
			return filepath.SkipDir
		case ctx.Err() != nil:
			return err
		}
		logError("index nested repo", err)
		return nil
	})
}

func (x index) isRepo(dir string) bool {
	return !x.guarded(dir) && exists(filepath.Join(dir, ".git"))
}

func (x index) guarded(dir string) bool {
	return x.home != "" && (dir == x.home || strings.HasPrefix(x.home, strings.TrimSuffix(dir, "/")+"/"))
}

func lsFiles(ctx context.Context, repo string, args ...string) ([]byte, error) {
	data, truncated, err := gitrepo.RunLimited(ctx, repo, lsFilesLimit, append([]string{"ls-files", "-z"}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("list files in %s: %w", repo, err)
	}
	if truncated {
		data = data[:bytes.LastIndexByte(data, 0)+1]
	}
	return data, nil
}

func newRoots(folders []string) []root {
	var all []root
	for i, label := range labels(folders) {
		resolved, err := filepath.EvalSymlinks(folders[i])
		if err != nil {
			logError("resolve "+folders[i], err)
			continue
		}
		all = append(all, root{folder: folders[i], resolved: resolved, label: label})
	}
	out := make([]root, 0, len(all))
	for _, r := range all {
		inner := slices.ContainsFunc(all, func(o root) bool { return o.resolved != r.resolved && within(o.resolved, r.resolved) })
		if !inner {
			out = append(out, r)
		}
	}
	return out
}

func labels(folders []string) []string {
	out := make([]string, len(folders))
	if len(folders) < 2 {
		return out
	}
	taken := map[string]bool{}
	for i, f := range folders {
		base := filepath.Base(f)
		label := base
		for n := 2; taken[label]; n++ {
			label = base + "-" + strconv.Itoa(n)
		}
		taken[label] = true
		out[i] = label
	}
	return out
}

func within(root, path string) bool {
	return path == root || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/")
}

func join(prefix, rel string) string {
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func skip(d fs.DirEntry) error {
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}
