package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

type archiveStep struct {
	Action string `json:"action"`
	Why    string `json:"why"`
}

type archivePlanRepo struct {
	Name      string      `json:"name"`
	Worktree  archiveStep `json:"worktree"`
	Branch    archiveStep `json:"branch"`
	IfRemoved archiveStep `json:"ifRemoved"`
	Anyway    bool        `json:"anyway"`
	Lost      string      `json:"lost"`
	Seen      string      `json:"seen"`
	Summary   string      `json:"summary"`
	State     struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
	} `json:"state"`
}

type archivePlan struct {
	Sessions int               `json:"sessions"`
	Repos    []archivePlanRepo `json:"repos"`
	Cron     []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		In   string `json:"in"`
	} `json:"cron"`
	Folders []struct {
		Path string `json:"path"`
	} `json:"folders"`
	Blockers []folderSession `json:"blockers"`
	Holds    []string        `json:"holds"`
}

type archiveOutcome struct {
	Closed  int      `json:"closed"`
	Removed []string `json:"removed"`
	Deleted []string `json:"deleted"`
	Kept    []string `json:"kept"`
}

func (l *landing) settle(t *testing.T) {
	t.Helper()
	for i := range l.repos {
		l.land(t, i)
		l.mergeInto(t, i)
		assert.Equal(t, "merged", l.report(t, i, "merged").State)
	}
	l.f.waitStatus(t, l.workspace, l.owner, "done")
}

func (l *landing) plan(t *testing.T) archivePlan {
	t.Helper()
	var out archivePlan
	require.NoError(t, l.f.run(t, l.workspace, "agents.archive-plan", map[string]any{"session": l.owner}, &out))
	return out
}

func (l *landing) branch(t *testing.T) string {
	t.Helper()
	return git(t, l.kids[0].Worktree, "symbolic-ref", "--short", "HEAD")
}

func (l *landing) archive(t *testing.T, choices ...map[string]any) archiveOutcome {
	t.Helper()
	var out archiveOutcome
	args := map[string]any{"session": l.owner, "choices": choices}
	testutil.Eventually(t, func() bool {
		err := l.f.run(t, l.workspace, "agents.archive", args, &out)
		require.True(t, err == nil || strings.Contains(err.Error(), "is running"), "%v", err)
		return err == nil
	}, wait)
	return out
}

func choose(r archivePlanRepo) map[string]any {
	return map[string]any{"path": r.State.Path, "seen": r.Seen}
}

func (l *landing) press(t *testing.T, node, action string, done func(sdui.Node) bool) sdui.Node {
	t.Helper()
	var found sdui.Node
	testutil.Eventually(t, func() bool {
		l.f.h.Event(t, l.workspace, "agents.sheet", node, action, nil)
		found = l.f.h.WaitSlot(t, l.workspace, "agents.sheet", func(sdui.Node) bool { return true })
		return done(found)
	}, wait)
	return found
}

func noSheet(n sdui.Node) bool {
	return n.Type == "empty"
}

func (l *landing) openArchiveSheet(t *testing.T) {
	t.Helper()
	require.NoError(t, l.f.run(t, l.workspace, "agents.archive-sheet", map[string]any{"session": l.owner}, nil))
	l.f.h.WaitSlot(t, l.workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
}

func (f *fixture) startTaskFor(t *testing.T, workspace, owner, slug string, repos []string) *landing {
	t.Helper()
	l := &landing{f: f, workspace: workspace, owner: owner, repos: repos}
	declared := make([]map[string]any, 0, len(repos))
	for i, repo := range repos {
		declared = append(declared, map[string]any{"path": repo, "order": i + 1, "landsBy": "merge", "source": "its CLAUDE.md"})
	}
	require.NoError(t, f.declareTask(t, workspace, owner, slug, declared...))
	for _, repo := range repos {
		var kid child
		require.NoError(t, f.runFrom(t, workspace, owner, "agents.new", map[string]any{"repo": repo, "slug": slug, "prompt": "work"}, &kid))
		f.waitBranchChildDone(t, workspace, kid.Session)
		write(t, filepath.Join(kid.Worktree, slug+".go"), "change\n")
		git(t, kid.Worktree, "add", ".")
		git(t, kid.Worktree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "change")
		l.kids = append(l.kids, kid)
	}
	return l
}

func (f *fixture) newSessionIn(t *testing.T, workspace, repo string) string {
	t.Helper()
	var out child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"repo": repo, "slug": "other"}, &out))
	return out.Session
}

