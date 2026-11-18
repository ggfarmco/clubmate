package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"time"

	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	gatePointTurn     = "turn"
	doneTop           = "top"
	doneChild         = "child"
	doneAFK           = "afk"
	gatePointDone     = "Done"
	gateNameDone      = gatePointDone + " · "
	gateNameAnyTurn   = gateNameDone + "any turn"
	gateNamePlanSpecs = gateNameDone + "plans and specs"
	gateNameAnyFile   = gateNameDone + "any file"
	globAny           = "**"
	turnVerdictKey    = "turn-verdict/"
	byGatePaused      = "gate-paused"
	byGateDeleted     = "gate-deleted"
	byGateChanged     = "gate-changed"
	recheckEnv        = "CLUBMATE_AGENTS_RECHECK"
	lastGatesFile     = "last-gates.json"
	gateReleased      = "released"
	gateStopped       = "stopped"
)

var (
	recheckDefault      = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
	doneSessionsDefault = []string{doneTop, doneAFK}
	donePlansAndSpecs   = []string{"**/plans/*.md", "**/specs/*.md"}

	errGatePoint    = errors.New("point is land or turn")
	errGateSessions = errors.New("sessions are top, child and afk")
	errGateWrote    = errors.New("wrote holds file globs such as **/plans/*.md")
	errGateTurnInto = errors.New("a Done gate matches sessions and wrote, not into")
	errGateLandOnly = errors.New("sessions and wrote are for a Done gate")
)

type heldTurn struct {
	Gate     string      `json:"gate"`
	Key      string      `json:"key"`
	Tree     string      `json:"tree,omitempty"`
	Reviewed string      `json:"reviewed,omitempty"`
	Run      string      `json:"run,omitempty"`
	Reviewer string      `json:"reviewer,omitempty"`
	TreeKey  string      `json:"treeKey,omitempty"`
	Round    int         `json:"round,omitempty"`
	Extra    int         `json:"extra,omitempty"`
	Last     string      `json:"last,omitempty"`
	Retried  bool        `json:"retried,omitempty"`
	Moves    int         `json:"moves,omitempty"`
	Wait     string      `json:"wait,omitempty"`
	Release  string      `json:"release,omitempty"`
	Request  string      `json:"request,omitempty"`
	Graph    string      `json:"graph,omitempty"`
	Name     string      `json:"name,omitempty"`
	Repo     string      `json:"repo,omitempty"`
	Asked    time.Time   `json:"asked,omitzero"`
	Findings []string    `json:"findings,omitempty"`
	Deferred []string    `json:"deferred,omitempty"`
	Summary  string      `json:"summary,omitempty"`
	Since    time.Time   `json:"since"`
	TreeAt   time.Time   `json:"treeAt,omitzero"`
	Unread   bool        `json:"unread,omitempty"`
	ReadRepo string      `json:"readRepo,omitempty"`
	Reason   string      `json:"reason,omitempty"`
	Tried    []time.Time `json:"tried,omitempty"`
}

type turnOutcome struct {
	cancelled bool
}

type donePlan struct {
	from    *heldTurn
	held    *heldTurn
	base    *gateBase
	stop    bool
	release string
}

type gateBase struct {
	since string
	at    time.Time
}

var clearedBase = &gateBase{}

