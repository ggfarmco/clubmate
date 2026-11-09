package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	landWaits    = "waits"
	landRunning  = "running"
	landAsking   = "asking"
	landFailed   = "failed"
	landReady    = "ready"
	landOnBranch = "onBranch"
	landPROpen   = "prOpen"
	landMerged   = "merged"
	landLeft     = "left"

	landActionReview = "review"
	landActionMerge  = "merge"
	landActionLeave  = "leave"

	landPromptDue  = "due"
	landPromptSent = "sent"

	pendingLand   = "land"
	pendingFailed = "failed"

	landIDPrefix = "lr"
	originRemote = "origin"
	tailRunes    = 2000
	prRunes      = 100
	prCheckEnv   = "CLUBMATE_AGENTS_PR_CHECK"
	prCheckEvery = 5 * time.Minute
	reportFetch  = 20 * time.Second

	waitsForReport = "its report"
	waitsFailed    = "%s, which failed"
	waitsLeft      = "%s, left unlanded"
	waitsFolder    = "task %s, landing there"

	landTurnFormat   = "[Clubmate] Land %s: it is this repo's turn. "
	landAgreedFormat = "[Clubmate] Land %s: the user agreed. "
	landCapText      = "This repo was connected while the user was away, so for this task open a pull request at most, even if its rules say merge; never merge it. "
	landMergedDenied = "git does not show this change in the default branch, so it stays not merged. Land it first, or report what really happened."
	landSettledNote  = "This repo is already settled; the report changes nothing."
	landNoReportTail = "The land turn ended with no report, so Clubmate can't tell how this repo landed. Ask the child, or leave the repo."

	landManyFormat   = "Land %d repos?"
	landOneFormat    = "Land %s?"
	failedManyFormat = "%d repos failed"
	failedOneFormat  = "%s failed"
	taskFailedFormat = "%d failed"
	taskReposFormat  = "%d repos"
)

var (
	errNotRepoChild = errors.New("only a repo child of a task reports its landing")
	errReportState  = errors.New("state is ready, failed, onBranch, prOpen or merged")
	errLandUser     = errors.New("only the user answers a landing request")
	errLandStale    = errors.New("this landing request was already answered")
	errLandAction   = errors.New("action is review, merge or leave")
	errLandMoved    = errors.New("this branch changed since you saw it")
	errNotItsTurn   = errors.New("it isn't this repo's turn to land; report ready and wait for Clubmate's prompt")
	errRepoSettled  = errors.New("this repo already landed or was left")
)

type taskReportArgs struct {
	State string `json:"state"`
	PR    string `json:"pr"`
	Tail  string `json:"tail"`
}

type taskReportResult struct {
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
}

type landAnswerArgs struct {
	Request string `json:"request"`
	Action  string `json:"action"`
	Tip     string `json:"tip"`
}

type repoLeaveArgs struct {
	Session string `json:"session"`
	Repo    string `json:"repo"`
}

type landReviewResult struct {
	Session  string `json:"session"`
	Worktree string `json:"worktree"`
}

func (a *app) taskReportCommand(ctx context.Context, cmd pluginsdk.CommandRun, args taskReportArgs) (any, error) {
	caller, err := a.sessionCaller(cmd)
	if err != nil {
		return nil, err
	}
	rec := caller.snapshot()
	if rec.RepoPath == "" {
		return nil, errNotRepoChild
	}
	if !slices.Contains([]string{landReady, landFailed, landOnBranch, landPROpen, landMerged}, args.State) {
		return nil, fmt.Errorf("%w: %s", errReportState, args.State)
	}
	a.fetchForReport(ctx, caller, rec)
	a.landMu.Lock()
	defer a.landMu.Unlock()
	owner := a.topLevel(caller)
	task := owner.snapshot().Task.clone()
	i := task.repoIndex(rec.RepoPath)
	if i < 0 {
		return nil, errNotRepoChild
	}
	r := &task.Repos[i]
	if r.ignores(args.State) {
		return taskReportResult{State: r.State, Note: landSettledNote}, nil
	}
	if !r.mayReport(args.State) {
		return nil, errNotItsTurn
	}
	r.Session, r.ReportTurn = rec.ID, caller.currentTurn()
	r.Tail = clipRunes(cmp.Or(args.Tail, r.Tail), tailRunes)
	r.PR = clipRunes(cmp.Or(args.PR, r.PR), prRunes)
	note := r.report(ctx, args.State, &rec)
	owner.putTask(task)
	a.saveNow(ctx, owner)
	a.checkLanded(ctx, owner)
	a.reconcileTasks(ctx)
	return taskReportResult{State: owner.snapshot().Task.Repos[i].State, Note: note}, nil
}

