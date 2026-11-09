package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"clubmate/plugins/internal/gitrepo"
)

const includeList = ".clubmate/worktree-include"

var errNotInRepo = errors.New("not a regular file in the repo")

func copyIncluded(src, dst string) error {
	data, err := os.ReadFile(filepath.Join(dst, includeList))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !filepath.IsLocal(line) {
			errs = append(errs, fmt.Errorf("%s: %q is not a path inside the repo", includeList, line))
			continue
		}
		if err := copyFile(src, dst, filepath.Clean(line)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %s: %w", includeList, line, err))
		}
	}
	return errors.Join(append(errs, lines.Err())...)
}

func copyFile(src, dst, rel string) error {
	from := filepath.Join(src, rel)
	info, err := os.Lstat(from)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errNotInRepo
	}
	to := filepath.Join(dst, rel)
	if !gitrepo.Within(gitrepo.Canonical(filepath.Dir(to)), gitrepo.Canonical(dst)) {
		return errors.New("target folder leaves the worktree")
	}
	in, err := openInRepo(src, from)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, opened.Mode().Perm())
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func openInRepo(src, from string) (*os.File, error) {
	in, err := os.OpenFile(from, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, errNotInRepo
	}
	if err != nil {
		return nil, err
	}
	opened, err := in.Stat()
	if err == nil && !opened.Mode().IsRegular() {
		err = errNotInRepo
	}
	if err == nil {
		err = sameFileInRepo(src, from, opened)
	}
	if err != nil {
		in.Close()
		return nil, err
	}
	return in, nil
}

func sameFileInRepo(src, from string, opened fs.FileInfo) error {
	canonical := gitrepo.Canonical(from)
	if !gitrepo.Within(canonical, gitrepo.Canonical(src)) {
		return errNotInRepo
	}
	there, err := os.Lstat(canonical)
	if err != nil || !os.SameFile(there, opened) {
		return errNotInRepo
	}
	return nil
}
