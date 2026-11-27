package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	touchedAge      = 2 * time.Minute
	touchedAgeEnv   = "CLUBMATE_AGENTS_TOUCHED_AGE"
	overlapsAge     = 10 * time.Second
	repoViewAge     = 2 * time.Second
	listRefreshAge  = time.Second
	neighboursShown = 4
	touchedWorkers  = 8
	neighbourFiles  = 10

	neighboursHead       = "[Clubmate neighbours] Open sessions change files that you change: "
	neighboursReadFormat = " Before you edit these files, read their version: %s. Do not edit their worktree. If your changes clash, %s."
	neighbourWhoFormat   = "%q (session %s, branch %s): %s"
	neighbourDiffFormat  = "git -C %s diff %s -- %s"
	neighbourNewFormat   = "%s (new, not committed)"
	tellParentText       = "tell your parent session"
	tellUserText         = "tell the user in your report"
	childNeighbourFormat = "It changes files that open session %q (%s, branch %s) changes too: %s."
	childMoreFormat      = "%d more open sessions change the same files."
	landNeighbourFormat  = "Open session %q (%s, branch %s) changes %s too; its landing can conflict with yours."
	landMoreFormat       = "%d more open sessions change the same files; their landings can conflict with yours."
	moreFilesFormat      = "%s, and %d more"
	moreWhoFormat        = "and %d more"
)

var (
	touchedMaxAge  = envDelay(touchedAgeEnv, touchedAge)
	overlapsMaxAge = min(touchedMaxAge, overlapsAge)
	repoViewMaxAge = min(touchedMaxAge, repoViewAge)
)

type touchedKey struct {
	head    string
	branch  string
	base    string
	baseTip string
	index   string
	moves   uint64
}

type treeState struct {
	ok      bool
	primary string
	real    string
	path    string
	def     string
	key     touchedKey
}

type touched struct {
	at        time.Time
	ok        bool
	key       touchedKey
	primary   string
	real      string
	branch    string
	def       string
	base      string
	head      string
	baseTip   string
	landedKey string
	landed    bool
	mergeBase string
	files     []string
	untracked []string
}

type neighbour struct {
	id        string
	title     string
	branch    string
	worktree  string
	mergeBase string
	created   time.Time
	files     []string
	untracked []string
}

type overlapEntry struct {
	Session string   `json:"session"`
	Branch  string   `json:"branch,omitempty"`
	Files   []string `json:"files"`
	More    int      `json:"more,omitempty"`
}

type overlapsAt struct {
	at   time.Time
	list []overlapEntry
}

type overlapCache struct {
	mu          sync.Mutex
	touched     map[string]touched
	flight      map[string]*touchedFlight
	overlaps    map[string]overlapsAt
	busy        map[string]bool
	passed      map[string]time.Time
	landing     map[string]landTold
	views       map[string]*repoView
	moved       map[string]treeMove
	viewAge     time.Duration
	landedCheck func(ctx context.Context, repo, tip, base string) bool
	treesOf     func(ctx context.Context, top string) ([]gitrepo.Worktree, error)
	touchedOf   func(ctx context.Context, worktree, base string) (gitrepo.Touched, error)
}

type treeMove struct {
	count uint64
	at    time.Time
}

type repoView struct {
	at    time.Time
	top   string
	built sync.Once
	bad   bool
	trees []gitrepo.Worktree
	def   string
	tipMu sync.Mutex
	tips  map[string]string
}

type overlapSet struct {
	recs    []record
	touched []touched
}

type touchedFlight struct {
	done chan struct{}
	t    touched
	ok   bool
}

type landTold struct {
	owner   string
	overlap []overlapEntry
	lander  string
	keys    []string
}

func treeKey(worktree string) string {
	if worktree == "" {
		return ""
	}
	top, _ := gitrepo.RepoRoot(gitrepo.Canonical(worktree))
	return top
}

type treeKeyCache struct {
	mu       sync.Mutex
	worktree string
	key      string
}

func (k *treeKeyCache) of(worktree string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key == "" || k.worktree != worktree {
		k.worktree, k.key = worktree, treeKey(worktree)
	}
	return k.key
}

func (c *overlapCache) move(worktree string) {
	if worktree == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.moved == nil {
		c.moved = map[string]treeMove{}
	}
	c.moved[worktree] = treeMove{count: c.moved[worktree].count + 1, at: time.Now()}
}

