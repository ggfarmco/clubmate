package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"clubmate/pkg/id"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	heldTitle          = "Done review · "
	heldFilesLimit     = 64 << 10
	heldMovesMax       = 3
	heldFirstFormat    = "[Clubmate Done review] Review this agent's turn before the user sees it as done (repo %s, gate %s).\nSession: %s\nThe change since its last reviewed done: `git diff %s %s` in %s.%s%s\nReport confirmed problems with file:line."
	heldFirstVerdict   = " Then call agents_land_verdict once with key %s: outcome changes only for confirmed problems, each finding as \"file:line — what is wrong\"; otherwise approved. Nits and suggestions go to deferred."
	heldDeltaFormat    = "[Clubmate Done review] The agent changed its work after your last review. Review what changed: `git diff %s %s` (the whole change is `git diff %s %s`) in %s.%s"
	heldDeltaVerdict   = " Then call agents_land_verdict again with key %s."
	heldFilesFormat    = "\nFiles the gate matched:\n%s"
	heldFileFormat     = "--- %s\n%s\n"
	heldFilesCut       = "(the rest is cut at 64 KiB; read it from disk)\n"
	heldReplyFormat    = "\nThe agent's last reply:\n%s"
	heldChangesFormat  = "[Clubmate] From the gate · %s · %s · round %d of %d · changes\n%s"
	heldNotBlocking    = "\nNot blocking:\n%s"
	heldChangesTail    = "\nFix these; your next finished turn goes back to the gate."
	heldNudgeText      = "[Clubmate] Your Done review ended without a verdict. Call agents_land_verdict now with the key your review prompt names: approved, or changes with the confirmed problems as findings."
	heldReviewerText   = "[Clubmate Done reviewer] You review an agent's finished turn before the user sees it as done. You are read-only: never edit files, commit, merge, reset, check out, stash or delete anything, and never start sessions. Read the change and the files around it, run read-only commands when they help, and end every review with exactly one agents_land_verdict call with the key your review prompt names."
	heldNodeText       = "[Clubmate Done gate node] You help a Done gate review an agent's finished turn. You are read-only: never edit files, commit, merge, reset, check out, stash or delete anything, and never start sessions. Do what your node's prompt asks and put your findings in your reply; another node of the gate's graph decides, so don't call agents_land_verdict."
	verdictKeyFormat   = "key must be the one your latest review prompt names: %s"
	heldReviewStartLog = "start the Done review"
	byChildren         = "children-running"
	byUnchanged        = "unchanged"
	byUnmatched        = "unmatched"
)

var errHeldMoved = errors.New("the session's files changed since this review began: review the new prompt, then give the verdict again")

type heldAsk struct {
	graph string
	name  string
	repo  string
}

type heldStep int

const (
	heldNothing heldStep = iota
	heldRetry
)

