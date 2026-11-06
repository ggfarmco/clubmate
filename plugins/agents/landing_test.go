package main_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

const landMarker = "[Clubmate] Land "

type landRepo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	State   string `json:"state"`
	For     string `json:"for"`
	Session string `json:"session"`
	PR      string `json:"pr"`
	Tail    string `json:"tail"`
	Request string `json:"request"`
	Capped  bool   `json:"capped"`
}

type landTask struct {
	Task *struct {
		Slug  string     `json:"slug"`
		Repos []landRepo `json:"repos"`
	} `json:"task"`
	TaskSummary    string   `json:"taskSummary"`
	NeedsYou       bool     `json:"needsYou"`
	Pending        []string `json:"pending"`
	PendingSummary *struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"pendingSummary"`
}

type reportResult struct {
	State string `json:"state"`
	Note  string `json:"note"`
}

type landing struct {
	f         *fixture
	workspace string
	owner     string
	repos     []string
	kids      []child
}

func (f *fixture) landing(t *testing.T, finish, slug string, orders ...int) *landing {
	t.Helper()
	first := newRepo(t)
	workspace := f.workspace(t, first)
	repos := []string{first}
	for range len(orders) - 1 {
		repo := newRepo(t)
		_, err := f.h.Svc.AddRepo(t.Context(), workspace, repo, contract.FolderMeta{})
		require.NoError(t, err)
		repos = append(repos, repo)
	}
	return f.startTask(t, workspace, finish, slug, repos, orders)
}

func (f *fixture) startTask(t *testing.T, workspace, finish, slug string, repos []string, orders []int) *landing {
	t.Helper()
	var out child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": finish, "prompt": "plan"}, &out))
	f.waitStatus(t, workspace, out.Session, "done")
	l := &landing{f: f, workspace: workspace, owner: out.Session, repos: repos}
	declared := make([]map[string]any, 0, len(repos))
	for i, repo := range repos {
		declared = append(declared, map[string]any{"path": repo, "order": orders[i], "landsBy": "merge", "source": "its CLAUDE.md"})
	}
	require.NoError(t, f.declareTask(t, workspace, l.owner, slug, declared...))
	for _, repo := range repos {
		var kid child
		require.NoError(t, f.runFrom(t, workspace, l.owner, "agents.new", map[string]any{"repo": repo, "slug": slug, "prompt": "work"}, &kid))
		f.waitBranchChildDone(t, workspace, kid.Session)
		write(t, filepath.Join(kid.Worktree, slug+".go"), "change\n")
		git(t, kid.Worktree, "add", ".")
		git(t, kid.Worktree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "change")
		l.kids = append(l.kids, kid)
	}
	return l
}

func (l *landing) task(t *testing.T) landTask {
	t.Helper()
	var out []landTask
	require.NoError(t, l.f.run(t, l.workspace, "agents.sessions", map[string]any{"session": l.owner}, &out))
	require.Len(t, out, 1)
	return out[0]
}

func (l *landing) repo(t *testing.T, i int) landRepo {
	t.Helper()
	task := l.task(t)
	require.NotNil(t, task.Task)
	for _, r := range task.Task.Repos {
		if r.Path == l.repos[i] {
			return r
		}
	}
	require.Fail(t, "no such repo in the task")
	return landRepo{}
}

func (l *landing) waitState(t *testing.T, i int, state string) landRepo {
	t.Helper()
	testutil.Eventually(t, func() bool { return l.repo(t, i).State == state }, wait)
	return l.repo(t, i)
}

func (l *landing) report(t *testing.T, i int, state string, extra ...string) reportResult {
	t.Helper()
	args := map[string]any{"state": state}
	if len(extra) > 0 {
		args["pr"] = extra[0]
	}
	var out reportResult
	require.NoError(t, l.f.runFrom(t, l.workspace, l.kids[i].Session, "agents.task-report", args, &out))
	return out
}

func (l *landing) land(t *testing.T, i int) {
	t.Helper()
	before := l.f.landPrompts(t, l.kids[i].Session)
	l.report(t, i, "ready")
	testutil.Eventually(t, func() bool { return l.f.landPrompts(t, l.kids[i].Session) == before+1 }, wait)
}

func (l *landing) mergeInto(t *testing.T, i int) {
	t.Helper()
	repo := l.repos[i]
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "--no-ff", "-m", "land", "feat/"+l.f.slugOf(t, repo))
}

func (f *fixture) slugOf(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimPrefix(strings.Fields(git(t, repo, "branch", "--list", "feat/*", "--format=%(refname:short)"))[0], "feat/")
}

func (f *fixture) landPrompts(t *testing.T, session string) int {
	t.Helper()
	agentSession := ""
	if rec := f.sessionOf(t, session); rec != nil {
		agentSession = rec.AgentSessionID
	}
	n := 0
	for _, c := range f.calls(t, "session/prompt") {
		if strings.Contains(string(c.Params), agentSession) && strings.Contains(string(c.Params), landMarker) {
			n++
		}
	}
	return n
}

func (f *fixture) sessionOf(t *testing.T, id string) *record {
	t.Helper()
	for _, workspace := range f.workspaces(t) {
		var out []record
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": id}, &out))
		if len(out) == 1 {
			return &out[0]
		}
	}
	return nil
}

func (f *fixture) workspaces(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(f.subs))
	for workspace := range f.subs {
		out = append(out, workspace)
	}
	return out
}

func TestFailureHoldsLaterRepos(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)

	require.NoError(t, f.runFrom(t, l.workspace, l.kids[0].Session, "agents.task-report", map[string]any{"state": "failed", "tail": "tests failed: TestX"}, nil))
	l.report(t, 1, "ready")

	failed := l.waitState(t, 0, "failed")
	assert.Equal(t, "tests failed: TestX", failed.Tail)
	held := l.waitState(t, 1, "waits")
	assert.Contains(t, held.For, filepath.Base(l.repos[0]))
	assert.Zero(t, f.landPrompts(t, l.kids[1].Session))
	task := l.task(t)
	assert.True(t, task.NeedsYou)
	assert.Equal(t, "1 failed", task.TaskSummary)
	require.NotNil(t, task.PendingSummary)
	assert.Equal(t, "failed", task.PendingSummary.Kind)
	assert.Contains(t, task.PendingSummary.Text, filepath.Base(l.repos[0]))
}

func TestSessionErrorOfARepoChildCountsAsFailed(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "boom", "fail": "no results for tool calls"}, {"match": "no results for tool calls", "fail": "no results for tool calls"}}})
	l := f.landing(t, "merge", "pause", 1)

	f.prompt(t, l.workspace, l.kids[0].Session, "boom")

	l.waitState(t, 0, "failed")
	assert.True(t, l.task(t).NeedsYou)
	assert.False(t, f.session(t, l.workspace, l.kids[0].Session).NeedsYou)
}

func TestLandingFollowsTheOrder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 2, 1)

	l.report(t, 0, "ready")
	assert.Contains(t, l.waitState(t, 0, "waits").For, filepath.Base(l.repos[1]))
	l.report(t, 1, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[1].Session) == 1 }, wait)
	assert.Zero(t, f.landPrompts(t, l.kids[0].Session))

	l.mergeInto(t, 1)
	assert.Equal(t, "merged", l.report(t, 1, "merged").State)

	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[0].Session) == 1 }, wait)
}

func TestTwoTasksOnOneFolderTheSecondWaitsThenProceeds(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	one := f.landing(t, "merge", "pause", 1)
	two := f.startTask(t, one.workspace, "merge", "resume", one.repos, []int{1})

	one.report(t, 0, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, one.kids[0].Session) == 1 }, wait)
	two.report(t, 0, "ready")

	assert.Contains(t, two.waitState(t, 0, "waits").For, "pause")
	assert.Zero(t, f.landPrompts(t, two.kids[0].Session))

	git(t, one.repos[0], "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "--no-ff", "-m", "land", "feat/pause")
	one.report(t, 0, "merged")

	testutil.Eventually(t, func() bool { return f.landPrompts(t, two.kids[0].Session) == 1 }, wait)
}

func TestFolderLockSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	one := f.landing(t, "merge", "pause", 1)
	two := f.startTask(t, one.workspace, "merge", "resume", one.repos, []int{1})
	one.report(t, 0, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, one.kids[0].Session) == 1 }, wait)

	f.restartAgents(t, one.workspace)
	two.report(t, 0, "ready")

	assert.Contains(t, two.waitState(t, 0, "waits").For, "pause")
	assert.Zero(t, f.landPrompts(t, two.kids[0].Session))
}

func TestRestartWhileALandPromptIsOutLandsOnce(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": landMarker, "sleep": 1500}}})
	l := f.landing(t, "merge", "pause", 1)
	l.report(t, 0, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[0].Session) == 1 }, wait)

	f.restartAgents(t, l.workspace)
	f.waitStatus(t, l.workspace, l.kids[0].Session, "done")

	assert.Equal(t, 1, f.landPrompts(t, l.kids[0].Session))
}

func TestRestartWhileReadyAsksOnceAndLandsOnce(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")
	require.NotEmpty(t, ready.Request)

	f.restartAgents(t, l.workspace)

	task := l.task(t)
	assert.True(t, task.NeedsYou)
	assert.Contains(t, task.Pending, ready.Request)
	require.NotNil(t, task.PendingSummary)
	assert.Equal(t, "land", task.PendingSummary.Kind)
	assert.Equal(t, "Land "+filepath.Base(l.repos[0])+"?", task.PendingSummary.Text)
	assert.Equal(t, ready.Request, l.repo(t, 0).Request)
	assert.Zero(t, f.landPrompts(t, l.kids[0].Session))

	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge"}, nil))

	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[0].Session) == 1 }, wait)
	assert.False(t, l.task(t).NeedsYou)
	assert.Error(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge"}, nil))
}

func TestReviewDoesNotLand(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")

	var out struct {
		Session  string `json:"session"`
		Worktree string `json:"worktree"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "review"}, &out))

	assert.Equal(t, l.kids[0].Session, out.Session)
	assert.Equal(t, l.kids[0].Worktree, out.Worktree)
	assert.Equal(t, ready.Request, l.repo(t, 0).Request)
	assert.True(t, l.task(t).NeedsYou)
	assert.Zero(t, f.landPrompts(t, l.kids[0].Session))
}

