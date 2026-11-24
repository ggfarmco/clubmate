package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"clubmate/pkg/id"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	heldTitle          = "Done review · "
	heldFilesLimit     = 64 << 10
	heldMovesMax       = 3
	regateTries        = 3
	heldFirstFormat    = "[Clubmate Done review] Review this agent's turn before the user sees it as done (repo %s, gate %s).\nSession: %s\nThe change since its last reviewed done: `git diff %s %s` in %s.%s%s\nReport confirmed problems with file:line."
	heldFirstVerdict   = " Then call agents_land_verdict once with key %s: outcome changes only for confirmed problems, each finding as \"file:line — what is wrong\"; otherwise approved. Nits and suggestions go to deferred."
	heldDeltaFormat    = "[Clubmate Done review] The agent changed its work after your last review. Review what changed: `git diff %s %s` (the whole change is `git diff %s %s`) in %s.%s"
	heldDeltaVerdict   = " Then call agents_land_verdict again with key %s."
	heldFilesFormat    = "\nFiles the gate matched:\n%s"
	heldFileFormat     = "--- %s\n%s\n"
	heldFileBinary     = "--- %s (binary, not shown)\n"
	heldFilesCut       = "(the rest is cut at 64 KiB; read it from disk)\n"
	heldReplyFormat    = "\nThe agent's last reply:\n%s"
	heldChangesFormat  = "[Clubmate] From the gate · %s · %s · round %d of %d · changes\n%s"
	heldNotBlocking    = "\nNot blocking:\n%s"
	heldChangesTail    = "\nFix these; your next finished turn goes back to the gate."
	heldFixDeferred    = "\nFix the items that don't block too."
	heldFixAsk         = "\n" + gateFixAsk
	heldNudgeText      = "[Clubmate] Your Done review ended without a verdict. Call agents_land_verdict now with the key your review prompt names: approved, or changes with the confirmed problems as findings."
	heldReviewerText   = "[Clubmate Done reviewer] You review an agent's finished turn before the user sees it as done. You are read-only: never edit files, commit, merge, reset, check out, stash or delete anything, and never start sessions. Read the change and the files around it, run read-only commands when they help, and end every review with exactly one agents_land_verdict call with the key your review prompt names."
	heldNodeText       = "[Clubmate Done gate node] You help a Done gate review an agent's finished turn. You are read-only: never edit files, commit, merge, reset, check out, stash or delete anything, and never start sessions. Do what your node's prompt asks and put your findings in your reply; only a deciding node of the gate's graph gives the verdict, so don't call agents_land_verdict: it refuses you."
	verdictKeyFormat   = "key must be the one your latest review prompt names: %s"
	heldReviewStartLog = "start the Done review"
	byChildren         = "children-running"
	byUnchanged        = "unchanged"
	byUnmatched        = "unmatched"
)

var errHeldMoved = errors.New("the session's files changed since this review began: review the new prompt, then give the verdict again")

type heldAsk struct {
	graph  string
	name   string
	repo   string
	rounds int
}

type heldStep int

const (
	heldNothing heldStep = iota
	heldRetry
)

type holdRef struct {
	s    *session
	gate string
}

func (s *session) doneHold() holdRef {
	return holdRef{s: s}
}

func (s *session) holds() []holdRef {
	out := []holdRef{s.doneHold()}
	for _, c := range s.snapshot().Calls {
		out = append(out, holdRef{s: s, gate: c.Hold.Gate})
	}
	return out
}

func holdOfNode(s *session, gf gateFor) holdRef {
	if gf.Point == gatePointTurn {
		return s.doneHold()
	}
	return holdRef{s: s, gate: gf.Gate}
}

func (r holdRef) of(rec record) *heldTurn {
	if r.gate == "" {
		return rec.Held
	}
	if c := rec.call(r.gate); c != nil {
		return c.Hold
	}
	return nil
}

func (r holdRef) get() *heldTurn {
	return r.of(r.s.snapshot())
}

func (r holdRef) cas(old, next *heldTurn) bool {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.of(r.s.rec) != old || r.s.rec.closed() {
		return false
	}
	if r.gate == "" {
		r.s.rec.Held = next
	} else {
		if next != nil && next.Request != "" && (old == nil || old.Request != next.Request) {
			next.Request = cmp.Or(r.s.rec.callCard(r.gate), next.Request)
		}
		r.s.rec.Calls = withCallHold(r.s.rec.Calls, r.gate, next)
	}
	r.s.rec.UpdatedAt = time.Now().UTC()
	return true
}