func (f *fixture) close(t *testing.T, workspace, session string) {
	t.Helper()
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": session}, nil))
	f.waitStatus(t, workspace, session, "closed")
}

func sheetText(n sdui.Node) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func hasBranch(t *testing.T, repo, branch string) bool {
	t.Helper()
	return git(t, repo, "branch", "--list", branch) != ""
}

func TestArchiveRemovesWhatLandedAndClosesTheSessions(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	l.settle(t)
	branch := l.branch(t)

	plan := l.plan(t)
	require.Len(t, plan.Repos, 2)
	assert.Equal(t, 3, plan.Sessions)
	assert.Equal(t, "remove", plan.Repos[0].Worktree.Action)
	assert.Equal(t, "delete", plan.Repos[0].Branch.Action)
	assert.Equal(t, "Worktree removed · branch deleted", plan.Repos[0].Summary)

	out := l.archive(t, choose(plan.Repos[0]), choose(plan.Repos[1]))

	assert.Empty(t, out.Kept)
	assert.Len(t, out.Removed, 2)
	assert.NoDirExists(t, l.kids[0].Worktree)
	assert.False(t, hasBranch(t, l.repos[0], branch))
	f.waitStatus(t, l.workspace, l.owner, "closed")
	f.waitStatus(t, l.workspace, l.kids[1].Session, "closed")
}

func TestArchiveRefusedWhileAChildRuns(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "long", "wait": filepath.Join(t.TempDir(), "hold")}}})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	plan := l.plan(t)
	f.prompt(t, l.workspace, l.kids[0].Session, "long")
	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return strings.Contains(string(c.Params), "long") })

	assert.NotEmpty(t, l.plan(t).Blockers)
	err := f.run(t, l.workspace, "agents.archive", map[string]any{"session": l.owner, "choices": []map[string]any{choose(plan.Repos[0])}}, nil)

	require.ErrorContains(t, err, "refused")
	assert.DirExists(t, l.kids[0].Worktree)
	assert.Equal(t, "running", f.session(t, l.workspace, l.kids[0].Session).Status)
	assert.NotEqual(t, "closed", f.session(t, l.workspace, l.owner).Status)
}

func TestArchiveKeepsADirtyWorktreeUnlessTicked(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	branch := l.branch(t)
	write(t, filepath.Join(l.kids[0].Worktree, "scratch.go"), "x\n")

	plan := l.plan(t)
	r := plan.Repos[0]
	assert.Equal(t, "keep", r.Worktree.Action)
	assert.True(t, r.Anyway)
	assert.Equal(t, "1 file", r.Lost)
	assert.Equal(t, "keep", r.Branch.Action, "a branch can't go while its worktree stays")
	assert.Equal(t, "delete", r.IfRemoved.Action)

	out := l.archive(t, choose(r))
	assert.Len(t, out.Kept, 1)
	assert.FileExists(t, filepath.Join(l.kids[0].Worktree, "scratch.go"))
	assert.True(t, hasBranch(t, l.repos[0], branch))
}

func TestArchiveRemovesADirtyWorktreeOnlyWhenToldAnyway(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	branch := l.branch(t)
	write(t, filepath.Join(l.kids[0].Worktree, "scratch.go"), "x\n")
	choice := choose(l.plan(t).Repos[0])
	choice["anyway"] = true

	out := l.archive(t, choice)

	assert.Empty(t, out.Kept)
	assert.NoDirExists(t, l.kids[0].Worktree)
	assert.False(t, hasBranch(t, l.repos[0], branch))
}

func TestArchiveOpenPRKeepsTheBranch(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.land(t, 0)
	l.report(t, 0, "prOpen", "https://github.com/acme/ads/pull/412")
	branch := l.branch(t)

	r := l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Branch.Action)
	assert.Contains(t, r.Branch.Why, "PR #412 is open")

	out := l.archive(t, choose(r))

	assert.Empty(t, out.Kept)
	assert.NoDirExists(t, l.kids[0].Worktree)
	assert.True(t, hasBranch(t, l.repos[0], branch))
}