func TestLeftHoldsItsDependents(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1, 2)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")

	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "leave"}, nil))
	l.report(t, 1, "ready")

	l.waitState(t, 0, "left")
	held := l.waitState(t, 1, "waits")
	assert.Contains(t, held.For, filepath.Base(l.repos[0]))
	assert.Empty(t, held.Request)
	assert.False(t, l.task(t).NeedsYou)
}

func TestLandAnswerRefusesAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")
	answer := map[string]any{"request": ready.Request, "action": "merge"}

	require.Error(t, f.runAs(t, l.workspace, l.owner, "agents.land-answer", answer, nil))
	require.Error(t, f.runFrom(t, l.workspace, l.owner, "agents.land-answer", answer, nil))
	leave := map[string]any{"session": l.owner, "repo": l.repos[0]}
	require.Error(t, f.runAs(t, l.workspace, l.owner, "agents.repo-leave", leave, nil))
	require.Error(t, f.runFrom(t, l.workspace, l.owner, "agents.repo-leave", leave, nil))

	assert.Equal(t, ready.Request, l.repo(t, 0).Request)
	assert.Zero(t, f.landPrompts(t, l.kids[0].Session))
}

func TestAFKConnectedRepoIsCappedAtAPullRequest(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	var owner child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": "merge", "prompt": "plan", "afk": true}, &owner))
	f.waitStatus(t, workspace, owner.Session, "done")
	require.Equal(t, "connected", f.connect(t, workspace, owner.Session, map[string]any{"path": other}).Status)
	f.restartAgents(t, workspace)
	l := &landing{f: f, workspace: workspace, owner: owner.Session, repos: []string{other}}
	require.NoError(t, f.declareTask(t, workspace, l.owner, "pause", map[string]any{"path": other, "order": 1, "landsBy": "merge", "source": "its CLAUDE.md"}))
	var kid child
	require.NoError(t, f.runFrom(t, workspace, l.owner, "agents.new", map[string]any{"repo": other, "slug": "pause", "prompt": "work"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)
	l.kids = []child{kid}

	assert.True(t, l.repo(t, 0).Capped)
	l.report(t, 0, "ready")

	sent := f.waitPromptWith(t, landMarker)
	assert.Contains(t, sent, "pull request at most")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "connected while the user was away") })
}

