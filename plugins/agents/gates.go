package main

import (
	"cmp"
	"context"
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
	Round     int       `json:"round,omitempty"`
	Reviewer  string    `json:"reviewer,omitempty"`
	Findings  []string  `json:"findings,omitempty"`
	Deferred  []string  `json:"deferred,omitempty"`
	At        time.Time `json:"at"`
}

type gateLanding struct {
	Lander   string    `json:"lander"`
	Title    string    `json:"title,omitempty"`
	Into     string    `json:"into"`
	Source   string    `json:"source,omitempty"`
	State    string    `json:"state"`
	Round    int       `json:"round"`
	Reviewer string    `json:"reviewer,omitempty"`
	Findings []string  `json:"findings,omitempty"`
	Since    time.Time `json:"since,omitzero"`
}

type gateRow struct {
	gateConfig
	Name     string        `json:"name"`
	State    string        `json:"state"`
	Last     *gateVerdict  `json:"last,omitempty"`
	Landings []gateLanding `json:"landings,omitempty"`
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
	var out []gateVerdict
	_, err := a.host.StorageGet(ctx, gateHistoryKey+gateID, &out)
	return out, err
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
	a.gateLogMu.Lock()
	history, err := a.readGateHistory(ctx, v.Gate)
	if err == nil {
		history = slices.Insert(history, 0, v)
		err = a.host.StorageSet(ctx, gateHistoryKey+v.Gate, history[:min(len(history), gateHistoryKeep)])
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
	for _, st := range rec.LandReviews {
		if !a.gateLive(st.Gate) {
			continue
		}
		state := st.Last
		if _, ok := a.reviewing(rec.ID, st); ok {
			state = gateStateReview
		}
		if st.Request != "" {
			state = gateStateNeedsMe
		}
		if gateRank[state] > gateRank[best] {
			best = state
		}
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
		for _, st := range rec.LandReviews {
			if st.Gate != gateID {
				continue
			}
			l := gateLanding{Lander: rec.ID, Title: rec.Title, Into: st.Into, Source: st.Source, Round: st.Rounds, Findings: st.Findings, Since: st.Since}
			if r, ok := a.reviewing(rec.ID, st); ok && st.Request == "" {
				l.State, l.Reviewer, l.Round, l.Since = gateStateReview, r.snapshot().ID, st.Rounds+1, cmp.Or(r.turnStart(), r.snapshot().UpdatedAt)
			}
			if st.Request != "" {
				l.State, l.Reviewer = gateStateNeedsMe, st.Reviewer
			}
			if l.State != "" {
				out = append(out, l)
			}
		}
	}
	slices.SortFunc(out, func(x, y gateLanding) int { return y.Since.Compare(x.Since) })
	return out
}

func (a *app) gateRow(ctx context.Context, workspaceID string, g gateConfig) gateRow {
	row := gateRow{gateConfig: g, Name: gateName(g), State: gateStateActive, Landings: a.gateLandings(workspaceID, g.ID)}
	if history := a.gateHistory(ctx, workspaceID, g.ID); len(history) > 0 {
		row.Last = &history[0]
	}
	switch {
	case slices.ContainsFunc(row.Landings, func(l gateLanding) bool { return l.State == gateStateNeedsMe }):
		row.State = gateStateNeedsMe
	case g.Paused:
		row.State = gateStatePaused
	case len(row.Landings) > 0:
		row.State = gateStateReview
	}
	return row
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