func TestArchiveKeepsABranchOnTheRemoteAndNeverTouchesIt(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	git(t, l.repos[0], "remote", "add", "origin", remote)
	branch := l.branch(t)
	git(t, l.kids[0].Worktree, "push", "-q", "-u", "origin", branch)
	l.settle(t)

	r := l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Branch.Action)
	assert.Contains(t, r.Branch.Why, "remote")

	l.archive(t, choose(r))

	assert.NoDirExists(t, l.kids[0].Worktree)
	assert.True(t, hasBranch(t, l.repos[0], branch))
	assert.Contains(t, git(t, remote, "branch", "--list"), branch)
}

func TestArchiveDeletesASquashMergedBranch(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.land(t, 0)
	branch := l.branch(t)
	git(t, l.repos[0], "merge", "-q", "--squash", branch)
	git(t, l.repos[0], "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "squashed")
	require.Equal(t, "merged", l.report(t, 0, "merged").State)

	r := l.plan(t).Repos[0]
	require.Equal(t, "delete", r.Branch.Action)

	out := l.archive(t, choose(r))

	assert.Empty(t, out.Kept)
	assert.False(t, hasBranch(t, l.repos[0], branch))
}

func TestCloseSessionsOnlyTouchesNoWorktreeOrBranch(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	branch := l.branch(t)
	l.openArchiveSheet(t)

	l.press(t, "archiveSheet/closeOnly", "archiveClose", noSheet)

	f.waitStatus(t, l.workspace, l.kids[0].Session, "closed")
	assert.DirExists(t, l.kids[0].Worktree)
	assert.True(t, hasBranch(t, l.repos[0], branch))
}

func TestArchiveSheetIsPlainUntilTheTickLosesSomething(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	tree := l.kids[0].Worktree
	write(t, filepath.Join(tree, "scratch.go"), "x\n")
	l.openArchiveSheet(t)
	sheet := f.h.WaitSlot(t, l.workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "button")) == 3 })
	archive := sheetNodes(sheet, "button")[2]
	assert.Equal(t, sdui.VariantPrimary, archive.Props["variant"])
	assert.Equal(t, "Archive task", archive.Props["label"])

	f.h.Event(t, l.workspace, "agents.sheet", "archiveSheet/tick/"+tree, "archiveTick", true)

	red := f.h.WaitSlot(t, l.workspace, "agents.sheet", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "button"), func(b sdui.Node) bool { return b.Props["variant"] == sdui.VariantDanger })
	})
	assert.Equal(t, "Archive · lose 1 file", sheetNodes(red, "button")[2].Props["label"])

	l.press(t, "archiveSheet/archive", "archiveConfirm", noSheet)

	assert.NoDirExists(t, tree)
}

func TestWorktreeThatBecameDirtyAfterTheSheetOpenedIsKept(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	branch := l.branch(t)
	l.openArchiveSheet(t)
	late := filepath.Join(l.kids[0].Worktree, "late.go")
	write(t, late, "x\n")

	sheet := l.press(t, "archiveSheet/archive", "archiveConfirm", func(n sdui.Node) bool {
		s := sheetNodes(n, "sheet")
		return len(s) == 1 && strings.Contains(s[0].Props["title"].(string), "is archived")
	})
	assert.Contains(t, sheetText(sheet), "it changed since you looked")
	assert.FileExists(t, late)
	assert.True(t, hasBranch(t, l.repos[0], branch))
	f.waitStatus(t, l.workspace, l.owner, "closed")
}

func TestMergedThatChangedSinceThePlanIsKept(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	branch := l.branch(t)
	r := l.plan(t).Repos[0]
	require.Equal(t, "delete", r.Branch.Action)
	git(t, l.repos[0], "reset", "-q", "--hard", "HEAD~1")

	out := l.archive(t, choose(r))

	require.Len(t, out.Kept, 1)
	assert.Contains(t, out.Kept[0], "it changed since you looked")
	assert.DirExists(t, l.kids[0].Worktree)
	assert.True(t, hasBranch(t, l.repos[0], branch))
}

func TestArchiveKeepsANestedWorktreeEvenWhenTold(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	nested := filepath.Join(l.kids[0].Worktree, "vendor")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	git(t, nested, "init", "-q")
	write(t, filepath.Join(nested, "x.go"), "x\n")
	git(t, nested, "add", ".")
	git(t, nested, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x")

	r := l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Worktree.Action)
	assert.False(t, r.Anyway)
	choice := choose(r)
	choice["anyway"] = true

	out := l.archive(t, choice)

	assert.Len(t, out.Kept, 1)
	assert.DirExists(t, nested)
}

