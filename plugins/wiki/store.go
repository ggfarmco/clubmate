package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/docext"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/wikiroot"
)

var revPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

type Store struct {
	p   wikiroot.Project
	now func() time.Time
	git func(ctx context.Context, dir string, args ...string) ([]byte, error)
}

func Open(p wikiroot.Project, now func() time.Time) *Store {
	return &Store{p: p, now: now, git: wikiroot.Git}
}

func (s *Store) lock() (func(), error) {
	return wikiroot.Lock(filepath.Join(s.p.Dir, ".git", "wiki.lock"))
}

func (s *Store) file(page string) (string, error) {
	root := gitrepo.Canonical(s.p.Dir)
	abs := filepath.Join(s.p.Dir, filepath.FromSlash(page))
	if !gitrepo.Within(gitrepo.Canonical(abs), root) || gitrepo.Canonical(abs) == root {
		return "", errBadPath
	}
	return abs, nil
}

func (s *Store) writeFile(page string, data []byte) error {
	root, err := os.OpenRoot(s.p.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(path.Dir(page), 0o700); err != nil {
		return err
	}
	return root.WriteFile(page, data, 0o600)
}

func (s *Store) readFile(page string) ([]byte, error) {
	root, err := os.OpenRoot(s.p.Dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(page)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPage+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPage {
		return nil, errTooLarge
	}
	return data, nil
}

func (s *Store) remove(page string) error {
	root, err := os.OpenRoot(s.p.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(page)
}

func (s *Store) Write(ctx context.Context, req WriteReq) (Page, error) {
	page, err := cleanPage(req.Page)
	if err != nil {
		return Page{}, err
	}
	if len(req.Content) > maxPage {
		return Page{}, errTooLarge
	}
	if _, err := s.Sweep(ctx); err != nil {
		return Page{}, err
	}
	unlock, err := s.lock()
	if err != nil {
		return Page{}, err
	}
	defer unlock()
	abs, err := s.file(page)
	if err != nil {
		return Page{}, err
	}
	if err := s.writeFile(page, []byte(req.Content)); err != nil {
		return Page{}, err
	}
	sources := cleanSources(req.Worktree, req.Sources)
	m := Meta{Title: docext.Title(abs), Sources: sources, Blobs: blobsAt(ctx, req.Worktree, sources), WrittenAt: s.now().UTC(), By: authorName(req.By)}
	metas, err := s.loadMeta()
	if err != nil {
		return Page{}, err
	}
	metas[page] = m
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = "updated"
	}
	if err := s.commit(ctx, metas, page, m.By, summary); err != nil {
		return Page{}, err
	}
	return s.page(page, m), nil
}

func (s *Store) Read(ctx context.Context, page string) (Page, string, error) {
	page, err := cleanPage(page)
	if err != nil {
		return Page{}, "", err
	}
	if _, err := s.Sweep(ctx); err != nil {
		return Page{}, "", err
	}
	abs, err := s.file(page)
	if err != nil {
		return Page{}, "", err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return Page{}, "", errNotFound
	}
	if err != nil {
		return Page{}, "", err
	}
	if !info.Mode().IsRegular() {
		return Page{}, "", errNotPage
	}
	if info.Size() > maxPage {
		return Page{}, "", errTooLarge
	}
	data, err := s.readFile(page)
	if err != nil {
		return Page{}, "", err
	}
	metas, err := s.loadMeta()
	if err != nil {
		return Page{}, "", err
	}
	p := s.pageAt(page, abs, info, metas)
	newStaleCheck(ctx, s.p.Checkout, []Meta{metas[page]}).apply(&p, metas[page])
	return p, string(data), nil
}

func (s *Store) List(ctx context.Context, shadow bool) ([]Page, error) {
	if _, err := s.Sweep(ctx); err != nil {
		return nil, err
	}
	metas, err := s.loadMeta()
	if err != nil {
		return nil, err
	}
	var pages []Page
	var listed []Meta
	err = s.walk(func(page, abs string, info fs.FileInfo) {
		if shadow || !isShadow(page) {
			pages = append(pages, s.pageAt(page, abs, info, metas))
			listed = append(listed, metas[page])
		}
	})
	check := newStaleCheck(ctx, s.p.Checkout, listed)
	for i := range pages {
		check.apply(&pages[i], listed[i])
	}
	return pages, err
}

func (s *Store) Delete(ctx context.Context, page, by string) error {
	page, err := cleanPage(page)
	if err != nil {
		return err
	}
	if _, err := s.Sweep(ctx); err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	abs, err := s.file(page)
	if err != nil {
		return err
	}
	if err := s.remove(page); errors.Is(err, fs.ErrNotExist) {
		return errNotFound
	} else if err != nil {
		return err
	}
	s.pruneDirs(filepath.Dir(abs))
	metas, err := s.loadMeta()
	if err != nil {
		return err
	}
	delete(metas, page)
	return s.commit(ctx, metas, page, authorName(by), "deleted")
}

func (s *Store) History(ctx context.Context, page string, limit int) ([]Change, error) {
	return s.history(ctx, page, limit, nil)
}

func (s *Store) HistoryStats(ctx context.Context, page string, limit int) ([]Change, error) {
	return s.history(ctx, page, limit, []string{"--numstat"})
}

func (s *Store) history(ctx context.Context, page string, limit int, stats []string) ([]Change, error) {
	page, err := cleanPage(page)
	if err != nil {
		return nil, err
	}
	args := append([]string{"log", "--no-renames", "--format=%x1e%H%x00%at%x00%an%x00%s"}, stats...)
	if limit > 0 {
		args = append(args, "-n", strconv.Itoa(limit))
	}
	out, err := s.git(ctx, s.p.Dir, append(args, "--", page)...)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for entry := range strings.SplitSeq(string(out), "\x1e") {
		head, stat, _ := strings.Cut(entry, "\n")
		parts := strings.SplitN(head, "\x00", 4)
		if len(parts) != 4 {
			continue
		}
		sec, _ := strconv.ParseInt(parts[1], 10, 64)
		c := Change{Rev: parts[0], At: time.Unix(sec, 0).UTC(), By: parts[2], Summary: strings.TrimPrefix(parts[3], page+": ")}
		for line := range strings.SplitSeq(strings.TrimSpace(stat), "\n") {
			if f := strings.SplitN(line, "\t", 3); len(f) == 3 {
				added, _ := strconv.Atoi(f[0])
				removed, _ := strconv.Atoi(f[1])
				c.Added, c.Removed = c.Added+added, c.Removed+removed
			}
		}
		changes = append(changes, c)
	}
	return changes, nil
}

func (s *Store) Count(ctx context.Context, page string) (int, error) {
	page, err := cleanPage(page)
	if err != nil {
		return 0, err
	}
	out, err := s.git(ctx, s.p.Dir, "rev-list", "--count", "HEAD", "--", page)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

func (s *Store) Diff(ctx context.Context, page, rev string) (string, error) {
	page, err := cleanPage(page)
	if err != nil {
		return "", err
	}
	if !revPattern.MatchString(rev) {
		return "", errNotFound
	}
	out, err := s.git(ctx, s.p.Dir, "show", "--format=", "--no-color", "--no-ext-diff", "--no-renames", "-U3", rev, "--", page)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Store) Version(ctx context.Context, page, rev string) ([]byte, error) {
	page, err := cleanPage(page)
	if err != nil {
		return nil, err
	}
	if !revPattern.MatchString(rev) {
		return nil, errNotFound
	}
	out, err := s.git(ctx, s.p.Dir, "cat-file", "blob", rev+":"+page)
	if err != nil {
		return nil, errNotFound
	}
	if len(out) > maxPage {
		return nil, errTooLarge
	}
	return out, nil
}

func (s *Store) Restore(ctx context.Context, page, rev, by, summary string) (Page, error) {
	page, err := cleanPage(page)
	if err != nil {
		return Page{}, err
	}
	if _, err := s.Sweep(ctx); err != nil {
		return Page{}, err
	}
	unlock, err := s.lock()
	if err != nil {
		return Page{}, err
	}
	defer unlock()
	abs, err := s.file(page)
	if err != nil {
		return Page{}, err
	}
	data, err := s.Version(ctx, page, rev)
	if err != nil {
		return Page{}, err
	}
	metas, err := s.loadMeta()
	if err != nil {
		return Page{}, err
	}
	if cur, err := s.readFile(page); err == nil && bytes.Equal(cur, data) {
		return s.page(page, metas[page]), nil
	}
	if err := s.writeFile(page, data); err != nil {
		return Page{}, err
	}
	m := metas[page]
	m.Title, m.WrittenAt, m.By = docext.Title(abs), s.now().UTC(), authorName(by)
	metas[page] = m
	if err := s.commit(ctx, metas, page, m.By, summary); err != nil {
		return Page{}, err
	}
	return s.page(page, m), nil
}

func (s *Store) Sweep(ctx context.Context) ([]string, error) {
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	changed, err := s.dirty(ctx)
	if err != nil || len(changed) == 0 {
		return nil, err
	}
	metas, err := s.loadMeta()
	if err != nil {
		return nil, err
	}
	var swept []string
	for _, page := range changed {
		abs, err := s.file(page)
		if err != nil {
			continue
		}
		if _, err := os.Lstat(abs); errors.Is(err, fs.ErrNotExist) {
			delete(metas, page)
			if err := s.commit(ctx, metas, page, userName, "deleted"); err != nil {
				return swept, err
			}
			swept = append(swept, page)
			continue
		}
		m := metas[page]
		m.Title, m.WrittenAt, m.By = docext.Title(abs), s.now().UTC(), userName
		metas[page] = m
		if err := s.commit(ctx, metas, page, userName, "edited"); err != nil {
			return swept, err
		}
		swept = append(swept, page)
	}
	return swept, nil
}

func (s *Store) dirty(ctx context.Context) ([]string, error) {
	out, err := s.git(ctx, s.p.Dir, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var pages []string
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		if f[0] == 'R' || f[0] == 'C' {
			i++
		}
		if page, err := cleanPage(f[3:]); err == nil {
			pages = append(pages, page)
		}
	}
	slices.Sort(pages)
	return slices.Compact(pages), nil
}

func (s *Store) commit(ctx context.Context, metas map[string]Meta, page, by, summary string) error {
	if err := s.saveMeta(metas); err != nil {
		return err
	}
	if _, err := s.git(ctx, s.p.Dir, "add", "-A", "-f", "--", page, metaFile); err != nil {
		return err
	}
	_, err := s.git(ctx, s.p.Dir, "commit", "-q", "--allow-empty", "--author", by+" <"+wikiroot.Email+">", "-m", page+": "+summary)
	return err
}

func (s *Store) loadMeta() (map[string]Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.p.Dir, metaFile))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Meta{}, nil
	}
	if err != nil {
		return nil, err
	}
	metas := map[string]Meta{}
	if err := json.Unmarshal(data, &metas); err != nil {
		return map[string]Meta{}, nil
	}
	return metas, nil
}

func (s *Store) saveMeta(metas map[string]Meta) error {
	data, err := json.MarshalIndent(metas, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.p.Dir, metaFile+".tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.p.Dir, metaFile))
}

func (s *Store) walk(fn func(page, abs string, info fs.FileInfo)) error {
	return filepath.WalkDir(s.p.Dir, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(s.p.Dir, abs)
		if err != nil {
			return nil
		}
		page, err := cleanPage(filepath.ToSlash(rel))
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fn(page, abs, info)
		return nil
	})
}

func (s *Store) pageAt(page, abs string, info fs.FileInfo, metas map[string]Meta) Page {
	m, ok := metas[page]
	if !ok {
		m = Meta{Title: docext.Title(abs), WrittenAt: info.ModTime().UTC()}
	}
	return s.page(page, m)
}

func (s *Store) page(page string, m Meta) Page {
	title := m.Title
	if title == "" {
		title = docext.FileTitle(page)
	}
	return Page{Path: page, Title: title, Shadow: isShadow(page), Updated: m.WrittenAt, By: m.By, Sources: m.Sources}
}

func (s *Store) pruneDirs(dir string) {
	for dir != s.p.Dir && gitrepo.Within(dir, s.p.Dir) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
