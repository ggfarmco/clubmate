package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"clubmate/plugins/internal/gitrepo"
)

const (
	maxFiles     = 200_000
	lsFilesLimit = 64 << 20
)

type index struct {
	home string
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
	for i, label := range labels(folders) {
		if x.blocked(folders[i]) {
			continue
		}
		if err := x.list(ctx, folders[i], label, &out); err != nil {
			return nil, err
		}
		if len(out) >= maxFiles {
			return out[:maxFiles], nil
		}
	}
	return out, nil
}

func (x index) list(ctx context.Context, dir, prefix string, out *[]string) error {
	if dir != x.home && exists(filepath.Join(dir, ".git")) {
		return x.listRepo(ctx, dir, prefix, out)
	}
	return x.walk(ctx, dir, prefix, out)
}

func (x index) listRepo(ctx context.Context, repo, prefix string, out *[]string) error {
	data, truncated, err := gitrepo.RunLimited(ctx, repo, lsFilesLimit, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return fmt.Errorf("list files in %s: %w", repo, err)
	}
	if truncated {
		data = data[:bytes.LastIndexByte(data, 0)+1]
	}
	submodules, err := submodules(ctx, repo)
	if err != nil {
		return err
	}
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		if len(*out) >= maxFiles {
			return nil
		}
		rel := string(entry)
		if rel == "" {
			continue
		}
		nested, isDir := strings.CutSuffix(rel, "/")
		if isDir || submodules[rel] {
			if err := x.list(ctx, filepath.Join(repo, nested), join(prefix, nested), out); err != nil {
				return err
			}
			continue
		}
		*out = append(*out, join(prefix, rel))
	}
	return nil
}

func (x index) walk(ctx context.Context, root, prefix string, out *[]string) error {
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
		if !d.IsDir() {
			if d.Type().IsRegular() || d.Type()&fs.ModeSymlink != 0 {
				*out = append(*out, join(prefix, filepath.ToSlash(rel)))
			}
			return nil
		}
		if path == root {
			return nil
		}
		if name := d.Name(); name == "node_modules" || strings.HasPrefix(name, ".") || x.blocked(path) {
			return filepath.SkipDir
		}
		if exists(filepath.Join(path, ".git")) {
			if err := x.listRepo(ctx, path, join(prefix, filepath.ToSlash(rel)), out); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		return nil
	})
}

func (x index) blocked(dir string) bool {
	if x.home == "" {
		return false
	}
	library := filepath.Join(x.home, "Library")
	return dir == library || strings.HasPrefix(dir, library+"/") || filepath.Dir(dir) == x.home
}

func submodules(ctx context.Context, repo string) (map[string]bool, error) {
	if !exists(filepath.Join(repo, ".gitmodules")) {
		return nil, nil
	}
	data, err := gitrepo.Run(ctx, repo, "config", "--file", ".gitmodules", "--get-regexp", `^submodule\..*\.path$`)
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("read submodules of %s: %w", repo, err)
	}
	paths := map[string]bool{}
	for line := range strings.Lines(string(data)) {
		if _, path, ok := strings.Cut(strings.TrimSuffix(line, "\n"), " "); ok {
			paths[path] = true
		}
	}
	return paths, nil
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