func TestArchiveKeepsAWorktreeOpenInCode(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	r := l.plan(t).Repos[0]
	require.NoError(t, f.run(t, l.workspace, "git.worktree-switch", map[string]any{"repo": l.repos[0], "path": r.State.Path}, nil))

	r = l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Worktree.Action)
	assert.False(t, r.Anyway)

	out := l.archive(t, choose(r))

	assert.Len(t, out.Kept, 1)
	assert.DirExists(t, l.kids[0].Worktree)
}

func TestArchiveKeepsAWorktreeASessionOfAnotherWorkspaceHolds(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "long", "wait": filepath.Join(t.TempDir(), "hold")}}})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	f.runningInTree(t, l.repos[0], l.kids[0].Worktree)

	r := l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Worktree.Action)
	assert.Equal(t, "a session works in it", r.Worktree.Why)

	out := l.archive(t, choose(r))

	assert.Len(t, out.Kept, 1)
	assert.DirExists(t, l.kids[0].Worktree)
}

func TestArchiveKeepsAWorktreeWithATerminalAndNamesIt(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	branch := l.branch(t)
	f.terminalIn(t, f.workspace(t, newRepo(t)), l.kids[0].Worktree, "Build")

	out := l.archive(t, choose(l.plan(t).Repos[0]))

	require.Len(t, out.Kept, 1)
	assert.Contains(t, out.Kept[0], "terminal Build is open in it")
	assert.DirExists(t, l.kids[0].Worktree)
	assert.True(t, hasBranch(t, l.repos[0], branch))
}

func TestArchiveNamesAndPausesACronJobInTheWorktree(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	job := f.cronJob(t, l.workspace, l.repos[0], l.branch(t))

	plan := l.plan(t)
	require.Len(t, plan.Cron, 1)
	assert.Equal(t, "nightly", plan.Cron[0].Name)
	assert.Equal(t, l.kids[0].Worktree, plan.Cron[0].In)

	l.archive(t, choose(plan.Repos[0]))

	var shown struct {
		Enabled bool `json:"enabled"`
	}
	require.NoError(t, f.run(t, l.workspace, "cron.show", map[string]any{"id": job}, &shown))
	assert.False(t, shown.Enabled)
}

func TestArchiveResumesTheJobOfAWorktreeThatStays(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	job := f.cronJob(t, l.workspace, l.repos[0], l.branch(t))
	r := l.plan(t).Repos[0]
	require.NoError(t, f.run(t, l.workspace, "git.worktree-switch", map[string]any{"repo": l.repos[0], "path": r.State.Path}, nil))
	r = l.plan(t).Repos[0]
	require.Equal(t, "keep", r.Worktree.Action)
	choice := choose(r)

	l.archive(t, choice)

	var shown struct {
		Enabled bool `json:"enabled"`
	}
	require.NoError(t, f.run(t, l.workspace, "cron.show", map[string]any{"id": job}, &shown))
	assert.True(t, shown.Enabled)
}

func TestArchiveRemovesAFolderOnlyThisTaskAdded(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	first, second, third := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	var owner child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": "merge", "prompt": "plan", "title": "Pause"}, &owner))
	f.waitStatus(t, workspace, owner.Session, "done")
	title := f.session(t, workspace, owner.Session).Title
	for _, repo := range []string{second, third} {
		_, err := f.h.Svc.AddRepo(t.Context(), workspace, repo, contract.FolderMeta{AddedBy: title})
		require.NoError(t, err)
	}
	l := f.startTaskFor(t, workspace, owner.Session, "pause", []string{second, third})
	l.settle(t)
	other := f.newSessionIn(t, workspace, third)

	plan := l.plan(t)
	require.Len(t, plan.Folders, 1, "another session holds the third folder")
	assert.Equal(t, second, plan.Folders[0].Path)
	f.close(t, workspace, other)

	out := l.archive(t, choose(plan.Repos[0]), choose(plan.Repos[1]), map[string]any{"path": second, "folder": true})

	assert.Empty(t, out.Kept)
	assert.Equal(t, []string{first, third}, f.folderPaths(t, workspace))
	assert.DirExists(t, second)
}

