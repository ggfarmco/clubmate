package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
)

const (
	repoFactsCmd = "git.repo-facts"
	gitCloneCmd  = "git.clone"

	connectIDPrefix  = "cr"
	connectTextRunes = 500
	cloneLimit       = 30 * time.Minute

	repoPending   = "pending"
	repoCloning   = "cloning"
	repoConnected = "connected"
	repoSkipped   = "skipped"
	repoFailed    = "failed"

	answerConnect = "connect"
	answerSkip    = "skip"
	answerOther   = "other"

	landsByPR     = "pr"
	landsByMerge  = "merge"
	landsByBranch = "branch"

	connectNextText    = "End your turn; Clubmate prompts you with the answer."
	connectChildFormat = "The request went to your orchestrator %q (%s), which gets the answer. Carry on without this repo or end your turn."
	connectOutcomeHead = "[Clubmate] The user answered your request to connect repos:"
	connectChildHead   = "[Clubmate] The user answered the request of child %q (%s) to connect repos:"
	connectAFKTitle    = "Connected %s while you were away"
	connectOneFormat   = "Connect %s?"
	connectManyFormat  = "Connect %d repos?"
	connectWants       = "Wants to connect"
)

var (
	errNeedsSession  = errors.New("this needs an agent session of Clubmate")
	errConnectUser   = errors.New("only the user answers a connect request")
	errConnectStale  = errors.New("this connect request was already answered")
	errConnectSeen   = errors.New("this connect request changed since you saw it")
	errConnectRepo   = errors.New("not a repo of this connect request")
	errConnectAnswer = errors.New("an answer is connect, skip or other, and other needs otherPath")
	errConnectOther  = errors.New("another folder must be a git repo on disk")
	errConnectBoth   = errors.New("a repo has either a path or a url")
	errConnectName   = errors.New("can't tell the repo's name from its url")
	errConnectHelper = errors.New("a remote helper url can't be cloned from here")
	errConnectEmpty  = errors.New("repos is empty")
	errServiceRepo   = errors.New("a service session can't connect repos")
	errRelativeRepo  = errors.New("a repo's path must be absolute")
	errLandsBy       = errors.New("landsBy is pr, merge or branch")

	remoteHelper = regexp.MustCompile(`^[A-Za-z0-9+.-]+::`)
)

type connectRequest struct {
	ID        string        `json:"id"`
	From      string        `json:"from,omitempty"`
	FromTitle string        `json:"fromTitle,omitempty"`
	At        time.Time     `json:"at"`
	Repos     []connectRepo `json:"repos"`
}

type connectRepo struct {
	Path    string          `json:"path,omitempty"`
	URL     string          `json:"url,omitempty"`
	Name    string          `json:"name"`
	Reason  string          `json:"reason,omitempty"`
	LandsBy string          `json:"landsBy,omitempty"`
	Source  string          `json:"source,omitempty"`
	Facts   json.RawMessage `json:"facts,omitempty"`
	State   string          `json:"state"`
	Into    string          `json:"into,omitempty"`
	Folder  string          `json:"folder,omitempty"`
	Op      string          `json:"op,omitempty"`
	Note    string          `json:"note,omitempty"`
}

type connectRepoArgs struct {
	Repos []connectAsk `json:"repos"`
}

type connectAsk struct {
	Path    string `json:"path"`
	URL     string `json:"url"`
	Reason  string `json:"reason"`
	LandsBy string `json:"landsBy"`
	Source  string `json:"source"`
}

type connectResult struct {
	Request  string          `json:"request,omitempty"`
	Status   string          `json:"status"`
	Next     string          `json:"next,omitempty"`
	RaisedOn string          `json:"raisedOn,omitempty"`
	Repos    []connectedRepo `json:"repos"`
}

type connectedRepo struct {
	Path  string `json:"path,omitempty"`
	URL   string `json:"url,omitempty"`
	State string `json:"state"`
}

type connectAnswerArgs struct {
	Request string       `json:"request"`
	Answers []repoAnswer `json:"answers"`
	Seen    string       `json:"seen"`
	Key     string       `json:"key"`
}

type repoAnswer struct {
	Path      string `json:"path"`
	Action    string `json:"action"`
	OtherPath string `json:"otherPath"`
}

type repoFactsHead struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type cloneRef struct {
	workspace string
	op        string
	into      string
}

