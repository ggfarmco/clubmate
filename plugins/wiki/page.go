package main

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
	"time"

	"clubmate/pkg/docext"
)

const (
	metaFile  = ".pages.json"
	shadowDir = ".agents"
	maxPage   = 4 << 20
	userName  = "you"
)

var (
	errNoWiki   = errors.New("no wiki here")
	errBadPath  = errors.New("bad page path")
	errNotPage  = errors.New("not a page")
	errTooLarge = errors.New("too large")
	errNotFound = errors.New("not found")
)

type Meta struct {
	Title     string            `json:"title"`
	Sources   []string          `json:"sources,omitempty"`
	Blobs     map[string]string `json:"blobs,omitempty"`
	WrittenAt time.Time         `json:"writtenAt"`
	By        string            `json:"by"`
}

type Page struct {
	Path        string
	Title       string
	Shadow      bool
	Updated     time.Time
	By          string
	Stale       bool
	Sources     []string
	StaleSource string
	StaleAt     time.Time
	StaleMore   int
}

type Change struct {
	At      time.Time
	By      string
	Summary string
}

type WriteReq struct {
	Page     string
	Content  string
	Summary  string
	By       string
	Worktree string
	Sources  []string
}

func cleanPage(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\\") {
		return "", errBadPath
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", errBadPath
	}
	for part := range strings.SplitSeq(c, "/") {
		if dotGit(part) {
			return "", errBadPath
		}
	}
	if !docext.Is(c) && !docext.IsArtifact(c) {
		return "", errNotPage
	}
	return c, nil
}

func dotGit(part string) bool {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 0x200c && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x206a && r <= 0x206f, r == 0xfeff:
			return -1
		}
		return r
	}, strings.ToLower(part))
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimRight(name, " .")
	return name == ".git" || name == "git~1"
}

func isShadow(page string) bool {
	return page == shadowDir || strings.HasPrefix(page, shadowDir+"/")
}

func cleanSources(worktree string, sources []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, src := range sources {
		src = strings.TrimSpace(src)
		if filepath.IsAbs(src) {
			rel, err := filepath.Rel(worktree, src)
			if worktree == "" || err != nil {
				continue
			}
			src = rel
		}
		c := path.Clean(filepath.ToSlash(src))
		if src == "" || c == "." || c == ".." || strings.HasPrefix(c, "../") || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func authorName(by string) string {
	by = strings.Map(func(r rune) rune {
		if r == '<' || r == '>' || r < ' ' {
			return -1
		}
		return r
	}, by)
	if by = strings.TrimSpace(by); by == "" {
		return userName
	}
	return by
}
