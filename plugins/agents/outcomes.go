package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	outcomesDirName = "outcomes"
	outcomesKeep    = 90 * 24 * time.Hour
	outcomeVerdict  = "verdict"
	outcomeLanding  = "landing"
	outcomeSession  = "session"
	outcomesMonth   = "2006-01"
)

var (
	expLine        = regexp.MustCompile(`^[ \t]*\[exp((?:[ \t]+[a-z]+=[^\s\]]*)+)[ \t]*\][ \t]*$`)
	errOutcomeTime = errors.New("since is an RFC 3339 time or a YYYY-MM-DD date")
)

type expTag struct {
	Kind string `json:"kind,omitempty"`
	Size string `json:"size,omitempty"`
	Arm  string `json:"arm,omitempty"`
}

type outcomeEntry struct {
	Type      string          `json:"type"`
	At        time.Time       `json:"at"`
	Workspace string          `json:"workspace,omitempty"`
	Verdict   *gateVerdict    `json:"verdict,omitempty"`
	Landing   *landingOutcome `json:"landing,omitempty"`
	Session   *sessionOutcome `json:"session,omitempty"`
}

type landingOutcome struct {
	Lander   string             `json:"lander"`
	Builder  string             `json:"builder,omitempty"`
	Fixers   []string           `json:"fixers,omitempty"`
	Into     string             `json:"into"`
	Source   string             `json:"source,omitempty"`
	Worktree string             `json:"worktree,omitempty"`
	Squash   string             `json:"squash"`
	Costs    map[string]float64 `json:"costs,omitempty"`
}

type sessionOutcome struct {
	Session     string    `json:"session"`
	Workspace   string    `json:"workspace"`
	Title       string    `json:"title"`
	Parent      string    `json:"parent,omitempty"`
	Worktree    string    `json:"worktree,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Size        string    `json:"size,omitempty"`
	Arm         string    `json:"arm,omitempty"`
	Model       string    `json:"model,omitempty"`
	Effort      string    `json:"effort,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	LastTurnEnd time.Time `json:"lastTurnEnd,omitzero"`
	Turns       int       `json:"turns"`
	Cost        float64   `json:"cost"`
	GateFor     *gateFor  `json:"gateFor,omitempty"`
	Night       bool      `json:"night,omitempty"`
	Closed      bool      `json:"closed,omitempty"`
}

type landingVerdict struct {
	Round    int       `json:"round,omitempty"`
	Outcome  string    `json:"outcome"`
	By       string    `json:"by"`
	Reviewer string    `json:"reviewer,omitempty"`
	At       time.Time `json:"at"`
}

type landingRow struct {
	landingOutcome
	At        time.Time        `json:"at"`
	Gate      string           `json:"gate,omitempty"`
	Rounds    int              `json:"rounds"`
	Changes   int              `json:"changes"`
	Verdicts  []landingVerdict `json:"verdicts"`
	Reviewers []string         `json:"reviewers"`
}

type outcomeRow struct {
	sessionOutcome
	WallSeconds   int64       `json:"wallSeconds"`
	Landing       *landingRow `json:"landing,omitempty"`
	ReviewersCost float64     `json:"reviewersCost"`
	FixersCost    float64     `json:"fixersCost"`
	CostToLanding float64     `json:"costToLanding,omitempty"`
}

type outcomesArgs struct {
	Since     string `json:"since"`
	Workspace string `json:"workspace"`
}

type outcomesResult struct {
	Outcomes []outcomeRow `json:"outcomes"`
}

type outcomeJournal struct {
	sessions map[string]sessionOutcome
	landings []outcomeEntry
	verdicts []gateVerdict
	costs    map[string]float64
}

func parseExp(prompt string) *expTag {
	first, _, _ := strings.Cut(prompt, "\n")
	m := expLine.FindStringSubmatch(strings.TrimSuffix(first, "\r"))
	if m == nil {
		return nil
	}
	var tag expTag
	for kv := range strings.FieldsSeq(m[1]) {
		key, value, _ := strings.Cut(kv, "=")
		switch key {
		case "kind":
			tag.Kind = value
		case "size":
			tag.Size = value
		case "arm":
			tag.Arm = value
		}
	}
	return &tag
}

func (a *app) outcomesDir() string {
	return filepath.Join(a.stateDir, outcomesDirName)
}