func TestRestoreBringsATaskBackAfterArchive(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	write(t, filepath.Join(l.kids[0].Worktree, "scratch.go"), "x\n")
	l.archive(t, choose(l.plan(t).Repos[0]))
	f.waitStatus(t, l.workspace, l.owner, "closed")

	require.NoError(t, f.run(t, l.workspace, "agents.restore", map[string]any{"session": l.owner}, nil))

	f.waitStatus(t, l.workspace, l.owner, "idle")
	f.waitStatus(t, l.workspace, l.kids[0].Session, "idle")
}

func TestRestoreRefusedWhenTheWorktreeIsGone(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	l.archive(t, choose(l.plan(t).Repos[0]))
	f.waitStatus(t, l.workspace, l.owner, "closed")

	err := f.run(t, l.workspace, "agents.restore", map[string]any{"session": l.kids[0].Session}, nil)

	require.ErrorContains(t, err, "its worktree is gone")
	assert.Equal(t, "closed", f.session(t, l.workspace, l.kids[0].Session).Status)
}

func TestRestoreRefusedWhenTheFolderLeft(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{"resume": true})
	workspace, first, second := f.twoFolders(t)
	var out child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"repo": second, "slug": "x", "prompt": "x"}, &out))
	f.waitStatus(t, workspace, out.Session, "done")
	f.close(t, workspace, out.Session)
	assert.True(t, f.folderRemove(t, workspace, second).Removed)
	require.Equal(t, []string{first}, f.folderPaths(t, workspace))

	err := f.run(t, workspace, "agents.restore", map[string]any{"session": out.Session}, nil)

	require.ErrorContains(t, err, "its folder left")
}

func TestArchiveAndRestoreRefuseAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	session := l.kids[0].Session
	args := map[string]any{"session": l.owner}

	for _, command := range []string{"agents.archive-plan", "agents.archive", "agents.archive-sheet", "agents.restore"} {
		require.Error(t, f.runAs(t, l.workspace, session, command, args, nil), command)
		require.Error(t, f.runFrom(t, l.workspace, session, command, args, nil), command)
	}
	assert.DirExists(t, l.kids[0].Worktree)
}

func TestRepoCardAndRowOfferArchiveOnceEveryChildIsDone(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)
	require.NoError(t, f.run(t, l.workspace, "agents.reveal", map[string]any{"session": l.owner}, nil))
	running := f.h.WaitSlot(t, l.workspace, "agents.main", func(n sdui.Node) bool { return len(cardsOf(n, "repos")) == 1 })
	assert.Empty(t, cardsOf(running, "repos")[0].Props["footerAction"])

	l.settle(t)

	done := f.h.WaitSlot(t, l.workspace, "agents.main", func(n sdui.Node) bool {
		cards := cardsOf(n, "repos")
		return len(cards) == 1 && cards[0].Props["footerAction"] != nil
	})
	assert.Equal(t, "All children done · 2 worktrees left behind", cardsOf(done, "repos")[0].Props["footer"])
	f.h.WaitSlot(t, l.workspace, "agents.sidebar", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), "Archive Task…")
	})
}

func TestRestoreBringsBackWhatItCanAndNamesTheRest(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1, 2)
	l.settle(t)
	write(t, filepath.Join(l.kids[1].Worktree, "scratch.go"), "x\n")
	plan := l.plan(t)
	l.archive(t, choose(plan.Repos[0]), choose(plan.Repos[1]))
	f.waitStatus(t, l.workspace, l.owner, "closed")

	var out struct {
		Restored []string `json:"restored"`
		Stayed   []struct {
			Session string `json:"session"`
			Title   string `json:"title"`
			Why     string `json:"why"`
		} `json:"stayed"`
	}
	require.NoError(t, f.run(t, l.workspace, "agents.restore", map[string]any{"session": l.owner}, &out))

	assert.Contains(t, out.Restored, l.owner)
	assert.Contains(t, out.Restored, l.kids[1].Session)
	require.Len(t, out.Stayed, 1)
	assert.Equal(t, l.kids[0].Session, out.Stayed[0].Session)
	assert.Contains(t, out.Stayed[0].Why, "its worktree is gone")
	f.waitStatus(t, l.workspace, l.owner, "idle")
	f.waitStatus(t, l.workspace, l.kids[1].Session, "idle")
	assert.Equal(t, "closed", f.session(t, l.workspace, l.kids[0].Session).Status)
}