func (c *overlapCache) view(ctx context.Context, top, worktree string, exact bool) (*repoView, uint64) {
	c.mu.Lock()
	m := c.moved[worktree]
	v := c.views[top]
	if v == nil || v.bad || time.Since(v.at) >= cmp.Or(c.viewAge, repoViewMaxAge) || exact && !v.at.After(m.at) {
		v = &repoView{at: time.Now(), top: top}
		if c.views == nil {
			c.views = map[string]*repoView{}
		}
		c.views[top] = v
	}
	list := c.treesOf
	c.mu.Unlock()
	if list == nil {
		list = gitrepo.Worktrees
	}
	v.built.Do(func() {
		trees, err := list(ctx, top)
		def := ""
		if err == nil && len(trees) > 0 {
			def, _ = gitrepo.DefaultBranch(ctx, top)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		v.trees, v.def, v.bad = trees, def, err != nil || len(trees) == 0
		if v.bad {
			return
		}
		for _, t := range trees {
			if cur := c.views[t.Real]; t.Real != top && (cur == nil || cur.at.Before(v.at)) {
				c.views[t.Real] = v
			}
		}
	})
	return v, m.count
}

func (v *repoView) tip(ctx context.Context, base string) string {
	v.tipMu.Lock()
	defer v.tipMu.Unlock()
	if tip, ok := v.tips[base]; ok {
		return tip
	}
	tip, _ := gitrepo.ResolveCommit(ctx, v.top, base)
	if ctx.Err() != nil {
		return tip
	}
	if v.tips == nil {
		v.tips = map[string]string{}
	}
	v.tips[base] = tip
	return tip
}

func (k touchedKey) git() touchedKey {
	k.moves = 0
	return k
}

func (t touched) fresh(key touchedKey, exact bool) bool {
	age := time.Since(t.at)
	return age < touchedMaxAge && (t.ok || age < overlapsMaxAge) && (t.key == key || !exact && age < overlapsMaxAge && t.key.git() == key.git())
}

func (a *app) treeState(ctx context.Context, rec record, exact bool) treeState {
	if rec.closed() || rec.Worktree == "" {
		return treeState{}
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return treeState{}
	}
	real := gitrepo.Canonical(rec.Worktree)
	top, ok := gitrepo.RepoRoot(real)
	if !ok {
		return treeState{}
	}
	v, moves := a.overlapCache.view(ctx, cmp.Or(sessionRepo(ctx, rec.Worktree), top), top, exact)
	if len(v.trees) == 0 {
		return treeState{}
	}
	tree := gitrepo.Longest(v.trees, real)
	if tree == nil || (&gitrepo.Folder{Trees: v.trees}).GateTree(*tree) {
		return treeState{}
	}
	base := cmp.Or(a.baseOf(ctx, rec), v.def)
	if base == "" {
		return treeState{}
	}
	return treeState{ok: true, primary: v.trees[0].Real, real: tree.Real, path: tree.Path, def: v.def, key: touchedKey{
		head: tree.Head, branch: tree.Branch, base: base, baseTip: v.tip(ctx, base), index: gitrepo.IndexStamp(tree.Path), moves: moves,
	}}
}

func (a *app) touchedFiles(ctx context.Context, rec record, exact bool) touched {
	st := a.treeState(ctx, rec, exact)
	if !st.ok {
		return touched{at: time.Now()}
	}
	return a.overlapCache.shared(ctx, rec.ID, st.key, exact, func() touched { return a.overlapCache.compute(ctx, st) })
}

func (c *overlapCache) shared(ctx context.Context, id string, key touchedKey, exact bool, compute func() touched) touched {
	for {
		c.mu.Lock()
		if t, ok := c.touched[id]; ok && t.fresh(key, exact) {
			c.mu.Unlock()
			return t
		}
		f, busy := c.flight[id]
		if !busy {
			f = &touchedFlight{done: make(chan struct{})}
			if c.flight == nil {
				c.flight = map[string]*touchedFlight{}
			}
			c.flight[id] = f
			c.mu.Unlock()
			return c.lead(ctx, id, f, compute)
		}
		c.mu.Unlock()
		select {
		case <-f.done:
			if f.ok && f.t.fresh(key, exact) {
				return f.t
			}
		case <-ctx.Done():
			return touched{at: time.Now()}
		}
	}
}

func (c *overlapCache) lead(ctx context.Context, id string, f *touchedFlight, compute func() touched) touched {
	defer close(f.done)
	f.t = compute()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.flight, id)
	if f.ok = ctx.Err() == nil; !f.ok {
		return f.t
	}
	if c.touched == nil {
		c.touched = map[string]touched{}
	}
	old := c.touched[id]
	f.t.landedKey, f.t.landed = old.landedKey, old.landed
	c.touched[id] = f.t
	return f.t
}

