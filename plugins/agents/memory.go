package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	memoryRoot     = "memory"
	memoryIndex    = "MEMORY.md"
	memoryBaseRef  = "refs/tags/base"
	memoryExt      = ".md"
	memoryBlobMode = "100644"
	memoryGoneMode = "000000"
)

var (
	errMemoryMoved = errors.New("memory changed during the night")
	errMemoryPath  = errors.New("only .md files in memory")
)

type memorySandbox struct {
	Dir  string
	Dirs map[string]string
}

type memoryChange struct {
	root string
	rel  string
	data []byte
	gone bool
}

func memoryDirs(p pluginsdk.Workspace, home string) []string {
	if dir := autoMemoryDirectory(home); dir != "" {
		if !isMemoryDir(dir) {
			return nil
		}
		return []string{dir}
	}
	var out []string
	for _, folder := range p.Paths() {
		dir := filepath.Join(home, ".claude", "projects", claudeProjectDir(folder), memoryRoot)
		if isMemoryDir(dir) && !slices.Contains(out, dir) {
			out = append(out, dir)
		}
	}
	return out
}

func claudeProjectDir(path string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, path)
}

func newMemorySandbox(ctx context.Context, dir string, real []string) (memorySandbox, error) {
	m := memorySandbox{Dir: dir, Dirs: make(map[string]string, len(real))}
	if err := initMemoryRepo(ctx, dir); err != nil {
		return m, err
	}
	for i, src := range real {
		slot := strconv.Itoa(i)
		m.Dirs[slot] = src
		if err := copyMemory(src, filepath.Join(dir, memoryRoot, slot)); err != nil {
			return m, err
		}
	}
	if _, err := sandboxGit(ctx, dir, "add", "-A"); err != nil {
		return m, fmt.Errorf("stage memory: %w", err)
	}
	if _, err := sandboxGit(ctx, dir, "commit", "-q", "--allow-empty", "-m", "baseline"); err != nil {
		return m, fmt.Errorf("commit memory: %w", err)
	}
	if _, err := sandboxGit(ctx, dir, "update-ref", memoryBaseRef, "HEAD"); err != nil {
		return m, fmt.Errorf("tag memory: %w", err)
	}
	return m, nil
}

func sandboxGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return gitrepo.Mutate(ctx, dir, append(sandboxGitArgs(dir), args...)...)
}

func sandboxGitArgs(dir string) []string {
	return []string{"--git-dir=" + filepath.Clean(dir) + ".git", "--work-tree=" + dir}
}

