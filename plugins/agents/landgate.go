package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	gitPlugin         = "git"
	gatesKey          = "land-gates"
	verdictKey        = "land-verdict/"
	gateKindReview    = "fable-review"
	gateRoundsDefault = 2
	gateModel         = "fable"
	gateIDPrefix      = "gt"
	shortSha          = 10
	gateRequestPrefix = "lg"
	hooksDirName      = "githooks"
	verdictsDirName   = "land-verdicts"
	gateCardNode      = "landgate"

	gateApproved = landgate.Approved
	gateChanges  = landgate.Changes
	gatePending  = "pending"
	gateUser     = "user"

	gateReviewerText  = "[Clubmate land reviewer] You review a change before it lands. You are read-only: never edit files, commit, merge, reset, check out, stash or delete anything, and never start sessions. Read the diff and the code around it, run read-only commands and tests when they help, and end every review with exactly one agents_land_verdict call."
	gateFirstFormat   = "[Clubmate land review] Review this change before it lands in %s (repo %s).\nGoal: %s\nThe change: `git diff %s %s` (%s's tip, then the tree %s will hold)%s.%s\nReport confirmed correctness bugs, regressions and missed cases with file:line. Then call agents_land_verdict once with tree %s: outcome changes only for confirmed bugs, each finding as \"file:line — what breaks\"; otherwise approved. Nits and suggestions go to deferred."
	gateDeltaFormat   = "[Clubmate land review] The lander changed the change after your findings. Review only the delta: `git diff %s %s` (the whole change is `git diff %s %s`). Then call agents_land_verdict again with tree %s."
	gateLandedFormat  = "\nThese commits already landed on %s through the gate, each for its own diff: %s. Still review the whole change above; spend most of the time on the other commits: %s."
	verdictTreeFormat = "tree must be the one your latest review prompt names: %s"
	gateBusyText      = "a review of an earlier version of this change is still running (%s). End your turn; when its verdict comes, call git_land again and the reviewer gets what changed since."
	gateCommitsFormat = ", commits `git log %s..%s`"
	gateNudgeText     = "[Clubmate] Your land review ended without a verdict. Call agents_land_verdict now with the tree your review prompt names: approved, or changes with the confirmed bugs as findings."
	gateNoVerdictText = "[Clubmate] The land review of %s ended without a verdict. Call git_land again to start it over."
	gateApprovedText  = "[Clubmate] Land review of %s: approved by %s. Call git_land again to land it."
	gateChangesText   = "[Clubmate] Land review of %s asks for changes (round %d of %d). Fix these, commit, and call git_land again:\n%s"
	gateDeferredText  = "\nDeferred (file them in the backlog, they don't block):\n%s"
	gateUserLandText  = "[Clubmate] The user agreed to land %s without another review. Call git_land again to land it."
	gateUserStopText  = "[Clubmate] The user stopped landing %s. Don't land it; report what is left."
	gatePendingText   = "review pending: %s. End your turn; Clubmate prompts you with the verdict."
	gateWaitsUserText = "the review asked for changes %d times, so this landing waits for the user's answer. End your turn; Clubmate prompts you with it."
	gateChangesResult = "the review asked for changes; fix them and call git_land again:\n%s"
	gateCardTitle     = "Land %s without another review?"
	gateCardCaption   = "The review asked for changes %d times"
	gateAgainCaption  = "; asked again because the change or its target moved after the last answer"
	gateCardRule      = "Land anyway squashes it into %s as it is. Stop tells the agent not to land it."
	gateLandLabel     = "Land anyway"
	gateStopLabel     = "Stop"
	gateMovedLog      = "agents: %s moved without a land verdict: %s"
)