func TestRestoreRefusesAWorktreeRecreatedOnAnotherBranch(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	tree := l.kids[0].Worktree
	f.close(t, l.workspace, l.kids[0].Session)
	git(t, l.repos[0], "worktree", "remove", "--force", tree)
	git(t, l.repos[0], "worktree", "add", "-q", "-b", "someone-else", tree)

	err := f.run(t, l.workspace, "agents.restore", map[string]any{"session": l.kids[0].Session}, nil)

	require.ErrorContains(t, err, "someone-else")
	assert.Equal(t, "closed", f.session(t, l.workspace, l.kids[0].Session).Status)
}

func TestClosedTaskOffersRestoreAndAPlainSessionKeepsReopen(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	f.close(t, l.workspace, l.owner)

	assert.Equal(t, "agents.restore", f.session(t, l.workspace, l.owner).Reopen.Command)
	reopen := f.session(t, l.workspace, l.kids[0].Session).Reopen
	require.NotNil(t, reopen)
	assert.Equal(t, "agents.resume", reopen.Command)
}

func TestArchiveKeepsALockedWorktreeWithNoBox(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	git(t, l.repos[0], "worktree", "lock", "--reason", "on a USB drive", l.kids[0].Worktree)

	r := l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Worktree.Action)
	assert.Contains(t, r.Worktree.Why, "on a USB drive")
	assert.False(t, r.Anyway)
	choice := choose(r)
	choice["anyway"] = true

	out := l.archive(t, choice)

	assert.Len(t, out.Kept, 1)
	assert.DirExists(t, l.kids[0].Worktree)
}

func TestArchiveKeepsAWorktreeAnOutsideOpenSessionUsesAndNamesIt(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	outside := f.doneSessionIn(t, l.repos[0], l.kids[0].Worktree, "Outsider")

	r := l.plan(t).Repos[0]
	assert.Equal(t, "keep", r.Worktree.Action)
	assert.Contains(t, r.Worktree.Why, "Outsider")
	assert.True(t, r.Anyway)

	l.archive(t, choose(r))

	assert.DirExists(t, l.kids[0].Worktree)
	assert.NotEqual(t, "closed", f.session(t, workspaceOf(t, f, outside), outside).Status)
}

func TestReopenIsRefusedLikeRestore(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{"resume": true})
	workspace, _, second := f.twoFolders(t)
	var gone, moved, left child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"branch": "feat-gone", "prompt": "x"}, &gone))
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"branch": "feat-moved", "prompt": "x"}, &moved))
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"repo": second, "slug": "left", "prompt": "x"}, &left))
	for _, kid := range []child{gone, moved, left} {
		f.waitBranchChildDone(t, workspace, kid.Session)
		f.close(t, workspace, kid.Session)
	}
	repo := f.folderPaths(t, workspace)[0]
	git(t, repo, "worktree", "remove", "--force", gone.Worktree)
	git(t, repo, "worktree", "remove", "--force", moved.Worktree)
	git(t, repo, "worktree", "add", "-q", "-b", "someone-else", moved.Worktree)
	require.True(t, f.folderRemove(t, workspace, second).Removed)

	for kid, want := range map[string]string{gone.Session: "its worktree is gone", moved.Session: "someone-else", left.Session: "its folder left"} {
		require.ErrorContains(t, f.run(t, workspace, "agents.resume", map[string]any{"session": kid}, nil), want)
		assert.Equal(t, "closed", f.session(t, workspace, kid).Status)
	}
}

func TestTickingAnOutsideSessionNamesItAndTurnsTheButtonRed(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	tree := l.kids[0].Worktree
	f.doneSessionIn(t, l.repos[0], tree, "Outsider")
	l.openArchiveSheet(t)

	f.h.Event(t, l.workspace, "agents.sheet", "archiveSheet/tick/"+tree, "archiveTick", true)

	sheet := f.h.WaitSlot(t, l.workspace, "agents.sheet", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "button"), func(b sdui.Node) bool { return b.Props["variant"] == sdui.VariantDanger })
	})
	assert.Equal(t, "Archive · closes Outsider", sheetNodes(sheet, "button")[2].Props["label"])
	assert.Contains(t, sheetText(sheet), "closes Outsider")
	assert.NotContains(t, sheetText(sheet), `"loses`)
}
