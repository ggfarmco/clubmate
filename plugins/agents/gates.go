package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	gateHistoryKey   = "land-history/"
	gateHistoryKeep  = 100
	gateVerdictsMax  = 50
	gateStateActive  = "active"
	gateStatePaused  = "paused"
	gateStateReview  = "reviewing"
	gateStateNeedsMe = "needs-you"
	gateStateTrust   = "trust"
	gateNameLanding  = "Landing "
	gateNameFormat   = gateNameLanding + "into %s"
	gateNameAny      = "any branch"
	gateNameBase     = "the parent's branch"
	gateNameDefault  = "the default branch"
	gateUngated      = "ungated"
)

var gateRank = map[string]int{gateApproved: 1, gateChanges: 2, gateStateReview: 3, gateStateNeedsMe: 4}

type gateVerdict struct {
	Gate      string    `json:"gate"`
	Workspace string    `json:"workspace,omitempty"`
	Outcome   string    `json:"outcome"`
	By        string    `json:"by"`
	Lander    string    `json:"lander,omitempty"`
	Title     string    `json:"title,omitempty"`
	Into      string    `json:"into"`
	Source    string    `json:"source,omitempty"`
	Call      string    `json:"call,omitempty"`
	Round     int       `json:"round,omitempty"`
	Reviewer  string    `json:"reviewer,omitempty"`
	Findings  []string  `json:"findings,omitempty"`
	Deferred  []string  `json:"deferred,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	At        time.Time `json:"at"`
}

type gateLanding struct {
	Lander   string    `json:"lander"`
	Title    string    `json:"title,omitempty"`
	Call     string    `json:"call,omitempty"`
	Caption  string    `json:"caption,omitempty"`
	Into     string    `json:"into"`
	Source   string    `json:"source,omitempty"`
	State    string    `json:"state"`
	Round    int       `json:"round"`
	Reviewer string    `json:"reviewer,omitempty"`
	Findings []string  `json:"findings,omitempty"`
	Since    time.Time `json:"since,omitzero"`
	Ask      string    `json:"ask,omitempty"`
	Run      string    `json:"run,omitempty"`
	Wait     string    `json:"wait,omitempty"`
	Trust    bool      `json:"trust,omitempty"`
}

type gateRow struct {
	gateConfig
	Name       string        `json:"name"`
	Words      string        `json:"words,omitempty"`
	State      string        `json:"state"`
	Hash       string        `json:"hash,omitempty"`
	Unreadable bool          `json:"unreadable,omitempty"`
	Last       *gateVerdict  `json:"last,omitempty"`
	Landings   []gateLanding `json:"landings,omitempty"`
}

type gateVerdictsArgs struct {
	ID    string `json:"id"`
	Limit int    `json:"limit"`
}

type gateVerdictsResult struct {
	Landings []gateLanding `json:"landings"`
	Verdicts []gateVerdict `json:"verdicts"`
}

type gateRepo struct {
	Path    string
	Primary string
	Branch  string
}

func (a *app) gateVerdictsCommand(ctx context.Context, cmd pluginsdk.CommandRun, args gateVerdictsArgs) (any, error) {
	gates, _, err := a.workspaceGates(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(gates, func(g gateConfig) bool { return g.ID == args.ID }) {
		return nil, errGateUnknown
	}
	history := a.gateHistory(ctx, cmd.WorkspaceID, args.ID)
	limit := min(cmp.Or(args.Limit, gateVerdictsMax), gateHistoryKeep)
	landings := a.gateLandings(cmd.WorkspaceID, args.ID)
	if landings == nil {
		landings = []gateLanding{}
	}
	return gateVerdictsResult{Landings: landings, Verdicts: history[:min(len(history), max(limit, 0))]}, nil
}

func (a *app) readGateHistory(ctx context.Context, gateID string) ([]gateVerdict, error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if out, ok := a.historyCache[gateID]; ok {
		return out, nil
	}
	var out []gateVerdict
	if _, err := a.host.StorageGet(ctx, gateHistoryKey+gateID, &out); err != nil {
		return nil, err
	}
	if a.historyCache == nil {
		a.historyCache = map[string][]gateVerdict{}
	}
	a.historyCache[gateID] = out
	return out, nil
}

func (a *app) cacheGateHistory(gateID string, history []gateVerdict) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if history == nil {
		delete(a.historyCache, gateID)
		return
	}
	if a.historyCache == nil {
		a.historyCache = map[string][]gateVerdict{}
	}
	a.historyCache[gateID] = history
}

func (a *app) forgetGateHistory(ctx context.Context, gateID string) error {
	err := a.host.StorageDelete(ctx, gateHistoryKey+gateID)
	a.cacheGateHistory(gateID, nil)
	return err
}

func (a *app) gateHistory(ctx context.Context, workspaceID, gateID string) []gateVerdict {
	all, err := a.readGateHistory(ctx, gateID)
	logError("read land gate history", err)
	out := []gateVerdict{}
	for _, v := range all {
		if v.Workspace == workspaceID {
			out = append(out, v)
		}
	}
	return out
}

func (a *app) logGateVerdict(ctx context.Context, v gateVerdict) {
	if v.Gate == "" {
		return
	}
	if s := a.session(v.Lander); s != nil {
		v.Workspace = s.snapshot().Workspace
	}
	a.journalVerdict(v)
	a.gateLogMu.Lock()
	history, err := a.readGateHistory(ctx, v.Gate)
	if err == nil {
		history = append([]gateVerdict{v}, history[:min(len(history), gateHistoryKeep-1)]...)
		if err = a.host.StorageSet(ctx, gateHistoryKey+v.Gate, history); err == nil {
			a.cacheGateHistory(v.Gate, history)
		}
	}
	a.gateLogMu.Unlock()
	logError("log land gate verdict", err)
	a.gatesChanged()
}

func (a *app) workspaceGates(ctx context.Context, workspaceID string) ([]gateConfig, []gateRepo, error) {
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	all, err := a.gates(ctx)
	if err != nil || len(all) == 0 {
		return nil, nil, err
	}
	repos := a.workspaceRepos(ctx, w)
	var out []gateConfig
	for _, g := range all {
		if slices.ContainsFunc(repos, func(r gateRepo) bool { return r.Primary == g.Repo }) {
			out = append(out, g)
		}
	}
	return out, repos, nil
}

func (a *app) titleOf(sid string) string {
	if s := a.session(sid); s != nil {
		return s.snapshot().Title
	}
	return ""
}

func (a *app) gatesChanged() {
	a.uiMu.Lock()
	ids := make([]string, 0, len(a.present))
	for id, here := range a.present {
		if here {
			ids = append(ids, id)
		}
	}
	a.uiMu.Unlock()
	for _, id := range ids {
		a.kick(id)
	}
}

func (a *app) reviewing(lander string, st landReview) (*session, bool) {
	r := a.session(st.Reviewer)
	gf := r.runningGate()
	if gf == nil || r.snapshot().closed() || gf.Lander != lander || gf.Into != st.Into || gf.Source != st.Source {
		return nil, false
	}
	return r, true
}

func (a *app) gateState(rec record) string {
	if rec.closed() {
		return ""
	}
	best := ""
	asks := a.gateAsks(rec)
	for _, st := range rec.LandReviews {
		if !a.landingShown(st) {
			continue
		}
		state := st.Last
		if _, ok := a.reviewing(rec.ID, st); ok {
			state = gateStateReview
		}
		if st.Request != "" || asks[st.Run] != nil {
			state = gateStateNeedsMe
		}
		if gateRank[state] > gateRank[best] {
			best = state
		}
	}
	if rec.heldWaits() || len(rec.callRequests()) > 0 || asks[heldRun(rec)] != nil {
		best = gateStateNeedsMe
	}
	return best
}

func (a *app) gateLandings(workspaceID, gateID string) []gateLanding {
	var out []gateLanding
	for _, s := range a.workspaceSessions(workspaceID) {
		rec := s.snapshot()
		if rec.closed() {
			continue
		}
		asks := a.gateAsks(rec)
		for _, st := range rec.LandReviews {
			if st.Gate != gateID {
				continue
			}
			l := gateLanding{Lander: rec.ID, Title: rec.Title, Into: st.Into, Source: st.Source, Round: st.Rounds, Findings: st.Findings, Since: st.Since}
			if r, ok := a.reviewing(rec.ID, st); ok && st.Request == "" {
				l.State, l.Reviewer, l.Round, l.Since = gateStateReview, r.snapshot().ID, st.Rounds+1, cmp.Or(r.turnStart(), r.snapshot().UpdatedAt)
			}
			if n := asks[st.Run]; n != nil {
				l.State, l.Reviewer, l.Round, l.Since, l.Ask, l.Run = gateStateNeedsMe, st.Reviewer, st.Rounds+1, n.Since, n.Text, n.Run
			}
			if st.Request != "" {
				l.State, l.Reviewer = gateStateNeedsMe, st.Reviewer
			}
			if l.State != "" {
				out = append(out, l)
			}
		}
		if h := rec.Held; h != nil && h.Gate == gateID {
			if l, ok := a.heldLanding(rec, asks); ok {
				out = append(out, l)
			}
		}
		if l, ok := a.callGateLanding(rec, gateID); ok {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(x, y gateLanding) int { return y.Since.Compare(x.Since) })
	return out
}

func (a *app) heldLanding(rec record, asks map[string]*runNeed) (gateLanding, bool) {
	h := rec.Held
	l := gateLanding{Lander: rec.ID, Title: rec.Title, State: gateStateNeedsMe, Round: max(h.Round, 1), Findings: h.Findings, Since: cmp.Or(h.Asked, h.Since)}
	if n := asks[heldRun(rec)]; n != nil {
		l.Since, l.Ask, l.Run = n.Since, n.Text, n.Run
		return l, true
	}
	if h.Request != "" {
		return l, true
	}
	if !a.heldIdle(rec) {
		return gateLanding{}, false
	}
	l.State, l.Findings = gateStateReview, nil
	if l.Wait = heldWaitWords(h); l.Wait != "" {
		l.Trust = holdWaitsTrust(h)
		return l, true
	}
	if r := a.heldReviewer(rec); r != nil {
		l.Reviewer, l.Since = r.ID, r.UpdatedAt
		if s := a.session(r.ID); s != nil {
			l.Since = cmp.Or(s.turnStart(), r.UpdatedAt)
		}
	}
	return l, true
}

func (a *app) callGateLanding(rec record, gateID string) (gateLanding, bool) {
	c := rec.call(gateID)
	if c == nil || c.Hold == nil || c.Call == "" && c.Hold.Request == "" {
		return gateLanding{}, false
	}
	h := c.Hold
	l := gateLanding{Lander: rec.ID, Title: rec.Title, Call: c.Input, Caption: c.Caption, State: gateStateReview, Round: max(h.Round, 1), Since: h.Since}
	if h.Request != "" {
		l.State, l.Reviewer, l.Findings, l.Since = gateStateNeedsMe, h.Reviewer, h.Findings, cmp.Or(h.Asked, h.Since)
		return l, true
	}
	if l.Wait = heldWaitWords(h); l.Wait != "" {
		l.Trust = holdWaitsTrust(h)
		return l, true
	}
	if n, ok := a.runNeeds.byRun(rec.Workspace)[h.Run]; ok && h.Run != "" {
		l.State, l.Ask, l.Run, l.Since = gateStateNeedsMe, n.Text, n.Run, n.Since
		return l, true
	}
	for _, s := range a.workspaceSessions(rec.Workspace) {
		r := s.snapshot()
		if gf := r.GateFor; gf != nil && gf.Point == gatePointTool && gf.Gate == gateID && gf.Lander == rec.ID && !gf.Done && gf.Verdict && !r.closed() && holdsNode(*h, *gf) {
			l.Reviewer = r.ID
			break
		}
	}
	return l, true
}

func (a *app) heldReviewer(rec record) *record {
	h := rec.Held
	if h == nil {
		return nil
	}
	for _, s := range a.workspaceSessions(rec.Workspace) {
		r := s.snapshot()
		if gf := r.GateFor; gf != nil && gf.Point == gatePointTurn && gf.Lander == rec.ID && !gf.Done && gf.Verdict && !r.closed() && holdsNode(*h, *gf) {
			return &r
		}
	}
	return nil
}

func (a *app) gateRunLanding(workspaceID, run string) (gate, pick string, ok bool) {
	for _, s := range a.workspaceSessions(workspaceID) {
		rec := s.snapshot()
		n := a.gateAsks(rec)[run]
		if n == nil {
			continue
		}
		if heldRun(rec) == run {
			return rec.Held.Gate, gateRowID(n.Since, rec.ID), true
		}
		if i := slices.IndexFunc(rec.LandReviews, func(st landReview) bool { return st.Run == run }); i >= 0 {
			return rec.LandReviews[i].Gate, gateRowID(n.Since, rec.ID), true
		}
	}
	return "", "", false
}

func (a *app) gateRow(ctx context.Context, workspaceID string, g gateConfig) gateRow {
	row := gateRow{gateConfig: g, Name: a.gateMatchName(ctx, g), State: gateStateActive, Landings: a.gateLandings(workspaceID, g.ID)}
	if g.Point == gatePointTool {
		row.Words = toolMatchWords(g)
	}
	if history := a.gateHistory(ctx, workspaceID, g.ID); len(history) > 0 {
		row.Last = &history[0]
	}
	trust := a.gateGraphTrust(ctx, g)
	row.Hash = trust.hash
	switch {
	case !trust.ok:
		row.State, row.Unreadable = gateStateTrust, trust.hash == ""
	case slices.ContainsFunc(row.Landings, func(l gateLanding) bool { return l.State == gateStateNeedsMe }):
		row.State = gateStateNeedsMe
	case g.Paused:
		row.State = gateStatePaused
	case len(row.Landings) > 0:
		row.State = gateStateReview
	}
	return row
}

func (a *app) gateMatchName(ctx context.Context, g gateConfig) string {
	switch g.Point {
	case gatePointTurn:
		return doneGateName(g.Wrote)
	case gatePointTool:
		return toolGateName(g)
	}
	return gateNameOf(g.Into, a.gateRepo(ctx, g.Repo).Branch)
}

func gateNameOf(into, branch string) string {
	switch into {
	case gateIntoAny:
		into = gateNameAny
	case gateIntoBase:
		into = gateNameBase
	case gateIntoDefault:
		into = cmp.Or(branch, gateNameDefault)
	}
	return fmt.Sprintf(gateNameFormat, into)
}

func (a *app) gateRepo(ctx context.Context, path string) gateRepo {
	if v, ok := a.gateRepos.Load(path); ok {
		return v.(gateRepo)
	}
	r := gateRepo{Path: path, Primary: primaryOf(ctx, path)}
	if r.Primary == "" {
		return r
	}
	r.Branch, _ = gitrepo.DefaultBranch(ctx, r.Primary)
	a.gateRepos.Store(path, r)
	return r
}

func (a *app) workspaceRepos(ctx context.Context, w pluginsdk.Workspace) []gateRepo {
	var out []gateRepo
	for _, root := range w.Roots() {
		r := a.gateRepo(ctx, root)
		if r.Primary != "" && !slices.ContainsFunc(out, func(o gateRepo) bool { return o.Primary == r.Primary }) {
			out = append(out, r)
		}
	}
	return out
}