func (a *app) fetchForReport(ctx context.Context, caller *session, rec record) {
	ctx, cancel := context.WithTimeout(ctx, reportFetch)
	defer cancel()
	paths := []string{rec.RepoPath}
	if task := a.topLevel(caller).snapshot().Task; task != nil {
		paths = append(paths, task.openPRs()...)
	}
	a.fetchSeen(ctx, rec.Workspace, paths)
}

func (a *app) repoLeaveCommand(ctx context.Context, cmd pluginsdk.CommandRun, args repoLeaveArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errLandUser
	}
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	a.landMu.Lock()
	defer a.landMu.Unlock()
	owner := a.handedTo(s)
	task := owner.snapshot().Task.clone()
	i := task.repoIndex(args.Repo)
	if i < 0 {
		return nil, errNotRepoChild
	}
	r := &task.Repos[i]
	if r.landed() || r.State == landLeft {
		return nil, errRepoSettled
	}
	r.release()
	r.State = landLeft
	owner.putTask(task)
	a.saveNow(ctx, owner)
	a.reconcileTasks(ctx)
	return nil, nil
}

func (a *app) landAnswerCommand(ctx context.Context, cmd pluginsdk.CommandRun, args landAnswerArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errLandUser
	}
	res, err := a.answerLand(ctx, cmd.WorkspaceID, args)
	if err != nil || args.Action != landActionReview {
		return nil, err
	}
	return res, nil
}

func (a *app) answerLand(ctx context.Context, workspaceID string, args landAnswerArgs) (landReviewResult, error) {
	a.landMu.Lock()
	defer a.landMu.Unlock()
	owner, task, i, ok := a.findLand(workspaceID, args.Request)
	if !ok {
		return landReviewResult{}, errLandStale
	}
	r := &task.Repos[i]
	switch args.Action {
	case landActionReview:
		out := landReviewResult{Session: r.Session}
		if kid := a.session(r.Session); kid != nil {
			out.Worktree = kid.snapshot().Worktree
		}
		return out, nil
	case landActionMerge:
		if args.Tip != "" && args.Tip != a.liveTip(ctx, *r) {
			return landReviewResult{}, errLandMoved
		}
		r.Request, r.State, r.LandPrompt, r.Since = "", landRunning, landPromptDue, time.Time{}
	case landActionLeave:
		r.release()
		r.State = landLeft
	default:
		return landReviewResult{}, fmt.Errorf("%w: %s", errLandAction, args.Action)
	}
	owner.putTask(task)
	a.saveNow(ctx, owner)
	a.reconcileTasks(ctx)
	return landReviewResult{}, nil
}

func (a *app) landingChanged() {
	a.landWork.run(func() {
		a.landMu.Lock()
		defer a.landMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
		defer cancel()
		a.reconcileTasks(ctx)
	})
}

type coalesced struct {
	mu      sync.Mutex
	running bool
	dirty   bool
}

func (c *coalesced) run(work func()) {
	if !c.enter() {
		return
	}
	for {
		work()
		if !c.again() {
			return
		}
	}
}

func (c *coalesced) enter() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		c.dirty = true
		return false
	}
	c.running = true
	return true
}

func (c *coalesced) again() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty {
		c.dirty = false
		return true
	}
	c.running = false
	return false
}

func (a *app) requeueLandPrompts() {
	for _, owner := range a.taskOwners() {
		task := owner.snapshot().Task.clone()
		for i := range task.Repos {
			r := &task.Repos[i]
			kid := a.session(r.Session)
			if kid == nil || !r.Landing || r.LandPrompt != landPromptSent || kid.snapshot().Status == statusClosed {
				continue
			}
			if kid.currentTurn() <= r.LandTurn {
				r.LandPrompt = landPromptDue
			}
		}
		owner.putTask(task)
	}
}

func (a *app) landingAtStart() {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	for _, rec := range a.taskRecords() {
		if rec.closed() {
			continue
		}
		var paths []string
		for _, r := range rec.Task.Repos {
			if r.State == landPROpen || r.Landing {
				paths = append(paths, r.Path)
			}
		}
		a.fetchSeen(ctx, rec.Workspace, paths)
	}
	a.checkAllLanded()
}