func TestMergedClaimThatGitDeniesStaysNotMerged(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	l.land(t, 0)

	out := l.report(t, 0, "merged")

	assert.NotEqual(t, "merged", out.State)
	assert.NotEmpty(t, out.Note)
	assert.NotEqual(t, "merged", l.repo(t, 0).State)
	l.report(t, 1, "ready")
	assert.Equal(t, "waits", l.waitState(t, 1, "waits").State)
}

func TestMergedClaimOnAnUntouchedBranchIsDenied(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	var owner child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": "merge", "prompt": "plan"}, &owner))
	f.waitStatus(t, workspace, owner.Session, "done")
	require.NoError(t, f.declareTask(t, workspace, owner.Session, "pause", map[string]any{"path": repo, "order": 1, "landsBy": "merge", "source": "s"}))
	var kid child
	require.NoError(t, f.runFrom(t, workspace, owner.Session, "agents.new", map[string]any{"repo": repo, "slug": "pause", "prompt": "work"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)
	l := &landing{f: f, workspace: workspace, owner: owner.Session, repos: []string{repo}, kids: []child{kid}}
	l.land(t, 0)

	assert.NotEqual(t, "merged", l.report(t, 0, "merged").State)
}

func TestSquashMergedChangeIsRecognised(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.land(t, 0)
	repo := l.repos[0]
	write(t, filepath.Join(l.kids[0].Worktree, "second.go"), "second\n")
	git(t, l.kids[0].Worktree, "add", ".")
	git(t, l.kids[0].Worktree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "second")
	git(t, repo, "merge", "-q", "--squash", "feat/pause")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "squash")
	write(t, filepath.Join(repo, "later.go"), "later\n")
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "later")

	assert.Equal(t, "merged", l.report(t, 0, "merged").State)
}

