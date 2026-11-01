package fileindex

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clubmate/plugins/internal/gitrepo"
)

const (
	MaxFiles     = 200_000
	lsFilesLimit = 64 << 20
	gitlinkMode  = "160000 "
)

type Index struct {
	Home string
}

type root struct {
	resolved string
	label    string
}

func New() Index {
	home, err := os.UserHomeDir()
	if err != nil {
		return Index{}
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	return Index{Home: home}
}

func (x Index) Files(ctx context.Context, folders []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, r := range newRoots(folders) {
		var listed []string
		var err error
		if x.isRepo(r.resolved) {
			err = x.listRepo(ctx, r.resolved, r.label, &listed)
		} else {
			err = x.walk(ctx, r.resolved, r.label, &listed)
		}
		if err != nil {
			return nil, err
		}
		for _, path := range listed {
			file := r.resolved + "/" + strings.TrimPrefix(path, join(r.label, ""))
			if seen[file] {
				continue
			}
			seen[file] = true
			out = append(out, path)
			if len(out) >= MaxFiles {
				return out, nil
			}
		}
	}
	return out, nil
}

func (x Index) listRepo(ctx context.Context, repo, prefix string, out *[]string) error {
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
		if err != nil || len(*out) >= MaxFiles {
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
		if err != nil || len(*out) >= MaxFiles {
			return err
		}
	}
	return nil
}

func (x Index) nested(ctx context.Context, repo, rel, prefix string, out *[]string) error {
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

func (x Index) walk(ctx context.Context, root, prefix string, out *[]string) error {
	guarded := x.guarded(root)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return skip(d)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(*out) >= MaxFiles {
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
		if name := d.Name(); name == "node_modules" || strings.HasPrefix(name, ".") || guarded && filepath.Dir(path) == x.Home {
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

func (x Index) isRepo(dir string) bool {
	return !x.guarded(dir) && exists(filepath.Join(dir, ".git"))
}

func (x Index) guarded(dir string) bool {
	return x.Home != "" && (dir == x.Home || strings.HasPrefix(x.Home, strings.TrimSuffix(dir, "/")+"/"))
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
	var out []root
	for i, label := range Labels(folders) {
		resolved, err := filepath.EvalSymlinks(folders[i])
		if err != nil {
			logError("resolve "+folders[i], err)
			continue
		}
		out = append(out, root{resolved: resolved, label: label})
	}
	return out
}

func Labels(folders []string) []string {
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

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s: %v\n", filepath.Base(os.Args[0]), what, err)
	}
}

func skip(d fs.DirEntry) error {
	if d != nil && d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}
