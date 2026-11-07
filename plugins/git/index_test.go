package main_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

type indexEntry struct {
	Repo       string    `json:"repo"`
	Path       string    `json:"path"`
	Branch     string    `json:"branch"`
	Head       string    `json:"head"`
	Committed  time.Time `json:"committed"`
	Main       bool      `json:"main"`
	Current    bool      `json:"current"`
	Created    time.Time `json:"created"`
	LastOpened time.Time `json:"lastOpened"`
	MadeBy     string    `json:"madeBy"`
	Merged     bool      `json:"merged"`
	RemoteGone bool      `json:"remoteGone"`
	Locked     bool      `json:"locked"`
	LockWhy    string    `json:"lockWhy"`
}

type stateEntry struct {
	Path     string `json:"path"`
	Dirty    int    `json:"dirty"`
	Unpushed int    `json:"unpushed"`
	Error    string `json:"error"`
	Ignored  int    `json:"ignored"`
	Orphans  int    `json:"orphans"`
	Nested   int    `json:"nested"`
	Tip      string `json:"tip"`
	Merged   bool   `json:"merged"`
	Landed   bool   `json:"landed"`
	Remote   bool   `json:"remote"`
	Locked   bool   `json:"locked"`
	LockWhy  string `json:"lockWhy"`
}

func worktreeIndex(t *testing.T, h *testutil.PluginHarness, id string) map[string]indexEntry {
	t.Helper()
	var list []indexEntry
	require.NoError(t, runCommand(t, h, id, "git.worktree-index", nil, &list))
	out := make(map[string]indexEntry, len(list))
	for _, e := range list {
		out[filepath.Base(e.Path)] = e
	}
	return out
}

func worktreeStates(t *testing.T, h *testutil.PluginHarness, id string, paths ...string) []stateEntry {
	t.Helper()
	var list []stateEntry
	require.NoError(t, runCommand(t, h, id, "git.worktree-state", map[string]any{"paths": paths}, &list))
	return list
}

func branchTree(t *testing.T, repo, name string, commits int, pushed bool) string {
	t.Helper()
	dir := filepath.Join(repo, ".worktrees", name)
	git(t, repo, "worktree", "add", "-q", "-b", name, dir)
	for i := range commits {
		write(t, filepath.Join(dir, fmt.Sprintf("%s-%d.go", name, i)), "one\n")
		git(t, dir, "add", ".")
		git(t, dir, "commit", "-qm", name)
	}
	if pushed {
		git(t, dir, "push", "-q", "-u", "origin", name)
	}
	return dir
}

func dropRemoteBranch(t *testing.T, repo, name string) {
	t.Helper()
	git(t, repo, "push", "-q", "origin", "--delete", name)
	git(t, repo, "fetch", "-q", "--prune")
}

func TestWorktreeIndexKeepsWhatWorktreesReports(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "pay", "switch": false})

	index := worktreeIndex(t, h, id)

	list := listWorktrees(t, h, id)
	require.Len(t, index, len(list))
	for _, want := range list {
		got := index[filepath.Base(want.Path)]
		assert.Equal(t, want, worktreeEntry{Repo: got.Repo, Path: got.Path, Branch: got.Branch, Head: got.Head, Main: got.Main, Current: got.Current})
	}
}