func (r holdRef) swap(h *heldTurn, change func(*heldTurn)) {
	n := *h
	change(&n)
	if r.cas(h, &n) {
		r.s.changed(changeStatus, true)
	}
}

func (r holdRef) point() string {
	if r.gate == "" {
		return gatePointTurn
	}
	return gatePointTool
}

func (r holdRef) name(g gateConfig) string {
	if r.point() == gatePointTool {
		return toolMatchName(g)
	}
	return doneGateName(g.Wrote)
}

func (a *app) releaseHold(r holdRef, key, by string) {
	if r.point() == gatePointTool {
		a.approveCall(r, key)
		return
	}
	a.releaseHeld(r.s, key, by)
}

func (r holdRef) verdictPath(gate, key string) string {
	if r.point() == gatePointTool {
		return toolVerdictKey + gate + "/" + key
	}
	return turnVerdictPath(gate, key)
}

func (r holdRef) title() string {
	if r.point() == gatePointTool {
		return callReviewTitle
	}
	return heldTitle
}

func (a *app) holdVerdict(ctx context.Context, r holdRef, gate, key string) (landgate.Verdict, bool) {
	var v landgate.Verdict
	found, err := a.host.StorageGet(ctx, r.verdictPath(gate, key), &v)
	return v, err == nil && found
}

func (a *app) holdBase(ctx context.Context, r holdRef, h *heldTurn) string {
	if r.point() == gatePointTool {
		return h.Head
	}
	return gateBaseOf(ctx, r.s).since
}

func heldCopies(rec record) map[string]heldTurn {
	out := make(map[string]heldTurn, len(rec.Calls)+1)
	if rec.Held != nil {
		out[""] = *rec.Held
	}
	for _, c := range rec.Calls {
		out[c.Hold.Gate] = *c.Hold
	}
	return out
}

