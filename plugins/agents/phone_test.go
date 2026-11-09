package main_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

type phoneTranscript struct {
	Request *sdui.Node `json:"request"`
	Repos   *sdui.Node `json:"repos"`
	Entries []struct {
		Kind string `json:"kind"`
		AFK  *struct {
			Detail string `json:"detail"`
			Remove string `json:"remove"`
		} `json:"afk"`
	} `json:"entries"`
}

type phoneSession struct {
	Requests []string `json:"requests"`
	TaskRows []struct {
		Name    string `json:"name"`
		Session string `json:"session"`
		Word    string `json:"word"`
		Tone    string `json:"tone"`
		Connect bool   `json:"connect"`
	} `json:"taskRows"`
	TaskBadge      string `json:"taskBadge"`
	Archivable     bool   `json:"archivable"`
	TaskFinished   bool   `json:"taskFinished"`
	PendingSummary *struct {
		Lead    string `json:"lead"`
		Subject string `json:"subject"`
	} `json:"pendingSummary"`
}

type phoneRow struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Session string `json:"session"`
	Open    bool   `json:"open"`
	Tail    string `json:"tail"`
	Actions string `json:"actions"`
	FoldKey string `json:"foldKey"`
}

func (f *fixture) phoneTranscript(t *testing.T, workspace, session string) phoneTranscript {
	t.Helper()
	var out phoneTranscript
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session, "compact": true}, &out))
	return out
}

func (f *fixture) phoneSession(t *testing.T, workspace, session string) phoneSession {
	t.Helper()
	var out []phoneSession
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": session}, &out))
	require.Len(t, out, 1)
	return out[0]
}

func nodeRows(t *testing.T, n *sdui.Node) []phoneRow {
	t.Helper()
	require.NotNil(t, n)
	raw, err := json.Marshal(n.Props["rows"])
	require.NoError(t, err)
	var rows []phoneRow
	require.NoError(t, json.Unmarshal(raw, &rows))
	return rows
}

func TestConnectCardReachesThePhoneAndItsRowSaysNotConnected(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": other, "reason": "scopes live there", "landsBy": "merge"})

	tr := f.phoneTranscript(t, workspace, session)
	require.NotNil(t, tr.Request)
	assert.Equal(t, "request.connectRepo", tr.Request.Type)
	assert.Equal(t, res.Request, tr.Request.Props["request"])
	assert.NotEmpty(t, tr.Request.Props["seen"])

	info := f.phoneSession(t, workspace, session)
	assert.Equal(t, []string{res.Request}, info.Requests)
	require.NotNil(t, info.PendingSummary)
	assert.Equal(t, "Wants to connect", info.PendingSummary.Lead)
	assert.Equal(t, filepath.Base(other), info.PendingSummary.Subject)
	require.Len(t, info.TaskRows, 1)
	assert.True(t, info.TaskRows[0].Connect)
	assert.Equal(t, "not connected yet", info.TaskRows[0].Word)
	assert.Equal(t, "warning", info.TaskRows[0].Tone)
}

func TestConnectAnswerFromAStaleCardChangesNothing(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, a, b := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	asker := f.doneSession(t, workspace)
	both := f.connect(t, workspace, asker, map[string]any{"path": a}, map[string]any{"path": b})
	seen := f.phoneTranscript(t, workspace, asker).Request.Props["seen"]
	other := f.doneSession(t, workspace)
	one := f.connect(t, workspace, other, map[string]any{"path": a})
	require.NoError(t, f.answerConnect(t, workspace, one.Request, connectAnswer{"path": a, "action": "connect"}))

	err := f.run(t, workspace, "agents.connect-answer", map[string]any{"request": both.Request, "seen": seen, "answers": []connectAnswer{{"path": b, "action": "connect"}}}, nil)

	require.ErrorContains(t, err, "changed since you saw it")
	assert.NotContains(t, f.folderPaths(t, workspace), b)
}