func (a *app) prCheckLoop(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			a.checkOpenPRs()
		}
	}
}

func (a *app) checkOpenPRs() {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	found := false
	for _, rec := range a.taskRecords() {
		if rec.closed() {
			continue
		}
		paths := rec.Task.openPRs()
		a.fetchSeen(ctx, rec.Workspace, paths)
		found = found || len(paths) > 0 || slices.ContainsFunc(rec.Task.Repos, func(r taskRepo) bool { return r.Landing })
	}
	if found {
		a.checkAllLanded()
	}
}

func (a *app) checkAllLanded() {
	a.landMu.Lock()
	defer a.landMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	for _, owner := range a.taskOwners() {
		if !owner.snapshot().closed() {
			a.checkLanded(ctx, owner)
		}
	}
	a.reconcileTasks(ctx)
}

func (a *app) checkLanded(ctx context.Context, owner *session) {
	task := owner.snapshot().Task.clone()
	found := false
	for i := range task.Repos {
		r := &task.Repos[i]
		if r.State != landPROpen {
			continue
		}
		var kid *record
		if s := a.session(r.Session); s != nil {
			rec := s.snapshot()
			kid = &rec
		}
		if r.landedNow(ctx, kid) {
			r.release()
			r.State, found = landMerged, true
		}
	}
	if found {
		owner.putTask(task)
		a.saveNow(ctx, owner)
	}
}

func (a *app) reconcileTasks(ctx context.Context) {
	for _, owner := range a.taskOwners() {
		a.reconcile(ctx, owner)
	}
}

func (a *app) reconcile(ctx context.Context, owner *session) {
	rec := owner.snapshot()
	if rec.closed() || rec.Task == nil {
		return
	}
	task := rec.Task.clone()
	kids := a.repoChildren(owner)
	for i := range task.Repos {
		r := &task.Repos[i]
		r.Capped = slices.ContainsFunc(rec.AfkConnected, func(p string) bool { return gitrepo.Canonical(p) == gitrepo.Canonical(r.Path) })
		a.syncRepo(ctx, r, kids[gitrepo.Canonical(r.Path)])
	}
	for i := range task.Repos {
		a.gate(ctx, rec, task, i, kids[gitrepo.Canonical(task.Repos[i].Path)])
	}
	if !reflect.DeepEqual(task, rec.Task) {
		owner.putTask(task)
		a.saveNow(ctx, owner)
	}
	if a.sendLandPrompts(ctx, owner, rec.Finish) {
		a.reconcile(ctx, owner)
	}
	if now := owner.snapshot(); !rec.taskWaits() && now.taskWaits() {
		owner.mu.Lock()
		t := owner.turnLocked()
		owner.mu.Unlock()
		a.notifyEnd(owner, t)
	}
}

func (a *app) syncRepo(ctx context.Context, r *taskRepo, child *session) {
	if r.State == "" {
		r.State = landWaits
	}
	if r.settled() {
		return
	}
	if child == nil {
		if r.Session != "" {
			r.release()
			r.State = landLeft
		}
		return
	}
	kid := child.snapshot()
	if r.Session != "" && r.Session != kid.ID {
		r.ReportTurn, r.LandTurn, r.TakenTurn = 0, 0, child.currentTurn()
	}
	r.Session = kid.ID
	switch {
	case kid.closed():
		r.release()
		r.State = landLeft
	case kid.Status == statusError:
		r.fail()
	case r.Request != "":
		r.State = landReady
	case kid.Status == statusWaiting || child.asks():
		r.State = landAsking
	case kid.Status == statusRunning:
		turn := child.currentTurn()
		if turn <= r.ReportTurn {
			return
		}
		if !r.Landing && turn > r.TakenTurn {
			r.Finished = false
		}
		r.State, r.For = landRunning, ""
	case r.State == landFailed || r.Finished && !r.Landing:
	case r.Landing && r.LandPrompt == landPromptDue:
	case r.Landing:
		if kid.Status != statusDone || child.currentTurn() <= r.LandTurn {
			return
		}
		r.settleLandTurn(ctx, &kid)
	case child.currentTurn() > 0:
		r.State, r.For = landWaits, waitsForReport
	}
}