func (a *app) reviewHeld(r holdRef) {
	ctx := context.Background()
	rec := r.s.snapshot()
	h := r.of(rec)
	if h == nil || h.Last != "" || h.Request != "" || rec.closed() {
		return
	}
	if h.Unread {
		a.recheckLater(r.s, h)
		return
	}
	g := a.gateByID(ctx, h.Gate)
	if g == nil {
		return
	}
	if v, ok := a.holdVerdict(ctx, r, h.Gate, h.Key); ok {
		switch v.Outcome {
		case gateApproved:
			a.releaseHold(r, h.Key, landgate.ByReviewer)
		case gateChanges:
			a.heldChanges(r, h, *g, v)
		}
		return
	}
	graph := a.gateGraphReady(ctx, *g)
	if !graph.ok {
		if h.Wait != graph.text {
			r.swap(h, func(n *heldTurn) { n.Wait = graph.text })
		}
		a.pollTrust(r)
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
		if dir, err = a.heldGateTree(ctx, rec, *g, a.holdBase(ctx, r, h), n.TreeKey, h.Tree); err != nil {
			logError(heldReviewStartLog, err)
			a.dropHeldTree(ctx, rec.Workspace, *g, h.TreeKey, n.TreeKey)
			a.heldEnded(r, h)
			return
		}
	}
	first, delta := a.heldPrompts(ctx, r, rec, *g, h)
	a.gateMu.Lock()
	if r.get() != h {
		a.gateMu.Unlock()
		a.dropHeldTree(ctx, rec.Workspace, *g, h.TreeKey, n.TreeKey)
		return
	}
	gf := gateFor{Gate: g.ID, Point: r.point(), Key: h.Key, Lander: rec.ID, Run: h.Run}
	if reuse {
		text, verdict := first, first+fmt.Sprintf(heldFirstVerdict, h.Key)
		if delta != "" {
			text, verdict = delta, delta+fmt.Sprintf(heldDeltaVerdict, h.Key)
		}
		if err := a.graphs.PromptGate(ctx, h.Run, verdict, text); err != nil {
			a.gateMu.Unlock()
			logError("prompt the Done review", err)
			a.heldEnded(r, h)
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
			NewNode:   a.gateNode(rec.Workspace, r.title()+rec.Title, rec.Worktree, gf),
		})
		if err != nil {
			a.gateMu.Unlock()
			logError(heldReviewStartLog, err)
			a.dropHeldTree(ctx, rec.Workspace, *g, h.TreeKey, n.TreeKey)
			a.heldEnded(r, h)
			return
		}
		n.Run, n.Reviewer = run, reviewer
		a.kick(rec.Workspace)
	}
	n.Reviewed = h.Tree
	swapped := r.cas(h, &n)
	a.gateMu.Unlock()
	switch {
	case swapped:
		r.s.changed(changeStatus, true)
	case !reuse:
		go a.endHoldRun(rec.Workspace, n, true, n.TreeKey != "" && n.TreeKey != h.TreeKey)
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
	r := holdOfNode(s, gf)
	a.gateMu.Lock()
	h := r.get()
	a.gateMu.Unlock()
	if h == nil || h.Key != gf.Key || h.Run != gf.Run || h.Last != "" {
		return nil, errNoReview
	}
	g := a.gateByID(ctx, h.Gate)
	if g == nil {
		return nil, errNoReview
	}
	taken := time.Now().UTC()
	key, tree, head, ok := a.heldKeyNow(ctx, r, *g)
	ask := a.heldAskOf(ctx, g)
	a.gateMu.Lock()
	if r.get() != h {
		a.gateMu.Unlock()
		return nil, errNoReview
	}
	if _, decided := a.holdVerdict(ctx, r, h.Gate, h.Key); decided {
		a.gateMu.Unlock()
		return nil, errDecided
	}
	if ok && key != h.Key {
		n := *h
		n.Key, n.Tree, n.TreeAt, n.Moves = key, tree, taken, h.Moves+1
		if r.point() == gatePointTool {
			n.Head = head
		}
		if n.Moves >= heldMovesMax {
			n.ask(ask)
		}
		swapped := r.cas(h, &n)
		a.gateMu.Unlock()
		if swapped {
			a.callRekeyed(r, key)
			r.s.changed(changeStatus, true)
			a.joinCard(r)
			a.reviewHeld(r)
		}
		return nil, errHeldMoved
	}
	v := landgate.Verdict{Tree: h.Key, Outcome: args.Outcome, By: landgate.ByReviewer, Reviewer: rec.ID, Findings: args.Findings, Deferred: args.Deferred, Summary: args.Summary, At: time.Now().UTC()}
	if err := a.host.StorageSet(ctx, r.verdictPath(h.Gate, h.Key), v); err != nil {
		a.gateMu.Unlock()
		return nil, err
	}
	a.closeGateRound(ctx, rec.Workspace, gf.Run, node)
	if gf.Run != "" {
		a.graphs.GateOutcome(ctx, gf.Run, rec.ID, args.Outcome)
	}
	held := s.snapshot()
	a.logGateVerdict(ctx, gateVerdict{Gate: h.Gate, Workspace: held.Workspace, Outcome: args.Outcome, By: landgate.ByReviewer, Lander: held.ID, Title: held.Title, Call: callLine(held, r.gate), Round: h.Round, Reviewer: rec.ID, Findings: args.Findings, Deferred: args.Deferred, Summary: args.Summary, At: v.At})
	a.gateMu.Unlock()
	if args.Outcome == gateApproved {
		a.releaseHold(r, h.Key, landgate.ByReviewer)
		return nil, nil
	}
	a.heldChanges(r, h, *g, v)
	return nil, nil
}

func (a *app) heldChanges(r holdRef, h *heldTurn, g gateConfig, v landgate.Verdict) {
	n := *h
	n.Last, n.Findings, n.Deferred, n.Summary = gateChanges, v.Findings, v.Deferred, v.Summary
	capped := r.point() == gatePointTurn && n.Round >= g.Rounds+n.Extra
	if capped {
		n.ask(a.heldAskOf(context.Background(), &g))
	}
	if !r.cas(h, &n) {
		return
	}
	r.s.changed(changeStatus, true)
	switch {
	case r.point() == gatePointTool:
		a.decideCall(r, n.Key, true)
	case !capped:
		a.promptChanges(r, g, n)
	}
}

func (a *app) promptChanges(r holdRef, g gateConfig, h heldTurn) {
	text := fmt.Sprintf(heldChangesFormat, filepath.Base(g.Repo), r.name(g), h.Round, g.Rounds+h.Extra, bullets(h.Findings))
	if len(h.Deferred) > 0 {
		text += fmt.Sprintf(heldNotBlocking, bullets(h.Deferred))
	}
	logError("send the gate's findings", a.submit(r.s, &promptArgs{Text: text + fixRule(len(h.Deferred) > 0) + heldChangesTail, Gate: true}))
}

