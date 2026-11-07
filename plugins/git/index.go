package main

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	indexMaxAge    = time.Minute
	indexTimeout   = 30 * time.Second
	stateParallel  = 8
	landedParallel = 4
	landedTimeout  = time.Minute
	metaPrefix     = "worktrees/"
	gitLink        = ".git"
	ignoredMark    = "!! "
	gitlinkMode    = "160000 "
	modulesDir     = "modules"
	goneTrack      = "[gone]"
	refsFormat     = "--format=%(refname) %(objectname) %(upstream:track)"
	mergedFormat   = "--format=%(refname)"
	parentsFormat  = "--format=%P"
	remoteFormat   = "--format=%(refname) %(upstream) %(upstream:track)"
	remoteHead     = "HEAD"
)

var errNotWorktree = errors.New("not a worktree of this workspace")

type indexEntry struct {
	worktreeInfo
	Created    time.Time `json:"created,omitzero"`
	LastOpened time.Time `json:"lastOpened,omitzero"`
	MadeBy     string    `json:"madeBy,omitempty"`
	Merged     bool      `json:"merged"`
	RemoteGone bool      `json:"remoteGone"`
	Locked     bool      `json:"locked,omitempty"`
	LockWhy    string    `json:"lockWhy,omitempty"`
	tree       string
	mapped     string
	tip        string
	landedKey  string
	squashable bool
}

type folderIndex struct {
	top     string
	key     string
	entries []indexEntry
	built   time.Time
}

type landedCheck struct {
	top string
	tip string
	key string
}

type repoRefs struct {
	name     string
	gone     map[string]bool
	bases    []string
	tips     []string
	mergedTo map[string]bool
	landed   map[string]bool
}

type branchLanding struct {
	merged     bool
	remoteGone bool
	key        string
	squashable bool
}

type worktreeMeta struct {
	LastOpened time.Time `json:"lastOpened,omitzero"`
	MadeBy     string    `json:"madeBy,omitempty"`
}

type metaStore struct {
	mu sync.Mutex
}