func (a *app) reviewHeld(s *session) {
	ctx := context.Background()
	rec := s.snapshot()
	h := rec.Held
	if h == nil || h.Last != "" || h.Request != "" || rec.closed() {
		return
	}
	g := a.gateByID(ctx, h.Gate)
	if g == nil {
		return
	}
	if v, ok := a.turnVerdict(ctx, h.Gate, h.Key); ok {
		switch v.Outcome {
		case gateApproved:
			a.releaseHeld(s, h.Key, v.By)
		case gateChanges:
			a.heldChanges(s, h, *g, v)
		}
		return
	}
	graph := a.gateGraphReady(ctx, *g)
	if !graph.ok {
		a.swapHeld(s, h, func(n *heldTurn) { n.Wait = graph.text })
		return
	}
	reuse := a.gateRunLive(ctx, rec.Workspace, h.Run) && a.graphs.GateRunOf(ctx, h.Run, g.Graph, graph.hash) && a.graphs.GateStepsIdle(ctx, h.Run)
	if reuse && slices.ContainsFunc(a.gateNodes(rec.Workspace, h.Run), func(x *session) bool {
		node := x.snapshot().GateFor
		return node.open() && node.Key == h.Key
	}) {
		return
	}
	n := *h
	n.Wait = ""
	if !reuse {
		n.TreeKey = ""
	}
	dir := ""
	if a.graphs.GateSteps(ctx, g.Repo, g.Graph) {
		if n.TreeKey == "" {
			n.TreeKey = id.New(gateTreeKeyPrefix)
		}
		var err error
		if dir, err = a.heldGateTree(ctx, rec, *g, gateBaseOf(ctx, s).since, n.TreeKey, h.Tree); err != nil {
			logError(heldReviewStartLog, err)
			a.dropHeldTree(ctx, rec.Workspace, *g, h.TreeKey, n.TreeKey)
			a.heldEnded(s, h)
			return
		}
	}
	first, delta := a.heldPrompts(ctx, s, rec, *g, h)
	a.gateMu.Lock()
	if s.snapshot().Held != h {
		a.gateMu.Unlock()
		a.dropHeldTree(ctx, rec.Workspace, *g, h.TreeKey, n.TreeKey)
		return
	}
	gf := gateFor{Gate: g.ID, Point: gatePointTurn, Key: h.Key, Lander: rec.ID, Run: h.Run}
	if reuse {
		text, verdict := first, first+fmt.Sprintf(heldFirstVerdict, h.Key)
		if delta != "" {
			text, verdict = delta, delta+fmt.Sprintf(heldDeltaVerdict, h.Key)
		}
		if err := a.graphs.PromptGate(ctx, h.Run, verdict, text); err != nil {
			a.gateMu.Unlock()
			logError("prompt the Done review", err)
			a.heldEnded(s, h)
			return
		}
		a.openGateRound(ctx, rec.Workspace, gf)
	} else {
		gf.Run = ""
		run, reviewer, err := a.graphs.StartGate(ctx, graphs.GateRun{
			Workspace: rec.Workspace,
			Graph:     g.Graph,
			Pin:       graph.hash,
			Repo:      g.Repo,
			Input:     first + fmt.Sprintf(heldFirstVerdict, h.Key),
			Other:     first,
			Dir:       dir,
			NewNode:   a.gateNode(rec.Workspace, heldTitle+rec.Title, rec.Worktree, gf),
		})
		if err != nil {
			a.gateMu.Unlock()
			logError(heldReviewStartLog, err)
			a.dropHeldTree(ctx, rec.Workspace, *g, h.TreeKey, n.TreeKey)
			a.heldEnded(s, h)
			return
		}
		n.Run, n.Reviewer = run, reviewer
		a.kick(rec.Workspace)
	}
	n.Reviewed = h.Tree
	swapped := s.casHeld(h, &n)
	a.gateMu.Unlock()
	if swapped {
		s.changed(changeStatus, true)
	}
}

func (a *app) dropHeldTree(ctx context.Context, workspaceID string, g gateConfig, kept, made string) {
	if made != "" && made != kept {
		a.dropGateTree(ctx, workspaceID, landReview{Repo: g.Repo, Gate: g.ID, TreeKey: made})
	}
}