func (a *app) connectRepoCommand(ctx context.Context, cmd pluginsdk.CommandRun, args connectRepoArgs) (any, error) {
	caller, err := a.sessionCaller(cmd)
	if err != nil {
		return nil, err
	}
	repos, err := a.askedRepos(ctx, cmd.WorkspaceID, args.Repos)
	if err != nil {
		return nil, err
	}
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	owner := a.topLevel(caller)
	w, err := a.host.GetWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, err
	}
	rec := owner.snapshot()
	for i := range repos {
		w = a.arrive(ctx, w, owner, rec, &repos[i])
		if repos[i].URL != "" && len(w.Folders) > 0 {
			repos[i].Into = cloneInto(w.Folders[0].Path, repos[i].Name)
		}
	}
	if len(owner.snapshot().AfkConnected) != len(rec.AfkConnected) {
		a.saveNow(ctx, owner)
	}
	res := connectResult{Status: repoConnected, Repos: stateOf(repos)}
	waiting := slices.DeleteFunc(slices.Clone(repos), func(r connectRepo) bool { return r.State != repoPending })
	if len(waiting) > 0 {
		req := connectRequest{ID: id.New(connectIDPrefix), At: time.Now().UTC(), Repos: waiting}
		res.Status, res.Request, res.Next = repoPending, req.ID, connectNextText
		if owner != caller {
			from := caller.snapshot()
			req.From, req.FromTitle = from.ID, from.Title
			res.RaisedOn, res.Next = rec.ID, fmt.Sprintf(connectChildFormat, firstLine(rec.Title), rec.ID)
		}
		owner.addConnect(req)
	}
	a.finishConnects(ctx, cmd.WorkspaceID)
	if owner.snapshot().connectWaits() {
		owner.mu.Lock()
		t := owner.turnLocked()
		owner.mu.Unlock()
		a.notifyEnd(owner, t)
	}
	return res, nil
}

func (a *app) connectAnswerCommand(ctx context.Context, cmd pluginsdk.CommandRun, args connectAnswerArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errConnectUser
	}
	return a.once(ctx, cmd, args.Key, func() (any, error) {
		return nil, a.answerConnect(ctx, cmd.WorkspaceID, args)
	})
}

func (a *app) answerConnect(ctx context.Context, workspaceID string, args connectAnswerArgs) error {
	answers, err := answersByRepo(args.Answers)
	if err != nil {
		return err
	}
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	owner, req, ok := a.findConnect(workspaceID, args.Request)
	if !ok || !req.waits() {
		return errConnectStale
	}
	if args.Seen != "" && args.Seen != req.seen() {
		return errConnectSeen
	}
	if err := req.check(answers); err != nil {
		return err
	}
	w, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	title := owner.snapshot().Title
	for _, repo := range req.Repos {
		if repo.State != repoPending {
			continue
		}
		ans, ticked := answers[repo.key()]
		switch {
		case !ticked || ans.Action == answerSkip:
			setRepo(owner, req.ID, repo, repoSkipped, "")
		case repo.URL != "":
			a.queueClone(w, repo, ans.OtherPath)
		default:
			w = a.connectPath(ctx, w, owner, req.ID, repo, cmp.Or(ans.OtherPath, repo.Path), title)
		}
	}
	a.finishConnects(ctx, workspaceID)
	return nil
}

func (a *app) userWord(cmd pluginsdk.CommandRun) bool {
	return !cmd.Agent && a.session(cmd.Caller) == nil && (cmd.Via == "" || asUser(cmd))
}

func (a *app) sessionCaller(cmd pluginsdk.CommandRun) (*session, error) {
	if cmd.Caller == "" {
		return nil, errNeedsSession
	}
	s, err := a.lookup(cmd.WorkspaceID, cmd.Caller)
	if err != nil {
		return nil, err
	}
	if s.snapshot().restricted() {
		return nil, errServiceRepo
	}
	return s, nil
}

func (a *app) topLevel(s *session) *session {
	top := s
	for rec := range a.ancestry(s.snapshot()) {
		if p := a.handedTo(a.session(rec.ID)); p != nil && !p.snapshot().closed() {
			top = p
		}
	}
	return top
}

func (a *app) handedTo(s *session) *session {
	for s != nil && s.snapshot().HandoffTo != "" {
		s = a.session(s.snapshot().HandoffTo)
	}
	return s
}