func (a *app) planDone(s *session, cancelled bool) donePlan {
	rec := a.afkRec(s.snapshot())
	prior := rec.Held
	switch {
	case cancelled && prior != nil:
		return donePlan{stop: true}
	case cancelled || rec.closed():
		return donePlan{}
	case !doneHoldable(rec):
		return donePlan{base: clearedBase}
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	if prior != nil && prior.Release != "" {
		return donePlan{base: a.releasedBase(ctx, rec, *prior, prior.Release), release: prior.Release}
	}
	gates, err := a.gates(ctx)
	if err != nil {
		logError("read the gates for a finished turn", err)
		return a.unreadPlan(ctx, prior, rec, a.lastGatesRead(), false, err)
	}
	primary := ""
	if prior != nil || turnGated(gates) {
		if primary, err = primaryRead(ctx, rec.Worktree); err != nil {
			return a.unreadPlan(ctx, prior, rec, &gates, true, err)
		}
	}
	switch {
	case prior != nil && primary == "" && !prior.Unread:
		return sameRound(prior)
	case primary == "" && turnGated(gates):
		return donePlan{release: releasedBy(prior, byUnmatched)}
	case !doneGated(gates, primary, rec):
		return donePlan{base: clearedBase, release: releasedBy(prior, byGateChanged)}
	case a.runningChildren(rec.ID) > 0:
		return donePlan{release: releasedBy(prior, byChildren)}
	}
	taken := time.Now().UTC()
	tree, head, err := gitrepo.Snapshot(ctx, rec.Worktree)
	logError("snapshot a finished turn", err)
	if err != nil && prior != nil && !prior.Unread {
		return sameRound(prior)
	}
	wrote := a.turnWrote(ctx, s, tree)
	if len(wrote) == 0 && unchangedSince(ctx, rec.Worktree, gateBaseOf(ctx, s).since, tree) {
		return donePlan{release: releasedBy(prior, byUnchanged)}
	}
	g := heldGate(gates, primary, rec, wrote, err == nil)
	if g == nil {
		return donePlan{release: releasedBy(prior, byUnmatched)}
	}
	key := heldKey(g.ID, head, tree, matchedPaths(*g, rec.Worktree, wrote))
	if a.turnApproved(ctx, g.ID, key) {
		return donePlan{base: a.reviewedBase(ctx, rec, tree, taken), release: releasedBy(prior, landgate.ByReviewer)}
	}
	return donePlan{from: prior, held: nextHold(prior, g.ID, key, tree, taken)}
}

func (a *app) unreadPlan(ctx context.Context, prior *heldTurn, rec record, read *[]gateConfig, repoUnread bool, cause error) donePlan {
	if prior != nil && !prior.Unread {
		return sameRound(prior)
	}
	var g *gateConfig
	_, inRepo := gitrepo.RepoRoot(gitrepo.Canonical(rec.Worktree))
	switch {
	case read != nil:
		g = unreadGate(*read, rec, prior)
	case prior != nil && inRepo:
		g = &gateConfig{ID: prior.Gate}
	}
	if g == nil {
		return donePlan{release: releasedBy(prior, byUnmatched)}
	}
	h := &heldTurn{Round: 1, Since: time.Now().UTC(), Unread: true}
	if prior != nil {
		n := *prior
		n.Tried = append(slices.Clone(prior.Tried), time.Now().UTC())
		h = &n
	}
	h.Gate, h.ReadRepo, h.Reason = g.ID, "", causeOf(cause)
	if repoUnread {
		h.ReadRepo = readRepoName(rec.Worktree)
	}
	switch {
	case h.Request != "" || len(h.Tried) < len(a.recheckDelays()):
	case read == nil:
		h.ask(heldAsk{})
	default:
		h.ask(a.heldAskOf(ctx, g))
	}
	return donePlan{from: prior, held: h}
}

func unreadGate(gates []gateConfig, rec record, prior *heldTurn) *gateConfig {
	if _, ok := gitrepo.RepoRoot(gitrepo.Canonical(rec.Worktree)); !ok {
		return nil
	}
	repo := sessionRepo(rec.Worktree)
	first := -1
	for i, g := range gates {
		switch {
		case g.Paused || g.Point != gatePointTurn || !holdsKind(g.Sessions, rec):
		case prior != nil && g.ID == prior.Gate:
			return &gates[i]
		case first < 0 && (repo == "" || gitrepo.Canonical(g.Repo) == repo):
			first = i
		}
	}
	if first < 0 {
		return nil
	}
	return &gates[first]
}

func readRepoName(worktree string) string {
	if repo := sessionRepo(worktree); repo != "" {
		return filepath.Base(repo)
	}
	if top, ok := gitrepo.RepoRoot(gitrepo.Canonical(worktree)); ok {
		return filepath.Base(top)
	}
	return filepath.Base(worktree)
}

func sessionRepo(worktree string) string {
	top, ok := gitrepo.RepoRoot(gitrepo.Canonical(worktree))
	if !ok {
		return ""
	}
	dotgit := filepath.Join(top, ".git")
	if info, err := os.Stat(dotgit); err == nil && info.IsDir() {
		return top
	}
	raw, err := os.ReadFile(dotgit)
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if err != nil || !ok {
		return ""
	}
	gitdir = absUnder(top, strings.TrimSpace(gitdir))
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return ""
	}
	back, err := os.ReadFile(filepath.Join(gitdir, "gitdir"))
	if err != nil || gitrepo.Canonical(absUnder(gitdir, strings.TrimSpace(string(back)))) != gitrepo.Canonical(dotgit) {
		return ""
	}
	dir := filepath.Clean(absUnder(gitdir, strings.TrimSpace(string(common))))
	if filepath.Base(dir) == ".git" {
		dir = filepath.Dir(dir)
	}
	return gitrepo.Canonical(dir)
}