func fixRule(deferred bool) string {
	if deferred {
		return heldFixDeferred + heldFixAsk
	}
	return heldFixAsk
}

func (a *app) heldRunEnded(workspaceID, run string) {
	a.gateMu.Lock()
	var ended []holdRef
	for _, s := range a.workspaceSessions(workspaceID) {
		for _, r := range s.holds() {
			if h := r.get(); h != nil && h.Run == run {
				ended = append(ended, r)
			}
		}
	}
	a.gateMu.Unlock()
	for _, r := range ended {
		if h := r.get(); h != nil && h.Run == run {
			a.heldEnded(r, h)
		}
	}
}

func (a *app) heldEnded(r holdRef, h *heldTurn) {
	ctx := context.Background()
	ask := a.heldAskOf(ctx, a.gateByID(ctx, h.Gate))
	a.gateMu.Lock()
	n := *h
	n.Run, n.Reviewer = "", ""
	step := heldNothing
	_, decided := a.holdVerdict(ctx, r, h.Gate, h.Key)
	switch {
	case h.Last != "" || h.Request != "":
	case decided:
		step = heldRetry
	case !h.Retried:
		n.Retried, step = true, heldRetry
	default:
		n.ask(ask)
	}
	swapped := r.cas(h, &n)
	a.gateMu.Unlock()
	if !swapped {
		return
	}
	r.s.changed(changeStatus, true)
	a.joinCard(r)
	if step == heldRetry {
		a.reviewHeld(r)
	}
}

func (a *app) heldAskOf(ctx context.Context, g *gateConfig) heldAsk {
	out, _ := a.heldAskRead(ctx, g)
	return out
}

func (a *app) heldAskRead(ctx context.Context, g *gateConfig) (heldAsk, error) {
	if g == nil {
		return heldAsk{}, nil
	}
	out := heldAsk{graph: g.Graph, name: doneGateName(g.Wrote), repo: filepath.Base(g.Repo), rounds: g.Rounds}
	if g.Point == gatePointTool {
		out.name = toolGateName(*g)
	}
	info, err := a.graphs.GateGraphInfo(ctx, g.Repo, g.Graph)
	if err != nil {
		return out, err
	}
	out.graph = cmp.Or(info.Title, info.Name, g.Graph)
	return out, nil
}

type heldFacts struct {
	mu   sync.Mutex
	gen  int
	byID map[string]heldFact
}

type heldFact struct {
	ask   heldAsk
	repo  string
	graph string
	stamp string
}

func (f *heldFacts) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen++
	f.byID = nil
}

func (a *app) heldFactsOf(gateID string) heldAsk {
	a.heldFacts.mu.Lock()
	fact, ok := a.heldFacts.byID[gateID]
	gen := a.heldFacts.gen
	a.heldFacts.mu.Unlock()
	if ok && a.graphs.GateGraphStamp(fact.repo, fact.graph) == fact.stamp {
		return fact.ask
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	g := a.gateByID(ctx, gateID)
	if g == nil {
		return heldAsk{}
	}
	stamp := a.graphs.GateGraphStamp(g.Repo, g.Graph)
	facts, err := a.heldAskRead(ctx, g)
	if _, unread := errors.AsType[*fs.PathError](err); unread && stamp != "" {
		return facts
	}
	a.heldFacts.mu.Lock()
	defer a.heldFacts.mu.Unlock()
	if a.heldFacts.gen == gen {
		if a.heldFacts.byID == nil {
			a.heldFacts.byID = map[string]heldFact{}
		}
		a.heldFacts.byID[gateID] = heldFact{ask: facts, repo: g.Repo, graph: g.Graph, stamp: stamp}
	}
	return facts
}

func (h *heldTurn) ask(facts heldAsk) {
	h.Request, h.Asked = id.New(gateRequestPrefix), time.Now().UTC()
	h.Graph, h.Name, h.Repo, h.Of = facts.graph, facts.name, facts.repo, facts.rounds+h.Extra
}

func (s *session) dropCarriedGate(gateID string) {
	s.mu.Lock()
	drop := s.rec.HeldGate == gateID && (s.rec.Held == nil || !s.rec.Held.holding())
	if drop {
		s.rec.HeldGate = ""
	}
	s.mu.Unlock()
	if drop {
		s.changed(changeStatus, true)
	}
}

type gatePauses struct {
	mu  sync.Mutex
	gen uint64
	at  map[string]uint64
}

func (p *gatePauses) mark() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen
}