func (a *app) gate(ctx context.Context, rec record, task *taskRecord, i int, child *session) {
	r := &task.Repos[i]
	if child == nil || !r.Finished || r.Landing || r.Request != "" || r.settled() || r.State == landFailed {
		return
	}
	if st := child.snapshot().Status; st != statusDone && st != statusIdle {
		return
	}
	if held := task.heldBy(i); held != "" {
		r.State, r.For = landWaits, held
		return
	}
	if r.LandsBy == landsByBranch {
		r.release()
		r.State = landOnBranch
		return
	}
	if holder := a.folderHolder(rec.ID, r.Path); holder != "" {
		r.State, r.For = landWaits, fmt.Sprintf(waitsFolder, holder)
		return
	}
	r.Landing, r.For = true, ""
	if tip := headOf(ctx, child.snapshot().Worktree); tip != "" {
		r.Tip = tip
	}
	if rec.Finish == finishMerge {
		r.State, r.LandPrompt = landRunning, landPromptDue
		return
	}
	r.State, r.Request, r.Since = landReady, id.New(landIDPrefix), time.Now().UTC()
	r.noteLanding(ctx, child.snapshot().Worktree)
}

func (r *taskRepo) settleLandTurn(ctx context.Context, kid *record) {
	landed, err := r.landCheck(ctx, kid)
	switch {
	case landed:
		r.release()
		r.State = landMerged
	case err == nil:
		r.setFailed()
		r.Tail = landNoReportTail
	}
}

