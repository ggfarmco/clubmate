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

	"clubmate/plugins/internal/gitrepo"
)

const includeList = ".clubmate/worktree-include"

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
	if !info.Mode().IsRegular() || !gitrepo.Within(gitrepo.Canonical(from), gitrepo.Canonical(src)) {
		return errors.New("not a regular file in the repo")
	}
	to := filepath.Join(dst, rel)
	if !gitrepo.Within(gitrepo.Canonical(filepath.Dir(to)), gitrepo.Canonical(dst)) {
		return errors.New("target folder leaves the worktree")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