func (p *gatePauses) note(gateID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gen++
	if p.at == nil {
		p.at = map[string]uint64{}
	}
	p.at[gateID] = p.gen
}

func (p *gatePauses) since(gateID string, mark uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return gateID != "" && p.at[gateID] > mark
}

func (a *app) dropCarriedGates(gateID string) {
	a.pauses.note(gateID)
	for _, s := range a.allSessions() {
		s.dropCarriedGate(gateID)
	}
}

func (a *app) regateHolds(ctx context.Context, gates []gateConfig, gateID, by string) {
	var g *gateConfig
	if i := slices.IndexFunc(gates, func(x gateConfig) bool { return x.ID == gateID }); i >= 0 {
		g = &gates[i]
	}
	for _, s := range a.allSessions() {
		a.regateCalls(ctx, s, g, gateID, by)
		if by == byGatePaused {
			continue
		}
		for _, r := range s.holds() {
			for range regateTries {
				if !a.regateHold(ctx, r, gates, gateID, by) {
					break
				}
			}
		}
	}
}

func (a *app) regateHold(ctx context.Context, r holdRef, gates []gateConfig, gateID, by string) bool {
	if r.point() != gatePointTurn {
		return false
	}
	s := r.s
	rec := a.afkRec(s.snapshot())
	h := r.of(rec)
	switch {
	case h == nil || h.Gate != gateID:
		return false
	case h.Unread:
		return a.regateUnread(ctx, r, rec, gates, by)
	}
	taken := time.Now().UTC()
	tree, head, err := gitrepo.Snapshot(ctx, rec.Worktree)
	wrote := a.turnWrote(ctx, s, tree)
	g := doneGate(stillHolding(gates, h), primaryOf(ctx, rec.Worktree), rec, wrote)
	switch {
	case err != nil && by == byGateChanged:
	case g == nil:
		a.releaseHold(r, h.Key, by)
	case g.ID == gateID:
	default:
		n := &heldTurn{Gate: g.ID, Key: h.Key, Tree: h.Tree, Round: 1, Since: h.Since, TreeAt: h.TreeAt}
		if err == nil {
			n.Key, n.Tree, n.TreeAt = heldKey(g.ID, head, tree, matchedPaths(*g, rec.Worktree, wrote)), tree, taken
		}
		if !r.cas(h, n) {
			return true
		}
		s.changed(changeStatus, true)
		if !s.settling() {
			a.reviewHeld(r)
		}
	}
	return false
}

func (a *app) regateUnread(ctx context.Context, r holdRef, rec record, gates []gateConfig, by string) bool {
	s := r.s
	h := r.of(rec)
	g := unreadGate(ctx, stillHolding(gates, h), rec, h)
	switch {
	case g == nil:
		a.releaseHold(r, h.Key, by)
		return false
	case g.ID == h.Gate:
		return false
	}
	n := *h
	n.Gate = g.ID
	if _, err := primaryRead(ctx, rec.Worktree); err != nil {
		n.ReadRepo, n.Reason = readRepoName(ctx, rec.Worktree), causeOf(err)
	}
	if n.Request != "" {
		facts := a.heldAskOf(ctx, g)
		n.Graph, n.Name, n.Repo = facts.graph, facts.name, facts.repo
	}
	if !r.cas(h, &n) {
		return true
	}
	s.changed(changeStatus, true)
	if !s.settling() {
		a.reviewHeld(r)
	}
	return false
}

func (a *app) trustedHolds(gateID string) {
	for _, s := range a.allSessions() {
		for _, r := range s.holds() {
			if h := r.get(); h != nil && h.Gate == gateID && h.Wait != "" {
				a.reviewHeld(r)
			}
		}
	}
}

func (a *app) resumeHolds() {
	ctx := context.Background()
	for run, workspaceID := range a.unheldRuns() {
		a.endHoldRun(workspaceID, heldTurn{Run: run}, true, false)
	}
	for _, s := range a.allSessions() {
		a.resetStale(s, time.Now())
		queued := false
		for _, r := range s.holds() {
			h := r.get()
			switch {
			case h == nil || h.Request != "":
			case r.point() == gatePointTool:
				if h.Last == "" {
					a.reviewHeld(r)
				}
			case h.Last == gateChanges && s.gateQueued():
				queued = true
			case h.Last == gateChanges:
				if g := a.gateByID(ctx, h.Gate); g != nil {
					a.promptChanges(r, *g, *h)
				}
			default:
				a.reviewHeld(r)
			}
		}
		if queued {
			logError("send the queued gate findings", a.submit(s, nil))
		}
		a.tellFindings(s, false)
	}
}

