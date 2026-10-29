package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/fuzzy"
	"clubmate/plugins/internal/gitrepo"
)

const (
	reviewSlot      = "git.review"
	reviewTreeID    = "review"
	reviewTopic     = "review.changed"
	startedTopic    = "editor.started"
	openCommand     = "editor-nvim.review-open"
	entriesCommand  = "editor-nvim.review-entries"
	selectCommand   = "editor-nvim.review-select"
	reviewedCommand = "editor-nvim.review-reviewed"
	nextCommand     = "editor-nvim.review-next"
	excludesCommand = "editor-nvim.review-excludes"
	moduleMode      = "module"
	nativeMode      = "native"
	fileOpenKind    = "file_open"
	closedKind      = "closed"
	reviewedAction  = "reviewed"
	nextAction      = "next"
	focusAction     = "focus"
	collapseAction  = "collapse-others"
	rootCrumb       = "."
	chainSeparator  = " / "
	moreRowID       = "\x00more"
	maxReviewItems  = 4900
	maxRefs         = 200
	kindWorktree    = "worktree"
	kindBranch      = "branch"
	kindRemote      = "remote"
)

var (
	testGlobs      = []string{"*_test.go", "**/testdata/**", "*.spec.*", "*.test.*"}
	generatedGlobs = []string{"go.sum", "**/go.sum", "go.mod", "**/go.mod", "*-lock.json", "*-lock.yaml", "*.lock", "*.pb.go"}

	errNotCheckedOut = errors.New("is not checked out in a worktree")
	errNoReview      = errors.New("no review in progress")
	errBadGlob       = errors.New("glob may only contain letters, digits and * ? [ ] { } , . _ - / !")
	errReviewDir     = errors.New("dir must be 1 or -1")
	errUnknownFolder = errors.New("not a folder of the review")
	errBadRef        = errors.New("invalid ref")

	globCache sync.Map
)

type filters struct {
	Tests     bool   `json:"tests,omitempty"`
	Generated bool   `json:"generated,omitempty"`
	Glob      string `json:"glob,omitempty"`
}

type storedReview struct {
	Base    string  `json:"base"`
	Filters filters `json:"filters"`
}

type storedReviews struct {
	Reviews map[string]storedReview `json:"reviews,omitempty"`
	Filters filters                 `json:"filters"`
}

type review struct {
	head      string
	base      string
	filters   filters
	mergeBase string
	mode      string
	baseLabel string
	headLabel string
	warning   string
	err       string
	changes   []gitrepo.Change
	entries   []reviewEntry
	opened    string
	closed    bool
	selected  string
	focus     string
	folded    map[string]bool
}

type reviewEntry struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Reviewed  bool   `json:"reviewed"`
	Comments  int    `json:"comments"`
}

type reviewArgs struct {
	Base string `json:"base"`
	Head string `json:"head"`
}

type filterArgs struct {
	Tests     *bool   `json:"tests"`
	Generated *bool   `json:"generated"`
	Glob      *string `json:"glob"`
}

type refsArgs struct {
	Query string `json:"query"`
}

