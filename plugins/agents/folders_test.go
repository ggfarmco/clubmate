package main_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

type folderSession struct {
	Session  string `json:"session"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
}

type folderRemoveResult struct {
	Removed  bool            `json:"removed"`
	Blockers []folderSession `json:"blockers"`
	Tasks    []string        `json:"tasks"`
	Last     bool            `json:"last"`
}

type folderImpact struct {
	Sessions  []folderSession `json:"sessions"`
	Worktrees []struct {
		Path   string `json:"path"`
		Branch string `json:"branch"`
	} `json:"worktrees"`
}

func (f *fixture) twoFolders(t *testing.T) (string, string, string) {
	t.Helper()
	first, second := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, second, contract.FolderMeta{})
	require.NoError(t, err)
	return workspace, first, second
}

func (f *fixture) folderRemove(t *testing.T, workspace, path string) folderRemoveResult {
	t.Helper()
	var out folderRemoveResult
	require.NoError(t, f.run(t, workspace, "agents.folder-remove", map[string]any{"path": path}, &out))
	return out
}

func TestFolderRemoveRefusedForRunningSession(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"sleep": 2000}}})
	workspace, first, _ := f.twoFolders(t)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	out := f.folderRemove(t, workspace, first)

	assert.False(t, out.Removed)
	require.Len(t, out.Blockers, 1)
	assert.Equal(t, session, out.Blockers[0].Session)
	assert.Equal(t, "running", out.Blockers[0].Status)
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderRemoveDropsTheFolderWhenNothingRunsThere(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{"prompt": []map[string]any{{"sleep": 2000}}})
	workspace, first, second := f.twoFolders(t)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	out := f.folderRemove(t, workspace, second)

	assert.True(t, out.Removed)
	assert.Empty(t, out.Blockers)
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
}

func TestFolderRemoveRefusesAPathOutsideTheWorkspace(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, _ := f.twoFolders(t)

	err := f.run(t, workspace, "agents.folder-remove", map[string]any{"path": t.TempDir()}, nil)

	require.Error(t, err)
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderRemoveRefusesAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	session := f.doneSession(t, workspace)
	args := map[string]any{"path": second}

	require.Error(t, f.runAs(t, workspace, session, "agents.folder-remove", args, nil))
	require.Error(t, f.runFrom(t, workspace, session, "agents.folder-remove", args, nil))

	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderImpactListsSessionsAndWorktrees(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, first, second := f.twoFolders(t)
	parent := f.taskParent(t, workspace, second)
	var kid child
	require.NoError(t, f.runFrom(t, workspace, parent, "agents.new", map[string]any{"repo": second, "slug": "pause", "prompt": "hi"}, &kid))
	f.waitBranchChildDone(t, workspace, kid.Session)

	var there, here folderImpact
	require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": second}, &there))
	require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": first}, &here))

	require.Len(t, there.Sessions, 1)
	assert.Equal(t, kid.Session, there.Sessions[0].Session)
	require.Len(t, there.Worktrees, 1)
	assert.Equal(t, kid.Worktree, there.Worktrees[0].Path)
	assert.Equal(t, "feat/pause", there.Worktrees[0].Branch)
	require.Len(t, here.Sessions, 1)
	assert.Equal(t, parent, here.Sessions[0].Session)
	assert.Empty(t, here.Worktrees)
}

func TestFolderRemoveRefusedWhileATaskIsUnsettledThere(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1, 2)

	out := f.folderRemove(t, l.workspace, l.repos[1])

	assert.False(t, out.Removed)
	assert.Equal(t, []string{"pause"}, out.Tasks)
	assert.Len(t, f.folders(t, l.workspace), 2)
}

func TestTaskInAnotherWorkspaceDoesNotHoldTheFolder(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	l.waitOwnerWoken(t)
	other := f.workspace(t, newRepo(t))
	_, err := f.h.Svc.AddRepo(t.Context(), other, l.repos[0], contract.FolderMeta{})
	require.NoError(t, err)

	out := f.folderRemove(t, other, l.repos[0])

	assert.True(t, out.Removed)
	assert.Empty(t, out.Tasks)
}

func startWithCron(t *testing.T, script map[string]any) *fixture {
	t.Helper()
	return start(t, script, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"), testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"))
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", ".")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "work")
}

func addWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	path := filepath.Join(repo, ".worktrees", branch)
	out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", branch, path).CombinedOutput()
	require.NoError(t, err, string(out))
	return path
}

func (f *fixture) cronJob(t *testing.T, workspace, repo, branch string) string {
	t.Helper()
	var job struct {
		ID string `json:"id"`
	}
	args := fmt.Sprintf(`{"repo":%q,"branch":%q,"prompt":"go"}`, repo, branch)
	testutil.Eventually(t, func() bool {
		return f.run(t, workspace, "cron.add", map[string]any{"name": "nightly", "command": "agents.new", "args": args, "every": "1h"}, &job) == nil
	}, wait)
	return job.ID
}

func (f *fixture) openSheet(t *testing.T, workspace, path string) sdui.Node {
	t.Helper()
	require.NoError(t, f.run(t, workspace, "agents.folder-sheet", map[string]any{"path": path}, nil))
	return f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return len(sheetNodes(n, "sheet")) == 1 })
}

func sheetNodes(n sdui.Node, kind string) []sdui.Node {
	var out []sdui.Node
	if n.Type == kind {
		out = append(out, n)
	}
	for _, child := range n.Children {
		out = append(out, sheetNodes(child, kind)...)
	}
	return out
}

func TestFolderImpactNamesWhatIsTiedToTheFolder(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	require.NoError(t, os.MkdirAll(filepath.Join(second, ".clubmate", "graphs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(second, ".clubmate", "graphs", "scope-lint.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(second, "CLAUDE.md"), []byte("rules"), 0o600))
	tree := addWorktree(t, second, "feat-x")
	require.NoError(t, os.WriteFile(filepath.Join(tree, "new.go"), []byte("n"), 0o600))
	job := f.cronJob(t, workspace, second, "feat-x")

	var out struct {
		Name      string   `json:"name"`
		Graphs    []string `json:"graphs"`
		Worktrees []struct {
			Branch string `json:"branch"`
			Dirty  int    `json:"dirty"`
		} `json:"worktrees"`
		Instructions []string `json:"instructions"`
		CronJobs     []struct {
			ID string `json:"id"`
		} `json:"cronJobs"`
		Last bool `json:"last"`
	}
	require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": second}, &out))

	assert.Equal(t, filepath.Base(second), out.Name)
	assert.Equal(t, []string{"scope-lint"}, out.Graphs)
	assert.Equal(t, []string{"CLAUDE.md"}, out.Instructions)
	require.Len(t, out.Worktrees, 1)
	assert.Equal(t, "feat-x", out.Worktrees[0].Branch)
	assert.Equal(t, 1, out.Worktrees[0].Dirty)
	require.Len(t, out.CronJobs, 1)
	assert.Equal(t, job, out.CronJobs[0].ID)
	assert.False(t, out.Last)
}

func TestFolderJobsTieABareAgentJobToTheFirstGitFolder(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	plain, repo, other := t.TempDir(), newRepo(t), newRepo(t)
	workspace := f.workspace(t, plain)
	for _, path := range []string{repo, other} {
		_, err := f.h.Svc.AddRepo(t.Context(), workspace, path, contract.FolderMeta{})
		require.NoError(t, err)
	}
	addWorktree(t, other, "feat-x")
	args := `{"branch":"feat-x","prompt":"go"}`
	testutil.Eventually(t, func() bool {
		return f.run(t, workspace, "cron.add", map[string]any{"name": "nightly", "command": "agents.new", "args": args, "every": "1h"}, nil) == nil
	}, wait)
	jobsIn := func(path string) int {
		t.Helper()
		var out struct {
			CronJobs []struct {
				ID string `json:"id"`
			} `json:"cronJobs"`
		}
		require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": path}, &out))
		return len(out.CronJobs)
	}

	assert.Equal(t, 1, jobsIn(repo), "it runs in the first git folder, whose branch doesn't exist yet")
	assert.Zero(t, jobsIn(plain))
	assert.Zero(t, jobsIn(other), "a same-named branch in another folder is not it")
}

func TestFolderSheetRemovesTheTickedWorktreeAndPausesJobs(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, first, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	kept := addWorktree(t, second, "feat-y")
	require.NoError(t, os.WriteFile(filepath.Join(tree, "new.go"), []byte("n"), 0o600))
	job := f.cronJob(t, workspace, second, "feat-x")
	f.openSheet(t, workspace, second)

	f.h.Event(t, workspace, "agents.sheet", "folderSheet/tick/"+tree, "folderTick", true)
	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		return slices.ContainsFunc(sheetNodes(n, "button"), func(b sdui.Node) bool { return b.Props["variant"] == sdui.VariantDanger })
	})
	assert.Contains(t, sheetNodes(sheet, "button")[1].Props["label"], "lose 1 file")

	f.h.Event(t, workspace, "agents.sheet", "folderSheet/remove", "folderConfirm", nil)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
	assert.NoDirExists(t, tree)
	assert.DirExists(t, kept)
	assert.FileExists(t, filepath.Join(second, ".git", "HEAD"))
	var shown struct {
		Enabled bool `json:"enabled"`
		Paused  []struct {
			Reason string `json:"reason"`
		} `json:"paused"`
	}
	require.NoError(t, f.run(t, workspace, "cron.show", map[string]any{"id": job}, &shown))
	assert.False(t, shown.Enabled)
	require.Len(t, shown.Paused, 1)
	assert.Contains(t, shown.Paused[0].Reason, filepath.Base(second)+" left ")
}

func TestFolderSheetNamesWhatIsLost(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	require.NoError(t, os.WriteFile(filepath.Join(second, ".gitignore"), []byte("*.log\n.env\n"), 0o600))
	commitAll(t, second)
	ignored := addWorktree(t, second, "feat-x")
	detached := addWorktree(t, second, "feat-y")
	for _, name := range []string{"a.log", ".env"} {
		require.NoError(t, os.WriteFile(filepath.Join(ignored, name), []byte("n"), 0o600))
	}
	git(t, detached, "checkout", "-q", "--detach")
	require.NoError(t, os.WriteFile(filepath.Join(detached, "loose.go"), []byte("n"), 0o600))
	commitAll(t, detached)
	require.NoError(t, os.WriteFile(filepath.Join(detached, "new.go"), []byte("n"), 0o600))
	f.openSheet(t, workspace, second)

	f.h.Event(t, workspace, "agents.sheet", "folderSheet/tick/"+ignored, "folderTick", true)
	f.h.Event(t, workspace, "agents.sheet", "folderSheet/tick/"+detached, "folderTick", true)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		label, _ := sheetNodes(n, "button")[1].Props["label"].(string)
		return strings.Contains(label, "commit")
	})
	remove := sheetNodes(sheet, "button")[1]
	assert.Equal(t, "Remove · lose 1 file, .env and 1 commit", remove.Props["label"])
	assert.Equal(t, sdui.VariantDanger, remove.Props["variant"])
	assert.Empty(t, sheetNodes(sheet, "sheet")[0].Props["note"], "the button needs the room")
	var words []string
	for _, n := range sheetNodes(sheet, "text") {
		words = append(words, fmt.Sprint(n.Props["text"]))
	}
	assert.Contains(t, words, "loses .env")
	assert.Contains(t, words, "loses 1 file and 1 detached commit")
}

func (f *fixture) runningIn(t *testing.T, dir string) {
	t.Helper()
	other := f.workspace(t, dir)
	session := f.newSession(t, other)
	f.prompt(t, other, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })
}

func (f *fixture) tick(t *testing.T, workspace, path string) {
	t.Helper()
	f.h.Event(t, workspace, "agents.sheet", "folderSheet/tick/"+path, "folderTick", true)
}

func (f *fixture) confirmSheet(t *testing.T, workspace string) {
	t.Helper()
	f.h.Event(t, workspace, "agents.sheet", "folderSheet/remove", "folderConfirm", nil)
}

func (f *fixture) waitDanger(t *testing.T, workspace string) sdui.Node {
	t.Helper()
	return f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		s := sheetNodes(n, "sheet")
		return len(s) == 1 && s[0].Props["noteTone"] == "danger"
	})
}

func (f *fixture) jobEnabled(t *testing.T, workspace, job string) bool {
	t.Helper()
	var shown struct {
		Enabled bool `json:"enabled"`
	}
	require.NoError(t, f.run(t, workspace, "cron.show", map[string]any{"id": job}, &shown))
	return shown.Enabled
}

func TestFolderRemoveLetsASessionInAnotherWorkspaceBe(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{"prompt": []map[string]any{{"sleep": 3000}}})
	workspace, _, second := f.twoFolders(t)
	f.runningIn(t, addWorktree(t, second, "feat-x"))

	out := f.folderRemove(t, workspace, second)

	assert.True(t, out.Removed)
	assert.Empty(t, out.Blockers)
}

func TestFolderSheetHoldsATickedWorktreeBeyondTheFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"sleep": 3000}}})
	repo := newRepo(t)
	sub := filepath.Join(repo, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	write(t, filepath.Join(sub, "a.go"), "a")
	commitAll(t, repo)
	workspace := f.workspace(t, newRepo(t))
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, sub, contract.FolderMeta{})
	require.NoError(t, err)
	tree := addWorktree(t, repo, "feat-x")
	f.runningIn(t, tree)
	f.openSheet(t, workspace, sub)

	f.tick(t, workspace, tree)

	sheet := f.waitDanger(t, workspace)
	assert.Equal(t, true, sheetNodes(sheet, "button")[1].Props["disabled"])
	f.confirmSheet(t, workspace)
	assert.DirExists(t, tree)
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderSheetClosesSessionsOfEveryWorkspace(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	other := f.workspace(t, tree)
	session := f.doneSession(t, other)
	var impact struct {
		Worktrees []struct {
			Sessions int `json:"sessions"`
		} `json:"worktrees"`
	}
	require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": second}, &impact))
	require.Len(t, impact.Worktrees, 1)
	assert.Equal(t, 1, impact.Worktrees[0].Sessions)
	f.openSheet(t, workspace, second)

	f.tick(t, workspace, tree)
	f.confirmSheet(t, workspace)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.NoDirExists(t, tree)
	assert.Equal(t, "closed", f.session(t, other, session).Status)
}

func TestTaskInAnotherWorkspaceHoldsATickedWorktree(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	other := f.workspace(t, newRepo(t))
	_, err := f.h.Svc.AddRepo(t.Context(), other, l.repos[0], contract.FolderMeta{})
	require.NoError(t, err)
	f.openSheet(t, other, l.repos[0])

	f.tick(t, other, l.kids[0].Worktree)

	sheet := f.waitDanger(t, other)
	assert.Contains(t, sheetNodes(sheet, "sheet")[0].Props["note"], "pause")
	assert.Equal(t, true, sheetNodes(sheet, "button")[1].Props["disabled"])
}

func TestTaskHoldsATickedWorktreeFromASubfolderOfItsRepo(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	l := f.landing(t, "merge", "pause", 1)
	sub := filepath.Join(l.repos[0], "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	write(t, filepath.Join(sub, "a.go"), "a")
	commitAll(t, l.repos[0])
	other := f.workspace(t, newRepo(t))
	_, err := f.h.Svc.AddRepo(t.Context(), other, sub, contract.FolderMeta{})
	require.NoError(t, err)
	f.openSheet(t, other, sub)

	f.tick(t, other, l.kids[0].Worktree)

	sheet := f.waitDanger(t, other)
	assert.Contains(t, sheetNodes(sheet, "sheet")[0].Props["note"], "pause")
}

func TestFolderSheetResumesJobsWhenTheFolderStays(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, newRepo(t), contract.FolderMeta{})
	require.NoError(t, err)
	job := f.cronJob(t, workspace, second, "feat-x")
	f.openSheet(t, workspace, second)
	_, err = f.h.Svc.RemoveFolder(t.Context(), workspace, second)
	require.NoError(t, err)

	f.confirmSheet(t, workspace)

	f.waitDanger(t, workspace)
	assert.True(t, f.jobEnabled(t, workspace, job))
}

func TestFolderSheetRefusesWhenCronStopsAnswering(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	f.cronJob(t, workspace, second, "feat-x")
	f.openSheet(t, workspace, second)
	f.tick(t, workspace, tree)
	require.NoError(t, f.h.Sup.SetEnabled(t.Context(), "cron", false))

	f.confirmSheet(t, workspace)

	sheet := f.waitDanger(t, workspace)
	assert.Contains(t, sheetNodes(sheet, "sheet")[0].Props["note"], "Cron stopped answering")
	assert.Len(t, f.folders(t, workspace), 2)
	assert.DirExists(t, tree)

	f.confirmSheet(t, workspace)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.Len(t, f.folders(t, workspace), 1)
}

func TestFolderSheetSaysWhichWorktreeStayed(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, first, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	f.terminalIn(t, workspace, tree, "Build")
	f.openSheet(t, workspace, second)
	f.tick(t, workspace, tree)

	f.confirmSheet(t, workspace)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		s := sheetNodes(n, "sheet")
		return len(s) == 1 && strings.HasSuffix(s[0].Props["title"].(string), "left demo")
	})
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace), "the folder goes first")
	assert.DirExists(t, tree)
	assert.Contains(t, sheetNodes(sheet, "markdown")[0].Props["text"], "`"+tree+"` stays on disk: terminal Build is open in it")

	f.h.Event(t, workspace, "agents.sheet", "folderSheet/close", "folderCancel", nil)
	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
}

func TestFolderSheetOffersNoBoxForAWorktreeWithSubmodules(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	git(t, tree, "init", "-q", "vendored")

	sheet := f.openSheet(t, workspace, second)

	assert.Empty(t, sheetNodes(sheet, "toggle"))
	var words []string
	for _, n := range sheetNodes(sheet, "text") {
		words = append(words, fmt.Sprint(n.Props["text"]))
	}
	assert.Contains(t, words, "Also remove the worktree · holds a repo Clubmate can't read, so it stays")
}

func TestFolderRemoveRefusedWhenCronFails(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	f.cronJob(t, workspace, second, "feat-x")
	require.NoError(t, f.h.Sup.SetEnabled(t.Context(), "cron", false))

	err := f.run(t, workspace, "agents.folder-remove", map[string]any{"path": second}, nil)

	require.ErrorContains(t, err, "Cron stopped answering")
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderJobsOfOneSubfolderLeaveItsSibling(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	repo := newRepo(t)
	a, b := filepath.Join(repo, "a"), filepath.Join(repo, "b")
	for _, dir := range []string{a, b} {
		require.NoError(t, os.Mkdir(dir, 0o755))
		write(t, filepath.Join(dir, "f.go"), "f")
	}
	commitAll(t, repo)
	workspace := f.workspace(t, a)
	_, err := f.h.Svc.AddRepo(t.Context(), workspace, b, contract.FolderMeta{})
	require.NoError(t, err)
	f.cronJob(t, workspace, b, "feat-x")
	jobsIn := func(path string) int {
		t.Helper()
		var out struct {
			CronJobs []struct {
				ID string `json:"id"`
			} `json:"cronJobs"`
		}
		require.NoError(t, f.run(t, workspace, "agents.folder-impact", map[string]any{"path": path}, &out))
		return len(out.CronJobs)
	}

	assert.Zero(t, jobsIn(a))
	assert.Equal(t, 1, jobsIn(b))
}

type heldConfirm struct {
	asked   chan struct{}
	release chan struct{}
}

func (f *fixture) holdConfirms(t *testing.T) *heldConfirm {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := f.h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	held := &heldConfirm{asked: make(chan struct{}, 8), release: make(chan struct{})}
	handler := jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) {
		held.asked <- struct{}{}
		<-held.release
		return map[string]bool{"approved": true}, nil
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() {
		conn.Close()
		server.Close()
	})
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"ui.confirm"}}, nil))
	return held
}

func TestRemovingAFolderStallsNothingElseAndRefusesItsSessions(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	require.NoError(t, os.Mkdir(filepath.Join(second, ".clubmate"), 0o755))
	write(t, filepath.Join(second, ".clubmate", "teardown.sh"), "echo written > late.go\n")
	commitAll(t, second)
	tree := addWorktree(t, second, "feat-x")
	elsewhere := f.workspace(t, newRepo(t))
	there := f.workspace(t, second)
	idle := f.doneSession(t, there)
	held := f.holdConfirms(t)
	f.openSheet(t, workspace, second)
	f.tick(t, workspace, tree)
	f.confirmSheet(t, workspace)
	<-held.asked

	done := make(chan error, 1)
	go func() { done <- f.run(t, elsewhere, "agents.new", map[string]any{}, nil) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("a session elsewhere waited for the removal")
	}
	require.ErrorContains(t, f.run(t, there, "agents.new", map[string]any{}, nil), "being removed")
	require.ErrorContains(t, f.run(t, there, "agents.prompt", map[string]any{"session": idle, "text": "go"}, nil), "being removed")

	close(held.release)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return n.Type == "empty" })
	assert.NoDirExists(t, tree)
	require.NoError(t, f.run(t, there, "agents.new", map[string]any{}, nil))
}

func TestFolderSheetOffersNoBoxForALockedWorktree(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	git(t, second, "worktree", "lock", "--reason", "on a USB drive", tree)

	f.openSheet(t, workspace, second)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool { return strings.Contains(sheetText(n), "on a USB drive") })
	assert.False(t, slices.ContainsFunc(sheetNodes(sheet, "toggle"), func(b sdui.Node) bool { return b.ID == "folderSheet/tick/"+tree }))
}

func TestFolderSheetStopsWhenTheCostChanged(t *testing.T) {
	t.Parallel()
	f := startWithCron(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	tree := addWorktree(t, second, "feat-x")
	f.openSheet(t, workspace, second)
	f.h.Event(t, workspace, "agents.sheet", "folderSheet/tick/"+tree, "folderTick", true)
	require.NoError(t, os.WriteFile(filepath.Join(tree, "late.go"), []byte("n"), 0o600))

	f.h.Event(t, workspace, "agents.sheet", "folderSheet/remove", "folderConfirm", nil)

	sheet := f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		s := sheetNodes(n, "sheet")
		return len(s) == 1 && s[0].Props["note"] == "A worktree changed. Check what would be lost, then remove again."
	})
	assert.Equal(t, sdui.VariantDanger, sheetNodes(sheet, "button")[1].Props["variant"])
	assert.DirExists(t, tree)
	assert.FileExists(t, filepath.Join(tree, "late.go"))
	assert.Len(t, f.folders(t, workspace), 2)
}

func TestFolderSheetIsRefusedWhileASessionRuns(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"sleep": 1500}}})
	workspace, first, _ := f.twoFolders(t)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	sheet := f.openSheet(t, workspace, first)

	remove := sheetNodes(sheet, "button")[1]
	assert.Equal(t, true, remove.Props["disabled"])
	assert.Equal(t, "Refused: 1 session works there.", sheetNodes(sheet, "sheet")[0].Props["note"])
	f.h.Event(t, workspace, "agents.sheet", "folderSheet/remove", "folderConfirm", nil)
	assert.Len(t, f.folders(t, workspace), 2)

	f.h.WaitSlot(t, workspace, "agents.sheet", func(n sdui.Node) bool {
		b := sheetNodes(n, "button")
		return len(b) == 2 && b[1].Props["disabled"] == nil
	})
}

func TestFolderSheetRefusesTheLastFolder(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)

	sheet := f.openSheet(t, workspace, repo)

	assert.Equal(t, true, sheetNodes(sheet, "button")[1].Props["disabled"])
	assert.Equal(t, "A workspace keeps at least one folder.", sheetNodes(sheet, "sheet")[0].Props["note"])
	assert.True(t, f.folderRemove(t, workspace, repo).Last)
}

func TestFolderSheetRefusesAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace, _, second := f.twoFolders(t)
	session := f.doneSession(t, workspace)
	args := map[string]any{"path": second}

	require.Error(t, f.runAs(t, workspace, session, "agents.folder-sheet", args, nil))
	require.Error(t, f.runFrom(t, workspace, session, "agents.folder-sheet", args, nil))
}

func TestWorkspaceUpdateLeavesAChildWhoseWorktreeIsBeingCreated(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	gate := filepath.Join(t.TempDir(), "gate")
	hook := fmt.Sprintf("#!/bin/sh\nwhile [ ! -e %q ]; do sleep 0.02; done\ncat >/dev/null\n", gate)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".git", "hooks", "reference-transaction"), []byte(hook), 0o755))
	t.Cleanup(func() { _ = os.WriteFile(gate, nil, 0o600) })
	workspace := f.workspace(t, repo)
	var kid child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"branch": "feat-late", "prompt": "work"}, &kid))
	require.NoDirExists(t, kid.Worktree)

	_, err := f.h.Svc.AddRepo(t.Context(), workspace, newRepo(t), contract.FolderMeta{})
	require.NoError(t, err)
	testutil.Never(t, func() bool { return f.session(t, workspace, kid.Session).Status == "closed" }, time.Second)

	write(t, gate, "")
	assert.Equal(t, "done", f.waitBranchChildDone(t, workspace, kid.Session).Status)
}