func (a *app) askedRepos(ctx context.Context, workspaceID string, asks []connectAsk) ([]connectRepo, error) {
	if len(asks) == 0 {
		return nil, errConnectEmpty
	}
	out := make([]connectRepo, len(asks))
	errs := make([]error, len(asks))
	var wg sync.WaitGroup
	for i, ask := range asks {
		wg.Go(func() { out[i], errs[i] = a.askedRepo(ctx, workspaceID, ask) })
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

func (a *app) askedRepo(ctx context.Context, workspaceID string, ask connectAsk) (connectRepo, error) {
	if (ask.Path == "") == (ask.URL == "") {
		return connectRepo{}, errConnectBoth
	}
	if ask.LandsBy != "" && !validLandsBy(ask.LandsBy) {
		return connectRepo{}, fmt.Errorf("%w: %s", errLandsBy, ask.LandsBy)
	}
	repo := connectRepo{Reason: clipRunes(ask.Reason, connectTextRunes), LandsBy: ask.LandsBy, Source: clipRunes(ask.Source, connectTextRunes), State: repoPending}
	if ask.URL != "" {
		if remoteHelper.MatchString(ask.URL) {
			return connectRepo{}, fmt.Errorf("%w: %s", errConnectHelper, ask.URL)
		}
		name, err := repoNameOf(ask.URL)
		repo.URL, repo.Name = ask.URL, name
		return repo, err
	}
	if !filepath.IsAbs(ask.Path) {
		return connectRepo{}, fmt.Errorf("%w: %s", errRelativeRepo, ask.Path)
	}
	var facts json.RawMessage
	if err := a.host.RunCommand(ctx, repoFactsCmd, workspaceID, map[string]string{"path": ask.Path}, &facts); err != nil {
		return connectRepo{}, err
	}
	var head repoFactsHead
	if err := json.Unmarshal(facts, &head); err != nil {
		return connectRepo{}, err
	}
	repo.Path, repo.Name, repo.Facts = head.Path, head.Name, facts
	return repo, nil
}

func (a *app) arrive(ctx context.Context, w pluginsdk.Workspace, owner *session, rec record, repo *connectRepo) pluginsdk.Workspace {
	if _, ok := folderIn(w, repo.Path); ok {
		repo.State = repoConnected
		return w
	}
	if repo.Path == "" {
		return w
	}
	if !rec.userAFK() {
		return w
	}
	return a.connectAway(ctx, w, owner, rec, repo)
}

func (a *app) connectAway(ctx context.Context, w pluginsdk.Workspace, owner *session, rec record, repo *connectRepo) pluginsdk.Workspace {
	title := fmt.Sprintf(connectAFKTitle, repo.Name)
	next, err := a.host.AddFolder(ctx, w.ID, repo.Path, pluginsdk.FolderMeta{AddedBy: rec.Title, AddedAfk: true})
	if err != nil {
		repo.Note = err.Error()
		return w
	}
	repo.State, repo.Folder = repoConnected, repo.Path
	owner.noteAfkConnected(repo.Path)
	owner.noticeUpdate(noticeUpdate{SessionUpdate: acp.UpdateNotice, Title: title, Description: repo.Path, Repo: repo.Name, Path: repo.Path})
	a.repoConnected(w.ID, *repo, repo.Path)
	return next
}

func (a *app) connectWaitingAway(ctx context.Context, s *session) {
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	rec := s.snapshot()
	if !rec.userAFK() || !slices.ContainsFunc(rec.ConnectRequests, connectRequest.waits) {
		return
	}
	w, err := a.host.GetWorkspace(ctx, rec.Workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: connect while away:", err)
		return
	}
	for _, req := range rec.ConnectRequests {
		for _, repo := range req.Repos {
			if repo.State != repoPending || repo.Path == "" {
				continue
			}
			if folder, ok := folderIn(w, repo.Path); ok {
				a.repoConnected(w.ID, repo, folder)
				continue
			}
			if w = a.connectAway(ctx, w, s, rec, &repo); repo.State != repoConnected {
				setRepo(s, req.ID, repo, repoPending, repo.Note)
			}
		}
	}
	a.saveNow(ctx, s)
	a.finishConnects(ctx, rec.Workspace)
}

func (a *app) connectPath(ctx context.Context, w pluginsdk.Workspace, owner *session, request string, repo connectRepo, folder, title string) pluginsdk.Workspace {
	if _, ok := folderIn(w, folder); !ok {
		next, err := a.host.AddFolder(ctx, w.ID, folder, pluginsdk.FolderMeta{AddedBy: title})
		if err != nil {
			setRepo(owner, request, repo, repoFailed, err.Error())
			return w
		}
		w = next
	}
	a.repoConnected(w.ID, repo, folder)
	return w
}

func (a *app) queueClone(w pluginsdk.Workspace, repo connectRepo, other string) {
	into := cmp.Or(other, cloneInto(w.Folders[0].Path, repo.Name))
	a.editRepos(w.ID, func(_ connectRequest, r *connectRepo) bool {
		if r.State != repoPending || r.URL != repo.URL {
			return false
		}
		r.State, r.Folder = repoCloning, into
		return true
	})
	a.startCloner(w.ID)
}

func cloneInto(firstFolder, name string) string {
	return filepath.Join(filepath.Dir(firstFolder), name)
}

func (a *app) startCloner(workspaceID string) {
	if a.cloners == nil {
		a.cloners = map[string]bool{}
	}
	if !a.cloners[workspaceID] {
		a.cloners[workspaceID] = true
		go a.runClones(workspaceID)
	}
}

func (a *app) runClones(workspaceID string) {
	for {
		c, ok := a.nextClone(workspaceID)
		if !ok {
			return
		}
		a.awaitClone(c)
	}
}

func (a *app) nextClone(workspaceID string) (cloneRef, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	for {
		repo, ok := a.findCloning(workspaceID)
		if !ok {
			delete(a.cloners, workspaceID)
			return cloneRef{}, false
		}
		c := cloneRef{workspace: workspaceID, op: repo.Op, into: repo.Folder}
		if c.op != "" {
			return c, true
		}
		var op worktreeOp
		err := a.host.RunCommand(ctx, gitCloneCmd, workspaceID, map[string]string{"url": repo.URL, "into": repo.Folder}, &op)
		a.editRepos(workspaceID, func(_ connectRequest, r *connectRepo) bool {
			if r.State != repoCloning || r.URL != repo.URL {
				return false
			}
			r.Op = op.ID
			if err != nil {
				r.State, r.Note = repoFailed, err.Error()
			}
			return true
		})
		if err == nil {
			c.op = op.ID
			a.finishConnects(ctx, workspaceID)
			return c, true
		}
		a.finishConnects(ctx, workspaceID)
	}
}

func (a *app) findCloning(workspaceID string) (connectRepo, bool) {
	for _, s := range a.workspaceSessions(workspaceID) {
		for _, req := range s.snapshot().ConnectRequests {
			if i := slices.IndexFunc(req.Repos, func(r connectRepo) bool { return r.State == repoCloning }); i >= 0 {
				return req.Repos[i], true
			}
		}
	}
	return connectRepo{}, false
}

func (a *app) awaitClone(c cloneRef) {
	waited, stop := context.WithTimeout(context.Background(), cloneLimit)
	defer stop()
	_, err := a.awaitOp(waited, c.workspace, c.op)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err != nil && cloned(ctx, c.into) {
		err = nil
	}
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	a.cloneDone(ctx, c, err)
	a.finishConnects(ctx, c.workspace)
}

func (a *app) cloneDone(ctx context.Context, c cloneRef, failed error) {
	owner, repo, ok := a.findClone(c)
	if !ok {
		return
	}
	if failed == nil {
		var w pluginsdk.Workspace
		if w, failed = a.host.GetWorkspace(ctx, c.workspace); failed == nil {
			if _, in := folderIn(w, c.into); !in {
				_, failed = a.host.AddFolder(ctx, c.workspace, c.into, pluginsdk.FolderMeta{AddedBy: owner.snapshot().Title})
			}
		}
	}
	if failed == nil {
		a.repoConnected(c.workspace, repo, c.into)
		return
	}
	a.editRepos(c.workspace, func(_ connectRequest, r *connectRepo) bool {
		if r.Op != c.op {
			return false
		}
		r.State, r.Note = repoFailed, failed.Error()
		return true
	})
}

func (a *app) findClone(c cloneRef) (*session, connectRepo, bool) {
	for _, s := range a.workspaceSessions(c.workspace) {
		for _, req := range s.snapshot().ConnectRequests {
			if i := slices.IndexFunc(req.Repos, func(r connectRepo) bool { return r.Op == c.op && r.State == repoCloning }); i >= 0 {
				return s, req.Repos[i], true
			}
		}
	}
	return nil, connectRepo{}, false
}

func cloned(ctx context.Context, into string) bool {
	if top, ok := gitrepo.RepoRoot(into); !ok || top != into {
		return false
	}
	_, err := gitrepo.Run(ctx, into, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func (a *app) resumeClones() {
	var recs []record
	a.mu.Lock()
	for _, s := range a.sessions {
		recs = append(recs, s.snapshot())
	}
	a.mu.Unlock()
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	for _, workspaceID := range workspacesCloning(recs) {
		a.startCloner(workspaceID)
	}
}

func workspacesCloning(recs []record) []string {
	var out []string
	for _, rec := range recs {
		cloning := slices.ContainsFunc(rec.ConnectRequests, func(req connectRequest) bool {
			return slices.ContainsFunc(req.Repos, func(r connectRepo) bool { return r.State == repoCloning })
		})
		if cloning && !slices.Contains(out, rec.Workspace) {
			out = append(out, rec.Workspace)
		}
	}
	return out
}

func (a *app) repoConnected(workspaceID string, repo connectRepo, folder string) {
	asked := []string{gitrepo.Canonical(folder)}
	if repo.Path != "" {
		asked = append(asked, gitrepo.Canonical(repo.Path))
	}
	a.editRepos(workspaceID, func(_ connectRequest, r *connectRepo) bool {
		same := r.URL != "" && r.URL == repo.URL || r.Path != "" && slices.Contains(asked, gitrepo.Canonical(r.Path))
		if !same || r.State != repoPending && r.State != repoCloning {
			return false
		}
		r.State, r.Folder, r.Op = repoConnected, folder, ""
		return true
	})
}

func (a *app) editRepos(workspaceID string, edit func(connectRequest, *connectRepo) bool) {
	for _, s := range a.workspaceSessions(workspaceID) {
		s.editRepos(edit)
	}
}

func setRepo(s *session, request string, repo connectRepo, state, note string) {
	s.editRepos(func(req connectRequest, r *connectRepo) bool {
		if req.ID != request || r.key() != repo.key() {
			return false
		}
		r.State, r.Note = state, note
		return true
	})
}

func (a *app) finishConnects(ctx context.Context, workspaceID string) {
	for _, s := range a.workspaceSessions(workspaceID) {
		if len(s.snapshot().ConnectRequests) == 0 {
			continue
		}
		done := s.takeFinishedConnects()
		a.saveNow(ctx, s)
		for _, req := range done {
			if err := a.submit(s, &promptArgs{Text: req.outcome()}); err != nil {
				fmt.Fprintln(os.Stderr, "agents: connect outcome:", err)
			}
		}
	}
}

func (a *app) saveNow(ctx context.Context, s *session) {
	a.notes.persist(ctx, s)
	s.changed(changeStatus, true)
}

func (a *app) findConnect(workspaceID, request string) (*session, connectRequest, bool) {
	for _, s := range a.workspaceSessions(workspaceID) {
		reqs := s.snapshot().ConnectRequests
		if i := slices.IndexFunc(reqs, func(r connectRequest) bool { return r.ID == request }); i >= 0 {
			return s, reqs[i], true
		}
	}
	return nil, connectRequest{}, false
}

func (a *app) workspaceSessions(workspaceID string) []*session {
	recs := a.list(workspaceID)
	out := make([]*session, 0, len(recs))
	for _, rec := range recs {
		if s := a.session(rec.ID); s != nil {
			out = append(out, s)
		}
	}
	return out
}

func (s *session) addConnect(req connectRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec.ConnectRequests = append(slices.Clone(s.rec.ConnectRequests), req)
}

func (s *session) editRepos(edit func(connectRequest, *connectRepo) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := slices.Clone(s.rec.ConnectRequests)
	for i := range reqs {
		reqs[i].Repos = slices.Clone(reqs[i].Repos)
		for j := range reqs[i].Repos {
			edit(reqs[i], &reqs[i].Repos[j])
		}
	}
	s.rec.ConnectRequests = reqs
}

func (s *session) takeFinishedConnects() []connectRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var done, left []connectRequest
	for _, req := range s.rec.ConnectRequests {
		if req.open() {
			left = append(left, req)
		} else {
			done = append(done, req)
		}
	}
	s.rec.ConnectRequests = left
	return done
}

func (r connectRequest) check(answers map[string]repoAnswer) error {
	for key, ans := range answers {
		i := slices.IndexFunc(r.Repos, func(repo connectRepo) bool { return repo.key() == key })
		if i < 0 {
			return fmt.Errorf("%w: %s", errConnectRepo, key)
		}
		if _, ok := gitrepo.RepoRoot(ans.OtherPath); ans.Action == answerOther && r.Repos[i].Path != "" && !ok {
			return fmt.Errorf("%w: %s", errConnectOther, ans.OtherPath)
		}
	}
	return nil
}

func (r connectRequest) open() bool {
	return slices.ContainsFunc(r.Repos, func(repo connectRepo) bool { return repo.State == repoPending || repo.State == repoCloning })
}

func (r connectRequest) outcome() string {
	head := connectOutcomeHead
	if r.From != "" {
		head = fmt.Sprintf(connectChildHead, firstLine(r.FromTitle), r.From)
	}
	lines := make([]string, 0, len(r.Repos)+1)
	lines = append(lines, head)
	for _, repo := range r.Repos {
		lines = append(lines, repo.outcome())
	}
	return strings.Join(lines, "\n")
}

func (r connectRepo) key() string {
	return cmp.Or(r.Path, r.URL)
}

func (r connectRepo) outcome() string {
	line := "- " + r.key() + ": " + r.State
	if r.State == repoConnected && r.Folder != "" && r.Folder != r.Path {
		line += " at " + r.Folder
	}
	if r.Note != "" {
		line += " (" + r.Note + ")"
	}
	return line
}

func (r record) userAFK() bool {
	return r.AFK && r.AFKBy == ""
}

func (r record) connectWaits() bool {
	return r.turnEnded() && slices.ContainsFunc(r.ConnectRequests, connectRequest.waits)
}

func (r connectRequest) seen() string {
	h := fnv.New64a()
	for _, repo := range r.Repos {
		if repo.State == repoPending {
			fmt.Fprintln(h, repo.key(), repo.Note)
		}
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

func (r connectRequest) waits() bool {
	return slices.ContainsFunc(r.Repos, func(repo connectRepo) bool { return repo.State == repoPending })
}

func (r record) connectIDs() []string {
	var out []string
	for _, req := range r.ConnectRequests {
		if req.waits() {
			out = append(out, req.ID)
		}
	}
	return out
}

func (r record) connectSummary() *pendingSummary {
	var repos []string
	var at time.Time
	for _, req := range r.ConnectRequests {
		if !req.waits() {
			continue
		}
		if at.IsZero() || req.At.Before(at) {
			at = req.At
		}
		for _, repo := range req.Repos {
			if repo.State == repoPending && !slices.Contains(repos, repo.Name) {
				repos = append(repos, repo.Name)
			}
		}
	}
	text, subject := fmt.Sprintf(connectManyFormat, len(repos)), counted(len(repos), "repo")
	if len(repos) == 1 {
		text, subject = fmt.Sprintf(connectOneFormat, repos[0]), repos[0]
	}
	return &pendingSummary{Kind: pendingConnect, Text: text, Lead: connectWants, Subject: subject, At: at, OnMac: true}
}

func stateOf(repos []connectRepo) []connectedRepo {
	out := make([]connectedRepo, 0, len(repos))
	for _, r := range repos {
		out = append(out, connectedRepo{Path: r.Path, URL: r.URL, State: r.State})
	}
	return out
}

func answersByRepo(answers []repoAnswer) (map[string]repoAnswer, error) {
	out := make(map[string]repoAnswer, len(answers))
	for _, ans := range answers {
		switch ans.Action {
		case answerConnect, answerSkip:
			ans.OtherPath = ""
		case answerOther:
			if !filepath.IsAbs(ans.OtherPath) {
				return nil, errConnectAnswer
			}
			ans.OtherPath = filepath.Clean(ans.OtherPath)
		default:
			return nil, fmt.Errorf("%w: %s", errConnectAnswer, ans.Action)
		}
		out[ans.Path] = ans
	}
	return out, nil
}

func repoNameOf(url string) (string, error) {
	name := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	name = name[strings.LastIndexAny(name, "/:")+1:]
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("%w: %s", errConnectName, url)
	}
	return name, nil
}

func folderIn(w pluginsdk.Workspace, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	target := gitrepo.Canonical(filepath.Clean(path))
	for _, folder := range w.Paths() {
		if gitrepo.Canonical(folder) == target {
			return folder, true
		}
	}
	return "", false
}

func validLandsBy(rule string) bool {
	return rule == landsByPR || rule == landsByMerge || rule == landsByBranch
}
