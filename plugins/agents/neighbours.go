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
	touchedAge      = 10 * time.Second
	touchedAgeEnv   = "CLUBMATE_AGENTS_TOUCHED_AGE"
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

var touchedMaxAge = envDelay(touchedAgeEnv, touchedAge)

type touched struct {
	at        time.Time
	ok        bool
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
	landing     map[string]landTold
	landedCheck func(ctx context.Context, repo, tip, base string) bool
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

func (a *app) touchedFiles(ctx context.Context, rec record) touched {
	return a.overlapCache.shared(ctx, rec.ID, func() touched { return a.computeTouched(ctx, rec) })
}

func (c *overlapCache) shared(ctx context.Context, id string, compute func() touched) touched {
	for {
		c.mu.Lock()
		if t, ok := c.touched[id]; ok && time.Since(t.at) < touchedMaxAge {
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
			if f.ok {
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

func (a *app) computeTouched(ctx context.Context, rec record) touched {
	t := touched{at: time.Now()}
	if rec.closed() || rec.Worktree == "" {
		return t
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return t
	}
	f, err := gitrepo.OpenFolder(ctx, rec.Worktree)
	if err != nil || f.GateTree(f.Checkout) {
		return t
	}
	t.primary, t.real, t.branch = f.Trees[0].Real, f.Checkout.Real, f.Checkout.Branch
	t.def = f.DefaultName(ctx)
	t.base = cmp.Or(a.baseOf(ctx, rec), t.def)
	if t.base == "" {
		return t
	}
	got, err := gitrepo.TouchedFiles(ctx, f.Checkout.Path, t.base)
	if err != nil {
		return t
	}
	t.mergeBase, t.files, t.untracked = got.MergeBase, got.Files, got.Untracked
	if t.branch != "" && f.Checkout.Head != got.MergeBase {
		t.head = f.Checkout.Head
		t.baseTip, _ = gitrepo.ResolveCommit(ctx, t.primary, t.base)
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
	t := a.touchedFiles(ctx, rec)
	if !t.ok || len(t.files) == 0 {
		return nil
	}
	return a.neighboursOf(ctx, rec.ID, t, t.files)
}

func (a *app) neighboursOf(ctx context.Context, id string, self touched, files []string) []neighbour {
	var cands []record
	for _, s := range a.allSessions() {
		n := s.snapshot()
		if n.ID == id || !a.neighbourCandidate(n) || id != "" && (a.descendantOf(n.ID, id) || a.descendantOf(id, n.ID)) {
			continue
		}
		if repo := sessionRepo(ctx, n.Worktree); repo != "" && repo != gitrepo.Canonical(self.primary) {
			continue
		}
		cands = append(cands, n)
	}
	got := a.touchedAll(ctx, cands)
	var out []neighbour
	var outTouched []touched
	for i, n := range cands {
		nt := got[i]
		if !nt.ok || nt.primary != self.primary || nt.real == self.real || nt.branch == "" || nt.branch == nt.def || nt.branch == self.base || self.branch != "" && self.branch == nt.base {
			continue
		}
		shared := common(files, nt.files)
		if len(shared) == 0 {
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

func (a *app) touchedAll(ctx context.Context, recs []record) []touched {
	out := make([]touched, len(recs))
	limited(len(recs), func(i int) { out[i] = a.touchedFiles(ctx, recs[i]) })
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
	stale := false
	c.mu.Lock()
	for i := range infos {
		e, ok := c.overlaps[infos[i].ID]
		infos[i].Overlaps = e.list
		if !infos[i].closed() && (!ok || time.Since(e.at) >= max(touchedMaxAge, listRefreshAge)) {
			stale = true
		}
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
	defer func() {
		c.mu.Lock()
		delete(c.busy, workspaceID)
		c.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	changed := false
	for _, s := range a.workspaceSessions(workspaceID) {
		rec := s.snapshot()
		list := overlapEntries(a.neighbours(ctx, rec), true)
		c.mu.Lock()
		if c.overlaps == nil {
			c.overlaps = map[string]overlapsAt{}
		}
		old := c.overlaps[rec.ID].list
		c.overlaps[rec.ID] = overlapsAt{at: time.Now(), list: list}
		if rec.closed() {
			delete(c.touched, rec.ID)
		}
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
	live := map[string]bool{}
	for _, s := range a.allSessions() {
		live[s.snapshot().ID] = true
	}
	c := &a.overlapCache
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.DeleteFunc(c.overlaps, func(id string, _ overlapsAt) bool { return !live[id] })
	maps.DeleteFunc(c.touched, func(id string, _ touched) bool { return !live[id] })
	maps.DeleteFunc(c.landing, func(_ string, lt landTold) bool { return !live[lt.lander] })
}