var (
	gateReviewerDenied = []string{
		"Edit", "Write", "MultiEdit", "NotebookEdit", "mcp__clubmate__git_land", "mcp__clubmate__agents_new",
		"Bash(git commit:*)", "Bash(git merge:*)", "Bash(git reset:*)", "Bash(git checkout:*)", "Bash(git switch:*)",
		"Bash(git rebase:*)", "Bash(git stash:*)", "Bash(git push:*)", "Bash(git update-ref:*)", "Bash(rm:*)", "Bash(mv:*)",
	}
	docsOnlyRefused = []string{"claude.md", "agents.md", "gemini.md", "skill.md"}
	docsOnlyDirs    = []string{".claude/", ".clubmate/", ".github/", ".agents/", "skills/", "commands/", "agents/", "prompts/", "rules/"}

	errGitOnly      = errors.New("only the git plugin calls this")
	errGateUser     = errors.New("only the user changes land gates")
	errGateKind     = errors.New("kind is fable-review")
	errGateExists   = errors.New("this repo already has a land gate")
	errGateUnknown  = errors.New("no such land gate")
	errGateRounds   = errors.New("rounds is 1 or more")
	errNotReviewer  = errors.New("only a land reviewer session gives a verdict")
	errNoReview     = errors.New("no land review is waiting for a verdict")
	errVerdict      = errors.New("outcome is approved or changes, and changes needs findings")
	errGateNoLander = errors.New("git_land needs the calling agent session")
)