func (a *app) unheldRuns() map[string]string {
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	out := map[string]string{}
	for _, s := range a.allSessions() {
		rec := s.snapshot()
		gf := rec.GateFor
		if gf == nil || gf.Run == "" || rec.closed() || (gf.Point != gatePointTurn && gf.Point != gatePointTool) {
			continue
		}
		if lander := a.session(gf.Lander); lander != nil {
			if h := holdOfNode(lander, *gf).get(); h != nil && h.Run == gf.Run {
				continue
			}
		}
		out[gf.Run] = rec.Workspace
	}
	return out
}

func (s *session) settling() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busyLocked() || len(s.perms) > 0
}

func (s *session) gateQueued() bool {
	return slices.ContainsFunc(s.queued(), func(q queuedPrompt) bool { return q.Gate })
}

func (a *app) holdSynced(s *session) {
	s.mu.Lock()
	was, now := s.holdOf, heldCopies(s.rec)
	workspaceID := s.rec.Workspace
	s.holdOf = now
	s.mu.Unlock()
	for gate, w := range was {
		cur, ok := now[gate]
		run := w.Run != "" && (!ok || cur.Run != w.Run)
		tree := w.TreeKey != "" && (!ok || cur.TreeKey != w.TreeKey)
		if run || tree {
			go a.endHoldRun(workspaceID, w, run, tree)
		}
		if gate != "" && (!ok || cur.Key != w.Key) {
			a.dropInput(w.Key)
		}
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

func (a *app) closeHeldRound(r holdRef, h *heldTurn) {
	if h == nil || h.Run == "" {
		return
	}
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	a.closeGateRound(context.Background(), r.s.snapshot().Workspace, h.Run, nil)
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
	return a.holdVerdict(ctx, holdRef{}, gate, key)
}

func (a *app) heldKeyNow(ctx context.Context, r holdRef, g gateConfig) (string, string, string, bool) {
	if r.point() == gatePointTool {
		return a.callKeyNow(ctx, r, g)
	}
	s := r.s
	rec := s.snapshot()
	tree, head, err := gitrepo.Snapshot(ctx, rec.Worktree)
	if err != nil {
		return "", "", "", false
	}
	wrote := a.turnWrote(ctx, s, tree)
	return heldKey(g.ID, head, tree, matchedPaths(g, rec.Worktree, wrote)), tree, head, true
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

func (a *app) heldPrompts(ctx context.Context, r holdRef, rec record, g gateConfig, h *heldTurn) (string, string) {
	if r.point() == gatePointTool {
		return a.callPrompts(ctx, r, rec, g, h)
	}
	s := r.s
	since := gateBaseOf(ctx, s).since
	files := heldFiles(matchedPaths(g, rec.Worktree, a.turnWrote(ctx, s, h.Tree)))
	reply := ""
	if text := s.lastReply(); text != "" {
		reply = fmt.Sprintf(heldReplyFormat, text)
	}
	first := fmt.Sprintf(heldFirstFormat, filepath.Base(g.Repo), r.name(g), rec.Title, since, h.Tree, rec.Worktree, files, reply)
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
		if binaryText(data, len(data) > left) {
			b.WriteString(fmt.Sprintf(heldFileBinary, p))
			continue
		}
		if len(data) > left {
			end := left
			for end > 0 && !utf8.RuneStart(data[end]) {
				end--
			}
			b.WriteString(fmt.Sprintf(heldFileFormat, p, data[:end]))
			b.WriteString(heldFilesCut)
			break
		}
		left -= len(data)
		b.WriteString(fmt.Sprintf(heldFileFormat, p, data))
	}
	return fmt.Sprintf(heldFilesFormat, b.String())
}

func binaryText(data []byte, cut bool) bool {
	for len(data) > 0 && (!cut || utf8.FullRune(data)) {
		r, n := utf8.DecodeRune(data)
		if r == 0 || r == utf8.RuneError && n == 1 {
			return true
		}
		data = data[n:]
	}
	return false
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