func TestWorktreeIndexTellsMergedGoneAndSquashed(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	git(t, repo, "branch", "--set-upstream-to=origin/main", "main")
	branchTree(t, repo, "behind", 0, false)
	branchTree(t, repo, "behindGone", 0, true)
	dropRemoteBranch(t, repo, "behindGone")
	branchTree(t, repo, "merged", 2, false)
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "merged")
	git(t, repo, "worktree", "add", "-q", "-b", "inner", filepath.Join(repo, ".worktrees", "inner"), "merged~1")
	branchTree(t, repo, "fastForward", 1, false)
	git(t, repo, "merge", "-q", "--ff-only", "fastForward")
	write(t, filepath.Join(repo, "next.go"), "one\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "next")
	git(t, repo, "push", "-q", "origin", "main")
	branchTree(t, repo, "gone", 1, true)
	dropRemoteBranch(t, repo, "gone")
	squashed := branchTree(t, repo, "squashed", 2, true)
	git(t, repo, "merge", "-q", "--squash", "squashed")
	git(t, repo, "commit", "-qm", "squash")
	dropRemoteBranch(t, repo, "squashed")
	branchTree(t, repo, "open", 1, true)
	branchTree(t, repo, "fresh", 0, false)
	branchTree(t, repo, "freshGone", 0, true)
	dropRemoteBranch(t, repo, "freshGone")
	h, id := open(t, repo)

	testutil.Eventually(t, func() bool { return worktreeIndex(t, h, id)["squashed"].Merged }, readyWait)
	assert.True(t, worktreeStates(t, h, id, squashed)[0].Merged, "the re-read keeps a settled squash check")

	index := worktreeIndex(t, h, id)
	assert.False(t, index[filepath.Base(repo)].Merged, "the default branch is not merged into itself")
	assert.True(t, index["merged"].Merged)
	assert.False(t, index["merged"].RemoteGone)
	assert.False(t, index["gone"].Merged, "gone without the change in main is a closed PR")
	assert.True(t, index["gone"].RemoteGone)
	assert.True(t, index["squashed"].RemoteGone)
	assert.False(t, index["open"].Merged)
	assert.False(t, index["open"].RemoteGone)
	for _, name := range []string{"fresh", "freshGone", "behind", "behindGone", "inner", "fastForward"} {
		assert.False(t, index[name].Merged, "%s has no commits proven its own", name)
	}
}

func TestWorktreeStateRereadsTipAndMerged(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	dir := branchTree(t, repo, "pay", 1, false)
	h, id := open(t, repo)
	require.False(t, worktreeIndex(t, h, id)["pay"].Merged)
	git(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "pay")
	write(t, filepath.Join(repo, "next.go"), "one\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "next")

	got := worktreeStates(t, h, id, dir, repo)

	assert.True(t, got[0].Merged, "fresher than the cached index")
	assert.Equal(t, strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")), got[0].Tip)
	assert.False(t, got[1].Merged, "the main checkout is never merged")

	write(t, filepath.Join(dir, "late.go"), "one\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "late")

	got = worktreeStates(t, h, id, dir)

	assert.False(t, got[0].Merged, "a commit since the merge is not merged")
	assert.Equal(t, strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")), got[0].Tip)
}

func TestWorktreeIndexRecordsSelection(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "pay", "switch": false})
	require.Empty(t, res.Error)
	before := time.Now().Add(-2 * time.Second)
	assert.True(t, worktreeIndex(t, h, id)["pay"].LastOpened.IsZero())
	assert.True(t, worktreeIndex(t, h, id)["pay"].Created.After(before), "a new worktree says when it was made")

	require.NoError(t, runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": res.Path}, nil))

	assert.True(t, worktreeIndex(t, h, id)["pay"].Current)
	testutil.Eventually(t, func() bool { return worktreeIndex(t, h, id)["pay"].LastOpened.After(before) }, readyWait)
	testutil.Eventually(t, func() bool { return worktreeIndex(t, h, id)[filepath.Base(repo)].LastOpened.After(before) }, readyWait)
}

func TestWorktreeIndexLeavesMadeByEmptyForTheUser(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "pay", "switch": false})

	require.Empty(t, res.Error)
	assert.Empty(t, worktreeIndex(t, h, id)["pay"].MadeBy)
}

func TestWorktreeIndexSurvivesAFolderItCannotRead(t *testing.T) {
	repo := committedRepo(t)
	unborn := newRepo(t)
	h, id := open(t, repo)
	_, err := h.Svc.AddRepo(t.Context(), id, unborn, contract.FolderMeta{})
	require.NoError(t, err)

	index := worktreeIndex(t, h, id)

	assert.Contains(t, index, filepath.Base(repo))
	assert.NotContains(t, index, filepath.Base(unborn))
}

func TestWorktreeIndexIsFreshOnceAnOperationEnds(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	assert.Len(t, worktreeIndex(t, h, id), 1)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "pay", "switch": false})
	assert.Contains(t, worktreeIndex(t, h, id), "pay")

	require.Empty(t, awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": res.Path}).Error)
	assert.NotContains(t, worktreeIndex(t, h, id), "pay")
}