func absUnder(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func causeOf(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && len(exit.Stderr) > 0 {
		text = string(exit.Stderr)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	for _, p := range []string{"fatal: ", "error: "} {
		line = strings.TrimPrefix(line, p)
	}
	return line
}

func (a *app) recheckDelays() []time.Duration {
	if len(a.rechecks) > 0 {
		return a.rechecks
	}
	return recheckDefault
}

func recheckDelays(every time.Duration) []time.Duration {
	if every <= 0 {
		return recheckDefault
	}
	return []time.Duration{every, every, every}
}

func (a *app) recheckLater(s *session, h *heldTurn) {
	delays := a.recheckDelays()
	time.AfterFunc(delays[min(len(h.Tried), len(delays)-1)], func() {
		if s.snapshot().Held == h {
			a.finishTurn(s)
		}
	})
}

func (a *app) lastGatesRead() *[]gateConfig {
	if p := a.lastGates.Load(); p != nil || a.stateDir == "" {
		return p
	}
	raw, err := os.ReadFile(filepath.Join(a.stateDir, lastGatesFile))
	if err != nil {
		return nil
	}
	var kept []gateConfig
	if err := json.Unmarshal(raw, &kept); err != nil {
		logError("read the last good gates", err)
		return nil
	}
	a.lastGates.CompareAndSwap(nil, &kept)
	return a.lastGates.Load()
}

func (a *app) keepLastGates(gates []gateConfig) {
	kept := slices.Clone(gates)
	was := a.lastGates.Swap(&kept)
	if a.stateDir == "" || was != nil && a.lastGatesSaved.Load() && reflect.DeepEqual(*was, kept) {
		return
	}
	raw, err := json.Marshal(kept)
	if err == nil {
		err = writeAtomic(filepath.Join(a.stateDir, lastGatesFile), raw)
	}
	a.lastGatesSaved.Store(err == nil)
	logError("keep the last good gates", err)
}

func (a *app) afkRec(rec record) record {
	rec.AFK = a.effectiveAFK(rec)
	return rec
}

func sameRound(prior *heldTurn) donePlan {
	if prior == nil {
		return donePlan{}
	}
	return donePlan{from: prior, held: nextHold(prior, prior.Gate, prior.Key, prior.Tree, prior.TreeAt)}
}

func releasedBy(prior *heldTurn, by string) string {
	if prior == nil {
		return ""
	}
	return by
}

func nextHold(prior *heldTurn, gate, key, tree string, taken time.Time) *heldTurn {
	if prior == nil || prior.Gate != gate || prior.Unread {
		since := time.Now().UTC()
		if prior != nil {
			since = prior.Since
		}
		return &heldTurn{Gate: gate, Key: key, Tree: tree, Round: 1, Since: since, TreeAt: taken}
	}
	fixed := prior.Last == gateChanges && prior.Request == ""
	if !fixed && prior.Key == key && prior.Tree == tree {
		return prior
	}
	h := *prior
	if fixed {
		h.Round++
		h.Last, h.Retried, h.Moves = "", false, 0
	}
	if h.Tree != tree {
		h.TreeAt = taken
	}
	h.Key, h.Tree = key, tree
	return &h
}

func (h heldTurn) treeTaken() time.Time {
	switch {
	case !h.TreeAt.IsZero():
		return h.TreeAt
	case !h.Since.IsZero():
		return h.Since
	}
	return time.Now().UTC()
}

func unchangedSince(ctx context.Context, worktree, since, tree string) bool {
	if since == "" || tree == "" {
		return false
	}
	base, err := gitrepo.TreeOf(ctx, worktree, since)
	return err == nil && base == tree
}

func (a *app) reviewedBase(ctx context.Context, rec record, tree string, taken time.Time) *gateBase {
	sha, err := reviewedCheckpoint(ctx, rec, tree)
	if err != nil {
		logError("checkpoint a reviewed done", err)
		return nil
	}
	return &gateBase{since: sha, at: taken}
}

func (a *app) releasedBase(ctx context.Context, rec record, h heldTurn, by string) *gateBase {
	rec = a.afkRec(rec)
	switch {
	case h.Unread:
		return nil
	case by == landgate.ByUser || by == landgate.ByReviewer:
		return a.reviewedBase(ctx, rec, h.Tree, h.treeTaken())
	}
	gates, err := a.gates(ctx)
	if err != nil {
		return nil
	}
	if primary := primaryOf(ctx, rec.Worktree); primary == "" || doneGated(gates, primary, rec) {
		return nil
	}
	return clearedBase
}

func gateBaseOf(ctx context.Context, s *session) gateBase {
	s.mu.Lock()
	s.loadLocked()
	b, worktree := gateBase{since: s.rec.GateSince, at: s.rec.GateAt}, s.rec.Worktree
	for i := len(s.turns) - 1; b.since == "" && i >= 0; i-- {
		b.since = s.turns[i].CheckpointSha
	}
	s.mu.Unlock()
	if b.since != "" {
		return b
	}
	if head, err := gitrepo.Run(ctx, worktree, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
		b.since = strings.TrimSpace(string(head))
	}
	return b
}

func (a *app) turnApproved(ctx context.Context, gate, key string) bool {
	v, found := a.turnVerdict(ctx, gate, key)
	return found && v.Outcome == gateApproved
}

func turnVerdictPath(gate, key string) string {
	return turnVerdictKey + gate + "/" + key
}

func heldKey(gate, head, tree string, paths []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", gate, head, tree)
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%s\n", p, fileHash(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func fileHash(p string) string {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "-"
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return "-"
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "-"
	}
	return hex.EncodeToString(h.Sum(nil))
}

func matchedPaths(g gateConfig, worktree string, wrote []string) []string {
	if len(g.Wrote) == 0 {
		return wrote
	}
	var out []string
	for i, names := range wroteNames(worktree, wrote) {
		if globsMatch(g.Wrote, names) {
			out = append(out, wrote[i])
		}
	}
	return out
}

func (s *session) finishableLocked() bool {
	return !s.busyLocked() && s.rec.Status == statusRunning
}

func (s *session) finishLocked(p donePlan) (bool, bool) {
	s.ending = nil
	switch {
	case p.held != nil && s.rec.Held != p.from:
		return true, true
	case p.held != nil:
		s.rec.Held = p.held
		s.rec.UpdatedAt = time.Now().UTC()
		return true, false
	case p.stop && s.rec.Held != nil:
		s.setStatusLocked(statusIdle, "")
		return true, false
	}
	s.setStatusLocked(statusDone, "")
	s.rebaseLocked(p.base)
	return false, false
}

func (a *app) finishTurn(s *session) {
	s.mu.Lock()
	ok, epoch, cancelled := s.finishableLocked(), s.epoch, s.ending != nil && s.ending.cancelled
	s.mu.Unlock()
	if !ok {
		return
	}
	plan := a.planDone(s, cancelled)
	s.mu.Lock()
	if s.epoch != epoch || !s.finishableLocked() {
		s.mu.Unlock()
		return
	}
	prior := s.rec.Held
	held, stale := s.finishLocked(plan)
	ended := s.turnLocked()
	s.mu.Unlock()
	if stale {
		a.finishTurn(s)
		return
	}
	a.planned(s, plan, prior, held, ended, cancelled)
}

func (a *app) planned(s *session, plan donePlan, prior *heldTurn, held bool, ended api.Turn, cancelled bool) {
	switch {
	case plan.stop && prior != nil:
		s.changed(changeStatus, true)
		a.logHeld(s, *prior, gateStopped, landgate.ByUser)
	case held:
		s.changed(changeStatus, true)
		a.reviewHeld(s)
	default:
		a.closeHeldRound(s, prior)
		a.doneTail(s, ended, cancelled)
		if plan.release != "" && prior != nil {
			a.logReleased(s, *prior, plan.release)
		}
	}
}

func (a *app) doneTail(s *session, ended api.Turn, cancelled bool) {
	s.turned(ended)
	s.changed(changeStatus, true)
	if !cancelled {
		a.notifyEnd(s, ended)
	}
	a.markReview(s)
	a.gateTurnEnded(s, !cancelled)
	a.cleanupChild(s)
	a.closeVanishedChildren(s)
	a.handoffAfter(s, cancelled)
}

func (s *session) rebaseLocked(b *gateBase) {
	if b != nil {
		s.rec.GateSince, s.rec.GateAt = b.since, b.at
	}
}

func (a *app) releaseHeld(s *session, key, by string) {
	s.mu.Lock()
	s.loadLocked()
	h := s.rec.Held
	if h == nil || h.Key != key || s.rec.closed() {
		s.mu.Unlock()
		return
	}
	if s.busyLocked() || len(s.perms) > 0 {
		pending := h.Release == "" && by != landgate.ByReviewer
		if pending {
			n := *h
			n.Release = by
			s.rec.Held = &n
		}
		s.mu.Unlock()
		if pending {
			s.changed(changeStatus, true)
		}
		return
	}
	rec := s.rec
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	base := a.releasedBase(ctx, rec, *h, by)
	cancel()
	s.mu.Lock()
	if s.rec.Held != h || s.busyLocked() || len(s.perms) > 0 {
		s.mu.Unlock()
		a.releaseHeld(s, key, by)
		return
	}
	s.rec.Held = nil
	s.rebaseLocked(base)
	s.setStatusLocked(statusDone, "")
	ended := s.turnLocked()
	s.mu.Unlock()
	a.closeHeldRound(s, h)
	a.doneTail(s, ended, false)
	a.logReleased(s, *h, by)
}

func (a *app) logReleased(s *session, h heldTurn, by string) {
	switch by {
	case landgate.ByReviewer:
	case landgate.ByUser:
		a.logHeld(s, h, gateApproved, by)
	default:
		a.logHeld(s, h, gateReleased, by)
	}
}

func (a *app) logHeld(s *session, h heldTurn, outcome, by string) {
	rec := s.snapshot()
	a.logGateVerdict(context.Background(), gateVerdict{Gate: h.Gate, Workspace: rec.Workspace, Outcome: outcome, By: by, Lander: rec.ID, Title: rec.Title, Round: h.Round, Findings: h.Findings, Deferred: h.Deferred, Summary: h.Summary, At: time.Now().UTC()})
}

func (a *app) turnWrote(ctx context.Context, s *session, tree string) []string {
	base := gateBaseOf(ctx, s)
	if base.since == "" {
		return nil
	}
	s.mu.Lock()
	rec := s.rec
	paths := s.editedSinceLocked(base)
	s.mu.Unlock()
	if top, ok := gitrepo.RepoRoot(rec.Worktree); ok && tree != "" {
		changed, err := changedPaths(ctx, rec.Worktree, top, base.since, tree)
		logError("files changed since the last reviewed done", err)
		paths = append(paths, changed...)
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(rec.Worktree, p)
		}
		out = append(out, gitrepo.Canonical(filepath.Clean(p)))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func turnGated(gates []gateConfig) bool {
	return slices.ContainsFunc(gates, func(g gateConfig) bool { return !g.Paused && g.Point == gatePointTurn })
}

func doneGated(gates []gateConfig, primary string, rec record) bool {
	return primary != "" && doneHoldable(rec) && slices.ContainsFunc(gates, func(g gateConfig) bool {
		return g.Repo == primary && !g.Paused && g.Point == gatePointTurn && holdsKind(g.Sessions, rec)
	})
}

func doneGate(gates []gateConfig, primary string, rec record, wrote []string) *gateConfig {
	if primary == "" || !doneHoldable(rec) {
		return nil
	}
	var names [][]string
	var best *gateConfig
	bestRank := 0
	for i, g := range gates {
		if g.Repo != primary || g.Paused || g.Point != gatePointTurn || !holdsKind(g.Sessions, rec) {
			continue
		}
		rank := 1
		if len(g.Wrote) > 0 {
			if names == nil {
				names = wroteNames(rec.Worktree, wrote)
			}
			if !slices.ContainsFunc(names, func(n []string) bool { return globsMatch(g.Wrote, n) }) {
				continue
			}
			rank = 2
		}
		if rank > bestRank {
			best, bestRank = &gates[i], rank
		}
	}
	return best
}

func heldGate(gates []gateConfig, primary string, rec record, wrote []string, snapshotted bool) *gateConfig {
	if g := doneGate(gates, primary, rec, wrote); g != nil || snapshotted {
		return g
	}
	i := slices.IndexFunc(gates, func(g gateConfig) bool {
		return g.Repo == primary && !g.Paused && g.Point == gatePointTurn && holdsKind(g.Sessions, rec)
	})
	if i < 0 {
		return nil
	}
	return &gates[i]
}

func doneHoldable(rec record) bool {
	return rec.GateFor == nil && rec.Run == nil && !rec.isAside() && !rec.restricted()
}

func holdsKind(sessions []string, rec record) bool {
	kind := doneTop
	if rec.ParentSession != "" {
		kind = doneChild
	}
	return slices.Contains(sessions, kind) && (!rec.AFK || slices.Contains(sessions, doneAFK))
}

func wroteNames(worktree string, wrote []string) [][]string {
	root, _ := gitrepo.RepoRoot(gitrepo.Canonical(worktree))
	roots := map[string]string{}
	out := make([][]string, 0, len(wrote))
	for _, file := range wrote {
		var names []string
		if root != "" && gitrepo.Within(file, root) {
			names = append(names, relSlash(root, file))
		}
		dir := filepath.Dir(file)
		own, ok := roots[dir]
		if !ok {
			own, _ = gitrepo.RepoRoot(dir)
			roots[dir] = own
		}
		if own != "" && own != root {
			names = append(names, relSlash(own, file))
		}
		if len(names) == 0 {
			names = append(names, strings.TrimPrefix(filepath.ToSlash(file), "/"))
		}
		out = append(out, names)
	}
	return out
}

func globsMatch(globs, names []string) bool {
	return slices.ContainsFunc(globs, func(g string) bool {
		return slices.ContainsFunc(names, func(n string) bool { return globMatch(g, n) })
	})
}

func relSlash(root, file string) string {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func globMatch(pattern, name string) bool {
	return matchParts(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchParts(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == globAny {
		for i := range len(name) + 1 {
			if matchParts(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, _ := path.Match(pattern[0], name[0])
	return ok && matchParts(pattern[1:], name[1:])
}

func validGlob(glob string) error {
	if glob == "" || strings.TrimSpace(glob) != glob || strings.HasPrefix(glob, "/") {
		return errGateWrote
	}
	for part := range strings.SplitSeq(glob, "/") {
		if part == "" {
			return errGateWrote
		}
		if _, err := path.Match(part, ""); err != nil {
			return errGateWrote
		}
	}
	return nil
}

func gateMatchOf(args gateAddArgs) (gateConfig, error) {
	switch cmp.Or(args.Point, gatePointLand) {
	case gatePointLand:
		if len(args.Sessions) > 0 || len(args.Wrote) > 0 {
			return gateConfig{}, errGateLandOnly
		}
		into := cmp.Or(args.Into, gateIntoAny)
		if err := validInto(into); err != nil {
			return gateConfig{}, err
		}
		return gateConfig{Point: gatePointLand, Into: into}, nil
	case gatePointTurn:
		if args.Into != "" {
			return gateConfig{}, errGateTurnInto
		}
		kinds, globs, err := doneMatch(args.Sessions, args.Wrote)
		if err != nil {
			return gateConfig{}, err
		}
		return gateConfig{Point: gatePointTurn, Sessions: kinds, Wrote: globs}, nil
	}
	return gateConfig{}, errGatePoint
}

func doneMatch(sessions, wrote []string) ([]string, []string, error) {
	if sessions == nil {
		sessions = doneSessionsDefault
	}
	kinds := slices.Clone(sessions)
	for _, k := range kinds {
		if !slices.Contains([]string{doneTop, doneChild, doneAFK}, k) {
			return nil, nil, errGateSessions
		}
	}
	slices.Sort(kinds)
	kinds = slices.Compact(kinds)
	if len(kinds) == 0 || slices.Equal(kinds, []string{doneAFK}) {
		return nil, nil, errGateSessions
	}
	var globs []string
	for _, g := range wrote {
		if err := validGlob(g); err != nil {
			return nil, nil, err
		}
		if !slices.Contains(globs, g) {
			globs = append(globs, g)
		}
	}
	return kinds, globs, nil
}

func sameDone(x, y gateConfig) bool {
	return slices.Equal(x.Sessions, y.Sessions) && sameSet(x.Wrote, y.Wrote)
}

func sameSet(x, y []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(x)), slices.Sorted(slices.Values(y)))
}

func doneGateName(wrote []string) string {
	switch {
	case len(wrote) == 0:
		return gateNameAnyTurn
	case sameSet(wrote, donePlansAndSpecs):
		return gateNamePlanSpecs
	case slices.Equal(wrote, []string{globAny}):
		return gateNameAnyFile
	}
	return gateNameDone + wrote[0]
}

func (s *session) editedSinceLocked(base gateBase) []string {
	if base.since == "" {
		return nil
	}
	from := 0
	if i := slices.IndexFunc(s.turns, func(t turnInfo) bool { return t.CheckpointSha == base.since }); i >= 0 && base.at.IsZero() {
		from = s.turns[i].Turn
	}
	var out []string
	add := func(entries []*entry) {
		for _, en := range entries {
			if en.Turn >= from && (base.at.IsZero() || en.At.After(base.at)) && en.Tool != nil {
				out = append(out, editedPaths(en.Tool)...)
			}
		}
	}
	add(s.entries)
	for _, sub := range s.subagents {
		add(sub.entries)
	}
	return out
}

func editedPaths(tool map[string]json.RawMessage) []string {
	f, _, _ := parseTool(tool)
	if f.Status != "completed" || !slices.Contains(editKinds, f.Kind) {
		return nil
	}
	var out []string
	for _, l := range f.Locations {
		if l.Path != "" {
			out = append(out, l.Path)
		}
	}
	for _, c := range f.Content {
		if c.Type == "diff" && c.Path != "" {
			out = append(out, c.Path)
		}
	}
	return out
}

func changedPaths(ctx context.Context, worktree, top, since, tree string) ([]string, error) {
	out, err := gitrepo.Run(ctx, worktree, "diff", "--name-only", "-z", "--no-renames", since, tree)
	if err != nil {
		return nil, fmt.Errorf("diff %s: %w", since, err)
	}
	var paths []string
	for name := range strings.SplitSeq(strings.TrimRight(string(out), "\x00"), "\x00") {
		if name != "" {
			paths = append(paths, filepath.Join(top, filepath.FromSlash(name)))
		}
	}
	return paths, nil
}