func (m memorySandbox) changed(ctx context.Context) ([]string, error) {
	base, err := m.tree(ctx, memoryBaseRef)
	if err != nil {
		return nil, err
	}
	var real, tracked []string
	for slot, dir := range m.Dirs {
		files, err := memoryFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			real = append(real, filepath.Join(dir, filepath.FromSlash(rel)))
			tracked = append(tracked, path.Join(memoryRoot, slot, rel))
		}
	}
	blobs, err := m.hash(ctx, real)
	if err != nil {
		return nil, err
	}
	var out []string
	for i, file := range tracked {
		if base[file] != blobs[i] {
			out = append(out, real[i])
		}
		delete(base, file)
	}
	for file := range base {
		if root, rel, ok := m.realPath(file); ok {
			out = append(out, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	slices.Sort(out)
	return out, nil
}

func (m memorySandbox) apply(ctx context.Context) ([]byte, error) {
	declared, err := m.declared(ctx)
	if err != nil || len(declared) == 0 {
		return nil, err
	}
	moved, err := m.changed(ctx)
	if err != nil {
		return nil, err
	}
	if len(moved) > 0 {
		return nil, fmt.Errorf("%w: %s", errMemoryMoved, strings.Join(moved, ", "))
	}
	next, err := m.snapshot(ctx, "HEAD", declared)
	if err != nil {
		return nil, err
	}
	prev, err := m.snapshot(ctx, memoryBaseRef, declared)
	if err != nil {
		return nil, err
	}
	patch, err := sandboxGit(ctx, m.Dir, "diff", "--binary", "--no-color", "-U3", "--no-ext-diff",
		"--no-textconv", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/", memoryBaseRef, "HEAD", "--", memoryRoot+"/")
	if err != nil {
		return nil, fmt.Errorf("diff memory: %w", err)
	}
	if err := replaceMemory(next, prev); err != nil {
		return nil, err
	}
	return patch, nil
}

func reverseMemory(ctx context.Context, patch []byte, dirs map[string]string) error {
	if len(patch) == 0 {
		return nil
	}
	tmp, err := os.MkdirTemp("", "clubmate-memory-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	mirror := filepath.Join(tmp, "mirror")
	if err := initMemoryRepo(ctx, mirror); err != nil {
		return err
	}
	for slot, dir := range dirs {
		if err := copyMemory(dir, filepath.Join(mirror, memoryRoot, slot)); err != nil {
			return err
		}
	}
	file := filepath.Join(tmp, "memory.patch")
	if err := os.WriteFile(file, patch, 0o600); err != nil {
		return err
	}
	reverse := []string{"apply", "-R", "--whitespace=nowarn"}
	if _, err := sandboxGit(ctx, mirror, append(reverse, "--check", file)...); err != nil {
		return fmt.Errorf("check memory patch: %w", err)
	}
	if _, err := sandboxGit(ctx, mirror, append(reverse, file)...); err != nil {
		return fmt.Errorf("reverse memory patch: %w", err)
	}
	var next, prev []memoryChange
	for slot, dir := range dirs {
		slotNext, slotPrev, err := memoryDelta(dir, filepath.Join(mirror, memoryRoot, slot))
		if err != nil {
			return err
		}
		next, prev = append(next, slotNext...), append(prev, slotPrev...)
	}
	return replaceMemory(next, prev)
}

func (m memorySandbox) declared(ctx context.Context) ([]string, error) {
	out, err := sandboxGit(ctx, m.Dir, "diff-tree", "-r", "-z", "--no-renames", memoryBaseRef, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("list declared memory: %w", err)
	}
	var files []string
	for entry := range slices.Chunk(strings.Split(string(out), "\x00"), 2) {
		if len(entry) != 2 {
			break
		}
		_, modes, _ := strings.Cut(entry[0], " ")
		mode, _, _ := strings.Cut(modes, " ")
		_, _, known := m.realPath(entry[1])
		if !known || path.Ext(entry[1]) != memoryExt || mode != memoryBlobMode && mode != memoryGoneMode {
			return nil, fmt.Errorf("%w: %s", errMemoryPath, entry[1])
		}
		files = append(files, entry[1])
	}
	return files, nil
}

func (m memorySandbox) snapshot(ctx context.Context, rev string, files []string) ([]memoryChange, error) {
	tree, err := m.tree(ctx, rev)
	if err != nil {
		return nil, err
	}
	out := make([]memoryChange, 0, len(files))
	for _, file := range files {
		root, rel, _ := m.realPath(file)
		blob, ok := tree[file]
		if !ok {
			out = append(out, memoryChange{root: root, rel: rel, gone: true})
			continue
		}
		data, err := sandboxGit(ctx, m.Dir, "cat-file", "blob", blob)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		out = append(out, memoryChange{root: root, rel: rel, data: data})
	}
	return out, nil
}

func (m memorySandbox) tree(ctx context.Context, rev string) (map[string]string, error) {
	out, err := sandboxGit(ctx, m.Dir, "ls-tree", "-r", "-z", rev, "--", memoryRoot+"/")
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", rev, err)
	}
	tree := map[string]string{}
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, file, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		tree[file] = meta[strings.LastIndexByte(meta, ' ')+1:]
	}
	return tree, nil
}

func (m memorySandbox) hash(ctx context.Context, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	args := append([]string{"hash-object", "--no-filters", "--"}, files...)
	out, err := sandboxGit(ctx, m.Dir, args...)
	if err != nil {
		return nil, fmt.Errorf("hash memory: %w", err)
	}
	return strings.Fields(string(out)), nil
}

func (m memorySandbox) realPath(tracked string) (string, string, bool) {
	rest, ok := strings.CutPrefix(tracked, memoryRoot+"/")
	if !ok {
		return "", "", false
	}
	slot, rel, ok := strings.Cut(rest, "/")
	if !ok || slices.ContainsFunc(strings.Split(rel, "/"), climbsMemory) {
		return "", "", false
	}
	root, ok := m.Dirs[slot]
	return root, rel, ok
}

func climbsMemory(part string) bool {
	return part == "" || part == "." || part == ".."
}

func initMemoryRepo(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := sandboxGit(ctx, dir, "init", "-q"); err != nil {
		return fmt.Errorf("init memory repo: %w", err)
	}
	for _, kv := range [][2]string{
		{"user.name", "Clubmate"},
		{"user.email", "clubmate@localhost"},
		{"commit.gpgsign", "false"},
		{"core.hooksPath", "/dev/null"},
		{"core.excludesFile", "/dev/null"},
		{"core.attributesFile", "/dev/null"},
		{"core.autocrlf", "false"},
	} {
		if _, err := sandboxGit(ctx, dir, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("configure memory repo: %w", err)
		}
	}
	return nil
}

func memoryDelta(real, mirror string) ([]memoryChange, []memoryChange, error) {
	have, err := readMemory(real)
	if err != nil {
		return nil, nil, err
	}
	want, err := readMemory(mirror)
	if err != nil {
		return nil, nil, err
	}
	var next, prev []memoryChange
	for rel, data := range want {
		old, ok := have[rel]
		if ok && bytes.Equal(old, data) {
			continue
		}
		next = append(next, memoryChange{root: real, rel: rel, data: data})
		prev = append(prev, memoryChange{root: real, rel: rel, data: old, gone: !ok})
	}
	for rel, old := range have {
		if _, ok := want[rel]; !ok {
			next = append(next, memoryChange{root: real, rel: rel, gone: true})
			prev = append(prev, memoryChange{root: real, rel: rel, data: old})
		}
	}
	return next, prev, nil
}

func replaceMemory(next, prev []memoryChange) error {
	for _, c := range next {
		if passesSymlink(c.root, c.rel) {
			return fmt.Errorf("%w: %s is behind a symlink", errMemoryPath, c.rel)
		}
	}
	if err := writeMemory(next); err != nil {
		return errors.Join(err, writeMemory(prev))
	}
	return nil
}

func writeMemory(changes []memoryChange) error {
	var errs []error
	for _, c := range changes {
		if !c.gone {
			continue
		}
		if err := os.Remove(c.path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for _, c := range changes {
		if !c.gone {
			errs = append(errs, writeAtomic(c.path(), c.data))
		}
	}
	return errors.Join(errs...)
}

func (c memoryChange) path() string {
	return filepath.Join(c.root, filepath.FromSlash(c.rel))
}

func passesSymlink(root, rel string) bool {
	current := root
	for part := range strings.SplitSeq(rel, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return false
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func copyMemory(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	files, err := readMemory(src)
	if err != nil {
		return err
	}
	for rel, data := range files {
		if err := writeAtomic(filepath.Join(dst, filepath.FromSlash(rel)), data); err != nil {
			return err
		}
	}
	return nil
}

func readMemory(dir string) (map[string][]byte, error) {
	files, err := memoryFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(files))
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		out[rel] = data
	}
	return out, nil
}

func memoryFiles(dir string) ([]string, error) {
	var out []string
	err := fs.WalkDir(os.DirFS(dir), ".", func(file string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && path.Ext(file) == memoryExt {
			out = append(out, file)
		}
		return nil
	})
	return out, err
}

func dropEmptyMemory(sandbox string) error {
	root := filepath.Join(sandbox, memoryRoot)
	files, err := memoryFiles(root)
	if err != nil {
		return err
	}
	for _, rel := range files {
		if path.Base(rel) == memoryIndex {
			continue
		}
		file := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(data)) > 0 {
			continue
		}
		if err := os.Remove(file); err != nil {
			return err
		}
	}
	return nil
}

func autoMemoryDirectory(home string) string {
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var settings struct {
		AutoMemoryDirectory string `json:"autoMemoryDirectory"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	if rest, ok := strings.CutPrefix(settings.AutoMemoryDirectory, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return settings.AutoMemoryDirectory
}

func isMemoryDir(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
