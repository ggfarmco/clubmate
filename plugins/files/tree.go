package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	maxEntries = 1000
	maxItems   = 4900
	maxDepth   = 24
	moreSuffix = "\x00more"

	chainSeparator = " / "

	iconFolder  = "folder"
	iconLink    = "link"
	iconMissing = "exclamationmark.triangle"
	dirtyBadge  = "•"
	ignoredCode = "!"
)

type Root struct {
	Path string
}

type entry struct {
	name string
	dir  bool
	link bool
}

type builder struct {
	expanded map[string]bool
	status   map[string]string
	dirty    map[string]bool
	hide     bool
	count    int
	open     map[string]bool
	reads    map[string][]entry
	home     string
}

func buildTree(roots []Root, expanded map[string]bool, status map[string]string, hideIgnored bool) []sdui.TreeItem {
	b := &builder{expanded: expanded, status: status, dirty: dirtyDirs(status), hide: hideIgnored, open: openAncestors(expanded), reads: map[string][]entry{}, home: userHome()}
	items := make([]sdui.TreeItem, 0, len(roots))
	for _, r := range roots {
		items = append(items, b.root(r.Path))
	}
	return items
}

func (b *builder) root(path string) sdui.TreeItem {
	b.count++
	item := sdui.TreeItem{ID: path, Label: filepath.Base(path), Icon: iconFolder}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		item.Detail, item.Icon = "missing", iconMissing
		return item
	}
	item.HasChildren = true
	item.Badge = b.dirBadge(path)
	b.expand(&item, 1, false, b.expanded[path])
	return item
}

func (b *builder) expand(item *sdui.TreeItem, depth int, muted bool, open bool) {
	if !open || b.count >= maxItems || depth > maxDepth {
		return
	}
	item.Expanded = true
	item.Children = b.children(item.ID, depth, muted)
}

func (b *builder) children(dir string, depth int, muted bool) []sdui.TreeItem {
	entries := b.visibleEntries(dir)
	items := make([]sdui.TreeItem, 0, min(len(entries), maxEntries)+1)
	rest := 0
	for i, e := range entries {
		if i >= maxEntries || b.count >= maxItems-1 {
			rest = len(entries) - i
			break
		}
		b.count++
		path := filepath.Join(dir, e.name)
		label := e.name
		itemMuted := muted || b.status[path] == ignoredCode
		badge := b.status[path]
		open := b.expanded[path]
		if e.dir && !e.link {
			for {
				next, ok := b.onlyDir(path)
				if !ok {
					break
				}
				path = filepath.Join(path, next)
				label += chainSeparator + next
				itemMuted = itemMuted || b.status[path] == ignoredCode
				if badge == "" {
					badge = b.status[path]
				}
				open = open || b.expanded[path]
			}
			open = open || b.open[path]
		}
		item := sdui.TreeItem{ID: path, Label: label, Muted: itemMuted}
		if !itemMuted {
			item.Badge = badge
		}
		switch {
		case e.link:
			item.Icon = iconLink
		case e.dir:
			item.Icon, item.HasChildren = iconFolder, true
			if item.Badge == "" && !itemMuted {
				item.Badge = b.dirBadge(path)
			}
			b.expand(&item, depth+1, itemMuted, open)
		}
		items = append(items, item)
	}
	if rest > 0 {
		b.count++
		items = append(items, sdui.TreeItem{ID: dir + "/" + moreSuffix, Label: fmt.Sprintf("… %d more", rest)})
	}
	return items
}

func (b *builder) visibleEntries(dir string) []entry {
	entries, ok := b.reads[dir]
	if !ok {
		entries = readEntries(dir)
		b.reads[dir] = entries
	}
	if !b.hide {
		return entries
	}
	return slices.DeleteFunc(slices.Clone(entries), func(e entry) bool { return b.status[filepath.Join(dir, e.name)] == ignoredCode })
}

func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}

func (b *builder) onlyDir(dir string) (string, bool) {
	if filepath.Dir(dir) == b.home {
		return "", false
	}
	if _, ok := b.reads[dir]; ok {
		entries := b.visibleEntries(dir)
		if len(entries) != 1 || !entries[0].dir || entries[0].link {
			return "", false
		}
		return entries[0].name, true
	}
	f, err := os.Open(dir)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var only os.DirEntry
	for {
		dirents, err := f.ReadDir(16)
		for _, d := range dirents {
			name := d.Name()
			if name == ".git" || name == ".DS_Store" || (b.hide && b.status[filepath.Join(dir, name)] == ignoredCode) {
				continue
			}
			if only != nil {
				return "", false
			}
			only = d
		}
		if err != nil {
			break
		}
	}
	if only == nil || !only.IsDir() {
		return "", false
	}
	return only.Name(), true
}

func openAncestors(expanded map[string]bool) map[string]bool {
	open := map[string]bool{}
	for path, on := range expanded {
		if !on {
			continue
		}
		for dir := filepath.Dir(path); dir != "/" && dir != "." && !open[dir]; dir = filepath.Dir(dir) {
			open[dir] = true
		}
	}
	return open
}

func (b *builder) dirBadge(path string) string {
	if b.dirty[path] {
		return dirtyBadge
	}
	return ""
}

func readEntries(dir string) []entry {
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	entries := make([]entry, 0, len(dirents))
	for _, d := range dirents {
		name := d.Name()
		if name == ".git" || name == ".DS_Store" {
			continue
		}
		e := entry{name: name, dir: d.IsDir()}
		if d.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(dir, name))
			e.link = true
			e.dir = err == nil && info.IsDir()
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if a.dir != b.dir {
			if a.dir {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	return entries
}

func dirtyDirs(status map[string]string) map[string]bool {
	dirty := map[string]bool{}
	for path, code := range status {
		if code == ignoredCode {
			continue
		}
		for dir := filepath.Dir(path); dir != "/" && dir != "." && !dirty[dir]; dir = filepath.Dir(dir) {
			dirty[dir] = true
		}
	}
	return dirty
}