func (r *taskRepo) noteLanding(ctx context.Context, worktree string) {
	if into, err := gitrepo.DefaultBranch(ctx, repoTop(r.Path)); err == nil {
		r.Into = into
	}
	top := repoTop(worktree)
	if out, err := gitrepo.Run(ctx, top, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		r.Branch = strings.TrimSpace(string(out))
	}
	if r.Base == "" || r.Tip == "" {
		return
	}
	if out, err := gitrepo.Run(ctx, top, "rev-list", "--count", r.Base+".."+r.Tip); err == nil {
		r.Commits, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
}

func (a *app) folderHolder(ownerID, path string) string {
	target := gitrepo.Canonical(path)
	for _, rec := range a.taskRecords() {
		if rec.ID == ownerID || rec.closed() {
			continue
		}
		for _, r := range rec.Task.Repos {
			if r.Landing && gitrepo.Canonical(r.Path) == target {
				return rec.Task.Slug
			}
		}
	}
	return ""
}

func (a *app) sendLandPrompts(ctx context.Context, owner *session, finish string) bool {
	task := owner.snapshot().Task.clone()
	landed := false
	for i := range task.Repos {
		r := &task.Repos[i]
		kid := a.session(r.Session)
		if r.LandPrompt != landPromptDue || kid == nil {
			continue
		}
		rec := kid.snapshot()
		if r.landedNow(ctx, &rec) {
			r.release()
			r.State, landed = landMerged, true
			owner.putTask(task.clone())
			a.saveNow(ctx, owner)
			continue
		}
		r.LandPrompt, r.LandTurn = landPromptSent, kid.currentTurn()
		owner.putTask(task.clone())
		a.saveNow(ctx, owner)
		if err := a.submit(kid, &promptArgs{Text: r.landText(finish)}); err != nil {
			fmt.Fprintln(os.Stderr, "agents: land prompt:", err)
			r.LandPrompt = landPromptDue
			owner.putTask(task.clone())
			a.saveNow(ctx, owner)
		}
	}
	return landed
}

func (a *app) findLand(workspaceID, request string) (*session, *taskRecord, int, bool) {
	for _, s := range a.workspaceSessions(workspaceID) {
		rec := s.snapshot()
		if rec.Task == nil || rec.closed() {
			continue
		}
		if i := slices.IndexFunc(rec.Task.Repos, func(r taskRepo) bool { return r.Request == request }); request != "" && i >= 0 {
			return s, rec.Task.clone(), i, true
		}
	}
	return nil, nil, 0, false
}

func (a *app) taskOwners() []*session {
	a.mu.Lock()
	all := slices.Collect(maps.Values(a.sessions))
	a.mu.Unlock()
	all = slices.DeleteFunc(all, func(s *session) bool { return s.snapshot().Task == nil })
	slices.SortFunc(all, func(x, y *session) int { return x.snapshot().CreatedAt.Compare(y.snapshot().CreatedAt) })
	return all
}

func (a *app) taskRecords() []record {
	owners := a.taskOwners()
	out := make([]record, 0, len(owners))
	for _, s := range owners {
		if rec := s.snapshot(); rec.Task != nil {
			out = append(out, rec)
		}
	}
	return out
}

func (a *app) tasksOn(workspaceID, folder string) []string {
	var out []string
	for _, rec := range a.taskRecords() {
		if i := rec.Task.repoIndex(folder); rec.Workspace == workspaceID && !rec.closed() && i >= 0 && rec.Task.Repos[i].unsettled() {
			out = append(out, rec.Task.Slug)
		}
	}
	return out
}

func (a *app) tasksHolding(ticked []folderWorktree) []string {
	dirs := holdDirs(nil, ticked)
	var out []string
	for _, rec := range a.taskRecords() {
		held := !rec.closed() && slices.ContainsFunc(rec.Task.Repos, func(r taskRepo) bool {
			child := a.session(r.Session)
			return r.unsettled() && child != nil && child.worksIn(dirs)
		})
		if held {
			out = append(out, rec.Task.Slug)
		}
	}
	return out
}

func (a *app) repoChildren(owner *session) map[string]*session {
	out := map[string]*session{}
	for ; owner != nil; owner = a.session(owner.snapshot().HandoffFrom) {
		for _, s := range a.subtree(owner)[1:] {
			rec := s.snapshot()
			if rec.RepoPath == "" {
				continue
			}
			key := gitrepo.Canonical(rec.RepoPath)
			if cur := out[key]; cur == nil || cur.snapshot().closed() || !rec.closed() {
				out[key] = s
			}
		}
	}
	return out
}

func (a *app) ownerFinish(rec record) string {
	finish := rec.Finish
	for r := range a.ancestry(rec) {
		finish = r.Finish
	}
	return finish
}

func (r taskRepo) ignores(state string) bool {
	return r.landed() || r.State == landLeft || r.State == landPROpen && state != landMerged && state != landFailed
}

func (r taskRepo) mayReport(state string) bool {
	switch state {
	case landOnBranch, landPROpen:
		return r.LandPrompt != ""
	case landMerged:
		return r.LandPrompt != "" || r.State == landPROpen
	}
	return true
}

func (r *taskRepo) report(ctx context.Context, state string, kid *record) string {
	switch state {
	case landReady:
		if r.Request != "" {
			return ""
		}
		r.release()
		r.Finished, r.State = true, landReady
		if r.Tip = headOf(ctx, kid.Worktree); r.Tip != "" {
			r.Base = gitrepo.ForkPoint(ctx, repoTop(r.Path), r.Tip)
		}
	case landFailed:
		r.fail()
	case landMerged:
		if !r.landedNow(ctx, kid) {
			return landMergedDenied
		}
		r.release()
		r.State = landMerged
	default:
		r.release()
		r.State = state
	}
	return ""
}

func (r *taskRepo) landedNow(ctx context.Context, kid *record) bool {
	landed, _ := r.landCheck(ctx, kid)
	return landed
}

func (r *taskRepo) landCheck(ctx context.Context, kid *record) (bool, error) {
	if kid != nil {
		if tip := headOf(ctx, kid.Worktree); tip != "" {
			r.Tip = tip
		}
	}
	if r.Tip == "" || r.Base == "" || r.Tip == r.Base {
		return false, ctx.Err()
	}
	return gitrepo.Landed(ctx, repoTop(r.Path), r.Tip)
}

func (r *taskRepo) release() {
	r.Landing, r.LandPrompt, r.Request, r.Finished, r.For, r.Since = false, "", "", false, "", time.Time{}
}

func (r *taskRepo) fail() {
	if r.State == landFailed {
		return
	}
	r.release()
	r.setFailed()
}

func (r *taskRepo) setFailed() {
	r.State, r.For, r.Since, r.FixAsked = landFailed, "", time.Now().UTC(), false
}

func (r taskRepo) settled() bool {
	return r.landed() || r.State == landLeft || r.State == landPROpen
}

func (r taskRepo) landed() bool {
	return r.State == landMerged || r.State == landOnBranch
}

func (r taskRepo) unsettled() bool {
	return !r.landed() && r.State != landLeft
}

func (r taskRepo) landText(finish string) string {
	format := landAgreedFormat
	if finish == finishMerge {
		format = landTurnFormat
	}
	text := fmt.Sprintf(format, r.Name)
	if r.Capped {
		text += landCapText
	}
	return text + repoLandRules
}

func (t *taskRecord) clone() *taskRecord {
	if t == nil {
		return &taskRecord{}
	}
	return &taskRecord{Slug: t.Slug, Repos: slices.Clone(t.Repos)}
}

func (t taskRecord) open() bool {
	return slices.ContainsFunc(t.Repos, func(r taskRepo) bool { return !r.landed() && r.State != landLeft })
}

func (t taskRecord) openPRs() []string {
	var out []string
	for _, r := range t.Repos {
		if r.State == landPROpen {
			out = append(out, r.Path)
		}
	}
	return out
}

func (t taskRecord) repoIndex(path string) int {
	target := gitrepo.Canonical(path)
	return slices.IndexFunc(t.Repos, func(r taskRepo) bool { return gitrepo.Canonical(r.Path) == target })
}

func (t taskRecord) heldBy(i int) string {
	for _, r := range t.Repos {
		if r.Order >= t.Repos[i].Order || r.landed() {
			continue
		}
		switch r.State {
		case landFailed:
			return fmt.Sprintf(waitsFailed, r.Name)
		case landLeft:
			return fmt.Sprintf(waitsLeft, r.Name)
		}
		return r.Name
	}
	return ""
}

func (t taskRecord) needsYou() bool {
	return slices.ContainsFunc(t.Repos, func(r taskRepo) bool { return r.Request != "" || r.State == landFailed })
}

func (t taskRecord) requests() []string {
	var out []string
	for _, r := range t.Repos {
		if r.Request != "" {
			out = append(out, r.Request)
		}
	}
	return out
}

func (r record) taskPending(asks bool) *pendingSummary {
	summary := r.Task.pendingSummary()
	if asks && summary.Kind == pendingFailed {
		return nil
	}
	return summary
}

func (t taskRecord) pendingSummary() *pendingSummary {
	var land, failed []taskRepo
	for _, r := range t.Repos {
		switch {
		case r.Request != "":
			land = append(land, r)
		case r.State == landFailed:
			failed = append(failed, r)
		}
	}
	if len(land) > 0 {
		return &pendingSummary{Kind: pendingLand, Text: countText(land, landOneFormat, landManyFormat), At: oldestSince(land), OnMac: true}
	}
	return &pendingSummary{Kind: pendingFailed, Text: countText(failed, failedOneFormat, failedManyFormat), At: oldestSince(failed), OnMac: true}
}

func countText(repos []taskRepo, one, many string) string {
	if len(repos) == 1 {
		return fmt.Sprintf(one, repos[0].Name)
	}
	return fmt.Sprintf(many, len(repos))
}

func oldestSince(repos []taskRepo) time.Time {
	var at time.Time
	for _, r := range repos {
		if at.IsZero() || r.Since.Before(at) {
			at = r.Since
		}
	}
	return at
}

func (r record) taskWaits() bool {
	return !r.closed() && r.Task != nil && r.Task.needsYou()
}

func (r record) turnEnded() bool {
	return r.Status == statusDone || r.Status == statusIdle || r.Status == statusError
}

func (r record) taskOpen() bool {
	return r.Task != nil && r.Task.open()
}

func (s *session) putTask(t *taskRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec.Task = t
}

func (s *session) noteAfkConnected(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec.AfkConnected = append(slices.Clone(s.rec.AfkConnected), path)
}

func headOf(ctx context.Context, worktree string) string {
	out, err := gitrepo.Run(ctx, repoTop(worktree), "rev-parse", "--verify", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func repoTop(dir string) string {
	if top, ok := gitrepo.RepoRoot(dir); ok {
		return top
	}
	return dir
}

func (a *app) fetchSeen(ctx context.Context, workspaceID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: fetch:", err)
		return
	}
	var wg sync.WaitGroup
	for _, path := range paths {
		if unseenAFK(w, path) {
			continue
		}
		wg.Go(func() {
			if err := gitrepo.Fetch(ctx, repoTop(path), originRemote); err != nil {
				fmt.Fprintln(os.Stderr, "agents: fetch "+filepath.Base(path)+":", err)
			}
		})
	}
	wg.Wait()
}

func unseenAFK(w pluginsdk.Workspace, path string) bool {
	target := gitrepo.Canonical(path)
	return slices.ContainsFunc(w.Folders, func(f pluginsdk.Folder) bool {
		return f.AddedAfk && !f.Seen && gitrepo.Canonical(f.Path) == target
	})
}
