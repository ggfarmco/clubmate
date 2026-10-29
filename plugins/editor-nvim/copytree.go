package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

const tempInfix = ".clubmate-tmp-"

type copyPair struct {
	src string
	dst string
}

type copyRoot struct {
	src      string
	resolved string
	dst      string
}

type dirMode struct {
	path string
	mode fs.FileMode
}

type progressWriter struct {
	ctx context.Context
	w   io.Writer
	add func(int64)
}

func copyAll(ctx context.Context, pairs []copyPair, progress func(copied, total int64)) error {
	roots := make([]copyRoot, len(pairs))
	var total int64
	for i, p := range pairs {
		resolved, err := filepath.EvalSymlinks(p.src)
		if err != nil {
			return err
		}
		n, err := treeSize(ctx, resolved)
		if err != nil {
			return err
		}
		roots[i], total = copyRoot{src: filepath.Clean(p.src), resolved: resolved, dst: p.dst}, total+n
	}
	progress(0, total)
	temps := make([]string, 0, len(roots))
	defer func() {
		for _, tmp := range temps {
			removeTemp(tmp)
		}
	}()
	var copied int64
	add := func(n int64) {
		copied += n
		progress(copied, total)
	}
	relink := func(target string) string { return relinkTarget(roots, target) }
	for _, r := range roots {
		if err := os.MkdirAll(filepath.Dir(r.dst), 0o755); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp(filepath.Dir(r.dst), filepath.Base(r.dst)+tempInfix)
		if err != nil {
			return err
		}
		temps = append(temps, tmp)
		if err := copyTree(ctx, r.resolved, tmp, relink, add); err != nil {
			return err
		}
	}
	return commit(roots, temps)
}

func commit(roots []copyRoot, temps []string) error {
	for i, r := range roots {
		if err := renameExcl(temps[i], r.dst); err != nil {
			for k := i - 1; k >= 0; k-- {
				if back := renameExcl(roots[k].dst, temps[k]); back != nil {
					err = errors.Join(err, back)
				}
			}
			return err
		}
	}
	return nil
}

func relinkTarget(roots []copyRoot, target string) string {
	if !filepath.IsAbs(target) {
		return target
	}
	clean := filepath.Clean(target)
	for _, r := range roots {
		for _, src := range []string{r.src, r.resolved} {
			if clean == src {
				return r.dst
			}
			if rel, ok := strings.CutPrefix(clean, src+string(filepath.Separator)); ok {
				return filepath.Join(r.dst, rel)
			}
		}
	}
	return target
}

func copyTree(ctx context.Context, src, dst string, relink func(string) string, add func(int64)) error {
	var dirs []dirMode
	err := walkSource(src, func(rel string, d fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		from, to := filepath.Join(src, rel), filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if rel != "." {
				if err := os.Mkdir(to, 0o700); err != nil {
					return err
				}
			}
			dirs = append(dirs, dirMode{path: to, mode: info.Mode().Perm()})
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(from)
			if err != nil {
				return err
			}
			return os.Symlink(relink(link), to)
		case d.Type().IsRegular():
			return copyFile(ctx, from, to, info.Mode().Perm(), add)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, d := range slices.Backward(dirs) {
		if err := os.Chmod(d.path, d.mode); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(ctx context.Context, src, dst string, perm fs.FileMode, add func(int64)) error {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(progressWriter{ctx: ctx, w: out, add: add}, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

func (p progressWriter) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	p.add(int64(n))
	return n, err
}

func walkSource(root string, fn func(rel string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(rel, d)
	})
}

func treeSize(ctx context.Context, root string) (int64, error) {
	var total int64
	err := walkSource(root, func(_ string, d fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func sweepTemps(dst string) {
	parent, prefix := filepath.Dir(dst), filepath.Base(dst)+tempInfix
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			removeTemp(filepath.Join(parent, e.Name()))
		}
	}
}

func removeTemp(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}