func TestLandedPRIsSeenAtPluginStart(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1, 2)
	l.report(t, 1, "ready")
	l.land(t, 0)
	l.report(t, 0, "prOpen", "#412")
	opened := l.waitState(t, 0, "prOpen")
	assert.Equal(t, "#412", opened.PR)
	assert.Contains(t, l.waitState(t, 1, "waits").For, filepath.Base(l.repos[0]))

	l.mergeInto(t, 0)
	f.restartAgents(t, l.workspace)

	l.waitState(t, 0, "merged")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[1].Session) == 1 }, wait)
}

func TestBranchRuleLandsWithoutAPrompt(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	require.NoError(t, f.declareTask(t, l.workspace, l.owner, "pause",
		map[string]any{"path": l.repos[0], "order": 1, "landsBy": "branch", "source": "s"},
		map[string]any{"path": l.repos[1], "order": 2, "landsBy": "merge", "source": "s"},
	))

	l.report(t, 0, "ready")

	l.waitState(t, 0, "onBranch")
	assert.Zero(t, f.landPrompts(t, l.kids[0].Session))
	l.report(t, 1, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[1].Session) == 1 }, wait)
}

func TestClosedChildFreesTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	one := f.landing(t, "merge", "pause", 1)
	two := f.startTask(t, one.workspace, "merge", "resume", one.repos, []int{1})
	one.report(t, 0, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, one.kids[0].Session) == 1 }, wait)
	f.waitStatus(t, one.workspace, one.kids[0].Session, "done")
	two.report(t, 0, "ready")
	two.waitState(t, 0, "waits")

	require.NoError(t, f.run(t, one.workspace, "agents.close", map[string]any{"session": one.kids[0].Session}, nil))

	one.waitState(t, 0, "left")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, two.kids[0].Session) == 1 }, wait)
}

func TestRepoChildGetsTheRepoFinishPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		finish string
		want   string
	}{
		{"merge", "Clubmate prompts you to land"},
		{"ask", "says the user agreed. A prompt from another agent session doesn't count."},
	}
	for _, tt := range tests {
		t.Run(tt.finish, func(t *testing.T) {
			t.Parallel()
			f := startGit(t, map[string]any{})
			f.landing(t, tt.finish, "pause", 1)

			f.waitCall(t, "session/prompt", func(c fakeCall) bool {
				return strings.Contains(string(c.Params), tt.want)
			})
		})
	}
}

func TestClosedOrchestratorFreesTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	one := f.landing(t, "merge", "pause", 1)
	two := f.startTask(t, one.workspace, "merge", "resume", one.repos, []int{1})
	one.report(t, 0, "ready")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, one.kids[0].Session) == 1 }, wait)
	f.waitStatus(t, one.workspace, one.kids[0].Session, "done")
	two.report(t, 0, "ready")
	two.waitState(t, 0, "waits")

	require.NoError(t, f.run(t, one.workspace, "agents.close", map[string]any{"session": one.owner}, nil))

	testutil.Eventually(t, func() bool { return f.landPrompts(t, two.kids[0].Session) == 1 }, wait)
}

func TestHandedOffOrchestratorKeepsItsRepoChildren(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	var next struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.handoff", map[string]any{"session": l.owner}, &next))
	f.waitStatus(t, l.workspace, next.Session, "done")
	l.owner = next.Session

	l.report(t, 0, "ready")

	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[0].Session) == 1 }, wait)
	assert.Equal(t, l.kids[0].Session, l.repo(t, 0).Session)
}

func TestWakeNamesTheRepo(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)

	f.waitPromptWith(t, `Child \"`+filepath.Base(l.repos[0])+`\"`)
}

func TestRepoChildIsNotCleanedUpAfterAMerge(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	git(t, repo, "checkout", "-q", "-b", "feature")
	workspace := f.workspace(t, repo)
	parent := f.doneSession(t, workspace)
	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": repo, "slug": "pause", "prompt": "hi"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)
	write(t, filepath.Join(kid.Worktree, "b.go"), "two\n")
	git(t, kid.Worktree, "add", ".")
	git(t, kid.Worktree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "change")
	git(t, repo, "merge", "-q", "feat/pause")

	f.prompt(t, workspace, kid.Session, "again")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "again") })
	f.waitStatus(t, workspace, kid.Session, "done")
	time.Sleep(time.Second)

	assert.Equal(t, "done", f.session(t, workspace, kid.Session).Status)
	assert.DirExists(t, kid.Worktree)
}

func (f *fixture) handoff(t *testing.T, workspace, session string) string {
	t.Helper()
	var next struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.handoff", map[string]any{"session": session}, &next))
	f.waitStatus(t, workspace, next.Session, "done")
	return next.Session
}

func TestHandedOffRepoChildKeepsItsLanding(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	l.report(t, 1, "ready")
	held := l.waitState(t, 1, "waits")
	require.NotEmpty(t, held.For)

	l.kids[1].Session = f.handoff(t, l.workspace, l.kids[1].Session)

	testutil.Eventually(t, func() bool { return l.repo(t, 1).Session == l.kids[1].Session }, wait)
	assert.Equal(t, "waits", l.repo(t, 1).State)
	l.land(t, 0)
	l.mergeInto(t, 0)
	l.report(t, 0, "merged")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[1].Session) == 1 }, wait)
}

func TestHandedOffRepoChildKeepsTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	one := f.landing(t, "ask", "pause", 1)
	two := f.startTask(t, one.workspace, "ask", "resume", one.repos, []int{1})
	one.report(t, 0, "ready")
	ready := one.waitState(t, 0, "ready")

	one.kids[0].Session = f.handoff(t, one.workspace, one.kids[0].Session)
	testutil.Eventually(t, func() bool { return one.repo(t, 0).Session == one.kids[0].Session }, wait)
	two.report(t, 0, "ready")

	assert.Contains(t, two.waitState(t, 0, "waits").For, "pause")
	assert.Equal(t, ready.Request, one.repo(t, 0).Request)
	require.NoError(t, f.run(t, one.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge"}, nil))
	testutil.Eventually(t, func() bool { return f.landPrompts(t, one.kids[0].Session) == 1 }, wait)
}

func TestLandAnswerGoesToTheHandedOffOrchestrator(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")
	old := l.owner
	l.owner = f.handoff(t, l.workspace, old)
	assert.Nil(t, f.session(t, l.workspace, old).Task)

	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge"}, nil))

	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[0].Session) == 1 }, wait)
	assert.Empty(t, l.repo(t, 0).Request)
	assert.False(t, l.task(t).NeedsYou)
}

func TestReportDuringAnOrchestratorHandOffFindsTheTask(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"closeSleep": 1000})
	l := f.landing(t, "ask", "pause", 1)
	var next struct {
		Session string `json:"session"`
	}
	handed := make(chan error, 1)
	go func() {
		handed <- f.h.Client(t).Call(context.Background(), "command.run", map[string]any{"id": "agents.handoff", "workspaceId": l.workspace, "args": map[string]any{"session": l.owner}}, &next)
	}()

	for done := false; !done; {
		select {
		case err := <-handed:
			require.NoError(t, err)
			done = true
		case <-time.After(50 * time.Millisecond):
			require.NoError(t, f.runFrom(t, l.workspace, l.kids[0].Session, "agents.task-report", map[string]any{"state": "ready"}, nil))
		}
	}

	l.owner = next.Session
	require.NotNil(t, l.task(t).Task)
}

func TestLandTurnWithNoReportFailsAndKeepsTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	one := f.landing(t, "merge", "pause", 1)
	two := f.startTask(t, one.workspace, "merge", "resume", one.repos, []int{1})

	one.land(t, 0)

	failed := one.waitState(t, 0, "failed")
	assert.Contains(t, failed.Tail, "no report")
	testutil.Eventually(t, func() bool { return one.task(t).NeedsYou }, wait)
	two.report(t, 0, "ready")
	assert.Contains(t, two.waitState(t, 0, "waits").For, "pause")

	require.NoError(t, f.run(t, one.workspace, "agents.repo-leave", map[string]any{"session": one.owner, "repo": one.repos[0]}, nil))

	one.waitState(t, 0, "left")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, two.kids[0].Session) == 1 }, wait)
}

func TestLateReportAfterALandTurnReleasesTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	one := f.landing(t, "merge", "pause", 1)
	two := f.startTask(t, one.workspace, "merge", "resume", one.repos, []int{1})
	one.land(t, 0)
	one.waitState(t, 0, "failed")
	two.report(t, 0, "ready")
	two.waitState(t, 0, "waits")

	one.report(t, 0, "onBranch")

	one.waitState(t, 0, "onBranch")
	assert.False(t, one.task(t).NeedsYou)
	testutil.Eventually(t, func() bool { return f.landPrompts(t, two.kids[0].Session) == 1 }, wait)
}

func queuedLandPrompt(t *testing.T, f *fixture) *landing {
	t.Helper()
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")
	f.prompt(t, l.workspace, l.kids[0].Session, "hold")
	f.waitStatus(t, l.workspace, l.kids[0].Session, "running")
	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge"}, nil))
	return l
}

func TestQueuedLandPromptIsSentAgainAfterARestart(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "hold", "sleep": 1500}}})
	l := queuedLandPrompt(t, f)

	f.restartAgents(t, l.workspace)

	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[0].Session) == 1 }, wait)
}

func TestLostLandPromptOfALandedRepoIsNotSent(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true, "prompt": []map[string]any{{"match": "hold", "sleep": 1500}}})
	l := queuedLandPrompt(t, f)
	l.mergeInto(t, 0)

	f.restartAgents(t, l.workspace)

	l.waitState(t, 0, "merged")
	assert.Zero(t, f.landPrompts(t, l.kids[0].Session))
}