func TestWorktreeIndexRefreshesOnGitChanged(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	assert.Len(t, worktreeIndex(t, h, id), 1)

	git(t, repo, "worktree", "add", "-q", "-b", "pay", filepath.Join(repo, ".worktrees", "pay"))
	git(t, repo, "switch", "-q", "-c", "other")

	testutil.Eventually(t, func() bool { return len(worktreeIndex(t, h, id)) == 2 }, readyWait)
}

func TestWorktreeIndexServesTheCacheWithoutABatch(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE", trace)
	repo := committedRepo(t)
	h, id := open(t, repo)
	worktreeIndex(t, h, id)
	require.NoError(t, os.WriteFile(trace, nil, 0o600))

	worktreeIndex(t, h, id)

	raw, err := os.ReadFile(trace)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "rev-parse", "the plugin's git calls reach the trace")
	assert.NotContains(t, string(raw), "for-each-ref")
	assert.NotContains(t, string(raw), "worktree list")
}

func TestOperationDoesNotWaitForTheSquashCheck(t *testing.T) {
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	shim := t.TempDir()
	script := "#!/bin/sh\ncase \" $* \" in *\" cherry \"*) sleep 20;; esac\nexec " + real + " \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o700))
	repo := cloneOf(t, bareOrigin(t))
	branchTree(t, repo, "gone", 1, true)
	dropRemoteBranch(t, repo, "gone")
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
	h, id := open(t, repo)
	worktreeIndex(t, h, id)

	start := time.Now()
	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "pay", "switch": false})

	require.Empty(t, res.Error)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestWorktreeStateCountsDirtyAndUnpushed(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	pushed := branchTree(t, repo, "pushed", 1, true)
	local := branchTree(t, repo, "local", 2, false)
	write(t, filepath.Join(local, "a.go"), "two\n")
	write(t, filepath.Join(local, "untracked.go"), "one\n")
	h, id := open(t, repo)

	got := worktreeStates(t, h, id, pushed, local, "/nowhere")

	require.Len(t, got, 3)
	assert.Equal(t, pushed, got[0].Path)
	assert.Zero(t, got[0].Dirty)
	assert.Zero(t, got[0].Unpushed)
	assert.Equal(t, 2, got[1].Dirty)
	assert.Equal(t, 2, got[1].Unpushed)
	assert.Equal(t, "/nowhere", got[2].Path)
	assert.NotEmpty(t, got[2].Error)
}

func TestWorktreeStateCountsEveryFileInANewFolder(t *testing.T) {
	repo := committedRepo(t)
	dir := branchTree(t, repo, "pay", 0, false)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "new", "deep"), 0o755))
	for _, name := range []string{"a", "b", "c"} {
		write(t, filepath.Join(dir, "new", "deep", name), "one\n")
	}
	h, id := open(t, repo)

	assert.Equal(t, 3, worktreeStates(t, h, id, dir)[0].Dirty)
}

func TestWorktreeStateCountsIgnoredFilesApart(t *testing.T) {
	repo := committedRepo(t)
	write(t, filepath.Join(repo, ".gitignore"), "*.log\nbuild/\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "ignore")
	dir := branchTree(t, repo, "pay", 0, false)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "build"), 0o755))
	write(t, filepath.Join(dir, "a.log"), "one\n")
	write(t, filepath.Join(dir, "build", "b"), "one\n")
	write(t, filepath.Join(dir, "build", "c"), "one\n")
	write(t, filepath.Join(dir, "d.go"), "one\n")
	h, id := open(t, repo)

	got := worktreeStates(t, h, id, dir)[0]

	assert.Equal(t, 1, got.Dirty)
	assert.Equal(t, 3, got.Ignored)
}