func (a *app) appendOutcome(e outcomeEntry) {
	if a.stateDir == "" {
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		logError("outcome entry", err)
		return
	}
	a.outcomesMu.Lock()
	defer a.outcomesMu.Unlock()
	dir := a.outcomesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logError("outcomes dir", err)
		return
	}
	path := filepath.Join(dir, e.At.UTC().Format(outcomesMonth)+".jsonl")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		pruneOutcomes(dir, e.At)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logError("outcomes open", err)
		return
	}
	_, err = f.Write(append(line, '\n'))
	logError("outcomes write", errors.Join(err, f.Close()))
}

func pruneOutcomes(dir string, now time.Time) {
	names, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	for _, name := range names {
		month, err := time.Parse(outcomesMonth, strings.TrimSuffix(filepath.Base(name), ".jsonl"))
		if err == nil && now.Sub(month.AddDate(0, 1, 0)) > outcomesKeep {
			logError("prune outcomes", os.Remove(name))
		}
	}
}

func (a *app) readOutcomes() []outcomeEntry {
	a.outcomesMu.Lock()
	defer a.outcomesMu.Unlock()
	names, _ := filepath.Glob(filepath.Join(a.outcomesDir(), "*.jsonl"))
	slices.Sort(names)
	var out []outcomeEntry
	for _, name := range names {
		f, err := os.Open(name)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 4<<20)
		for sc.Scan() {
			var e outcomeEntry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				out = append(out, e)
			}
		}
		f.Close()
	}
	slices.SortStableFunc(out, func(x, y outcomeEntry) int { return x.At.Compare(y.At) })
	return out
}

func (a *app) journal() outcomeJournal {
	j := outcomeJournal{sessions: map[string]sessionOutcome{}, costs: map[string]float64{}}
	for _, e := range a.readOutcomes() {
		switch {
		case e.Type == outcomeSession && e.Session != nil:
			j.sessions[e.Session.Session] = *e.Session
		case e.Type == outcomeVerdict && e.Verdict != nil:
			j.verdicts = append(j.verdicts, *e.Verdict)
		case e.Type == outcomeLanding && e.Landing != nil:
			j.landings = append(j.landings, e)
			for id, c := range e.Landing.Costs {
				j.costs[id] = c
			}
		}
	}
	return j
}

func (a *app) journalVerdict(v gateVerdict) {
	a.appendOutcome(outcomeEntry{Type: outcomeVerdict, At: v.At, Workspace: v.Workspace, Verdict: &v})
}

func (a *app) journalSession(s *session) {
	o := s.outcome()
	key, err := json.Marshal(o)
	if err != nil {
		return
	}
	a.outcomesMu.Lock()
	if a.journaled == nil {
		a.journaled = map[string]string{}
	}
	same := a.journaled[o.Session] == string(key)
	a.journaled[o.Session] = string(key)
	a.outcomesMu.Unlock()
	if !same {
		a.appendOutcome(outcomeEntry{Type: outcomeSession, At: time.Now().UTC(), Workspace: o.Workspace, Session: &o})
	}
}

func (a *app) journalLanding(workspaceID string, args landGateArgs) {
	l := landingOutcome{Lander: args.Session, Into: args.Into, Source: args.Source, Worktree: args.Worktree, Squash: args.Commit, Costs: map[string]float64{}}
	builder, fixers := a.builderOf(args)
	if builder != nil {
		l.Builder = builder.snapshot().ID
		l.Costs[l.Builder] = builder.outcome().Cost
	}
	for _, s := range fixers {
		id := s.snapshot().ID
		l.Fixers = append(l.Fixers, id)
		l.Costs[id] = s.outcome().Cost
	}
	for _, s := range a.allSessions() {
		if rec := s.snapshot(); sameLanding(rec.GateFor, l) {
			l.Costs[rec.ID] = s.outcome().Cost
		}
	}
	a.appendOutcome(outcomeEntry{Type: outcomeLanding, At: time.Now().UTC(), Workspace: workspaceID, Landing: &l})
}

func sameLanding(gf *gateFor, l landingOutcome) bool {
	return gf != nil && gf.Lander == l.Lander && gf.Into == l.Into && gf.Source == l.Source
}