func TestLandedStateBeforeItsTurnIsRefused(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	for _, state := range []string{"onBranch", "prOpen", "merged"} {
		require.Error(t, f.runFrom(t, l.workspace, l.kids[1].Session, "agents.task-report", map[string]any{"state": state}, nil), state)
	}

	assert.NotContains(t, []string{"onBranch", "prOpen", "merged"}, l.repo(t, 1).State)
}

func TestPROpenCannotClearALandCard(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")

	require.Error(t, f.runFrom(t, l.workspace, l.kids[0].Session, "agents.task-report", map[string]any{"state": "prOpen"}, nil))

	assert.Equal(t, ready.Request, l.repo(t, 0).Request)
	assert.True(t, l.task(t).NeedsYou)
}

func TestReadyAfterMergedIsIgnored(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.land(t, 0)
	l.mergeInto(t, 0)
	l.report(t, 0, "merged")

	out := l.report(t, 0, "ready")

	assert.Equal(t, "merged", out.State)
	assert.NotEmpty(t, out.Note)
	assert.Equal(t, "merged", l.repo(t, 0).State)
}

func startPRCheck(t *testing.T, every time.Duration) *fixture {
	t.Helper()
	return startEnv(t, []string{"CLUBMATE_AGENTS_PR_CHECK=" + every.String()}, map[string]any{}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
}

func TestMergedPRIsSeenWithoutARestart(t *testing.T) {
	t.Parallel()
	f := startPRCheck(t, 200*time.Millisecond)
	l := f.landing(t, "merge", "pause", 1, 2)
	l.report(t, 1, "ready")
	l.land(t, 0)
	l.report(t, 0, "prOpen", "#412")

	l.mergeInto(t, 0)

	l.waitState(t, 0, "merged")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, l.kids[1].Session) == 1 }, wait)
}

func TestAnyChildReportFetchesEveryOpenPR(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 1)
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, l.repos[0], "clone", "--bare", "-q", l.repos[0], bare)
	git(t, l.repos[0], "remote", "add", "origin", bare)
	git(t, l.repos[0], "fetch", "-q", "origin")
	l.land(t, 0)
	l.report(t, 0, "prOpen", "#412")
	other := filepath.Join(t.TempDir(), "other")
	git(t, bare, "clone", "-q", bare, other)
	git(t, other, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "--no-ff", "-m", "land", "origin/feat/pause")
	git(t, other, "push", "-q", "origin", "main")

	l.report(t, 1, "ready")

	l.waitState(t, 0, "merged")
}

func TestDeletedChildDoesNotHideAMergedPR(t *testing.T) {
	t.Parallel()
	f := startPRCheck(t, 200*time.Millisecond)
	l := f.landing(t, "merge", "pause", 1)
	l.land(t, 0)
	l.report(t, 0, "prOpen", "#412")
	require.NoError(t, f.run(t, l.workspace, "agents.delete", map[string]any{"session": l.kids[0].Session}, nil))

	l.mergeInto(t, 0)

	l.waitState(t, 0, "merged")
}

func TestRepoLeaveFreesAFailedRepoAndHoldsItsDependents(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	require.NoError(t, f.runFrom(t, l.workspace, l.kids[0].Session, "agents.task-report", map[string]any{"state": "failed", "tail": "broke"}, nil))
	l.waitState(t, 0, "failed")

	require.NoError(t, f.run(t, l.workspace, "agents.repo-leave", map[string]any{"session": l.owner, "repo": l.repos[0]}, nil))
	l.report(t, 1, "ready")

	l.waitState(t, 0, "left")
	assert.Contains(t, l.waitState(t, 1, "waits").For, "left unlanded")
	assert.False(t, l.task(t).NeedsYou)
}

func TestRepoLeaveRefusesAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	args := map[string]any{"session": l.owner, "repo": l.repos[0]}

	require.Error(t, f.runAs(t, l.workspace, l.owner, "agents.repo-leave", args, nil))
	require.Error(t, f.runFrom(t, l.workspace, l.owner, "agents.repo-leave", args, nil))

	assert.NotEqual(t, "left", l.repo(t, 0).State)
}