type worktreeState struct {
	Path     string `json:"path"`
	Dirty    int    `json:"dirty"`
	Unpushed int    `json:"unpushed"`
	Ignored  int    `json:"ignored"`
	Orphans  int    `json:"orphans"`
	Nested   int    `json:"nested"`
	Tip      string `json:"tip,omitempty"`
	Merged   bool   `json:"merged"`
	Landed   bool   `json:"landed"`
	Remote   bool   `json:"remote"`
	Locked   bool   `json:"locked,omitempty"`
	LockWhy  string `json:"lockWhy,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (a *app) worktreeIndex(ctx context.Context, c call) ([]indexEntry, error) {
	p, err := c.h.GetWorkspace(ctx, c.workspaceID)
	if err != nil {
		return nil, err
	}
	out := []indexEntry{}
	if p.Home {
		return out, nil
	}
	cached, complete, fresh := c.state.indexSnapshot(p)
	switch {
	case !complete:
		select {
		case <-a.refreshIndex(c.h, c.workspaceID, c.state):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		cached, _, _ = c.state.indexSnapshot(p)
	case !fresh:
		a.refreshIndex(c.h, c.workspaceID, c.state)
	}
	for _, f := range p.Folders {
		idx := cached[f.Path]
		if idx == nil || len(idx.entries) == 0 {
			continue
		}
		meta, err := loadMeta(ctx, c.h, idx.key)
		if err != nil {
			return nil, err
		}
		root := gitrepo.Canonical(f.Root())
		start := len(out)
		for _, e := range idx.entries {
			e.Current, e.LastOpened, e.MadeBy = e.mapped == root, meta[e.tree].LastOpened, meta[e.tree].MadeBy
			out = append(out, e)
		}
		slices.SortStableFunc(out[start:], func(x, y indexEntry) int { return newestFirst(x.worktreeInfo, y.worktreeInfo) })
	}
	return out, nil
}

func (a *app) refreshIndex(h *pluginsdk.Host, id string, s *state) <-chan struct{} {
	done := make(chan struct{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index == nil {
		close(done)
		return done
	}
	s.indexWaiters = append(s.indexWaiters, done)
	if !s.indexBusy {
		s.indexBusy = true
		go a.indexLoop(h, id, s)
	}
	return done
}

func (a *app) indexLoop(h *pluginsdk.Host, id string, s *state) {
	for {
		waiters := s.takeIndexWaiters()
		if len(waiters) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), indexTimeout)
		rebuildIndex(ctx, h, id, s)
		cancel()
		for _, done := range waiters {
			close(done)
		}
		startSettle(s)
	}
}

func rebuildIndex(ctx context.Context, h *pluginsdk.Host, id string, s *state) {
	p, err := h.GetWorkspace(ctx, id)
	if err != nil {
		logError("index "+id, err)
		return
	}
	prior, known := s.indexCopy()
	if prior == nil {
		return
	}
	next := make(map[string]*folderIndex, len(p.Folders))
	for i, wf := range p.Folders {
		f, err := openFolder(ctx, p, i)
		if errors.Is(err, gitrepo.ErrNotRepo) {
			next[wf.Path] = &folderIndex{built: time.Now()}
			continue
		}
		var idx *folderIndex
		if err == nil {
			idx, err = readFolderIndex(ctx, f, known)
		}
		if err != nil {
			logError("index "+wf.Path, err)
			next[wf.Path] = cmp.Or(prior[wf.Path], &folderIndex{built: time.Now()})
			continue
		}
		next[wf.Path] = idx
	}
	s.putIndex(next)
}

func readFolderIndex(ctx context.Context, f *folder, known map[string]bool) (*folderIndex, error) {
	top := f.Checkout.Path
	heads, err := f.headsWith(ctx, inert)
	if err != nil {
		return nil, err
	}
	refs, err := readRefs(ctx, top, f.Trees)
	if err != nil {
		return nil, err
	}
	idx := &folderIndex{top: top, key: metaPrefix + f.Trees[0].Real, built: time.Now()}
	for j, t := range f.Trees {
		if t.Bare {
			continue
		}
		e := indexEntry{worktreeInfo: worktreeInfo{Repo: f.Repo, Path: t.Path, Branch: t.Branch, Head: heads[t.Head].short, Committed: heads[t.Head].committed, Main: j == 0 || refs.name != "" && t.Branch == refs.name}, Locked: t.Locked, LockWhy: t.LockWhy, tree: t.Real, mapped: f.Mapped(t), tip: t.Head}
		if j > 0 {
			l := refs.landing(t, known)
			e.Merged, e.RemoteGone, e.landedKey, e.squashable = l.merged, l.remoteGone, l.key, l.squashable
			e.Created = createdAt(t)
		}
		idx.entries = append(idx.entries, e)
	}
	return idx, nil
}

func createdAt(t gitrepo.Worktree) time.Time {
	info, err := os.Stat(filepath.Join(t.Path, gitLink))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func (r repoRefs) landing(t gitrepo.Worktree, known map[string]bool) branchLanding {
	if t.Branch == "" || t.Branch == r.name {
		return branchLanding{}
	}
	out := branchLanding{remoteGone: r.gone[t.Branch], merged: r.landed[t.Head] && !slices.Contains(r.tips, t.Head)}
	if out.remoteGone && !r.mergedTo[t.Branch] {
		out.key = t.Head + " " + strings.Join(r.tips, " ")
		verdict, settled := known[out.key]
		out.merged, out.squashable = verdict, !settled
	}
	return out
}

func readRefs(ctx context.Context, top string, trees []gitrepo.Worktree) (repoRefs, error) {
	out := repoRefs{gone: map[string]bool{}, mergedTo: map[string]bool{}, landed: map[string]bool{}}
	name, err := gitrepo.DefaultBranch(ctx, top)
	if err != nil && !errors.Is(err, gitrepo.ErrNoDefaultBase) {
		return repoRefs{}, gitError(nil, err)
	}
	out.name = name
	remote := remotesPrefix + originRemote + "/" + name
	raw, err := inert(ctx, top, "for-each-ref", refsFormat, headsPrefix, remote)
	if err != nil {
		return repoRefs{}, err
	}
	for line := range strings.Lines(string(raw)) {
		ref, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		sha, track, _ := strings.Cut(rest, " ")
		branch, isBranch := strings.CutPrefix(ref, headsPrefix)
		if ref == remote || name != "" && branch == name {
			out.bases, out.tips = append(out.bases, ref), append(out.tips, sha)
		}
		if isBranch && track == goneTrack {
			out.gone[branch] = true
		}
	}
	if len(out.bases) == 0 {
		return out, nil
	}
	args := []string{"for-each-ref", mergedFormat}
	for _, base := range out.bases {
		args = append(args, "--merged="+base)
	}
	raw, err = inert(ctx, top, append(args, headsPrefix)...)
	if err != nil {
		return repoRefs{}, err
	}
	for line := range strings.Lines(string(raw)) {
		out.mergedTo[strings.TrimPrefix(strings.TrimSpace(line), headsPrefix)] = true
	}
	if !slices.ContainsFunc(trees[1:], func(t gitrepo.Worktree) bool { return out.mergedTo[t.Branch] }) {
		return out, nil
	}
	raw, err = inert(ctx, top, append(append([]string{"log", "--first-parent", "--merges", parentsFormat}, out.bases...), "--")...)
	if err != nil {
		return repoRefs{}, err
	}
	for line := range strings.Lines(string(raw)) {
		_, others, _ := strings.Cut(strings.TrimSpace(line), " ")
		for _, parent := range strings.Fields(others) {
			out.landed[parent] = true
		}
	}
	return out, nil
}

func startSettle(s *state) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settleBusy {
		s.settleAgain = true
		return
	}
	s.settleBusy = true
	go settleLanded(s)
}

func settleLanded(s *state) {
	for {
		checks := s.landedChecks()
		forEachLimited(len(checks), landedParallel, func(i int) {
			ctx, cancel := context.WithTimeout(context.Background(), landedTimeout)
			defer cancel()
			verdict := gitrepo.LandedInert(ctx, checks[i].top, checks[i].tip)
			if ctx.Err() == nil {
				s.putLanded(checks[i].key, verdict)
			}
		})
		if s.settleDone() {
			return
		}
	}
}

func worktreeStates(ctx context.Context, h *pluginsdk.Host, workspaceID string, args worktreeArgs, known map[string]bool) ([]worktreeState, error) {
	paths := args.Paths
	out := make([]worktreeState, len(paths))
	found := make([]gitrepo.Worktree, len(paths))
	excludes := make([][]string, len(paths))
	proofs := make([]string, len(paths))
	for i, path := range paths {
		out[i] = worktreeState{Path: path, Error: errNotWorktree.Error()}
	}
	err := eachFolder(ctx, h, workspaceID, func(f *folder) error {
		held := slices.DeleteFunc(slices.Clone(paths), func(path string) bool {
			_, ok := f.Member(path)
			return !ok
		})
		if len(held) == 0 {
			return nil
		}
		exclude := defaultExclude(ctx, f.Checkout.Path)
		refs, err := readRefs(ctx, f.Checkout.Path, f.Trees)
		if err != nil {
			return err
		}
		remotes, err := remoteBranches(ctx, f.Checkout.Path)
		if err != nil {
			return err
		}
		for i, path := range paths {
			if t, ok := f.Member(path); ok {
				out[i].Error, found[i], excludes[i] = "", t, exclude
				out[i].Tip = t.Head
				out[i].Merged = t.Real != f.Trees[0].Real && refs.landing(t, known).merged
				out[i].Remote = remotes[t.Branch]
				out[i].Locked, out[i].LockWhy = t.Locked, t.LockWhy
				if args.Proof && t.Branch != "" && t.Branch != refs.name {
					proofs[i] = f.Checkout.Path
					out[i].Landed = out[i].Merged || refs.mergedTo[t.Branch]
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	forEachLimited(len(paths), stateParallel, func(i int) {
		if out[i].Error != "" {
			return
		}
		readState(ctx, &out[i], found[i].Path, excludes[i])
		if proofs[i] != "" && !out[i].Landed {
			out[i].Landed = gitrepo.LandedInert(ctx, proofs[i], out[i].Tip)
		}
	})
	return out, nil
}

func unpushedWorktrees(ctx context.Context, h *pluginsdk.Host, workspaceID string) ([]string, error) {
	out := []string{}
	err := eachFolder(ctx, h, workspaceID, func(f *folder) error {
		top := f.Checkout.Path
		args := append([]string{"rev-list", "--branches", "--not", "--remotes"}, defaultExclude(ctx, top)...)
		raw, err := inert(ctx, top, append(args, "--")...)
		if err != nil {
			return err
		}
		tips := map[string]bool{}
		for line := range strings.Lines(string(raw)) {
			tips[strings.TrimSpace(line)] = true
		}
		for _, t := range f.Trees[1:] {
			if tips[t.Head] {
				out = append(out, t.Path)
			}
		}
		return nil
	})
	return out, err
}

func remoteBranches(ctx context.Context, top string) (map[string]bool, error) {
	raw, err := inert(ctx, top, "for-each-ref", remoteFormat, headsPrefix, remotesPrefix)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for line := range strings.Lines(string(raw)) {
		ref, rest, _ := strings.Cut(strings.TrimRight(line, "\n"), " ")
		upstream, track, _ := strings.Cut(rest, " ")
		if branch, ok := strings.CutPrefix(ref, headsPrefix); ok {
			if upstream != "" && track != goneTrack {
				out[branch] = true
			}
			continue
		}
		if _, branch, ok := strings.Cut(strings.TrimPrefix(ref, remotesPrefix), "/"); ok && branch != remoteHead {
			out[branch] = true
		}
	}
	return out, nil
}

func readState(ctx context.Context, out *worktreeState, dir string, exclude []string) {
	status, err := inert(ctx, dir, "status", "--porcelain", "--ignore-submodules=all", "--untracked-files=all", "--ignored=traditional")
	if err != nil {
		out.Error = err.Error()
		return
	}
	for line := range strings.Lines(string(status)) {
		switch {
		case strings.HasSuffix(strings.TrimRight(line, "\"\n"), "/"):
			out.Nested++
		case strings.HasPrefix(line, ignoredMark):
			out.Ignored++
		default:
			out.Dirty++
		}
	}
	submodules, err := initialisedSubmodules(ctx, dir)
	if err != nil {
		out.Error = err.Error()
		return
	}
	out.Nested += submodules
	unpushed := append([]string{"rev-list", "--count", currentHead, "--not", "--remotes"}, exclude...)
	if out.Unpushed, err = countRevs(ctx, dir, unpushed...); err != nil {
		out.Error = err.Error()
		return
	}
	if out.Orphans, err = countRevs(ctx, dir, "rev-list", "--count", currentHead, "--not", "--branches", "--tags", "--remotes"); err != nil {
		out.Error = err.Error()
	}
}

func initialisedSubmodules(ctx context.Context, dir string) (int, error) {
	raw, err := inert(ctx, dir, "ls-files", "--stage", "-z")
	if err != nil {
		return 0, err
	}
	count := 0
	for entry := range strings.SplitSeq(string(raw), "\x00") {
		if rest, ok := strings.CutPrefix(entry, gitlinkMode); ok {
			_, path, _ := strings.Cut(rest, "\t")
			if _, err := os.Lstat(filepath.Join(dir, path, gitLink)); err == nil {
				count++
			}
		}
	}
	raw, err = inert(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", modulesDir)
	if err != nil {
		return 0, err
	}
	if kept, err := os.ReadDir(strings.TrimSpace(string(raw))); err == nil && len(kept) > 0 {
		return max(count, 1), nil
	}
	return count, nil
}

func countRevs(ctx context.Context, dir string, args ...string) (int, error) {
	count, err := inert(ctx, dir, append(args, "--")...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(count)))
}

func defaultExclude(ctx context.Context, top string) []string {
	name, err := gitrepo.DefaultBranch(ctx, top)
	if err != nil || !hasRef(ctx, top, headsPrefix+name) {
		return nil
	}
	return []string{headsPrefix + name}
}

func (a *app) recordOpened(ctx context.Context, h *pluginsdk.Host, root string) {
	err := a.meta.update(ctx, h, root, func(m *worktreeMeta) { m.LastOpened = time.Now() })
	if !errors.Is(err, gitrepo.ErrNotRepo) {
		logError("record "+root, err)
	}
}

func (m *metaStore) update(ctx context.Context, h *pluginsdk.Host, path string, edit func(*worktreeMeta)) error {
	f, err := gitrepo.OpenFolder(ctx, path)
	if err != nil {
		return err
	}
	key := metaPrefix + f.Trees[0].Real
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, err := loadMeta(ctx, h, key)
	if err != nil {
		return err
	}
	maps.DeleteFunc(doc, func(tree string, _ worktreeMeta) bool {
		return !slices.ContainsFunc(f.Trees, func(t gitrepo.Worktree) bool { return t.Real == tree })
	})
	entry := doc[f.Checkout.Real]
	edit(&entry)
	doc[f.Checkout.Real] = entry
	return h.StorageSet(ctx, key, doc)
}

func loadMeta(ctx context.Context, h *pluginsdk.Host, key string) (map[string]worktreeMeta, error) {
	doc := map[string]worktreeMeta{}
	if _, err := h.StorageGet(ctx, key, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *state) noteSelection(p pluginsdk.Workspace) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]string, len(p.Folders))
	var changed []string
	for _, f := range p.Folders {
		root := f.Root()
		next[f.Path] = root
		if was, ok := s.selected[f.Path]; ok && was != root {
			changed = append(changed, was, root)
		}
	}
	s.selected = next
	return changed
}

func (s *state) indexSnapshot(p pluginsdk.Workspace) (map[string]*folderIndex, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index == nil {
		s.index = map[string]*folderIndex{}
	}
	complete, fresh := true, true
	for _, f := range p.Folders {
		idx := s.index[f.Path]
		if idx == nil {
			complete = false
			continue
		}
		fresh = fresh && time.Since(idx.built) < indexMaxAge
	}
	return maps.Clone(s.index), complete, fresh
}

func (s *state) indexCopy() (map[string]*folderIndex, map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.index), maps.Clone(s.landed)
}

func (s *state) putIndex(next map[string]*folderIndex) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index == nil {
		return
	}
	s.index = next
	used := map[string]bool{}
	for _, idx := range next {
		for _, e := range idx.entries {
			used[e.landedKey] = true
		}
	}
	maps.DeleteFunc(s.landed, func(key string, _ bool) bool { return !used[key] })
}

func (s *state) landedChecks() []landedCheck {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []landedCheck
	for _, idx := range s.index {
		for _, e := range idx.entries {
			if e.squashable && !seen[e.landedKey] {
				seen[e.landedKey] = true
				out = append(out, landedCheck{top: idx.top, tip: e.tip, key: e.landedKey})
			}
		}
	}
	return out
}

func (s *state) putLanded(key string, verdict bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index == nil {
		return
	}
	if s.landed == nil {
		s.landed = map[string]bool{}
	}
	s.landed[key] = verdict
	for path, idx := range s.index {
		if !slices.ContainsFunc(idx.entries, func(e indexEntry) bool { return e.squashable && e.landedKey == key }) {
			continue
		}
		next := *idx
		next.entries = slices.Clone(idx.entries)
		for i, e := range next.entries {
			if e.landedKey == key {
				next.entries[i].Merged, next.entries[i].squashable = verdict, false
			}
		}
		s.index[path] = &next
	}
}

func (s *state) settleDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settleAgain {
		s.settleAgain = false
		return false
	}
	s.settleBusy = false
	return true
}

func (s *state) takeIndexWaiters() []chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	waiters := s.indexWaiters
	s.indexWaiters = nil
	s.indexBusy = len(waiters) > 0
	return waiters
}

func forEachLimited(n, limit int, fn func(int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(n, limit) {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(i)
			}
		})
	}
	wg.Wait()
}