func TestWorktreeStateCountsCommitsOnNoRef(t *testing.T) {
	repo := committedRepo(t)
	dir := branchTree(t, repo, "pay", 1, false)
	git(t, dir, "checkout", "-q", "--detach")
	kept := branchTree(t, repo, "kept", 1, false)
	h, id := open(t, repo)

	assert.Zero(t, worktreeStates(t, h, id, dir)[0].Orphans, "the commit is still on its branch")

	write(t, filepath.Join(dir, "x.go"), "one\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "loose")

	got := worktreeStates(t, h, id, dir, kept)

	assert.Equal(t, 1, got[0].Orphans)
	assert.Zero(t, got[1].Orphans)
}

func submoduleTree(t *testing.T) (string, string) {
	t.Helper()
	lib := committedRepo(t)
	repo := committedRepo(t)
	git(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	git(t, repo, "commit", "-qm", "lib")
	dir := branchTree(t, repo, "pay", 0, false)
	git(t, dir, "-c", "protocol.file.allow=always", "submodule", "update", "-q", "--init")
	return repo, dir
}

func TestWorktreeStateSeesNestedRepos(t *testing.T) {
	repo, dir := submoduleTree(t)
	write(t, filepath.Join(dir, "lib", "untracked.go"), "one\n")
	plain := branchTree(t, repo, "plain", 0, false)
	inner := branchTree(t, repo, "inner", 0, false)
	require.NoError(t, os.MkdirAll(filepath.Join(inner, "vendored"), 0o755))
	git(t, filepath.Join(inner, "vendored"), "init", "-q")
	h, id := open(t, repo)

	got := worktreeStates(t, h, id, dir, plain, inner)

	assert.Equal(t, 1, got[0].Nested, "an initialised submodule")
	assert.Zero(t, got[1].Nested, "a submodule nobody initialised holds nothing")
	assert.Equal(t, 1, got[2].Nested, "a repo cloned inside")
}

func TestWorktreeStateIgnoresARepoHidingUntrackedFiles(t *testing.T) {
	repo := committedRepo(t)
	git(t, repo, "config", "status.showUntrackedFiles", "no")
	write(t, filepath.Join(repo, "new.go"), "one\n")
	h, id := open(t, repo)

	assert.Equal(t, 1, worktreeStates(t, h, id, repo)[0].Dirty)
}

func TestWorktreeStateWithNoRemoteCountsOnlyTheBranch(t *testing.T) {
	repo := committedRepo(t)
	dir := branchTree(t, repo, "pay", 1, false)
	h, id := open(t, repo)

	got := worktreeStates(t, h, id, repo, dir)

	assert.Zero(t, got[0].Unpushed)
	assert.Equal(t, 1, got[1].Unpushed)
}

func TestWorktreeIndexAndStateRunNothingTheRepoNames(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "hook.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o700))
	write(t, filepath.Join(repo, ".gitattributes"), "*.go filter=evil diff=evil\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "attributes")
	dir := branchTree(t, repo, "pay", 2, true)
	forgeSignature(t, dir)
	git(t, repo, "merge", "-q", "--squash", "pay")
	git(t, repo, "commit", "-qm", "squash")
	dropRemoteBranch(t, repo, "pay")
	write(t, filepath.Join(dir, "a.go"), "changed\n")
	for key, value := range map[string]string{
		"core.fsmonitor":       script,
		"filter.evil.clean":    script,
		"filter.evil.smudge":   script,
		"filter.evil.required": "true",
		"diff.external":        script,
		"diff.evil.textconv":   script,
		"log.showSignature":    "true",
		"gpg.program":          script,
		"commit.gpgSign":       "true",
		"core.sshCommand":      script,
	} {
		git(t, repo, "config", key, value)
	}
	h, id := open(t, repo)

	require.Equal(t, 1, worktreeStates(t, h, id, dir)[0].Dirty)
	testutil.Eventually(t, func() bool { return worktreeIndex(t, h, id)["pay"].Merged }, readyWait)

	assert.NoFileExists(t, marker)
}

func forgeSignature(t *testing.T, dir string) {
	t.Helper()
	head, body, _ := strings.Cut(git(t, dir, "cat-file", "commit", "HEAD"), "\n\n")
	signed := head + "\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n abc\n -----END PGP SIGNATURE-----\n\n" + body + "\n"
	blob := filepath.Join(t.TempDir(), "commit")
	write(t, blob, signed)
	git(t, dir, "reset", "-q", "--hard", git(t, dir, "hash-object", "-t", "commit", "-w", blob))
}

func TestBranchDeleteIsTheAgentsPluginsAlone(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	h, id := open(t, repo)

	err := runCommand(t, h, id, "git.branch-delete", map[string]any{"repo": repo, "branch": "feat", "tip": "x"}, nil)

	require.ErrorContains(t, err, "only the app deletes a branch")
}

func TestWorktreeStateSaysWhatIsOnTheRemote(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	pushed := branchTree(t, repo, "pushed", 1, true)
	local := branchTree(t, repo, "local", 1, false)
	h, id := open(t, repo)

	got := worktreeStates(t, h, id, pushed, local)

	assert.True(t, got[0].Remote)
	assert.False(t, got[1].Remote)
}

func TestWorktreeStateProvesALandingOnlyWhenAsked(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	tree := branchTree(t, repo, "feat", 1, false)
	git(t, repo, "merge", "-q", "--squash", "feat")
	git(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "squashed")
	h, id := open(t, repo)

	assert.False(t, worktreeStates(t, h, id, tree)[0].Landed)

	var proved []stateEntry
	require.NoError(t, runCommand(t, h, id, "git.worktree-state", map[string]any{"paths": []string{tree}, "proof": true}, &proved))
	assert.True(t, proved[0].Landed)
}

func TestWorktreeUnpushedNamesTipsOnNoRemote(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	pushed := branchTree(t, repo, "pushed", 1, true)
	local := branchTree(t, repo, "local", 1, false)
	fresh := branchTree(t, repo, "fresh", 0, false)
	h, id := open(t, repo)

	var got []string
	require.NoError(t, runCommand(t, h, id, "git.worktree-unpushed", nil, &got))

	assert.Equal(t, []string{local}, got)
	assert.NotContains(t, got, pushed)
	assert.NotContains(t, got, fresh)
}

func TestLockedWorktreeReadsLockedAndIsNotRemoved(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	tree := branchTree(t, repo, "feat", 0, false)
	git(t, repo, "worktree", "lock", "--reason", "on a USB drive", tree)
	h, id := open(t, repo)

	entry := worktreeIndex(t, h, id)["feat"]
	assert.True(t, entry.Locked)
	assert.Equal(t, "on a USB drive", entry.LockWhy)
	state := worktreeStates(t, h, id, tree)[0]
	assert.True(t, state.Locked)
	assert.Equal(t, "on a USB drive", state.LockWhy)

	err := runCommand(t, h, id, "git.worktree-remove", map[string]any{"path": tree}, nil)

	require.ErrorContains(t, err, "it is locked: on a USB drive")
	assert.DirExists(t, tree)
}

func TestBareLayoutMarksTheDefaultWorktreeMainAndKeepsIt(t *testing.T) {
	origin := bareOrigin(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	tree, feat := filepath.Join(dir, "main"), filepath.Join(dir, "feat")
	git(t, origin, "worktree", "add", "-q", tree, "main")
	git(t, origin, "worktree", "add", "-q", "-b", "feat", feat)
	h, id := open(t, feat)

	index := worktreeIndex(t, h, id)
	assert.True(t, index["main"].Main)
	assert.False(t, index["feat"].Main)

	err = runCommand(t, h, id, "git.worktree-remove", map[string]any{"path": tree}, nil)

	require.ErrorContains(t, err, "main checkout")
	assert.DirExists(t, tree)
}

func TestWorktreeSessionsSeesAShellThatMovedIn(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	tree := branchTree(t, repo, "feat", 0, false)
	h, id := open(t, repo)
	s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: id, Title: "moved", Owner: "terminals", Command: []string{"sh", "-c", "cd '" + tree + "' && exec cat"}, Cwd: repo})
	require.NoError(t, err)

	testutil.Eventually(t, func() bool {
		var users []sessionEntry
		require.NoError(t, runCommand(t, h, id, "git.worktree-sessions", map[string]string{"path": tree}, &users))
		return slices.Contains(users, sessionEntry{ID: s.ID, Title: s.Title})
	}, 5*time.Second)
}