func (a *app) builderOf(args landGateArgs) (*session, []*session) {
	if args.Worktree == "" {
		return nil, nil
	}
	dir := gitrepo.Canonical(args.Worktree)
	if lander := a.session(args.Session); lander != nil && (args.Target != "" && gitrepo.Canonical(args.Target) == dir || gitrepo.Canonical(lander.snapshot().Worktree) == dir) {
		return lander, nil
	}
	var on []*session
	for _, s := range a.allSessions() {
		rec := s.snapshot()
		if rec.isAside() || rec.GateFor != nil || rec.Worktree == "" || gitrepo.Canonical(rec.Worktree) != dir {
			continue
		}
		on = append(on, s)
	}
	if len(on) == 0 {
		return nil, nil
	}
	slices.SortFunc(on, func(x, y *session) int { return x.snapshot().CreatedAt.Compare(y.snapshot().CreatedAt) })
	ids := map[string]bool{}
	for _, s := range on {
		ids[s.snapshot().ID] = true
	}
	builder := on[len(on)-1]
	for _, s := range slices.Backward(on) {
		if !ids[s.snapshot().ParentSession] {
			builder = s
			break
		}
	}
	seen := map[*session]bool{builder: true}
	var fixers []*session
	for _, s := range slices.Concat(on, a.subtree(builder)) {
		rec := s.snapshot()
		if seen[s] || rec.ID == args.Session || rec.isAside() || rec.GateFor != nil {
			continue
		}
		seen[s] = true
		fixers = append(fixers, s)
	}
	return builder, fixers
}

func (s *session) outcome() sessionOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	rec := s.rec
	o := sessionOutcome{
		Session:     rec.ID,
		Workspace:   rec.Workspace,
		Title:       rec.Title,
		Parent:      rec.ParentSession,
		Worktree:    rec.Worktree,
		Model:       cmp.Or(configCurrent(s.configs, acp.CategoryModel), rec.StartModel),
		Effort:      cmp.Or(configCurrent(s.configs, acp.CategoryThoughtLevel), rec.StartEffort),
		CreatedAt:   rec.CreatedAt,
		LastTurnEnd: s.lastEnd,
		Turns:       s.turn,
		Cost:        s.costTotal,
		GateFor:     rec.GateFor,
		Night:       rec.Night != "",
		Closed:      rec.closed(),
	}
	if rec.Exp != nil {
		o.Kind, o.Size, o.Arm = rec.Exp.Kind, rec.Exp.Size, rec.Exp.Arm
	}
	return o
}

func configCurrent(configs []acp.SessionConfigOption, category string) string {
	i := slices.IndexFunc(configs, func(o acp.SessionConfigOption) bool { return o.Category == category })
	if i < 0 {
		return ""
	}
	var v string
	if json.Unmarshal(configs[i].CurrentValue, &v) != nil {
		return ""
	}
	return v
}

func (t *transcriptState) addCost(amount float64) {
	if amount >= t.costLast {
		t.costTotal += amount - t.costLast
	} else {
		t.costTotal += amount
	}
	t.costLast = amount
}

func parseSince(since string) (time.Time, error) {
	if since == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.DateOnly, since); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w: %q", errOutcomeTime, since)
}