func (a *app) landedOf(ctx context.Context, id string, t touched) bool {
	if t.head == "" || t.baseTip == "" {
		return false
	}
	key := t.head + "\x00" + t.baseTip
	if t.landedKey == key {
		return t.landed
	}
	c := &a.overlapCache
	c.mu.Lock()
	kept, known := c.touched[id]
	c.mu.Unlock()
	if known && kept.landedKey == key {
		return kept.landed
	}
	check := c.landedCheck
	if check == nil {
		check = gitrepo.LandedIn
	}
	landed := check(ctx, t.primary, t.head, t.baseTip)
	if ctx.Err() != nil {
		return landed
	}
	c.mu.Lock()
	if e, ok := c.touched[id]; ok && e.head == t.head && e.baseTip == t.baseTip {
		e.landedKey, e.landed = key, landed
		c.touched[id] = e
	}
	c.mu.Unlock()
	return landed
}

func (c *overlapCache) compute(ctx context.Context, st treeState) touched {
	t := touched{at: time.Now(), key: st.key, primary: st.primary, real: st.real, branch: st.key.branch, def: st.def, base: st.key.base}
	run := c.touchedOf
	if run == nil {
		run = gitrepo.TouchedFiles
	}
	got, err := run(ctx, st.path, st.key.base)
	if err != nil {
		return t
	}
	t.mergeBase, t.files, t.untracked = got.MergeBase, got.Files, got.Untracked
	if t.branch != "" && st.key.head != got.MergeBase {
		t.head, t.baseTip = st.key.head, st.key.baseTip
	}
	t.ok = true
	return t
}

func (a *app) neighbourCandidate(rec record) bool {
	return !rec.closed() && !rec.isAside() && rec.GateFor == nil && rec.Night == "" && (rec.Run == nil || !a.gateRunOpen(rec.Run.ID))
}

func (a *app) neighbours(ctx context.Context, rec record) []neighbour {
	if !a.neighbourCandidate(rec) {
		return nil
	}
	t := a.touchedFiles(ctx, rec, true)
	if !t.ok || len(t.files) == 0 {
		return nil
	}
	return a.neighboursOf(ctx, rec.ID, t, t.files)
}

func (a *app) neighboursOf(ctx context.Context, id string, self touched, files []string) []neighbour {
	return a.neighboursIn(ctx, a.overlapSet(ctx, gitrepo.Canonical(self.primary)), id, self, files)
}

func (a *app) overlapSet(ctx context.Context, repo string) overlapSet {
	var set overlapSet
	for _, n := range a.openRecords() {
		if !a.neighbourCandidate(n) {
			continue
		}
		if own := sessionRepo(ctx, n.Worktree); repo != "" && own != "" && own != repo {
			continue
		}
		set.recs = append(set.recs, n)
	}
	set.touched = make([]touched, len(set.recs))
	limited(len(set.recs), func(i int) { set.touched[i] = a.touchedFiles(ctx, set.recs[i], false) })
	return set
}

func (set overlapSet) of(id string) (touched, bool) {
	for i, n := range set.recs {
		if n.ID == id {
			return set.touched[i], true
		}
	}
	return touched{}, false
}

func (a *app) neighboursIn(ctx context.Context, set overlapSet, id string, self touched, files []string) []neighbour {
	var out []neighbour
	var outTouched []touched
	for i, n := range set.recs {
		nt := set.touched[i]
		if n.ID == id || !nt.ok || nt.primary != self.primary || nt.real == self.real || nt.branch == "" || nt.branch == nt.def || nt.branch == self.base || self.branch != "" && self.branch == nt.base {
			continue
		}
		shared := common(files, nt.files)
		if len(shared) == 0 || id != "" && (a.descendantOf(n.ID, id) || a.descendantOf(id, n.ID)) {
			continue
		}
		out = append(out, neighbour{id: n.ID, title: childName(n), branch: nt.branch, worktree: n.Worktree, mergeBase: nt.mergeBase, created: n.CreatedAt, files: shared, untracked: common(shared, nt.untracked)})
		outTouched = append(outTouched, nt)
	}
	landed := make([]bool, len(out))
	limited(len(out), func(i int) { landed[i] = a.landedOf(ctx, out[i].id, outTouched[i]) })
	kept := out[:0]
	for i, n := range out {
		if !landed[i] {
			kept = append(kept, n)
		}
	}
	out = kept
	slices.SortFunc(out, func(x, y neighbour) int { return cmp.Or(x.created.Compare(y.created), strings.Compare(x.id, y.id)) })
	return out
}

