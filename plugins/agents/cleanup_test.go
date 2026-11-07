package main_test

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/session"
	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

type cleanupGroup struct {
	ID    string   `json:"id"`
	Count int      `json:"count"`
	Bulk  bool     `json:"bulk"`
	Rows  []string `json:"rows"`
}

type cleanupPlan struct {
	Summary string         `json:"summary"`
	Total   int            `json:"total"`
	Stale   int            `json:"stale"`
	ByHand  int            `json:"byHand"`
	Groups  []cleanupGroup `json:"groups"`
	Cron    []struct {
		Name string `json:"name"`
		In   string `json:"in"`
	} `json:"cron"`
}

func (p cleanupPlan) group(id string) []string {
	for _, g := range p.Groups {
		if g.ID == id {
			return g.Rows
		}
	}
	return nil
}

func (f *fixture) cleanupPlan(t *testing.T, workspace string) cleanupPlan {
	t.Helper()
	var out cleanupPlan
	require.NoError(t, f.run(t, workspace, "agents.cleanup-plan", map[string]any{}, &out))
	return out
}

func commitIn(t *testing.T, tree, name string) {
	t.Helper()
	write(t, filepath.Join(tree, name), "x\n")
	git(t, tree, "add", ".")
	git(t, tree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", name)
}

func (f *fixture) sessionTree(t *testing.T, workspace, repo, branch string) string {
	t.Helper()
	var out child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"repo": repo, "branch": branch, "prompt": "x"}, &out))
	f.waitBranchChildDone(t, workspace, out.Session)
	f.close(t, workspace, out.Session)
	return out.Worktree
}

func (f *fixture) mergedTree(t *testing.T, workspace, repo, branch string) string {
	t.Helper()
	tree := f.sessionTree(t, workspace, repo, branch)
	commitIn(t, tree, branch+".go")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "--no-ff", "-m", "land", branch)
	return tree
}

func (f *fixture) runningInTree(t *testing.T, repo, tree string) {
	t.Helper()
	created, err := f.h.Svc.CreateWorkspace(t.Context(), "other", []string{repo})
	require.NoError(t, err)
	other := created.ID
	require.NoError(t, f.run(t, other, "git.worktree-switch", map[string]any{"repo": repo, "path": tree}, nil))
	var out child
	require.NoError(t, f.run(t, other, "agents.new", map[string]any{"prompt": "long"}, &out))
	f.waitStatus(t, other, out.Session, "running")
	require.Equal(t, tree, f.session(t, other, out.Session).Worktree)
	require.NoError(t, f.run(t, other, "git.worktree-switch", map[string]any{"repo": repo, "path": repo}, nil))
}

func (f *fixture) terminalIn(t *testing.T, workspace, tree, title string) {
	t.Helper()
	_, err := f.h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: workspace, Title: title, Owner: "terminals", Command: []string{"sh", "-c", "exec cat"}, Cwd: tree})
	require.NoError(t, err)
}

func (f *fixture) openCleanup(t *testing.T, workspace string) {
	t.Helper()
	require.NoError(t, f.run(t, workspace, "agents.cleanup-sheet", map[string]any{}, nil))
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
}

func removeButton(n sdui.Node) (sdui.Node, bool) {
	buttons := sheetNodes(n, "button")
	i := slices.IndexFunc(buttons, func(b sdui.Node) bool { return b.ID == "cleanupSheet/remove" })
	if i < 0 {
		return sdui.Node{}, false
	}
	return buttons[i], true
}

func (f *fixture) waitButton(t *testing.T, workspace, want string) sdui.Node {
	t.Helper()
	return f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		b, ok := removeButton(n)
		return ok && b.Props["label"] == want && b.Props["disabled"] != true
	})
}

func (f *fixture) cleanupPress(t *testing.T, workspace string, action string, node string, value any) {
	t.Helper()
	f.h.Event(t, workspace, "agents.sheet", node, action, value)
}