type refInfo struct {
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type openArgs struct {
	Cwd      string   `json:"cwd"`
	Base     string   `json:"base"`
	Excludes []string `json:"excludes"`
}

type openResult struct {
	Mode string `json:"mode"`
}

type pathArgs struct {
	Path string `json:"path"`
}

type reviewedArgs struct {
	Path     string `json:"path"`
	Reviewed bool   `json:"reviewed"`
}

type dirArgs struct {
	Dir int `json:"dir"`
}

type excludesArgs struct {
	Excludes []string `json:"excludes"`
}

type reviewedResult struct {
	Reviewed bool `json:"reviewed"`
}

type rootEvent struct {
	ProjectID string `json:"projectId"`
	Root      string `json:"root"`
}

type reviewChanged struct {
	ProjectID string `json:"projectId"`
	Root      string `json:"root"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
}

type reviewRow struct {
	Path      string
	Status    string
	Additions int
	Deletions int
	Reviewed  bool
	Comments  int
}

type current struct {
	f *folder
	t gitrepo.Worktree
}

func (a *app) reviewCommand(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun, s *state) (any, error) {
	switch cmd.ID {
	case "git.review", "git.review-branch":
		var args reviewArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return nil, a.startReview(ctx, h, cmd.ProjectID, s, args.Base, args.Head)
	case "git.review-swap":
		return nil, a.swapReview(ctx, h, cmd.ProjectID, s)
	case "git.review-filter":
		var args filterArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return nil, a.filterReview(ctx, h, cmd.ProjectID, s, args)
	case "git.review-refs":
		var args refsArgs
		if err := decodeArgs(cmd, &args); err != nil {
			return nil, err
		}
		return a.reviewRefs(ctx, h, cmd.ProjectID, s, args.Query)
	case "git.end-review":
		return nil, a.endReview(ctx, h, cmd.ProjectID, s)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) startReview(ctx context.Context, h *pluginsdk.Host, id string, s *state, base, head string) error {
	for _, ref := range []string{base, head} {
		if strings.HasPrefix(ref, "-") {
			return fmt.Errorf("%w: %q", errBadRef, ref)
		}
	}
	f, t, err := headTarget(ctx, h, id, head)
	if err != nil {
		return err
	}
	if base == "" {
		if base, err = gitrepo.DefaultBase(ctx, t.Real); err != nil {
			return err
		}
	}
	ref, _, _, err := resolveBase(ctx, f, base)
	if err != nil {
		return err
	}
	if _, err := gitrepo.MergeBase(ctx, t.Real, ref, currentHead); err != nil {
		return errors.New(reviewError(base, err))
	}
	s.mu.Lock()
	r := &review{head: t.Real, base: base, filters: s.filters, folded: map[string]bool{}}
	if old := s.reviews[t.Real]; old != nil && old.base == base {
		r.filters, r.selected, r.focus, r.folded = old.filters, old.selected, old.focus, old.folded
	}
	s.reviews[t.Real] = r
	s.active = t.Real
	s.mu.Unlock()
	if err := a.saveReviews(ctx, h, id, s); err != nil {
		return err
	}
	if !f.current(t) {
		if _, err := h.SetWorktree(ctx, id, f.Repo, f.Selection(t)); err != nil {
			return err
		}
	}
	a.schedule(h, id, s, 0)
	logError("focus "+id, h.Focus(ctx, id, editorSection, editorSlot))
	return nil
}

func (a *app) swapReview(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	r := s.reviews[s.active]
	s.mu.Unlock()
	if r == nil {
		return errNoReview
	}
	if err := a.startReview(ctx, h, id, s, r.head, r.base); err != nil {
		return err
	}
	s.mu.Lock()
	if s.reviews[r.head] == r {
		delete(s.reviews, r.head)
	}
	s.mu.Unlock()
	return a.saveReviews(ctx, h, id, s)
}

func (a *app) filterReview(ctx context.Context, h *pluginsdk.Host, id string, s *state, args filterArgs) error {
	if args.Glob != nil {
		if g := strings.TrimSpace(*args.Glob); g != "" && !gitrepo.ValidGlob(g) {
			return errBadGlob
		}
	}
	s.mu.Lock()
	r := s.reviews[s.active]
	f := s.filters
	if r != nil {
		f = r.filters
	}
	if args.Tests != nil {
		f.Tests = *args.Tests
	}
	if args.Generated != nil {
		f.Generated = *args.Generated
	}
	if args.Glob != nil {
		f.Glob = strings.TrimSpace(*args.Glob)
	}
	s.filters = f
	if r != nil {
		r.filters = f
	}
	s.mu.Unlock()
	if err := a.saveReviews(ctx, h, id, s); err != nil {
		return err
	}
	if r != nil {
		logError("review-excludes "+id, h.RunCommand(ctx, excludesCommand, id, excludesArgs{Excludes: f.excludes()}, nil))
	}
	return a.render(ctx, h, id, s)
}

func (a *app) endReview(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	active := s.active
	delete(s.reviews, active)
	s.active = ""
	s.mu.Unlock()
	if active == "" {
		return nil
	}
	if err := a.saveReviews(ctx, h, id, s); err != nil {
		return err
	}
	logError("review-close "+id, h.RunCommand(ctx, closeCommand, id, nil, nil))
	return a.render(ctx, h, id, s)
}

func (a *app) reviewRefs(ctx context.Context, h *pluginsdk.Host, id string, s *state, query string) ([]refInfo, error) {
	s.mu.Lock()
	head := s.active
	s.mu.Unlock()
	f, current, err := headTarget(ctx, h, id, head)
	if err != nil {
		return nil, err
	}
	var worktrees, branches, remotes []refInfo
	for _, t := range f.Trees {
		if !t.Bare && t.Real != current.Real {
			worktrees = append(worktrees, refInfo{Ref: t.Path, Kind: kindWorktree, Label: cmp.Or(t.Branch, filepath.Base(t.Path))})
		}
	}
	local, remote, err := gitrepo.Branches(ctx, f.Checkout.Path)
	if err != nil {
		return nil, err
	}
	for _, b := range local {
		if b != current.Branch {
			branches = append(branches, refInfo{Ref: b, Kind: kindBranch, Label: b})
		}
	}
	for _, b := range remote {
		remotes = append(remotes, refInfo{Ref: b, Kind: kindRemote, Label: b})
	}
	refs := []refInfo{}
	for _, group := range [][]refInfo{worktrees, branches, remotes} {
		refs = append(refs, rankRefs(group, query)...)
	}
	return refs[:min(len(refs), maxRefs)], nil
}

func rankRefs(refs []refInfo, query string) []refInfo {
	if query == "" {
		return refs
	}
	labels := make([]string, len(refs))
	byLabel := map[string][]refInfo{}
	for i, r := range refs {
		labels[i] = r.Label
		byLabel[r.Label] = append(byLabel[r.Label], r)
	}
	var out []refInfo
	for _, m := range fuzzy.Rank(labels, query, len(labels)) {
		out = append(out, byLabel[m.Path][0])
		byLabel[m.Path] = byLabel[m.Path][1:]
	}
	return out
}

func (a *app) refreshReview(ctx context.Context, h *pluginsdk.Host, id string, s *state) {
	s.mu.Lock()
	none := len(s.reviews) == 0
	if none {
		s.active = ""
	}
	s.mu.Unlock()
	if none {
		return
	}
	p, err := h.GetProject(ctx, id)
	if err != nil {
		logError("project "+id, err)
		return
	}
	var currents []current
	for i := range p.Repos {
		if p.Home {
			break
		}
		if f, err := openFolder(ctx, p, i); err == nil {
			currents = append(currents, current{f: f, t: f.currentTree()})
		}
	}
	s.mu.Lock()
	at := slices.IndexFunc(currents, func(c current) bool { return c.t.Real == s.active && s.reviews[s.active] != nil })
	if at < 0 {
		at = slices.IndexFunc(currents, func(c current) bool { return s.reviews[c.t.Real] != nil })
	}
	if at < 0 {
		s.active = ""
		s.mu.Unlock()
		return
	}
	cur := currents[at]
	s.active = cur.t.Real
	r := s.reviews[s.active]
	base, opened, mode, excludes, closed := r.base, r.opened, r.mode, r.filters.excludes(), r.closed
	s.mu.Unlock()
	headLabel, err := gitrepo.Branch(ctx, cur.t.Real)
	logError("branch "+cur.t.Real, err)
	ref, baseLabel, warning, err := resolveBase(ctx, cur.f, base)
	var mergeBase string
	var changes []gitrepo.Change
	if err == nil {
		mergeBase, err = gitrepo.MergeBase(ctx, cur.t.Real, ref, currentHead)
	}
	if err == nil {
		changes, err = gitrepo.WorkingChanges(ctx, cur.t.Real, mergeBase)
	}
	if err != nil {
		s.mu.Lock()
		r.err, r.headLabel, r.baseLabel = reviewError(base, err), headLabel, cmp.Or(baseLabel, base)
		s.mu.Unlock()
		return
	}
	reopened := false
	var editorErr error
	if opened != mergeBase && !closed {
		var out openResult
		if editorErr = h.RunCommand(ctx, openCommand, id, openArgs{Cwd: cur.t.Real, Base: mergeBase, Excludes: excludes}, &out); editorErr != nil {
			logError("review-open "+id, editorErr)
		} else {
			mode, reopened = cmp.Or(out.Mode, nativeMode), true
		}
	}
	var entries []reviewEntry
	if mode == moduleMode && editorErr == nil && !closed {
		if editorErr = h.RunCommand(ctx, entriesCommand, id, nil, &entries); editorErr != nil {
			logError("review-entries "+id, editorErr)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviews[cur.t.Real] != r {
		return
	}
	r.err, r.headLabel, r.baseLabel, r.warning = editorError(editorErr), headLabel, baseLabel, warning
	r.mergeBase, r.changes, r.mode = mergeBase, changes, mode
	if reopened {
		r.opened = mergeBase
	}
	if mode != moduleMode || editorErr == nil && !closed {
		r.entries = entries
	}
}

func editorError(err error) string {
	var rpcErr *jsonrpc2.Error
	switch {
	case err == nil:
		return ""
	case errors.As(err, &rpcErr):
		return strings.TrimPrefix(rpcErr.Message, "plugin "+editorOwner+" command.run: ")
	}
	return err.Error()
}

func (a *app) editorStarted(h *pluginsdk.Host, p rootEvent) {
	s := a.lookup(p.ProjectID)
	if s == nil {
		return
	}
	reopen := false
	s.mu.Lock()
	for _, r := range s.reviews {
		r.opened, reopen = "", true
	}
	s.mu.Unlock()
	if reopen {
		a.schedule(h, p.ProjectID, s, 0)
	}
}

func (a *app) reviewChanged(ctx context.Context, h *pluginsdk.Host, p reviewChanged) {
	s := a.lookup(p.ProjectID)
	if s == nil {
		return
	}
	s.mu.Lock()
	r := s.reviews[s.active]
	switch {
	case r == nil:
	case p.Kind == fileOpenKind:
		r.selected = p.Path
	case p.Kind == closedKind:
		r.opened, r.closed = "", true
	}
	s.mu.Unlock()
	switch {
	case r == nil, p.Kind == closedKind:
	case p.Kind == fileOpenKind:
		logError("render "+p.ProjectID, a.render(ctx, h, p.ProjectID, s))
	default:
		a.schedule(h, p.ProjectID, s, refreshDelay)
	}
}

func (a *app) reviewEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent, s *state) error {
	if ev.Action == nextAction {
		var dir int
		if err := json.Unmarshal(ev.Value, &dir); err != nil {
			return fmt.Errorf("next value: %w", err)
		}
		return a.reviewNext(ctx, h, ev.ProjectID, s, dir)
	}
	var item string
	if err := json.Unmarshal(ev.Value, &item); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if ev.Action == "select" || ev.Action == reviewedAction {
		a.reopenClosed(ctx, h, ev.ProjectID, s)
	}
	s.mu.Lock()
	r := s.reviews[s.active]
	if r == nil {
		s.mu.Unlock()
		return errNoReview
	}
	rows, _ := r.rows()
	at := slices.IndexFunc(rows, func(row reviewRow) bool { return row.Path == item })
	isFile := at >= 0
	head, mergeBase, mode := r.head, r.mergeBase, r.mode
	var err error
	switch ev.Action {
	case "select":
		r.selected = item
	case "expand", "collapse":
		r.folded[item] = ev.Action == "collapse"
	case focusAction:
		err = r.setFocus(item, rows)
	case collapseAction:
		r.collapseOthers(item, rows)
	case reviewedAction:
	default:
		err = fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	switch {
	case ev.Action == "select" && isFile:
		err = a.openReviewFile(ctx, h, ev.ProjectID, head, mergeBase, mode, item)
	case ev.Action == reviewedAction && isFile && mode == moduleMode:
		err = a.setReviewed(ctx, h, ev.ProjectID, s, item, !rows[at].Reviewed)
	}
	if err != nil {
		return err
	}
	return a.render(ctx, h, ev.ProjectID, s)
}

func (a *app) openReviewFile(ctx context.Context, h *pluginsdk.Host, id, head, mergeBase, mode, rel string) error {
	if mode == moduleMode {
		return h.RunCommand(ctx, selectCommand, id, pathArgs{Path: rel}, nil)
	}
	return h.RunCommand(ctx, diffCommand, id, diffArgs{Path: filepath.Join(head, rel), Base: "rev:" + mergeBase, Target: "worktree"}, nil)
}

func (a *app) reopenClosed(ctx context.Context, h *pluginsdk.Host, id string, s *state) {
	s.mu.Lock()
	r := s.reviews[s.active]
	closed := r != nil && r.closed
	if closed {
		r.closed = false
	}
	s.mu.Unlock()
	if closed {
		a.refreshReview(ctx, h, id, s)
	}
}

func (a *app) setReviewed(ctx context.Context, h *pluginsdk.Host, id string, s *state, rel string, want bool) error {
	var out reviewedResult
	if err := h.RunCommand(ctx, reviewedCommand, id, reviewedArgs{Path: rel, Reviewed: want}, &out); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.reviews[s.active]; r != nil {
		r.entries = slices.Clone(r.entries)
		for i := range r.entries {
			if r.entries[i].Path == rel {
				r.entries[i].Reviewed = out.Reviewed
			}
		}
	}
	return nil
}

func (a *app) reviewNext(ctx context.Context, h *pluginsdk.Host, id string, s *state, dir int) error {
	if dir != 1 && dir != -1 {
		return errReviewDir
	}
	a.reopenClosed(ctx, h, id, s)
	s.mu.Lock()
	r := s.reviews[s.active]
	if r == nil {
		s.mu.Unlock()
		return errNoReview
	}
	rows, _ := r.rows()
	order := buildDirs(rows).order()
	head, mergeBase, mode, selected := r.head, r.mergeBase, r.mode, r.selected
	s.mu.Unlock()
	next := ""
	if mode == moduleMode {
		var out pathArgs
		if err := h.RunCommand(ctx, nextCommand, id, dirArgs{Dir: dir}, &out); err != nil {
			return err
		}
		next = out.Path
	} else if len(order) > 0 {
		at := slices.Index(order, selected)
		if at < 0 && dir < 0 {
			at = 0
		}
		next = order[(at+dir+len(order))%len(order)]
		if err := a.openReviewFile(ctx, h, id, head, mergeBase, mode, next); err != nil {
			return err
		}
	}
	if next == "" {
		return nil
	}
	s.mu.Lock()
	if cur := s.reviews[s.active]; cur == r {
		r.selected = next
	}
	s.mu.Unlock()
	return a.render(ctx, h, id, s)
}

func (a *app) saveReviews(ctx context.Context, h *pluginsdk.Host, id string, s *state) error {
	s.mu.Lock()
	st := storedReviews{Reviews: map[string]storedReview{}, Filters: s.filters}
	for head, r := range s.reviews {
		st.Reviews[head] = storedReview{Base: r.base, Filters: r.filters}
	}
	s.mu.Unlock()
	return h.StorageSet(ctx, reviewKey(id), st)
}

func (s *state) restore(st storedReviews) {
	s.filters = st.Filters.valid()
	for head, r := range st.Reviews {
		if r.Base != "" && filepath.IsAbs(head) {
			s.reviews[head] = &review{head: head, base: r.Base, filters: r.Filters.valid(), folded: map[string]bool{}}
		}
	}
}

func (f filters) valid() filters {
	if f.Glob != "" && !gitrepo.ValidGlob(f.Glob) {
		f.Glob = ""
	}
	return f
}

func (s *state) reviewModel() *review {
	r := s.reviews[s.active]
	if r == nil {
		return nil
	}
	c := *r
	c.folded = maps.Clone(r.folded)
	return &c
}

func (r *review) setFocus(item string, rows []reviewRow) error {
	if slices.ContainsFunc(rows, func(row reviewRow) bool { return row.Path == item }) {
		item = filepath.Dir(item)
	}
	if item == "" || item == rootCrumb {
		r.focus = ""
		return nil
	}
	if buildDirs(rows).find(item) == nil {
		return fmt.Errorf("%w: %q", errUnknownFolder, item)
	}
	r.focus = item
	for dir := item; dir != rootCrumb; dir = filepath.Dir(dir) {
		delete(r.folded, dir)
	}
	return nil
}

func (r *review) collapseOthers(item string, rows []reviewRow) {
	keep := map[string]bool{}
	for dir := filepath.Dir(cmp.Or(item, r.selected)); dir != rootCrumb; dir = filepath.Dir(dir) {
		keep[dir] = true
	}
	for _, row := range rows {
		for dir := filepath.Dir(row.Path); dir != rootCrumb; dir = filepath.Dir(dir) {
			if !keep[dir] {
				r.folded[dir] = true
			}
		}
	}
}

func (r *review) rows() ([]reviewRow, int) {
	globs := r.filters.excludes()
	hidden := 0
	for _, c := range r.changes {
		if excluded(c.Path, globs) {
			hidden++
		}
	}
	var rows []reviewRow
	if r.mode == moduleMode {
		for _, e := range r.entries {
			if !excluded(e.Path, globs) {
				rows = append(rows, reviewRow(e))
			}
		}
		return rows, hidden
	}
	for _, c := range r.changes {
		if !excluded(c.Path, globs) {
			rows = append(rows, reviewRow{Path: c.Path, Status: c.Status, Additions: c.Additions, Deletions: c.Deletions})
		}
	}
	return rows, hidden
}

func (f filters) excludes() []string {
	out := []string{}
	if f.Tests {
		out = append(out, testGlobs...)
	}
	if f.Generated {
		out = append(out, generatedGlobs...)
	}
	if f.Glob != "" {
		out = append(out, f.Glob)
	}
	return out
}

func excluded(rel string, globs []string) bool {
	return slices.ContainsFunc(globs, func(g string) bool { return globMatch(g, rel) })
}

func globMatch(pattern, rel string) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		return rel == pattern || strings.HasPrefix(rel, pattern+"/")
	}
	cached, ok := globCache.Load(pattern)
	if !ok {
		cached, _ = globCache.LoadOrStore(pattern, globRegexp(pattern))
	}
	re := cached.(*regexp.Regexp)
	return re != nil && re.MatchString(rel)
}

func globRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch c := runes[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			end := slices.Index(runes[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := string(runes[i+1 : i+1+end])
			if rest, ok := strings.CutPrefix(class, "!"); ok {
				class = "^" + rest
			}
			b.WriteString("[" + class + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

func headTarget(ctx context.Context, h *pluginsdk.Host, id, head string) (*folder, gitrepo.Worktree, error) {
	p, err := h.GetProject(ctx, id)
	if err != nil {
		return nil, gitrepo.Worktree{}, err
	}
	if p.Home {
		return nil, gitrepo.Worktree{}, errNoRepo
	}
	for i := range p.Repos {
		f, err := openFolder(ctx, p, i)
		if errors.Is(err, gitrepo.ErrNotRepo) {
			continue
		}
		if err != nil {
			return nil, gitrepo.Worktree{}, err
		}
		if head == "" {
			return f, f.currentTree(), nil
		}
		if t, ok := f.Member(head); ok {
			return f, t, nil
		}
		for _, t := range f.Trees {
			if !t.Bare && t.Branch == head {
				return f, t, nil
			}
		}
	}
	if head == "" {
		return nil, gitrepo.Worktree{}, errNoRepo
	}
	return nil, gitrepo.Worktree{}, fmt.Errorf("%s %w", head, errNotCheckedOut)
}

func resolveBase(ctx context.Context, f *folder, base string) (string, string, string, error) {
	if !filepath.IsAbs(base) {
		return base, base, "", nil
	}
	t, err := f.member(base)
	if err != nil {
		return "", "", "", err
	}
	label := cmp.Or(t.Branch, filepath.Base(t.Path))
	out, err := run(ctx, t.Path, "status", "--porcelain")
	if err != nil {
		return "", "", "", err
	}
	warning := ""
	if len(bytes.TrimSpace(out)) > 0 {
		warning = label + " has uncommitted changes that are not in the base"
	}
	return cmp.Or(t.Branch, t.Head), label, warning, nil
}

func decodeArgs(cmd pluginsdk.CommandRun, v any) error {
	if len(cmd.Args) == 0 || string(cmd.Args) == "null" {
		return nil
	}
	if err := json.Unmarshal(cmd.Args, v); err != nil {
		return fmt.Errorf("%s args: %w", cmd.ID, err)
	}
	return nil
}