func limited(n int, run func(int)) {
	slots := make(chan struct{}, touchedWorkers)
	var wg sync.WaitGroup
	for i := range n {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			run(i)
		})
	}
	wg.Wait()
}

func common(sorted, other []string) []string {
	var out []string
	for _, f := range sorted {
		if _, found := slices.BinarySearch(other, f); found {
			out = append(out, f)
		}
	}
	return out
}

func sessionPairKey(n neighbour, file string) string {
	return n.id + "\x00" + file
}

func childPairKey(child string) func(neighbour, string) string {
	return func(n neighbour, file string) string {
		return min(child, n.id) + "\x00" + max(child, n.id) + "\x00" + file
	}
}

func newPairs(told []string, key func(neighbour, string) string, ns []neighbour) []neighbour {
	var out []neighbour
	for _, n := range ns {
		files := slices.DeleteFunc(slices.Clone(n.files), func(f string) bool { return slices.Contains(told, key(n, f)) })
		if len(files) == 0 {
			continue
		}
		n.files, n.untracked = files, common(files, n.untracked)
		out = append(out, n)
	}
	return out
}

func shownPairs(ns []neighbour) ([]neighbour, int) {
	more := max(len(ns)-neighboursShown, 0)
	out := slices.Clone(ns[:len(ns)-more])
	for i := range out {
		out[i].files, out[i].untracked = clipFiles(out[i].files), common(clipFiles(out[i].files), out[i].untracked)
	}
	return out, more
}

func clipFiles(files []string) []string {
	return files[:min(len(files), neighbourFiles)]
}

func toldKeys(ns []neighbour, key func(neighbour, string) string) []string {
	var out []string
	for _, n := range ns {
		for _, f := range n.files {
			out = append(out, key(n, f))
		}
	}
	return out
}

func fileList(n neighbour, all []string) string {
	list := strings.Join(n.files, ", ")
	if more := len(all) - len(n.files); more > 0 {
		return fmt.Sprintf(moreFilesFormat, list, more)
	}
	return list
}

func fullFiles(ns []neighbour, id string) []string {
	if i := slices.IndexFunc(ns, func(n neighbour) bool { return n.id == id }); i >= 0 {
		return ns[i].files
	}
	return nil
}

func neighboursText(rec record, ns []neighbour) string {
	shown, more := shownPairs(ns)
	var who, reads []string
	for _, n := range shown {
		who = append(who, fmt.Sprintf(neighbourWhoFormat, n.title, n.id, n.branch, fileList(n, fullFiles(ns, n.id))))
		reads = append(reads, n.readCommand())
	}
	if more > 0 {
		who = append(who, fmt.Sprintf(moreWhoFormat, more))
	}
	tell := tellParentText
	if rec.ParentSession == "" {
		tell = tellUserText
	}
	return neighboursHead + strings.Join(who, "; ") + "." + fmt.Sprintf(neighboursReadFormat, strings.Join(reads, "; "), tell)
}

func (n neighbour) readCommand() string {
	tracked := slices.DeleteFunc(slices.Clone(n.files), func(f string) bool { return slices.Contains(n.untracked, f) })
	var parts []string
	if len(tracked) > 0 {
		parts = append(parts, fmt.Sprintf(neighbourDiffFormat, n.worktree, n.mergeBase, strings.Join(tracked, " ")))
	}
	for _, f := range n.untracked {
		parts = append(parts, fmt.Sprintf(neighbourNewFormat, n.worktree+"/"+f))
	}
	return strings.Join(parts, ", ")
}

func childNeighboursLine(ns []neighbour) string {
	return neighboursLine(ns, childNeighbourFormat, childMoreFormat)
}

func landNeighboursNote(ns []neighbour) string {
	return neighboursLine(ns, landNeighbourFormat, landMoreFormat)
}

func neighboursLine(ns []neighbour, format, moreFormat string) string {
	shown, more := shownPairs(ns)
	var out []string
	for _, n := range shown {
		out = append(out, fmt.Sprintf(format, n.title, n.id, n.branch, fileList(n, fullFiles(ns, n.id))))
	}
	if more > 0 {
		out = append(out, fmt.Sprintf(moreFormat, more))
	}
	return strings.Join(out, " ")
}