func (a *app) heldVerdictCommand(ctx context.Context, node *session, args landVerdictArgs) (any, error) {
	rec := node.snapshot()
	gf := *rec.GateFor
	if cmp.Or(args.Key, args.Tree) != gf.Key {
		return nil, fmt.Errorf(verdictKeyFormat, gf.Key)
	}
	s := a.session(gf.Lander)
	if gf.Done || s == nil {
		return nil, errNoReview
	}
	a.gateMu.Lock()
	h := s.snapshot().Held
	a.gateMu.Unlock()
	if h == nil || h.Key != gf.Key || h.Run != gf.Run || h.Last != "" {
		return nil, errNoReview
	}
	g := a.gateByID(ctx, h.Gate)
	if g == nil {
		return nil, errNoReview
	}
	taken := time.Now().UTC()
	key, tree, ok := a.heldKeyNow(ctx, s, *g)
	ask := a.heldAskOf(ctx, g)
	a.gateMu.Lock()
	if s.snapshot().Held != h {
		a.gateMu.Unlock()
		return nil, errNoReview
	}
	if _, decided := a.turnVerdict(ctx, h.Gate, h.Key); decided {
		a.gateMu.Unlock()
		return nil, errDecided
	}
	if ok && key != h.Key {
		n := *h
		n.Key, n.Tree, n.TreeAt, n.Moves = key, tree, taken, h.Moves+1
		if n.Moves >= heldMovesMax {
			n.ask(ask)
		}
		swapped := s.casHeld(h, &n)
		a.gateMu.Unlock()
		if swapped {
			s.changed(changeStatus, true)
			a.reviewHeld(s)
		}
		return nil, errHeldMoved
	}
	v := landgate.Verdict{Tree: h.Key, Outcome: args.Outcome, By: landgate.ByReviewer, Reviewer: rec.ID, Findings: args.Findings, Deferred: args.Deferred, Summary: args.Summary, At: time.Now().UTC()}
	if err := a.host.StorageSet(ctx, turnVerdictPath(h.Gate, h.Key), v); err != nil {
		a.gateMu.Unlock()
		return nil, err
	}
	a.closeGateRound(ctx, rec.Workspace, gf.Run, node)
	if gf.Run != "" {
		a.graphs.GateOutcome(ctx, gf.Run, rec.ID, args.Outcome)
	}
	held := s.snapshot()
	a.logGateVerdict(ctx, gateVerdict{Gate: h.Gate, Workspace: held.Workspace, Outcome: args.Outcome, By: landgate.ByReviewer, Lander: held.ID, Title: held.Title, Round: h.Round, Reviewer: rec.ID, Findings: args.Findings, Deferred: args.Deferred, Summary: args.Summary, At: v.At})
	a.gateMu.Unlock()
	if args.Outcome == gateApproved {
		a.releaseHeld(s, h.Key, landgate.ByReviewer)
		return nil, nil
	}
	a.heldChanges(s, h, *g, v)
	return nil, nil
}

func (a *app) heldChanges(s *session, h *heldTurn, g gateConfig, v landgate.Verdict) {
	n := *h
	n.Last, n.Findings, n.Deferred, n.Summary = gateChanges, v.Findings, v.Deferred, v.Summary
	capped := n.Round >= g.Rounds+n.Extra
	if capped {
		n.ask(a.heldAskOf(context.Background(), &g))
	}
	if !s.casHeld(h, &n) {
		return
	}
	s.changed(changeStatus, true)
	if !capped {
		a.promptChanges(s, g, n)
	}
}

func (a *app) promptChanges(s *session, g gateConfig, h heldTurn) {
	text := fmt.Sprintf(heldChangesFormat, filepath.Base(g.Repo), doneGateName(g.Wrote), h.Round, g.Rounds+h.Extra, bullets(h.Findings))
	if len(h.Deferred) > 0 {
		text += fmt.Sprintf(heldNotBlocking, bullets(h.Deferred))
	}
	logError("send the gate's findings", a.submit(s, &promptArgs{Text: text + heldChangesTail, Gate: true}))
}

func (a *app) heldRunEnded(workspaceID, run string) {
	a.gateMu.Lock()
	var ended []*session
	for _, s := range a.workspaceSessions(workspaceID) {
		if h := s.snapshot().Held; h != nil && h.Run == run {
			ended = append(ended, s)
		}
	}
	a.gateMu.Unlock()
	for _, s := range ended {
		if h := s.snapshot().Held; h != nil && h.Run == run {
			a.heldEnded(s, h)
		}
	}
}

func (a *app) heldEnded(s *session, h *heldTurn) {
	ctx := context.Background()
	ask := a.heldAskOf(ctx, a.gateByID(ctx, h.Gate))
	a.gateMu.Lock()
	n := *h
	n.Run, n.Reviewer = "", ""
	step := heldNothing
	_, decided := a.turnVerdict(ctx, h.Gate, h.Key)
	switch {
	case h.Last != "" || h.Request != "" || decided:
	case !h.Retried:
		n.Retried, step = true, heldRetry
	default:
		n.ask(ask)
	}
	swapped := s.casHeld(h, &n)
	a.gateMu.Unlock()
	if !swapped {
		return
	}
	s.changed(changeStatus, true)
	if step == heldRetry {
		a.reviewHeld(s)
	}
}