func TestLandCommitsListTheBranchAndGoStaleWithTheRequest(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")

	var out struct {
		Commits []struct {
			Sha   string `json:"sha"`
			Title string `json:"title"`
		} `json:"commits"`
		More int `json:"more"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.land-commits", map[string]any{"request": ready.Request}, &out))
	require.Len(t, out.Commits, 1)
	assert.Equal(t, "change", out.Commits[0].Title)
	assert.NotEmpty(t, out.Commits[0].Sha)
	assert.Zero(t, out.More)
	assert.Equal(t, "1 to land", f.phoneSession(t, l.workspace, l.owner).TaskBadge)
	tr := f.phoneTranscript(t, l.workspace, l.owner)
	assert.Equal(t, "request.land", tr.Request.Type)
	assert.NotContains(t, tr.Repos.Props, "collapsed", "a Land request opens the card on both clients")

	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "leave"}, nil))
	require.ErrorContains(t, f.run(t, l.workspace, "agents.land-commits", map[string]any{"request": ready.Request}, nil), "already answered")
}

func TestLandCommitsFollowTheLiveTipAndAMovedBranchRefusesTheMerge(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "ask", "pause", 1)
	l.report(t, 0, "ready")
	ready := l.waitState(t, 0, "ready")
	var saw struct {
		Tip     string `json:"tip"`
		Detail  string `json:"detail"`
		Commits []struct {
			Title string `json:"title"`
		} `json:"commits"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.land-commits", map[string]any{"request": ready.Request}, &saw))
	stale := saw.Tip
	write(t, filepath.Join(l.kids[0].Worktree, "more.go"), "more\n")
	git(t, l.kids[0].Worktree, "add", ".")
	git(t, l.kids[0].Worktree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "one more")

	require.NoError(t, f.run(t, l.workspace, "agents.land-commits", map[string]any{"request": ready.Request}, &saw))
	assert.Equal(t, "one more", saw.Commits[0].Title)
	assert.Len(t, saw.Commits, 2)
	assert.Contains(t, saw.Detail, "2 commits")

	err := f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge", "tip": stale}, nil)
	require.ErrorContains(t, err, "changed since you saw it")
	assert.Equal(t, ready.Request, l.repo(t, 0).Request)

	require.NoError(t, f.run(t, l.workspace, "agents.land-answer", map[string]any{"request": ready.Request, "action": "merge"}, nil), "the Mac sends no tip")
}

func TestFailedRepoRowOpensByItselfForThePhone(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	require.NoError(t, f.runFrom(t, l.workspace, l.kids[0].Session, "agents.task-report", map[string]any{"state": "failed", "tail": "tests failed: TestX"}, nil))
	l.waitState(t, 0, "failed")

	tr := f.phoneTranscript(t, l.workspace, l.owner)
	assert.Nil(t, tr.Request)
	rows := nodeRows(t, tr.Repos)
	assert.True(t, rows[0].Open)
	assert.NotEmpty(t, rows[0].FoldKey)
	assert.NotEmpty(t, tr.Repos.Props["foldKey"])
	assert.Equal(t, "tests failed: TestX", rows[0].Tail)
	assert.Equal(t, l.repos[0], rows[0].Path)
	assert.Equal(t, l.kids[0].Session, rows[0].Session)
	assert.False(t, rows[1].Open)
	require.NotEmpty(t, rows[0].Actions)
	assert.NotEmpty(t, rows[1].Actions, "the phone folds and opens a row by itself, so it gets the buttons up front")
	assert.NotContains(t, tr.Repos.Props, "collapsed")
}

func TestRepoRowsFollowTheLandingOrderAndLeaveARunningChildItsOwnLine(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "long", "wait": filepath.Join(t.TempDir(), "hold")}}})
	l := f.landing(t, "merge", "pause", 1, 2)
	f.prompt(t, l.workspace, l.kids[1].Session, "long")
	f.waitStatus(t, l.workspace, l.kids[1].Session, "running")
	l.report(t, 0, "ready")

	rows := f.phoneSession(t, l.workspace, l.owner).TaskRows

	require.Len(t, rows, 2)
	assert.Equal(t, l.kids[0].Session, rows[0].Session)
	assert.Equal(t, l.kids[1].Session, rows[1].Session)
	assert.Empty(t, rows[1].Word)
}

func TestArchiveFromAStalePlanDoesNothing(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	l.waitOwnerWoken(t)
	assert.True(t, f.phoneSession(t, l.workspace, l.owner).Archivable)
	assert.True(t, f.phoneSession(t, l.workspace, l.owner).TaskFinished)
	raw := map[string]any{}
	require.NoError(t, f.run(t, l.workspace, "agents.archive-plan", map[string]any{"session": l.owner}, &raw))
	plan := l.plan(t)
	write(t, filepath.Join(l.kids[0].Worktree, "scratch.go"), "x\n")

	err := f.run(t, l.workspace, "agents.archive", map[string]any{"session": l.owner, "seen": raw["seen"], "choices": []map[string]any{choose(plan.Repos[0])}}, nil)

	require.ErrorContains(t, err, "changed since you saw the plan")
	assert.DirExists(t, l.kids[0].Worktree)
	assert.NotEqual(t, "closed", f.session(t, l.workspace, l.owner).Status)
}