func (a *app) outcomesCommand(_ context.Context, cmd pluginsdk.CommandRun, args outcomesArgs) (any, error) {
	since, err := parseSince(args.Since)
	if err != nil {
		return nil, err
	}
	agent := a.bySession(cmd)
	if agent {
		args.Workspace = cmd.WorkspaceID
	}
	j := a.journal()
	live := map[string]*session{}
	recs := map[string]record{}
	for _, s := range a.allSessions() {
		rec := s.snapshot()
		if rec.isAside() || agent && rec.Night != "" && rec.ID != cmd.Caller {
			continue
		}
		live[rec.ID], recs[rec.ID] = s, rec
	}
	if agent {
		maps.DeleteFunc(j.sessions, func(id string, o sessionOutcome) bool { return o.Night && id != cmd.Caller })
	}
	facts := map[string]sessionOutcome{}
	factsOf := func(id string) (sessionOutcome, bool) {
		if o, ok := facts[id]; ok {
			return o, true
		}
		o, ok := j.sessions[id]
		if s := live[id]; s != nil {
			o, ok = s.outcome(), true
		}
		if ok {
			facts[id] = o
		}
		return o, ok
	}
	costOf := func(id string) float64 {
		if o, ok := factsOf(id); ok && (o.Cost > 0 || j.costs[id] == 0) {
			return o.Cost
		}
		return j.costs[id]
	}
	match := func(workspace string, created time.Time) bool {
		return (args.Workspace == "" || workspace == args.Workspace) && !created.Before(since)
	}
	builders := map[string]outcomeEntry{}
	for _, e := range j.landings {
		if e.Landing.Builder != "" {
			builders[e.Landing.Builder] = e
		}
	}
	ids := map[string]bool{}
	for id, rec := range recs {
		if rec.ParentSession != "" && rec.GateFor == nil && match(rec.Workspace, rec.CreatedAt) {
			ids[id] = true
		}
	}
	for id, o := range j.sessions {
		if o.Parent != "" && o.GateFor == nil && match(o.Workspace, o.CreatedAt) {
			ids[id] = true
		}
	}
	for id := range builders {
		if o, ok := factsOf(id); ok && match(o.Workspace, o.CreatedAt) {
			ids[id] = true
		}
	}
	gates := map[string]*gateFor{}
	created := map[string]time.Time{}
	for id, o := range j.sessions {
		gates[id], created[id] = o.GateFor, o.CreatedAt
	}
	for id, rec := range recs {
		gates[id], created[id] = rec.GateFor, rec.CreatedAt
	}
	rows := []outcomeRow{}
	for id := range ids {
		o, ok := factsOf(id)
		if !ok {
			continue
		}
		row := outcomeRow{sessionOutcome: o}
		if !o.LastTurnEnd.IsZero() {
			row.WallSeconds = int64(o.LastTurnEnd.Sub(o.CreatedAt).Seconds())
		}
		if e, ok := builders[id]; ok {
			l := j.landingRow(e, gates, created)
			row.Landing = &l
			for _, r := range l.Reviewers {
				row.ReviewersCost += costOf(r)
			}
			for _, f := range l.Fixers {
				row.FixersCost += costOf(f)
			}
			row.CostToLanding = costOf(id) + row.ReviewersCost + row.FixersCost
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(x, y outcomeRow) int {
		return cmp.Or(x.CreatedAt.Compare(y.CreatedAt), strings.Compare(x.Session, y.Session))
	})
	return outcomesResult{Outcomes: rows}, nil
}

func (j outcomeJournal) landingRow(e outcomeEntry, gates map[string]*gateFor, created map[string]time.Time) landingRow {
	l := *e.Landing
	var prev time.Time
	for _, p := range j.landings {
		if p.At.Before(e.At) && p.Landing.Lander == l.Lander && p.Landing.Into == l.Into && p.Landing.Source == l.Source {
			prev = p.At
		}
	}
	within := func(t time.Time) bool { return t.After(prev) && !t.After(e.At) }
	row := landingRow{landingOutcome: l, At: e.At, Verdicts: []landingVerdict{}, Reviewers: []string{}}
	for _, v := range j.verdicts {
		if v.Lander != l.Lander || v.Into != l.Into || v.Source != l.Source || !within(v.At) {
			continue
		}
		row.Gate = cmp.Or(row.Gate, v.Gate)
		row.Verdicts = append(row.Verdicts, landingVerdict{Round: v.Round, Outcome: v.Outcome, By: v.By, Reviewer: v.Reviewer, At: v.At})
		if v.Outcome == gateChanges {
			row.Changes++
		}
		if v.By == landgate.ByReviewer {
			row.Rounds++
		}
		if v.Reviewer != "" && !slices.Contains(row.Reviewers, v.Reviewer) {
			row.Reviewers = append(row.Reviewers, v.Reviewer)
		}
	}
	for id, gf := range gates {
		if sameLanding(gf, l) && within(created[id]) && !slices.Contains(row.Reviewers, id) {
			row.Reviewers = append(row.Reviewers, id)
		}
	}
	slices.Sort(row.Reviewers)
	return row
}

func (a *app) buildersOf(commits []string) map[string]string {
	out := map[string]string{}
	if len(commits) == 0 {
		return out
	}
	for _, e := range a.readOutcomes() {
		if e.Type == outcomeLanding && e.Landing != nil && e.Landing.Builder != "" && slices.Contains(commits, e.Landing.Squash) {
			out[e.Landing.Squash] = e.Landing.Builder
		}
	}
	return out
}

func (a *app) withBuilders(ats []attribution) []attribution {
	var commits []string
	for _, at := range ats {
		commits = append(commits, at.Commit)
	}
	builders := a.buildersOf(commits)
	for i := range ats {
		ats[i].Builder = builders[ats[i].Commit]
	}
	return ats
}