func TestCleanUpRemovesWhatIsTickedAndTheCountsReconcile(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	merged := f.mergedTree(t, workspace, repo, "feat-a")
	hand := addWorktree(t, repo, "by-hand")

	plan := f.cleanupPlan(t, workspace)
	assert.Equal(t, 2, plan.Total)
	assert.Equal(t, plan.Total, plan.Stale+plan.ByHand)
	assert.Equal(t, "2 · 1 stale, 1 by hand", plan.Summary)
	assert.Equal(t, []string{merged}, plan.group("merged"))
	assert.Equal(t, []string{hand}, plan.group("byHand"))

	f.openCleanup(t, workspace)
	f.waitButton(t, workspace, "Remove 1")
	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", noSheet)
	assert.NoDirExists(t, merged)
	assert.False(t, hasBranch(t, repo, "feat-a"))
	assert.DirExists(t, hand)
}

func TestCleanUpNeverListsALiveWorktreeOrOneOpenInCode(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"match": "long", "sleep": 3000}}})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	inCode := addWorktree(t, repo, "in-code")
	idle := addWorktree(t, repo, "idle")
	f.runningInTree(t, repo, addWorktree(t, repo, "live"))
	require.NoError(t, f.run(t, workspace, "git.worktree-switch", map[string]any{"repo": repo, "path": inCode}, nil))

	assert.Equal(t, []string{idle}, f.cleanupPlan(t, workspace).group("byHand"))
}

func TestCleanUpLeavesOutAChildOfAnOpenUnsettledTask(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.mergeInto(t, 0)

	assert.Zero(t, f.cleanupPlan(t, l.workspace).Total)

	settled := f.landing(t, "merge", "pause", 1)
	settled.settle(t)

	assert.Equal(t, []string{settled.kids[0].Worktree}, f.cleanupPlan(t, settled.workspace).group("merged"), "once it landed nothing holds it")
}

func TestCleanUpKeepsAWorktreeWithATerminalAndNamesIt(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	f.terminalIn(t, workspace, tree, "Build")
	f.openCleanup(t, workspace)
	f.waitButton(t, workspace, "Remove 1")

	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return strings.Contains(sheetText(n), "terminal Build is open in it") })
	assert.DirExists(t, tree)
	assert.True(t, hasBranch(t, repo, "feat-a"))
}

func TestCleanUpTurnsRedOnlyOneAtATime(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	hand := addWorktree(t, repo, "by-hand")
	write(t, filepath.Join(hand, "scratch.go"), "x\n")
	require.False(t, slices.ContainsFunc(f.cleanupPlan(t, workspace).Groups, func(g cleanupGroup) bool { return g.Bulk }))
	f.openCleanup(t, workspace)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "toggle"), func(b sdui.Node) bool { return b.ID == "cleanupSheet/tick/"+hand })
	})

	f.cleanupPress(t, workspace, "cleanupTick", "cleanupSheet/tick/"+hand, true)

	sheet := f.waitButton(t, workspace, "Remove 1 · lose 1 file")
	button, _ := removeButton(sheet)
	assert.Equal(t, sdui.VariantDanger, button.Props["variant"])
	assert.Contains(t, sheetText(sheet), "1 file will be lost")
}

func TestCleanUpLeavesIgnoredFilesUntickedUntilTheGroupIsTicked(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	write(t, filepath.Join(tree, ".git-ignored-here"), "x\n")
	write(t, filepath.Join(repo, ".git", "info", "exclude"), ".git-ignored-here\n")
	f.openCleanup(t, workspace)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		return strings.Contains(sheetText(n), "1 worktree hold ignored files")
	})

	f.cleanupPress(t, workspace, "cleanupGroup", "cleanupSheet/group/merged", true)

	f.waitButton(t, workspace, "Remove 1 · lose 1 ignored file")
}