func (a *app) markTold(ctx context.Context, s *session, keys []string) {
	if len(keys) == 0 {
		return
	}
	gone := map[string]bool{}
	for _, key := range append(slices.Clone(s.snapshot().NeighboursTold), keys...) {
		parts := strings.Split(key, "\x00")
		for _, id := range parts[:len(parts)-1] {
			if _, seen := gone[id]; !seen {
				o := a.session(id)
				gone[id] = o == nil || o.snapshot().closed()
			}
		}
	}
	s.mu.Lock()
	all := append(slices.Clone(s.rec.NeighboursTold), keys...)
	s.rec.NeighboursTold = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(all))), func(key string) bool {
		parts := strings.Split(key, "\x00")
		return slices.ContainsFunc(parts[:len(parts)-1], func(id string) bool { return gone[id] })
	})
	s.mu.Unlock()
	a.notes.persist(ctx, s)
}

func (a *app) neighboursBlock(ctx context.Context, rec record, turn int) (string, []string) {
	if turn < 2 {
		return "", nil
	}
	ns := newPairs(rec.NeighboursTold, sessionPairKey, a.neighbours(ctx, rec))
	if len(ns) == 0 {
		return "", nil
	}
	shown, _ := shownPairs(ns)
	return neighboursText(rec, ns), toldKeys(shown, sessionPairKey)
}

func (a *app) childNeighbours(ctx context.Context, told []string, k record) (string, []string) {
	var ns []neighbour
	if _, err := os.Stat(k.Worktree); err == nil && !k.closed() {
		ns = a.neighbours(ctx, k)
	} else {
		ns = a.landedNeighbours(k)
	}
	key := childPairKey(k.ID)
	ns = newPairs(told, key, ns)
	if len(ns) == 0 {
		return "", nil
	}
	shown, _ := shownPairs(ns)
	return childNeighboursLine(ns), toldKeys(shown, key)
}

func (a *app) landedNeighbours(k record) []neighbour {
	var out []neighbour
	for _, e := range k.LandOverlap {
		s := a.session(e.Session)
		if s == nil || s.snapshot().closed() {
			continue
		}
		n := s.snapshot()
		out = append(out, neighbour{id: n.ID, title: childName(n), branch: e.Branch, worktree: n.Worktree, created: n.CreatedAt, files: e.Files})
	}
	return out
}

func (a *app) landGate(ctx context.Context, cmd pluginsdk.CommandRun, args landGateArgs) (any, error) {
	res, err := a.landGateCommand(ctx, cmd, args)
	r, ok := res.(landGateResult)
	if err != nil || !ok || r.State != gateApproved {
		return res, err
	}
	r.Neighbours = a.landNeighbours(ctx, args)
	return r, nil
}

func landingKey(args landGateArgs) string {
	return args.Session + "\x00" + args.Old + "\x00" + args.Tree
}

func (a *app) landTold(ctx context.Context, args landGateArgs) {
	c := &a.overlapCache
	c.mu.Lock()
	lt, ok := c.landing[landingKey(args)]
	delete(c.landing, landingKey(args))
	c.mu.Unlock()
	if !ok {
		return
	}
	if owner := a.session(lt.owner); owner != nil {
		owner.mu.Lock()
		owner.rec.LandOverlap = lt.overlap
		owner.mu.Unlock()
		a.notes.persist(ctx, owner)
	}
	if lander := a.session(lt.lander); lander != nil {
		a.markTold(ctx, lander, lt.keys)
	}
}