func TestRunningFirstTurnReadsRunning(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "slow", "sleep": 2000}}})
	l := f.landing(t, "merge", "pause", 1)
	repo := newRepo(t)
	_, err := f.h.Svc.AddRepo(t.Context(), l.workspace, repo, contract.FolderMeta{})
	require.NoError(t, err)
	require.NoError(t, f.declareTask(t, l.workspace, l.owner, "pause",
		map[string]any{"path": l.repos[0], "order": 1, "landsBy": "merge", "source": "s"},
		map[string]any{"path": repo, "order": 2, "landsBy": "merge", "source": "s"},
	))
	var kid child
	require.NoError(t, f.runFrom(t, l.workspace, l.owner, "agents.new", map[string]any{"repo": repo, "slug": "pause", "prompt": "slow"}, &kid))
	l.repos = append(l.repos, repo)

	assert.Equal(t, "running", l.waitState(t, 1, "running").State)
}

func TestHandOffBriefTurnReadsRunningAndKeepsReady(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "[Clubmate handoff]", "sleep": 2000}}})
	l := f.landing(t, "merge", "pause", 1, 2)
	l.report(t, 1, "ready")
	l.waitState(t, 1, "waits")
	var next struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.handoff", map[string]any{"session": l.kids[1].Session}, &next))
	l.kids[1].Session = next.Session

	l.waitState(t, 1, "running")
	f.waitStatus(t, l.workspace, next.Session, "done")

	testutil.Eventually(t, func() bool { return l.repo(t, 1).State == "waits" }, wait)
	l.land(t, 0)
	l.mergeInto(t, 0)
	l.report(t, 0, "merged")
	testutil.Eventually(t, func() bool { return f.landPrompts(t, next.Session) == 1 }, wait)
}

func TestClosedPRCanBeReportedFailed(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.land(t, 0)
	l.report(t, 0, "prOpen", "#412")

	out := l.report(t, 0, "failed")

	assert.Equal(t, "failed", out.State)
	assert.Empty(t, out.Note)
}

func TestLandCardNeedsYouWhileTheOrchestratorRuns(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "hold", "sleep": 1500}}})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	l.waitState(t, 0, "ready")
	f.waitStatus(t, l.workspace, l.owner, "done")

	f.prompt(t, l.workspace, l.owner, "hold")
	f.waitStatus(t, l.workspace, l.owner, "running")

	task := l.task(t)
	assert.True(t, task.NeedsYou)
	require.NotNil(t, task.PendingSummary)
	assert.Equal(t, "land", task.PendingSummary.Kind)
}

func TestAFKConnectedRepoIsNotFetchedBeforeItIsSeen(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, other, "clone", "--bare", "-q", other, bare)
	git(t, other, "remote", "add", "origin", bare)
	git(t, other, "fetch", "-q", "origin")
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "pack.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\nexec git upload-pack \"$@\"\n"), 0o700))
	git(t, other, "config", "remote.origin.uploadpack", script)
	workspace := f.workspace(t, first)
	var owner child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": "merge", "prompt": "plan", "afk": true}, &owner))
	f.waitStatus(t, workspace, owner.Session, "done")
	require.Equal(t, "connected", f.connect(t, workspace, owner.Session, map[string]any{"path": other}).Status)
	l := &landing{f: f, workspace: workspace, owner: owner.Session, repos: []string{other}}
	require.NoError(t, f.declareTask(t, workspace, l.owner, "pause", map[string]any{"path": other, "order": 1, "landsBy": "pr", "source": "s"}))
	var kid child
	require.NoError(t, f.runFrom(t, workspace, l.owner, "agents.new", map[string]any{"repo": other, "slug": "pause", "prompt": "work"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)
	l.kids = []child{kid}

	l.land(t, 0)
	l.report(t, 0, "prOpen", "#1")

	assert.NoFileExists(t, marker)
	notices := ofKind(f.transcript(t, workspace, kid.Session).Entries, "notice")
	require.Len(t, notices, 1)
	assert.Contains(t, string(notices[0].Notice), "main")
}