func TestCleanUpKeepsAWorktreeThatBecameDirtyAfterItWasRead(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	f.openCleanup(t, workspace)
	f.waitButton(t, workspace, "Remove 1")
	late := filepath.Join(tree, "late.go")
	write(t, late, "x\n")

	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return strings.Contains(sheetText(n), "it changed since you looked") })
	assert.Len(t, sheetNodes(sheet, "button"), 1)
	assert.FileExists(t, late)
	assert.True(t, hasBranch(t, repo, "feat-a"))
}

func TestCleanUpKeepsAWorktreeWhoseMergedChangedAfterItWasRead(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	f.openCleanup(t, workspace)
	f.waitButton(t, workspace, "Remove 1")
	git(t, repo, "reset", "-q", "--hard", "HEAD~1")

	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return strings.Contains(sheetText(n), "it changed since you looked") })
	assert.DirExists(t, tree)
	assert.True(t, hasBranch(t, repo, "feat-a"))
}

func TestCleanUpWhoseCachedMergedIsStaleMovesTheRowUnticked(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	require.Equal(t, []string{tree}, f.cleanupPlan(t, workspace).group("merged"))
	git(t, repo, "reset", "-q", "--hard", "HEAD~1")

	got := f.cleanupPlan(t, workspace)

	assert.Empty(t, got.group("merged"), "the state read beats the cached index")
	f.openCleanup(t, workspace)
	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		b, ok := removeButton(n)
		return ok && b.Props["label"] == "Remove 0"
	})
	assert.NotContains(t, sheetText(sheet), `"Remove 1"`)
}

func TestCleanUpKeepsTheBranchThatIsStillOnTheRemote(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	git(t, repo, "remote", "add", "origin", remote)
	tree := f.sessionTree(t, workspace, repo, "feat-a")
	commitIn(t, tree, "a2.go")
	git(t, tree, "push", "-q", "-u", "origin", "feat-a")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "merge", "-q", "--no-ff", "-m", "land", "feat-a")
	f.openCleanup(t, workspace)
	sheet := f.waitButton(t, workspace, "Remove 1")
	assert.Contains(t, sheetText(sheet), "still on the remote")

	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", noSheet)
	assert.NoDirExists(t, tree)
	assert.True(t, hasBranch(t, repo, "feat-a"))
	assert.Contains(t, git(t, remote, "branch", "--list"), "feat-a")
}

func TestCleanUpGivesANestedWorktreeNoBox(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	nested := filepath.Join(tree, "vendor")
	require.NoError(t, exec.Command("mkdir", nested).Run())
	git(t, nested, "init", "-q")
	commitIn(t, nested, "x.go")
	f.openCleanup(t, workspace)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return strings.Contains(sheetText(n), "holds a repo") })

	assert.False(t, slices.ContainsFunc(sheetNodes(sheet, "toggle"), func(b sdui.Node) bool { return b.ID == "cleanupSheet/tick/"+tree }))
	assert.Equal(t, []string{tree}, f.cleanupPlan(t, workspace).group("risky"))
}

func TestCleanUpNamesAndPausesACronJob(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	job := f.cronJob(t, workspace, repo, "feat-a")
	plan := f.cleanupPlan(t, workspace)
	require.Len(t, plan.Cron, 1)
	assert.Equal(t, tree, plan.Cron[0].In)
	f.openCleanup(t, workspace)
	sheet := f.waitButton(t, workspace, "Remove 1")
	assert.Contains(t, sheetText(sheet), "nightly")

	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", noSheet)
	var shown struct {
		Enabled bool `json:"enabled"`
	}
	require.NoError(t, f.run(t, workspace, "cron.show", map[string]any{"id": job}, &shown))
	assert.False(t, shown.Enabled)
}