func (a *app) heldAskOf(ctx context.Context, g *gateConfig) heldAsk {
	if g == nil {
		return heldAsk{}
	}
	out := heldAsk{graph: g.Graph, name: doneGateName(g.Wrote), repo: filepath.Base(g.Repo)}
	if info, err := a.graphs.GateGraphInfo(ctx, g.Repo, g.Graph); err == nil {
		out.graph = cmp.Or(info.Title, info.Name, g.Graph)
	}
	return out
}

func (h *heldTurn) ask(facts heldAsk) {
	h.Request, h.Asked = id.New(gateRequestPrefix), time.Now().UTC()
	h.Graph, h.Name, h.Repo = facts.graph, facts.name, facts.repo
}

func (a *app) regateHolds(ctx context.Context, gateID, by string) {
	gates, err := a.gates(ctx)
	if err != nil {
		return
	}
	for _, s := range a.allSessions() {
		rec := s.snapshot()
		h := rec.Held
		if h == nil || h.Gate != gateID {
			continue
		}
		taken := time.Now().UTC()
		tree, head, err := gitrepo.Snapshot(ctx, rec.Worktree)
		wrote := a.turnWrote(ctx, s, tree)
		g := doneGate(gates, primaryOf(ctx, rec.Worktree), rec, wrote)
		switch {
		case err != nil && by == byGateChanged:
		case g == nil:
			a.releaseHeld(s, h.Key, by)
		case g.ID == gateID:
		default:
			n := &heldTurn{Gate: g.ID, Key: h.Key, Tree: h.Tree, Round: 1, Since: h.Since, TreeAt: h.TreeAt}
			if err == nil {
				n.Key, n.Tree, n.TreeAt = heldKey(g.ID, head, tree, matchedPaths(*g, rec.Worktree, wrote)), tree, taken
			}
			if s.casHeld(h, n) {
				s.changed(changeStatus, true)
				a.reviewHeld(s)
			}
		}
	}
}

func (a *app) trustedHolds(gateID string) {
	for _, s := range a.allSessions() {
		if h := s.snapshot().Held; h != nil && h.Gate == gateID && h.Wait != "" {
			a.reviewHeld(s)
		}
	}
}

func (a *app) resumeHolds() {
	ctx := context.Background()
	for _, s := range a.allSessions() {
		h := s.snapshot().Held
		switch {
		case h == nil || h.Request != "":
		case h.Last == gateChanges && s.gateQueued():
			logError("send the queued gate findings", a.submit(s, nil))
		case h.Last == gateChanges:
			if g := a.gateByID(ctx, h.Gate); g != nil {
				a.promptChanges(s, *g, *h)
			}
		default:
			a.reviewHeld(s)
		}
	}
}

func (s *session) gateQueued() bool {
	return slices.ContainsFunc(s.queued(), func(q queuedPrompt) bool { return q.Gate })
}

func (a *app) holdSynced(s *session) {
	s.mu.Lock()
	cur, was := s.rec.Held, s.holdOf
	workspaceID := s.rec.Workspace
	s.holdOf = nil
	if cur != nil {
		c := *cur
		s.holdOf = &c
	}
	s.mu.Unlock()
	if was == nil {
		return
	}
	run := was.Run != "" && (cur == nil || cur.Run != was.Run)
	tree := was.TreeKey != "" && (cur == nil || cur.TreeKey != was.TreeKey)
	if run || tree {
		go a.endHoldRun(workspaceID, *was, run, tree)
	}
}