func (a *app) landNeighbours(ctx context.Context, args landGateArgs) string {
	lander := a.session(args.Session)
	if lander == nil {
		return ""
	}
	f, err := gitrepo.OpenFolder(ctx, args.Target)
	if err != nil {
		return ""
	}
	out, err := gitrepo.Run(ctx, args.Target, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", args.Old, args.Tree)
	if err != nil {
		return ""
	}
	var files []string
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	slices.Sort(files)
	real := gitrepo.Canonical(args.Worktree)
	owner := lander
	if gitrepo.Canonical(lander.snapshot().Worktree) != real {
		owner = a.worktreeOwner(lander.snapshot().Workspace, real)
	}
	id := ""
	if owner != nil {
		id = owner.snapshot().ID
	}
	ns := a.neighboursOf(ctx, id, touched{primary: f.Trees[0].Real, real: real, branch: args.Source, base: args.Into}, files)
	lt := landTold{owner: id, overlap: overlapEntries(ns, false), lander: args.Session}
	ns = newPairs(lander.snapshot().NeighboursTold, sessionPairKey, ns)
	note := ""
	if len(ns) > 0 {
		shown, _ := shownPairs(ns)
		lt.keys, note = toldKeys(shown, sessionPairKey), landNeighboursNote(ns)
	}
	a.rememberLanding(args, lt)
	return note
}

func (a *app) rememberLanding(args landGateArgs, lt landTold) {
	c := &a.overlapCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.landing == nil {
		c.landing = map[string]landTold{}
	}
	maps.DeleteFunc(c.landing, func(_ string, old landTold) bool { return old.lander == args.Session })
	c.landing[landingKey(args)] = lt
}

func (a *app) worktreeOwner(workspaceID, real string) *session {
	for _, s := range a.workspaceSessions(workspaceID) {
		if rec := s.snapshot(); !rec.closed() && !rec.isAside() && gitrepo.Canonical(rec.Worktree) == real {
			return s
		}
	}
	return nil
}

func overlapEntries(ns []neighbour, clip bool) []overlapEntry {
	var out []overlapEntry
	for _, n := range ns {
		e := overlapEntry{Session: n.id, Branch: n.branch, Files: n.files}
		if clip {
			e.Files, e.More = clipFiles(n.files), max(len(n.files)-neighbourFiles, 0)
		}
		out = append(out, e)
	}
	return out
}

func (a *app) setOverlaps(workspaceID string, infos []sessionInfo) {
	c := &a.overlapCache
	c.mu.Lock()
	stale := time.Since(c.passed[workspaceID]) >= max(overlapsMaxAge, listRefreshAge)
	for i := range infos {
		if infos[i].closed() || infos[i].Kind != "" {
			continue
		}
		e, ok := c.overlaps[infos[i].ID]
		infos[i].Overlaps = e.list
		stale = stale || !ok
	}
	start := stale && !c.busy[workspaceID]
	if start {
		if c.busy == nil {
			c.busy = map[string]bool{}
		}
		c.busy[workspaceID] = true
	}
	c.mu.Unlock()
	if start {
		go a.refreshOverlaps(workspaceID)
	}
}

func (a *app) refreshOverlaps(workspaceID string) {
	c := &a.overlapCache
	started := time.Now()
	defer func() {
		c.mu.Lock()
		delete(c.busy, workspaceID)
		if c.passed == nil {
			c.passed = map[string]time.Time{}
		}
		c.passed[workspaceID] = started
		c.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	changed := false
	set := a.overlapSet(ctx, "")
	for _, rec := range a.openRecords() {
		if rec.Workspace != workspaceID {
			continue
		}
		var list []overlapEntry
		if t, ok := set.of(rec.ID); ok && t.ok && len(t.files) > 0 {
			list = overlapEntries(a.neighboursIn(ctx, set, rec.ID, t, t.files), true)
		}
		c.mu.Lock()
		if c.overlaps == nil {
			c.overlaps = map[string]overlapsAt{}
		}
		old := c.overlaps[rec.ID].list
		c.overlaps[rec.ID] = overlapsAt{at: time.Now(), list: list}
		c.mu.Unlock()
		changed = changed || !slices.EqualFunc(old, list, func(x, y overlapEntry) bool {
			return x.Session == y.Session && x.Branch == y.Branch && x.More == y.More && slices.Equal(x.Files, y.Files)
		})
	}
	a.pruneOverlaps()
	if changed {
		a.kick(workspaceID)
	}
}

func (a *app) pruneOverlaps() {
	open, trees := map[string]bool{}, map[string]bool{}
	for _, rec := range a.openRecords() {
		open[rec.ID], trees[treeKey(rec.Worktree)] = true, true
	}
	c := &a.overlapCache
	c.mu.Lock()
	gone := map[string]bool{}
	for _, lt := range c.landing {
		gone[lt.lander] = false
	}
	c.mu.Unlock()
	for id := range gone {
		gone[id] = a.session(id) == nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.DeleteFunc(c.overlaps, func(id string, _ overlapsAt) bool { return !open[id] })
	maps.DeleteFunc(c.touched, func(id string, _ touched) bool { return !open[id] })
	maps.DeleteFunc(c.moved, func(tree string, _ treeMove) bool { return !trees[tree] })
	maps.DeleteFunc(c.views, func(_ string, v *repoView) bool { return time.Since(v.at) >= max(repoViewMaxAge, overlapsAge) })
	maps.DeleteFunc(c.landing, func(_ string, lt landTold) bool { return gone[lt.lander] })
}