func TestCleanUpCountsOnlyWhatTheFilterShows(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	first := f.mergedTree(t, workspace, repo, "feat-a")
	second := f.mergedTree(t, workspace, repo, "feat-b")
	f.openCleanup(t, workspace)
	f.waitButton(t, workspace, "Remove 2")

	f.cleanupPress(t, workspace, "cleanupFilter", "cleanupSheet/filter", "feat-a")
	f.waitButton(t, workspace, "Remove 1")
	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", noSheet)
	assert.NoDirExists(t, first)
	assert.DirExists(t, second)
}

func TestCleanUpRefusesAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.doneSession(t, workspace)

	for _, command := range []string{"agents.cleanup-plan", "agents.cleanup-sheet"} {
		require.Error(t, f.runAs(t, workspace, session, command, map[string]any{}, nil), command)
		require.Error(t, f.runFrom(t, workspace, session, command, map[string]any{}, nil), command)
	}
}

func workspaceOf(t *testing.T, f *fixture, session string) string {
	t.Helper()
	spaces, err := f.h.Svc.ListWorkspaces(t.Context())
	require.NoError(t, err)
	for _, w := range spaces {
		var out []record
		if err := f.run(t, w.ID, "agents.sessions", map[string]any{"session": session}, &out); err == nil && len(out) == 1 {
			return w.ID
		}
	}
	require.Fail(t, "no workspace holds the session")
	return ""
}

func (f *fixture) doneSessionIn(t *testing.T, repo, tree, title string) string {
	t.Helper()
	created, err := f.h.Svc.CreateWorkspace(t.Context(), "other", []string{repo})
	require.NoError(t, err)
	require.NoError(t, f.run(t, created.ID, "git.worktree-switch", map[string]any{"repo": repo, "path": tree}, nil))
	var out child
	require.NoError(t, f.run(t, created.ID, "agents.new", map[string]any{"prompt": "x", "title": title}, &out))
	f.waitBranchChildDone(t, created.ID, out.Session)
	require.NoError(t, f.run(t, created.ID, "git.worktree-switch", map[string]any{"repo": repo, "path": repo}, nil))
	return out.Session
}

func TestCleanUpNeverTicksALockedWorktree(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	git(t, repo, "worktree", "lock", "--reason", "on a USB drive", tree)

	assert.Equal(t, []string{tree}, f.cleanupPlan(t, workspace).group("risky"))

	f.openCleanup(t, workspace)
	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		b, ok := removeButton(n)
		return ok && b.Props["label"] == "Remove 0" && strings.Contains(sheetText(n), "on a USB drive")
	})
	assert.False(t, slices.ContainsFunc(sheetNodes(sheet, "toggle"), func(b sdui.Node) bool { return b.ID == "cleanupSheet/tick/"+tree }))
}

func TestCleanUpDoesNotPreTickAWorktreeOfAnOpenSessionAndNamesIt(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	session := f.doneSessionIn(t, repo, tree, "Still open")
	f.openCleanup(t, workspace)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		b, ok := removeButton(n)
		return ok && b.Props["label"] == "Remove 0" && strings.Contains(sheetText(n), "closes Still open")
	})
	require.NotEmpty(t, sheetNodes(sheet, "toggle"))

	f.cleanupPress(t, workspace, "cleanupGroup", "cleanupSheet/group/merged", true)
	after := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "toggle")) > 0 })
	button, _ := removeButton(after)
	assert.Equal(t, "Remove 0", button.Props["label"])
	assert.NotEqual(t, "closed", f.session(t, workspaceOf(t, f, session), session).Status)
}

func TestCleanUpKeepsAWorktreeASessionOpenedAfterItWasRead(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	tree := f.mergedTree(t, workspace, repo, "feat-a")
	f.openCleanup(t, workspace)
	f.waitButton(t, workspace, "Remove 1")
	session := f.doneSessionIn(t, repo, tree, "Late")

	f.cleanupPress(t, workspace, "cleanupConfirm", "cleanupSheet/remove", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return strings.Contains(sheetText(n), "it changed since you looked") })
	assert.DirExists(t, tree)
	assert.NotEqual(t, "closed", f.session(t, workspaceOf(t, f, session), session).Status)
}