func (a *app) endHoldRun(workspaceID string, was heldTurn, run, tree bool) {
	ctx := context.Background()
	if run {
		logError("stop the Done review", a.graphs.StopGate(ctx, was.Run))
		a.closeGateNodes(ctx, a.gateNodes(workspaceID, was.Run))
	}
	if g := a.gateByID(ctx, was.Gate); tree && g != nil {
		a.dropGateTree(ctx, workspaceID, landReview{Repo: g.Repo, Gate: g.ID, TreeKey: was.TreeKey})
	}
}

func (a *app) closeHeldRound(s *session, h *heldTurn) {
	if h == nil || h.Run == "" {
		return
	}
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	a.closeGateRound(context.Background(), s.snapshot().Workspace, h.Run, nil)
}

func (s *session) casHeld(old, next *heldTurn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec.Held != old || s.rec.closed() {
		return false
	}
	s.rec.Held = next
	s.rec.UpdatedAt = time.Now().UTC()
	return true
}

func (a *app) swapHeld(s *session, h *heldTurn, change func(*heldTurn)) {
	n := *h
	change(&n)
	if s.casHeld(h, &n) {
		s.changed(changeStatus, true)
	}
}

func (a *app) gateByID(ctx context.Context, gateID string) *gateConfig {
	gates, err := a.gates(ctx)
	if err != nil {
		return nil
	}
	i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.ID == gateID })
	if i < 0 {
		return nil
	}
	return &gates[i]
}

func (a *app) turnVerdict(ctx context.Context, gate, key string) (landgate.Verdict, bool) {
	var v landgate.Verdict
	found, err := a.host.StorageGet(ctx, turnVerdictPath(gate, key), &v)
	return v, err == nil && found
}

func (a *app) heldKeyNow(ctx context.Context, s *session, g gateConfig) (string, string, bool) {
	rec := s.snapshot()
	tree, head, err := gitrepo.Snapshot(ctx, rec.Worktree)
	if err != nil {
		return "", "", false
	}
	wrote := a.turnWrote(ctx, s, tree)
	return heldKey(g.ID, head, tree, matchedPaths(g, rec.Worktree, wrote)), tree, true
}

func (a *app) heldGateTree(ctx context.Context, rec record, g gateConfig, since, key, tree string) (string, error) {
	var out struct {
		Path string `json:"path"`
	}
	err := a.runGateTree(ctx, rec.Workspace, map[string]any{"repo": g.Repo, "gate": g.ID, "key": key, "old": since, "tree": tree}, &out)
	if err != nil {
		return "", fmt.Errorf("prepare the gate's worktree: %w", err)
	}
	return out.Path, nil
}

func (a *app) heldPrompts(ctx context.Context, s *session, rec record, g gateConfig, h *heldTurn) (string, string) {
	since := gateBaseOf(ctx, s).since
	files := heldFiles(matchedPaths(g, rec.Worktree, a.turnWrote(ctx, s, h.Tree)))
	reply := ""
	if text := s.lastReply(); text != "" {
		reply = fmt.Sprintf(heldReplyFormat, text)
	}
	first := fmt.Sprintf(heldFirstFormat, filepath.Base(g.Repo), doneGateName(g.Wrote), rec.Title, since, h.Tree, rec.Worktree, files, reply)
	if h.Reviewed == "" || h.Reviewed == h.Tree {
		return first, ""
	}
	return first, fmt.Sprintf(heldDeltaFormat, h.Reviewed, h.Tree, since, h.Tree, rec.Worktree, files)
}

func heldFiles(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	left := heldFilesLimit
	for _, p := range paths {
		data, ok := readCapped(p, left+1)
		if !ok {
			continue
		}
		if len(data) > left {
			b.WriteString(fmt.Sprintf(heldFileFormat, p, data[:left]))
			b.WriteString(heldFilesCut)
			break
		}
		left -= len(data)
		b.WriteString(fmt.Sprintf(heldFileFormat, p, data))
	}
	return fmt.Sprintf(heldFilesFormat, b.String())
}

func readCapped(p string, limit int) ([]byte, bool) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	return data, err == nil
}

func (s *session) lastReply() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return s.replyLocked()
}