func TestArchivePlanNamesTheJobsThatPauseAndItsSeenFollowsThem(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	var before, after struct {
		Seen      string   `json:"seen"`
		CronLines []string `json:"cronLines"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.archive-plan", map[string]any{"session": l.owner}, &before))
	f.cronJob(t, l.workspace, l.repos[0], l.branch(t))

	require.NoError(t, f.run(t, l.workspace, "agents.archive-plan", map[string]any{"session": l.owner}, &after))

	assert.NotEqual(t, before.Seen, after.Seen)
	require.Len(t, after.CronLines, 1)
	assert.Contains(t, after.CronLines[0], "nightly runs in ")
	assert.Contains(t, after.CronLines[0], "pauses until you resume it in Cron")
}

func TestResendWithTheSameKeyGetsTheFirstResult(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	plan := l.plan(t)
	args := map[string]any{"session": l.owner, "key": "phone-1", "choices": []map[string]any{choose(plan.Repos[0])}}
	var first, again archiveOutcome
	testutil.Eventually(t, func() bool {
		err := f.run(t, l.workspace, "agents.archive", args, &first)
		require.True(t, err == nil || strings.Contains(err.Error(), "is running"), "%v", err)
		return err == nil
	}, wait)
	require.Equal(t, 2, first.Closed)
	f.restartAgents(t, l.workspace)

	require.NoError(t, f.run(t, l.workspace, "agents.archive", args, &again))

	assert.Equal(t, first, again)
}

func TestAConnectResendWithItsKeyIsNotRefused(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": other})
	args := map[string]any{"request": res.Request, "key": "phone-2", "answers": []connectAnswer{{"path": other, "action": "connect"}}}

	require.NoError(t, f.run(t, workspace, "agents.connect-answer", args, nil))
	require.NoError(t, f.run(t, workspace, "agents.connect-answer", args, nil))

	assert.Contains(t, f.folderPaths(t, workspace), other)
	require.ErrorContains(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": other, "action": "connect"}), "already answered")
}

func TestARefusalReachesAClientWithItsReason(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": other})
	require.NoError(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": other, "action": "connect"}))

	err := f.answerConnect(t, workspace, res.Request, connectAnswer{"path": other, "action": "connect"})

	var remote *rpc.RemoteError
	require.ErrorAs(t, err, &remote)
	assert.Equal(t, contract.ReasonAnswered, contract.ReasonIn(&remote.Data))
}

func TestADeletedWorkspaceReachesAClientAsStale(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	require.NoError(t, f.h.Svc.DeleteWorkspace(t.Context(), l.workspace))

	err := f.run(t, l.workspace, "agents.archive", map[string]any{"session": l.owner, "seen": "x"}, nil)

	var remote *rpc.RemoteError
	require.ErrorAs(t, err, &remote)
	assert.Equal(t, contract.ReasonStale, contract.ReasonIn(&remote.Data), remote.Message)
}

func TestArchivePlanSaysWhyInOneLine(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	write(t, filepath.Join(l.kids[0].Worktree, "scratch.go"), "x\n")

	var raw struct {
		Repos []struct {
			Why     string `json:"why"`
			WhyTone string `json:"whyTone"`
		} `json:"repos"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.archive-plan", map[string]any{"session": l.owner}, &raw))

	assert.Equal(t, "1 file would be lost — open it on the Mac", raw.Repos[0].Why)
	assert.Equal(t, "warning", raw.Repos[0].WhyTone)
}

func TestSessionWithoutATaskGetsNoTaskFields(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)

	var raw []map[string]any
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{"session": session}, &raw))
	var tr map[string]any
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &tr))

	for _, key := range []string{"taskRows", "taskBadge", "archivable", "requests"} {
		assert.NotContains(t, raw[0], key)
	}
	assert.NotContains(t, tr, "request")
	assert.NotContains(t, tr, "repos")
}

func TestAFKRowReachesThePhone(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.afk", map[string]any{"session": session, "on": true}, nil))
	f.connect(t, workspace, session, map[string]any{"path": other})

	tr := f.phoneTranscript(t, workspace, session)
	var detail, remove string
	for _, en := range tr.Entries {
		if en.AFK != nil {
			detail, remove = en.AFK.Detail, en.AFK.Remove
		}
	}
	assert.Contains(t, detail, "a PR at most")
	assert.True(t, strings.HasPrefix(remove, "Remove from "), remove)
}