func TestCleanUpNeverListsTheDefaultCheckoutOfABareLayout(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	seed := newRepo(t)
	bare := t.TempDir()
	git(t, bare, "clone", "-q", "--bare", seed, bare)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	mainTree, feat := filepath.Join(dir, "main"), filepath.Join(dir, "feat")
	git(t, bare, "worktree", "add", "-q", mainTree, "main")
	git(t, bare, "worktree", "add", "-q", "-b", "feat", feat)
	workspace := f.workspace(t, feat)

	assert.Zero(t, f.cleanupPlan(t, workspace).Total)
}

func TestCleanUpOfOneFolderCountsThatFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, first, second := f.twoFolders(t)
	addWorktree(t, first, "here")
	addWorktree(t, first, "there")
	tree := addWorktree(t, second, "only")
	require.NoError(t, f.run(t, workspace, "agents.cleanup-sheet", map[string]any{"folder": second}, nil))

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })

	assert.Contains(t, sheetText(sheet), "1 · 0 stale, 1 by hand")
	assert.Contains(t, sheetNodes(sheet, "sheet")[0].Props["title"], filepath.Base(second))
	assert.NotContains(t, sheetText(sheet), "3 · 0 stale")
	assert.DirExists(t, tree)
}

func TestCleanUpListsAWorktreeOfTwoFoldersOfOneRepoOnce(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	for _, sub := range []string{"one", "two"} {
		write(t, filepath.Join(repo, sub+".md"), "x\n")
		require.NoError(t, exec.Command("mkdir", filepath.Join(repo, sub)).Run())
		write(t, filepath.Join(repo, sub, "a.go"), "x\n")
	}
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "subs")
	workspace := f.workspace(t, filepath.Join(repo, "one"))
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, filepath.Join(repo, "two"), contract.FolderMeta{})
	require.NoError(t, err)
	tree := addWorktree(t, repo, "by-hand")

	plan := f.cleanupPlan(t, workspace)

	assert.Equal(t, 1, plan.Total)
	assert.Equal(t, []string{tree}, plan.group("byHand"))
}

func TestCleanUpDoesNotPreTickAChildJustRestored(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	l := f.landing(t, "merge", "pause", 1)
	l.settle(t)
	f.close(t, l.workspace, l.owner)
	require.NoError(t, f.run(t, l.workspace, "agents.restore", map[string]any{"session": l.owner}, nil))
	f.waitStatus(t, l.workspace, l.kids[0].Session, "idle")
	f.openCleanup(t, l.workspace)

	f.h.WaitSlot(t, l.workspace, "agents.sheet", func(n sdui.Node) bool {
		b, ok := removeButton(n)
		return ok && b.Props["label"] == "Remove 0" && strings.Contains(sheetText(n), "closes ")
	})
}

func TestCleanUpGroupBoxStaysOnBesideARowWithAnOpenSessionAndUnticksTheRest(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	first := f.mergedTree(t, workspace, repo, "feat-a")
	second := f.mergedTree(t, workspace, repo, "feat-b")
	f.doneSessionIn(t, repo, second, "Still open")
	require.NotEmpty(t, first)
	f.openCleanup(t, workspace)

	sheet := f.waitButton(t, workspace, "Remove 1")
	box := sheetNodes(sheet, "toggle")[slices.IndexFunc(sheetNodes(sheet, "toggle"), func(b sdui.Node) bool { return b.ID == "cleanupSheet/group/merged" })]
	assert.Equal(t, true, box.Props["value"])

	f.cleanupPress(t, workspace, "cleanupGroup", "cleanupSheet/group/merged", false)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		b, ok := removeButton(n)
		return ok && b.Props["label"] == "Remove 0"
	})
}