type gateConfig struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	Kind      string    `json:"kind"`
	Rounds    int       `json:"rounds"`
	Paused    bool      `json:"paused,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type gateFor struct {
	Gate   string `json:"gate,omitempty"`
	Lander string `json:"lander"`
	Into   string `json:"into"`
	Source string `json:"source,omitempty"`
	Old    string `json:"old,omitempty"`
	Tree   string `json:"tree,omitempty"`
	Done   bool   `json:"done,omitempty"`
	Nudged bool   `json:"nudged,omitempty"`
}

type landReview struct {
	Gate     string    `json:"gate,omitempty"`
	Last     string    `json:"last,omitempty"`
	Into     string    `json:"into"`
	Source   string    `json:"source,omitempty"`
	Repo     string    `json:"repo"`
	Reviewer string    `json:"reviewer,omitempty"`
	Old      string    `json:"old,omitempty"`
	Tree     string    `json:"tree,omitempty"`
	Rounds   int       `json:"rounds,omitempty"`
	Cap      int       `json:"cap,omitempty"`
	Request  string    `json:"request,omitempty"`
	Answered string    `json:"answered,omitempty"`
	Moved    bool      `json:"moved,omitempty"`
	Findings []string  `json:"findings,omitempty"`
	Since    time.Time `json:"since,omitzero"`
}

type gatesResult struct {
	Gates []gateRow `json:"gates"`
}

type gateAddArgs struct {
	Repo   string `json:"repo"`
	Kind   string `json:"kind"`
	Rounds int    `json:"rounds"`
}

type gateUpdateArgs struct {
	ID     string `json:"id"`
	Paused *bool  `json:"paused"`
	Rounds *int   `json:"rounds"`
}

type gateIDArgs struct {
	ID string `json:"id"`
}

type landGateArgs struct {
	Session  string `json:"session"`
	Target   string `json:"target"`
	Worktree string `json:"worktree"`
	Into     string `json:"into"`
	Source   string `json:"source"`
	Head     string `json:"head"`
	Old      string `json:"old"`
	Tree     string `json:"tree"`
	Commit   string `json:"commit"`
}

type landGateResult struct {
	State    string   `json:"state"`
	Reviewer string   `json:"reviewer,omitempty"`
	Text     string   `json:"text,omitempty"`
	Findings []string `json:"findings,omitempty"`
	Deferred []string `json:"deferred,omitempty"`
}

type landVerdictArgs struct {
	Tree     string   `json:"tree"`
	Outcome  string   `json:"outcome"`
	Findings []string `json:"findings"`
	Deferred []string `json:"deferred"`
}

func (r landReview) subject() string {
	if r.Source == "" {
		return "the staged change → " + r.Into
	}
	return r.Source + " → " + r.Into
}

func (a *app) hooksDir() string {
	return filepath.Join(a.stateDir, hooksDirName)
}

func (a *app) verdictsDir() string {
	return filepath.Join(a.stateDir, verdictsDirName)
}

func (a *app) gates(ctx context.Context) ([]gateConfig, error) {
	var out []gateConfig
	if _, err := a.host.StorageGet(ctx, gatesKey, &out); err != nil {
		return nil, err
	}
	a.keepGates(out)
	return out, nil
}

func (a *app) saveGates(ctx context.Context, gates []gateConfig) error {
	if err := a.host.StorageSet(ctx, gatesKey, gates); err != nil {
		return err
	}
	a.keepGates(gates)
	return nil
}

func (a *app) keepGates(gates []gateConfig) {
	live := map[string]bool{}
	for _, g := range gates {
		live[g.ID] = !g.Paused
	}
	a.liveGates.Store(&live)
}

func (a *app) gateLive(id string) bool {
	live := a.liveGates.Load()
	if live == nil {
		ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
		defer cancel()
		if _, err := a.gates(ctx); err != nil {
			return false
		}
		live = a.liveGates.Load()
	}
	return (*live)[id]
}

func (a *app) gateFor(ctx context.Context, worktree string) *gateConfig {
	gates, err := a.gates(ctx)
	if err != nil || len(gates) == 0 {
		return nil
	}
	primary := primaryOf(ctx, worktree)
	if primary == "" {
		return nil
	}
	if i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.Repo == primary && !g.Paused }); i >= 0 {
		return &gates[i]
	}
	return nil
}

func primaryOf(ctx context.Context, worktree string) string {
	f, err := gitrepo.OpenFolder(ctx, worktree)
	if err != nil || len(f.Trees) == 0 {
		return ""
	}
	return f.Trees[0].Real
}

func (a *app) gateEnv(ctx context.Context, rec record) map[string]string {
	none := map[string]string{}
	if a.gateFor(ctx, rec.Worktree) == nil {
		return none
	}
	_, common, err := gitrepo.Dirs(ctx, rec.Worktree)
	if err != nil {
		return none
	}
	branch, err := gitrepo.DefaultBranch(ctx, rec.Worktree)
	if err != nil {
		return none
	}
	protect := []string{"refs/heads/" + branch}
	if l := a.layout(ctx, rec); l != nil && !l.onDefault {
		protect = append(protect, "refs/heads/"+l.parent.Branch)
	}
	return landgate.SessionEnv(os.Environ(), a.hooksDir(), common, protect, a.verdictsDir())
}

func (a *app) gatedMeta(ctx context.Context, rec record) *acp.SessionMeta {
	meta := sessionMeta(rec, a.sessionAdvisor(ctx, rec), a.selfFor(ctx, rec), a.userStarted(rec) && chromeByDefault())
	maps.Copy(meta.ClaudeCode.Options.Env, a.gateEnv(ctx, rec))
	return meta
}

func (a *app) gatesCommand(ctx context.Context, cmd pluginsdk.CommandRun, _ struct{}) (any, error) {
	gates, _, err := a.workspaceGates(ctx, cmd.WorkspaceID)
	if a.userWord(cmd) {
		gates, err = a.gates(ctx)
	}
	if err != nil {
		return nil, err
	}
	rows := make([]gateRow, 0, len(gates))
	for _, g := range gates {
		rows = append(rows, a.gateRow(ctx, cmd.WorkspaceID, g))
	}
	return gatesResult{Gates: rows}, nil
}

func (a *app) gateAddCommand(ctx context.Context, cmd pluginsdk.CommandRun, args gateAddArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errGateUser
	}
	g, err := a.addGate(ctx, args)
	if err == nil {
		a.gatesChanged()
	}
	return g, err
}

func (a *app) addGate(ctx context.Context, args gateAddArgs) (gateConfig, error) {
	if args.Kind != "" && args.Kind != gateKindReview {
		return gateConfig{}, errGateKind
	}
	if args.Rounds < 0 {
		return gateConfig{}, errGateRounds
	}
	primary := primaryOf(ctx, args.Repo)
	if primary == "" {
		return gateConfig{}, errNotGit
	}
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	gates, err := a.gates(ctx)
	if err != nil {
		return gateConfig{}, err
	}
	if i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.Repo == primary }); i >= 0 {
		return gateConfig{}, fmt.Errorf("%w (%s)", errGateExists, gates[i].ID)
	}
	g := gateConfig{ID: id.New(gateIDPrefix), Repo: primary, Kind: gateKindReview, Rounds: cmp.Or(args.Rounds, gateRoundsDefault), CreatedAt: time.Now().UTC()}
	return g, a.saveGates(ctx, append(gates, g))
}

func (a *app) gateUpdateCommand(ctx context.Context, cmd pluginsdk.CommandRun, args gateUpdateArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errGateUser
	}
	return nil, a.updateGate(ctx, args)
}

func (a *app) updateGate(ctx context.Context, args gateUpdateArgs) error {
	return a.changeGate(ctx, args.ID, func(gates []gateConfig, i int) ([]gateConfig, error) {
		if args.Rounds != nil {
			if *args.Rounds < 1 {
				return nil, errGateRounds
			}
			gates[i].Rounds = *args.Rounds
		}
		if args.Paused != nil {
			gates[i].Paused = *args.Paused
		}
		return gates, nil
	})
}

func (a *app) gateDeleteCommand(ctx context.Context, cmd pluginsdk.CommandRun, args gateIDArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errGateUser
	}
	return nil, a.deleteGate(ctx, args.ID)
}

func (a *app) deleteGate(ctx context.Context, gateID string) error {
	err := a.changeGate(ctx, gateID, func(gates []gateConfig, i int) ([]gateConfig, error) {
		return slices.Delete(gates, i, i+1), nil
	})
	if err != nil {
		return err
	}
	return a.host.StorageDelete(ctx, gateHistoryKey+gateID)
}

func (a *app) changeGate(ctx context.Context, gateID string, change func([]gateConfig, int) ([]gateConfig, error)) error {
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	gates, err := a.gates(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.ID == gateID })
	if i < 0 {
		return errGateUnknown
	}
	if gates, err = change(gates, i); err != nil {
		return err
	}
	if err := a.saveGates(ctx, gates); err != nil {
		return err
	}
	a.gatesChanged()
	return nil
}

func (a *app) landGateCommand(ctx context.Context, cmd pluginsdk.CommandRun, args landGateArgs) (any, error) {
	if cmd.Via != gitPlugin {
		return nil, errGitOnly
	}
	g := a.gateFor(ctx, args.Target)
	if g == nil {
		return landGateResult{State: gateApproved}, nil
	}
	if v, ok := a.readVerdict(ctx, args.Old, args.Tree); ok {
		switch v.Outcome {
		case gateApproved:
			return landGateResult{State: gateApproved}, nil
		case gateChanges:
			return landGateResult{State: gateChanges, Reviewer: v.Reviewer, Findings: v.Findings, Deferred: v.Deferred, Text: fmt.Sprintf(gateChangesResult, bullets(v.Findings))}, nil
		}
	}
	if docsOnly(ctx, args.Target, args.Old, args.Tree) {
		v := landgate.Verdict{Old: args.Old, Tree: args.Tree, Outcome: gateApproved, By: landgate.ByDocs, At: time.Now().UTC()}
		if err := a.writeVerdict(ctx, v); err != nil {
			return nil, err
		}
		a.logGateVerdict(ctx, gateVerdict{Gate: g.ID, Workspace: cmd.WorkspaceID, Outcome: gateApproved, By: landgate.ByDocs, Lander: args.Session, Title: a.titleOf(args.Session), Into: args.Into, Source: args.Source, At: v.At})
		return landGateResult{State: gateApproved}, nil
	}
	lander, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, errGateNoLander
	}
	return a.reviewLanding(ctx, cmd.WorkspaceID, lander, *g, args)
}

func (a *app) reviewLanding(ctx context.Context, workspaceID string, lander *session, g gateConfig, args landGateArgs) (landGateResult, error) {
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	st := lander.landReview(args.Into, args.Source)
	st.Repo, st.Cap, st.Gate = args.Target, g.Rounds, g.ID
	if st.Request != "" {
		return landGateResult{State: gateUser, Text: fmt.Sprintf(gateWaitsUserText, st.Rounds)}, nil
	}
	if st.Rounds >= g.Rounds {
		st.Request, st.Old, st.Tree, st.Since = id.New(gateRequestPrefix), args.Old, args.Tree, time.Now().UTC()
		st.Moved = st.Answered != "" && st.Answered != args.Old+"-"+args.Tree
		lander.putLandReview(st)
		a.saveNow(ctx, lander)
		return landGateResult{State: gateUser, Text: fmt.Sprintf(gateWaitsUserText, st.Rounds)}, nil
	}
	reviewer := a.session(st.Reviewer)
	alive := reviewer != nil && !reviewer.snapshot().closed()
	if running := reviewer.runningGate(); alive && running != nil {
		if running.Old == args.Old && running.Tree == args.Tree {
			return landGateResult{State: gatePending, Reviewer: st.Reviewer, Text: fmt.Sprintf(gatePendingText, st.Reviewer)}, nil
		}
		return landGateResult{State: gatePending, Reviewer: st.Reviewer, Text: fmt.Sprintf(gateBusyText, st.Reviewer)}, nil
	}
	gf := &gateFor{Gate: g.ID, Lander: lander.snapshot().ID, Into: args.Into, Source: args.Source, Old: args.Old, Tree: args.Tree}
	if alive && st.Tree != "" {
		text := fmt.Sprintf(gateDeltaFormat, st.Tree, args.Tree, args.Old, args.Tree, args.Tree)
		if st.Old != args.Old {
			text = a.firstReviewPrompt(ctx, lander.snapshot(), args)
		}
		reviewer.setGateFor(gf)
		if err := a.submit(reviewer, &promptArgs{Text: text}); err != nil {
			return landGateResult{}, err
		}
	} else {
		cwd := cmp.Or(args.Worktree, args.Target)
		res, err := a.newSession(ctx, a.host, workspaceID, "", newArgs{
			Title:     "Land review · " + st.subject(),
			Prompt:    a.firstReviewPrompt(ctx, lander.snapshot(), args),
			Model:     gateModel,
			Notify:    notifyNeedsInput,
			worktree:  cwd,
			gate:      gf,
			startedBy: lander.snapshot().ID,
		})
		if err != nil {
			return landGateResult{}, err
		}
		st.Reviewer = res.Session
		a.kick(workspaceID)
	}
	st.Old, st.Tree, st.Last = args.Old, args.Tree, ""
	lander.putLandReview(st)
	a.saveNow(ctx, lander)
	a.gatesChanged()
	return landGateResult{State: gatePending, Reviewer: st.Reviewer, Text: fmt.Sprintf(gatePendingText, st.Reviewer)}, nil
}

func (a *app) firstReviewPrompt(ctx context.Context, lander record, args landGateArgs) string {
	commits, landed := "", ""
	if args.Source != "" && args.Head != "" {
		commits = fmt.Sprintf(gateCommitsFormat, args.Old, args.Head)
		if done, rest := splitLanded(ctx, args.Target, args.Old, args.Head, a.landedCommits(ctx)); len(done) > 0 {
			landed = fmt.Sprintf(gateLandedFormat, args.Source, strings.Join(done, ", "), cmp.Or(strings.Join(rest, ", "), "none"))
		}
	}
	return fmt.Sprintf(gateFirstFormat, args.Into, filepath.Base(primaryOf(ctx, args.Target)), lander.Title, args.Old, args.Tree, args.Into, args.Into, commits, landed, args.Tree)
}

func (a *app) readVerdict(ctx context.Context, old, tree string) (landgate.Verdict, bool) {
	var v landgate.Verdict
	found, err := a.host.StorageGet(ctx, verdictKey+old+"-"+tree, &v)
	return v, err == nil && found
}

func (a *app) writeVerdict(ctx context.Context, v landgate.Verdict) error {
	if err := a.host.StorageSet(ctx, verdictKey+v.Old+"-"+v.Tree, v); err != nil {
		return err
	}
	return landgate.Write(a.verdictsDir(), v)
}

func (a *app) landedCommits(ctx context.Context) map[string]string {
	out := map[string]string{}
	keys, err := a.host.StorageList(ctx, verdictKey)
	if err != nil {
		return out
	}
	for _, key := range keys {
		var v landgate.Verdict
		if found, err := a.host.StorageGet(ctx, key, &v); err == nil && found && v.Commit != "" {
			out[v.Commit] = v.By
		}
	}
	return out
}

func splitLanded(ctx context.Context, repo, old, head string, landed map[string]string) ([]string, []string) {
	if len(landed) == 0 {
		return nil, nil
	}
	out, err := gitrepo.Run(ctx, repo, "rev-list", "--reverse", old+".."+head)
	if err != nil {
		return nil, nil
	}
	var done, rest []string
	for line := range strings.Lines(string(out)) {
		sha := strings.TrimSpace(line)
		short := sha[:min(len(sha), shortSha)]
		switch landed[sha] {
		case landgate.ByReviewer:
			done = append(done, short+" (reviewed)")
		case landgate.ByDocs:
			done = append(done, short+" (docs only, not reviewed)")
		case landgate.ByUser:
			rest = append(rest, short+" (landed by the user's override, not reviewed)")
		default:
			rest = append(rest, short)
		}
	}
	return done, rest
}

func docsOnly(ctx context.Context, repo, old, tree string) bool {
	out, err := gitrepo.Run(ctx, repo, "diff", "--name-only", "--no-renames", old, tree)
	if err != nil {
		return false
	}
	paths := strings.Fields(string(out))
	return len(paths) > 0 && !slices.ContainsFunc(paths, func(p string) bool {
		p = strings.ToLower(p)
		return !strings.HasSuffix(p, ".md") || slices.Contains(docsOnlyRefused, path.Base(p)) || slices.ContainsFunc(docsOnlyDirs, func(d string) bool { return strings.HasPrefix(p, d) || strings.Contains(p, "/"+d) })
	})
}

func (a *app) landVerdictCommand(ctx context.Context, cmd pluginsdk.CommandRun, args landVerdictArgs) (any, error) {
	reviewer := a.session(cmd.Caller)
	if reviewer == nil || reviewer.snapshot().GateFor == nil {
		return nil, errNotReviewer
	}
	if args.Outcome != gateApproved && (args.Outcome != gateChanges || len(args.Findings) == 0) {
		return nil, errVerdict
	}
	a.gateMu.Lock()
	defer a.gateMu.Unlock()
	rec := reviewer.snapshot()
	gf := *rec.GateFor
	if gf.Old == "" || gf.Done {
		return nil, errNoReview
	}
	if args.Tree != gf.Tree {
		return nil, fmt.Errorf(verdictTreeFormat, gf.Tree)
	}
	v := landgate.Verdict{Old: gf.Old, Tree: gf.Tree, Outcome: args.Outcome, By: landgate.ByReviewer, Reviewer: rec.ID, Findings: args.Findings, Deferred: args.Deferred, At: time.Now().UTC()}
	if err := a.writeVerdict(ctx, v); err != nil {
		return nil, err
	}
	gf.Done = true
	reviewer.setGateFor(&gf)
	a.saveNow(ctx, reviewer)
	lander := a.session(gf.Lander)
	st := landReview{Into: gf.Into, Source: gf.Source}
	if lander != nil {
		st = lander.landReview(gf.Into, gf.Source)
	}
	logged := gateVerdict{Gate: cmp.Or(gf.Gate, st.Gate), Outcome: args.Outcome, By: landgate.ByReviewer, Lander: gf.Lander, Title: a.titleOf(gf.Lander), Into: gf.Into, Source: gf.Source, Round: st.Rounds + 1, Reviewer: rec.ID, Findings: args.Findings, Deferred: args.Deferred, At: v.At}
	a.logGateVerdict(ctx, logged)
	if lander == nil || lander.snapshot().closed() {
		return nil, nil
	}
	st.Last = args.Outcome
	text := fmt.Sprintf(gateApprovedText, st.subject(), rec.ID)
	if args.Outcome == gateChanges {
		st.Rounds++
		st.Findings = args.Findings
		text = fmt.Sprintf(gateChangesText, st.subject(), st.Rounds, max(st.Cap, st.Rounds), bullets(args.Findings))
	}
	if len(args.Deferred) > 0 {
		text += fmt.Sprintf(gateDeferredText, bullets(args.Deferred))
	}
	lander.putLandReview(st)
	a.saveNow(ctx, lander)
	return nil, a.submit(lander, &promptArgs{Text: text})
}

func (a *app) gateTurnEnded(s *session, nudge bool) {
	rec := s.snapshot()
	if rec.GateFor == nil || rec.GateFor.Done || rec.GateFor.Old == "" {
		return
	}
	gf := *rec.GateFor
	ctx := context.Background()
	if nudge && !gf.Nudged && !rec.closed() {
		gf.Nudged = true
		s.setGateFor(&gf)
		a.saveNow(ctx, s)
		logError("land review nudge", a.submit(s, &promptArgs{Text: gateNudgeText}))
		return
	}
	a.gateMu.Lock()
	gf.Done = true
	s.setGateFor(&gf)
	lander := a.session(gf.Lander)
	var subject string
	if lander != nil {
		st := lander.landReview(gf.Into, gf.Source)
		st.Old, st.Tree, subject = "", "", st.subject()
		lander.putLandReview(st)
	}
	a.gateMu.Unlock()
	a.saveNow(ctx, s)
	a.gatesChanged()
	if lander != nil && !lander.snapshot().closed() {
		a.saveNow(ctx, lander)
		logError("land review missing", a.submit(lander, &promptArgs{Text: fmt.Sprintf(gateNoVerdictText, subject)}))
	}
}

func (a *app) landDoneCommand(ctx context.Context, cmd pluginsdk.CommandRun, args landGateArgs) (any, error) {
	if cmd.Via != gitPlugin {
		return nil, errGitOnly
	}
	if v, ok := a.readVerdict(ctx, args.Old, args.Tree); ok {
		v.Commit = args.Commit
		logError("land verdict commit", a.writeVerdict(ctx, v))
	}
	lander := a.session(args.Session)
	if lander == nil {
		return nil, nil
	}
	a.gateMu.Lock()
	st := lander.landReview(args.Into, args.Source)
	reviewer := a.session(st.Reviewer)
	lander.dropLandReview(args.Into, args.Source)
	a.gateMu.Unlock()
	a.saveNow(ctx, lander)
	if reviewer != nil {
		a.closeIdle(ctx, reviewer)
	}
	a.gatesChanged()
	return nil, nil
}

func (a *app) answerGate(ctx context.Context, workspaceID string, args landAnswerArgs) error {
	a.gateMu.Lock()
	lander, st, ok := a.findGateRequest(workspaceID, args.Request)
	if !ok || lander.snapshot().closed() {
		a.gateMu.Unlock()
		return errLandStale
	}
	var text string
	switch args.Action {
	case landActionMerge:
		v := landgate.Verdict{Old: st.Old, Tree: st.Tree, Outcome: gateApproved, By: landgate.ByUser, At: time.Now().UTC()}
		if err := a.writeVerdict(ctx, v); err != nil {
			a.gateMu.Unlock()
			return err
		}
		text = fmt.Sprintf(gateUserLandText, st.subject())
		a.logGateVerdict(ctx, gateVerdict{Gate: st.Gate, Outcome: gateApproved, By: landgate.ByUser, Lander: lander.snapshot().ID, Title: lander.snapshot().Title, Into: st.Into, Source: st.Source, Round: st.Rounds, At: v.At})
		st.Last = gateApproved
	case landActionLeave:
		text = fmt.Sprintf(gateUserStopText, st.subject())
	default:
		a.gateMu.Unlock()
		return fmt.Errorf("%w: %s", errLandAction, args.Action)
	}
	st.Request, st.Since, st.Answered = "", time.Time{}, st.Old+"-"+st.Tree
	lander.putLandReview(st)
	a.gateMu.Unlock()
	a.saveNow(ctx, lander)
	a.gatesChanged()
	return a.submit(lander, &promptArgs{Text: text})
}

func (a *app) gateCommits(ctx context.Context, workspaceID, request string) (landCommitsResult, bool, error) {
	a.gateMu.Lock()
	_, st, ok := a.findGateRequest(workspaceID, request)
	a.gateMu.Unlock()
	if !ok {
		return landCommitsResult{}, false, nil
	}
	out := landCommitsResult{Commits: []landCommit{}, Tip: st.Tree, Detail: bullets(st.Findings)}
	if st.Source == "" {
		return out, true, nil
	}
	log, err := gitrepo.Run(ctx, st.Repo, "log", "--format="+landCommitsLine, "-n", landCommitsMax, st.Old+".."+"refs/heads/"+st.Source)
	if err != nil {
		return out, true, err
	}
	for line := range strings.Lines(string(log)) {
		sha, title, _ := strings.Cut(strings.TrimRight(line, "\n"), landCommitsSep)
		out.Commits = append(out.Commits, landCommit{Sha: sha, Title: title})
	}
	return out, true, nil
}

func (a *app) findGateRequest(workspaceID, request string) (*session, landReview, bool) {
	if !strings.HasPrefix(request, gateRequestPrefix+"_") {
		return nil, landReview{}, false
	}
	for _, s := range a.workspaceSessions(workspaceID) {
		reviews := s.snapshot().LandReviews
		if i := slices.IndexFunc(reviews, func(r landReview) bool { return r.Request == request }); i >= 0 {
			return s, reviews[i], true
		}
	}
	return nil, landReview{}, false
}

func (a *app) logUngatedMain(rec record, commits []string) {
	if len(commits) == 0 || rec.GateFor != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	if a.gateFor(ctx, rec.Worktree) == nil {
		return
	}
	branch, err := gitrepo.Branch(ctx, rec.Worktree)
	if err != nil {
		return
	}
	if def, err := gitrepo.DefaultBranch(ctx, rec.Worktree); err != nil || def != branch {
		return
	}
	for _, sha := range commits {
		out, err := gitrepo.Run(ctx, rec.Worktree, "rev-parse", sha+"^", sha+"^{tree}")
		parts := strings.Fields(string(out))
		if err != nil || len(parts) != 2 {
			continue
		}
		if _, ok := a.readVerdict(ctx, parts[0], parts[1]); !ok {
			fmt.Fprintf(os.Stderr, gateMovedLog+"\n", branch, sha)
		}
	}
}

func gateCard(rec record, st landReview) sdui.Node {
	return sdui.WithDock(sdui.LandRequestCard(rec.ID+"/"+gateCardNode+"/"+st.Request, st.Request, sdui.LandRequestProps{
		Title:   fmt.Sprintf(gateCardTitle, cmp.Or(st.Source, st.Into)),
		Caption: gateCaption(st),
		Name:    cmp.Or(st.Source, st.Into),
		Detail:  bullets(st.Findings),
		Rule:    fmt.Sprintf(gateCardRule, st.Into),
		Options: []sdui.PermissionOption{
			{ID: landActionMerge, Label: gateLandLabel, Kind: "allow_once", Shortcut: "opt+1"},
			{ID: landActionLeave, Label: gateStopLabel, Kind: "reject_once", Shortcut: "opt+2"},
		},
	}))
}

func gateCaption(st landReview) string {
	text := fmt.Sprintf(gateCardCaption, st.Rounds)
	if st.Moved {
		text += gateAgainCaption
	}
	return text
}

func (r record) gateWaits() bool {
	return !r.closed() && slices.ContainsFunc(r.LandReviews, func(st landReview) bool { return st.Request != "" })
}

func (r record) gateRequests() []string {
	var out []string
	for _, st := range r.LandReviews {
		if st.Request != "" {
			out = append(out, st.Request)
		}
	}
	return out
}

func (r record) gatePending() *pendingSummary {
	for _, st := range r.LandReviews {
		if st.Request != "" {
			return &pendingSummary{Kind: pendingLand, Text: fmt.Sprintf(gateCardTitle, cmp.Or(st.Source, st.Into)), At: st.Since, OnMac: true}
		}
	}
	return nil
}

func (s *session) landReview(into, source string) landReview {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.IndexFunc(s.rec.LandReviews, func(r landReview) bool { return r.Into == into && r.Source == source }); i >= 0 {
		return s.rec.LandReviews[i]
	}
	return landReview{Into: into, Source: source}
}

func (s *session) putLandReview(st landReview) {
	s.mu.Lock()
	reviews := slices.DeleteFunc(slices.Clone(s.rec.LandReviews), func(r landReview) bool { return r.Into == st.Into && r.Source == st.Source })
	s.rec.LandReviews = append(reviews, st)
	s.mu.Unlock()
}

func (s *session) dropLandReview(into, source string) {
	s.mu.Lock()
	s.rec.LandReviews = slices.DeleteFunc(slices.Clone(s.rec.LandReviews), func(r landReview) bool { return r.Into == into && r.Source == source })
	s.mu.Unlock()
}

func (s *session) runningGate() *gateFor {
	if s == nil {
		return nil
	}
	gf := s.snapshot().GateFor
	if gf == nil || gf.Done || gf.Old == "" {
		return nil
	}
	return gf
}

func (s *session) setGateFor(gf *gateFor) {
	s.mu.Lock()
	s.rec.GateFor = gf
	s.mu.Unlock()
}

func bullets(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "- " + strings.Join(lines, "\n- ")
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents:", what+":", err)
	}
}

func installHooks(stateDir string) {
	logError("install git hooks", landgate.Install(filepath.Join(stateDir, hooksDirName)))
}
