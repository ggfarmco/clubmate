package main_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot    = "git.sidebar"
	editorSlot     = "editor-nvim.main"
	reviewSlot     = "git.review"
	editorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","order":10}],"commands":[{"id":"editor-nvim.diff","title":"Diff","hidden":true},{"id":"editor-nvim.review-open","title":"Open Review","hidden":true},{"id":"editor-nvim.review-entries","title":"Review Entries","hidden":true},{"id":"editor-nvim.review-select","title":"Select","hidden":true},{"id":"editor-nvim.review-reviewed","title":"Reviewed","hidden":true},{"id":"editor-nvim.review-next","title":"Next","hidden":true},{"id":"editor-nvim.review-excludes","title":"Excludes","hidden":true},{"id":"editor-nvim.review-close","title":"Close Branch Review","hidden":true},{"id":"editor-nvim.checktime","title":"Checktime","hidden":true},{"id":"editor-nvim.unsaved","title":"Unsaved","hidden":true}]}}`
	restartWait    = 15 * time.Second
	agentsManifest = `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"agents.stop-root","title":"Stop Worktree Agents","hidden":true}]},"echoResults":{"agents.stop-root":{}}}`
)

func TestMain(m *testing.M) {
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

func git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T, files ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.name", "t")
	git(t, repo, "config", "user.email", "t@t")
	for _, f := range files {
		write(t, filepath.Join(repo, f), "one\n")
	}
	return repo
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func open(t *testing.T, repo string, dirs ...string) (*testutil.PluginHarness, string) {
	t.Helper()
	h := testutil.NewPluginHarness(t, append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json")}, dirs...)...)
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, nil))
	return h, p.ID
}

func roots(t *testing.T, n sdui.Node) []sdui.TreeItem {
	t.Helper()
	tree, ok := treeOf(n)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(tree.Props["items"])
	require.NoError(t, err)
	var out []sdui.TreeItem
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func groups(t *testing.T, n sdui.Node) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, r := range roots(t, n) {
		for _, g := range r.Children {
			for _, l := range g.Children {
				out[g.Label] = append(out[g.Label], l.Label)
			}
		}
	}
	return out
}

func treeOf(n sdui.Node) (sdui.Node, bool) {
	if n.Type == "tree" {
		return n, true
	}
	for _, c := range n.Children {
		if got, ok := treeOf(c); ok {
			return got, true
		}
	}
	return sdui.Node{}, false
}

func committed(t *testing.T, h *testutil.PluginHarness, id, repo, form, message string) {
	t.Helper()
	h.Event(t, id, sidebarSlot, form, "commit", map[string]string{"message": message})
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return formOf(n) != form })
	assert.Equal(t, message, git(t, repo, "log", "-1", "--format=%s"))
}

func formOf(n sdui.Node) string {
	for _, c := range n.Children {
		if c.Type == "form" {
			return c.ID
		}
	}
	return ""
}

func TestGitSidebarGroups(t *testing.T) {
	repo := newRepo(t, "a.go", "b.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	git(t, repo, "add", "a.go")
	write(t, filepath.Join(repo, "b.go"), "two\n")
	write(t, filepath.Join(repo, "c.go"), "one\n")
	h, id := open(t, repo)

	tree := h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)) == 3 })
	rs := roots(t, tree)
	require.Len(t, rs, 1)
	assert.Equal(t, repo, rs[0].ID)
	assert.Equal(t, filepath.Base(repo), rs[0].Label)
	assert.Equal(t, "main", rs[0].Detail)
	assert.True(t, rs[0].Expanded)
	var labels, leaves []string
	for _, g := range rs[0].Children {
		labels = append(labels, g.Label)
		assert.True(t, g.Expanded)
		for _, l := range g.Children {
			leaves = append(leaves, l.ID+"|"+l.Label+"|"+l.Badge)
		}
	}
	assert.Equal(t, []string{"Staged (1)", "Changes (1)", "Untracked (1)"}, labels)
	assert.Equal(t, []string{
		repo + "\x00staged\x00" + filepath.Join(repo, "a.go") + "|a.go|M",
		repo + "\x00changes\x00" + filepath.Join(repo, "b.go") + "|b.go|M",
		repo + "\x00untracked\x00" + filepath.Join(repo, "c.go") + "|c.go|U",
	}, leaves)

	expanded := func(want ...bool) func(sdui.Node) bool {
		return func(n sdui.Node) bool {
			rs := roots(t, n)
			return len(rs) == 1 && len(rs[0].Children) == 3 && rs[0].Expanded == want[0] && rs[0].Children[1].Expanded == want[1]
		}
	}
	h.Event(t, id, sidebarSlot, "git", "collapse", repo+"\x00changes")
	h.WaitSlot(t, id, sidebarSlot, expanded(true, false))
	h.Event(t, id, sidebarSlot, "git", "collapse", repo)
	h.WaitSlot(t, id, sidebarSlot, expanded(false, false))
	h.Event(t, id, sidebarSlot, "git", "expand", repo+"\x00changes")
	h.WaitSlot(t, id, sidebarSlot, expanded(false, true))
}

func TestStageUnstageCommit(t *testing.T) {
	repo := newRepo(t, "b.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	write(t, filepath.Join(repo, "b.go"), "two\n")
	h, id := open(t, repo)
	under := func(group string) func(sdui.Node) bool {
		return func(n sdui.Node) bool { g := groups(t, n); return len(g) == 1 && len(g[group]) == 1 }
	}
	h.WaitSlot(t, id, sidebarSlot, under("Changes (1)"))

	changes := repo + "\x00changes\x00" + filepath.Join(repo, "b.go")
	staged := repo + "\x00staged\x00" + filepath.Join(repo, "b.go")
	form := formOf(h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return formOf(n) != "" }))
	stale := h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": sidebarSlot, "nodeId": "git", "action": "discard", "value": repo + "\x00changes\x00" + filepath.Join(repo, "a.go")}, nil)
	assert.ErrorContains(t, stale, "the file is no longer listed")
	h.Event(t, id, sidebarSlot, "git", "stage", changes)
	h.WaitSlot(t, id, sidebarSlot, under("Staged (1)"))
	h.Event(t, id, sidebarSlot, "git", "unstage", staged)
	h.WaitSlot(t, id, sidebarSlot, under("Changes (1)"))
	h.Event(t, id, sidebarSlot, "git", "stage", changes)
	h.WaitSlot(t, id, sidebarSlot, under("Staged (1)"))

	committed(t, h, id, repo, form, "m")
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 && len(groups(t, n)) == 0 })

	err := h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": sidebarSlot, "nodeId": form, "action": "commit", "value": map[string]string{"message": ""}}, nil)
	assert.ErrorContains(t, err, "commit message is required")
}

func TestFreshRepoStageCommit(t *testing.T) {
	repo := newRepo(t, "x")
	h, id := open(t, repo)
	untracked := func(n sdui.Node) bool { return len(groups(t, n)["Untracked (1)"]) == 1 }
	staged := func(n sdui.Node) bool { return len(groups(t, n)["Staged (1)"]) == 1 }
	tree := h.WaitSlot(t, id, sidebarSlot, untracked)
	assert.Equal(t, "main", roots(t, tree)[0].Detail)

	h.Event(t, id, sidebarSlot, "git", "stage", repo+"\x00untracked\x00"+filepath.Join(repo, "x"))
	h.WaitSlot(t, id, sidebarSlot, staged)
	h.Event(t, id, sidebarSlot, "git", "unstage", repo+"\x00staged\x00"+filepath.Join(repo, "x"))
	h.WaitSlot(t, id, sidebarSlot, untracked)
	h.Event(t, id, sidebarSlot, "git", "stage", repo+"\x00untracked\x00"+filepath.Join(repo, "x"))
	committed(t, h, id, repo, formOf(h.WaitSlot(t, id, sidebarSlot, staged)), "first")
}

func TestSelectOpensDiffInEditor(t *testing.T) {
	repo := newRepo(t, "a.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	git(t, repo, "add", "a.go")
	require.NoError(t, os.Mkdir(filepath.Join(repo, "newpkg"), 0o700))
	write(t, filepath.Join(repo, "newpkg", "b.go"), "b\n")
	manifest := strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim","echoResults":{"editor-nvim.diff":null},`, 1)
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, manifest))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)) == 2 })

	dir := repo + "\x00untracked\x00" + filepath.Join(repo, "newpkg")
	child := repo + "\x00untracked\x00" + filepath.Join(repo, "newpkg", "b.go")
	h.Event(t, id, sidebarSlot, "git", "select", dir)
	h.Event(t, id, sidebarSlot, "git", "expand", dir)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool {
		items := roots(t, n)[0].Children[1].Children
		return len(items) == 1 && len(items[0].Children) == 1 && items[0].Children[0].ID == child && items[0].Children[0].Label == "b.go"
	})
	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00staged\x00"+filepath.Join(repo, "a.go"))
	h.Event(t, id, sidebarSlot, "git", "select", child)
	diffs := commandArgs(t, h, "editor-nvim.diff")
	require.Len(t, diffs, 2)
	assert.JSONEq(t, fmt.Sprintf(`{"path":%q,"base":"head","target":"index"}`, filepath.Join(repo, "a.go")), string(diffs[0]))
	assert.JSONEq(t, fmt.Sprintf(`{"path":%q,"base":"index","target":"worktree"}`, filepath.Join(repo, "newpkg", "b.go")), string(diffs[1]))
}

func TestPlainDirIsNotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	h, id := open(t, dir)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool {
		rs := roots(t, n)
		return len(rs) == 1 && rs[0].Detail == "not a git repository" && len(rs[0].Children) == 0
	})
}

func TestDiscard(t *testing.T) {
	repo := newRepo(t, "b.go")
	for name, content := range map[string]string{"c.go": "cee\n", "a1": "a\n", "n": "n\n"} {
		write(t, filepath.Join(repo, name), content)
	}
	require.NoError(t, os.Mkdir(filepath.Join(repo, "d"), 0o700))
	write(t, filepath.Join(repo, "d", "x"), "x\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	write(t, filepath.Join(repo, "b.go"), "two\n")
	write(t, filepath.Join(repo, "f.go"), "keep\n")
	git(t, repo, "add", "-N", "f.go")
	require.NoError(t, os.Rename(filepath.Join(repo, "c.go"), filepath.Join(repo, "d.go")))
	git(t, repo, "add", "-N", "d.go")
	replace := func(name string) string {
		path := filepath.Join(repo, name)
		require.NoError(t, os.RemoveAll(path))
		require.NoError(t, os.Mkdir(path, 0o700))
		return path
	}
	write(t, filepath.Join(replace("a1"), "u"), "u\n")
	require.NoError(t, os.RemoveAll(filepath.Join(repo, "d")))
	write(t, filepath.Join(repo, "d"), "file\n")
	nested := replace("n")
	git(t, nested, "init", "-q")
	write(t, filepath.Join(nested, "z"), "z\n")
	git(t, nested, "add", "z")
	git(t, nested, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "z")
	h, id := open(t, repo)
	tree := h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Changes (6)"]) == 6 })
	discard := func(leaf string) error {
		return h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": sidebarSlot, "nodeId": "git", "action": "discard", "value": leaf}, nil)
	}
	discardable := map[string]bool{}
	for _, g := range roots(t, tree)[0].Children {
		for _, l := range g.Children {
			discardable[l.ID] = slices.ContainsFunc(l.Menu, func(m sdui.MenuEntry) bool { return m.Action == "discard" })
		}
	}

	kept := map[string]string{"f.go": "keep\n", "d.go": "cee\n", "a1/u": "u\n", "d": "file\n", "n/z": "z\n"}
	for _, name := range []string{"f.go", "d.go", "a1", "d/x", "n"} {
		leaf := repo + "\x00changes\x00" + filepath.Join(repo, name)
		assert.False(t, discardable[leaf], name)
		assert.ErrorContains(t, discard(leaf), "cannot discard "+name+": ", name)
	}
	for name, content := range kept {
		got, err := os.ReadFile(filepath.Join(repo, name))
		require.NoError(t, err)
		assert.Equal(t, content, string(got), name)
	}
	assert.DirExists(t, filepath.Join(nested, ".git"))

	modified := repo + "\x00changes\x00" + filepath.Join(repo, "b.go")
	assert.True(t, discardable[modified])
	require.NoError(t, discard(modified))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Changes (5)"]) == 5 })
	content, err := os.ReadFile(filepath.Join(repo, "b.go"))
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(content))
}

func branchRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t, "a.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	git(t, repo, "checkout", "-qb", "feature/x")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	write(t, filepath.Join(repo, "c.go"), "c\n")
	write(t, filepath.Join(repo, "d.go"), "d\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "feature")
	return repo
}

func reviewView(t *testing.T, n sdui.Node) ([]string, map[string]any) {
	t.Helper()
	if n.Type != "scroll" || len(n.Children) != 1 {
		return nil, nil
	}
	header, _ := n.Children[0].Props["review"].(map[string]any)
	var files []string
	var walk func([]sdui.TreeItem)
	walk = func(items []sdui.TreeItem) {
		for _, it := range items {
			if it.HasChildren {
				walk(it.Children)
			} else {
				files = append(files, it.ID)
			}
		}
	}
	walk(roots(t, n))
	return files, header
}

func reviewing(t *testing.T, files ...string) func(sdui.Node) bool {
	return func(n sdui.Node) bool {
		got, header := reviewView(t, n)
		return header != nil && slices.Equal(got, files)
	}
}

func review(t *testing.T, h *testutil.PluginHarness, id string, args any) {
	t.Helper()
	require.NoError(t, runCommand(t, h, id, "git.review", args, nil))
}

func restartGit(t *testing.T, h *testutil.PluginHarness) {
	t.Helper()
	var pid int
	for _, st := range h.Sup.Statuses() {
		if st.ID == "git" {
			pid = st.PID
		}
	}
	require.NotZero(t, pid)
	require.NoError(t, syscall.Kill(pid, syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		pids := h.Sup.PIDs()
		return len(pids) > 0 && !slices.Contains(pids, pid)
	}, restartWait)
}

func TestReviewLiteralBase(t *testing.T) {
	repo := committedRepo(t)
	write(t, filepath.Join(repo, "a.go"), "before\n")
	write(t, filepath.Join(repo, "n.go"), "n\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "checkpoint")
	sha := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	write(t, filepath.Join(repo, "b.go"), "b\n")
	git(t, repo, "add", "b.go")
	git(t, repo, "commit", "-qm", "after")
	write(t, filepath.Join(repo, "n.go"), "n\nm\n")
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, editorManifest))

	review(t, h, id, map[string]string{"base": "commit:" + sha, "baseLabel": "before turn 2"})
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "b.go", "n.go"))
	_, header := reviewView(t, n)
	assert.Equal(t, "before turn 2", header["base"])
	assert.Equal(t, "commit:"+sha, header["baseRef"])

	err := runCommand(t, h, id, "git.review", map[string]string{"base": "commit:" + strings.Repeat("0", 40)}, nil)
	assert.ErrorContains(t, err, "invalid ref")
}

func TestReviewAgainstWorktreeUsesMergeBase(t *testing.T) {
	repo := committedRepo(t)
	a := filepath.Join(repo, ".worktrees", "a")
	git(t, repo, "worktree", "add", "-q", "-b", "a", a)
	b := filepath.Join(repo, ".worktrees", "b")
	git(t, repo, "worktree", "add", "-q", "-b", "b", b, "a")
	write(t, filepath.Join(a, "a2.go"), "a2\n")
	git(t, a, "add", ".")
	git(t, a, "commit", "-qm", "a2")
	write(t, filepath.Join(a, "dirty.go"), "x\n")
	write(t, filepath.Join(b, "b.go"), "b\n")
	git(t, b, "add", ".")
	git(t, b, "commit", "-qm", "b")
	write(t, filepath.Join(b, "a.go"), "two\n")
	write(t, filepath.Join(b, "n.go"), "n\nn\n")
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, editorManifest))

	review(t, h, id, map[string]string{"head": b, "base": a})
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "b.go", "n.go"))
	_, header := reviewView(t, n)
	assert.Equal(t, "a", header["base"])
	assert.Equal(t, "b", header["head"])
	assert.Equal(t, "a has uncommitted changes that are not in the base", header["warning"])
	assert.Equal(t, "3 files", header["progress"])
	var badges []string
	for _, it := range roots(t, n) {
		badges = append(badges, fmt.Sprintf("%s %s +%d -%d", it.ID, it.Badge, it.Additions, it.Deletions))
	}
	assert.Equal(t, []string{"a.go M +1 -1", "b.go A +1 -0", "n.go ? +2 -0"}, badges)
	assert.Equal(t, b, firstRoot(t, h, id))
}

func TestReviewPausesPerWorktree(t *testing.T) {
	repo := committedRepo(t)
	b := filepath.Join(repo, ".worktrees", "b")
	git(t, repo, "worktree", "add", "-q", "-b", "b", b)
	write(t, filepath.Join(b, "b.go"), "b\n")
	write(t, filepath.Join(b, "b_test.go"), "b\n")
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, editorManifest))

	review(t, h, id, map[string]string{"head": b})
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "b.go", "b_test.go"))
	require.NoError(t, runCommand(t, h, id, "git.review-filter", map[string]bool{"tests": true}, nil))
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "b.go"))

	_, err := h.Svc.SetWorktree(t.Context(), id, repo, "")
	require.NoError(t, err)
	h.WaitSlot(t, id, reviewSlot, func(n sdui.Node) bool { return n.Type == "empty" })
	restartGit(t, h)
	_, err = h.Svc.SetWorktree(t.Context(), id, repo, b)
	require.NoError(t, err)
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "b.go"))
	_, header := reviewView(t, n)
	assert.Equal(t, "main", header["base"])
	assert.Equal(t, "1 file · 1 hidden", header["progress"])
}

func TestReviewFiltersHideTests(t *testing.T) {
	repo := branchRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "pkg", "testdata"), 0o700))
	write(t, filepath.Join(repo, "pkg", "x_test.go"), "x\n")
	write(t, filepath.Join(repo, "pkg", "testdata", "in.txt"), "x\n")
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, editorManifest))

	review(t, h, id, nil)
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "pkg/testdata/in.txt", "pkg/x_test.go", "a.go", "c.go", "d.go"))
	require.NoError(t, runCommand(t, h, id, "git.review-filter", map[string]bool{"tests": true}, nil))
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go"))
	_, header := reviewView(t, n)
	assert.Equal(t, "3 files · 2 hidden", header["progress"])

	testutil.Eventually(t, func() bool {
		events, err := h.Bus.Read(t.Context(), "echo.command", 0, 100)
		require.NoError(t, err)
		for _, ev := range events {
			var call struct {
				ID   string `json:"id"`
				Args struct {
					Excludes []string `json:"excludes"`
				} `json:"args"`
			}
			require.NoError(t, json.Unmarshal(ev.Payload, &call))
			if call.ID == "editor-nvim.review-excludes" && slices.Contains(call.Args.Excludes, "*_test.go") {
				return true
			}
		}
		return false
	}, 5*time.Second)
}

func opens(t *testing.T, h *testutil.PluginHarness) int {
	t.Helper()
	events, err := h.Bus.Read(t.Context(), "echo.command", 0, 1000)
	require.NoError(t, err)
	n := 0
	for _, ev := range events {
		var call struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(ev.Payload, &call))
		if call.ID == "editor-nvim.review-open" {
			n++
		}
	}
	return n
}

func TestReviewRetriesFailedOpen(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, editorManifest))
	review(t, h, id, nil)
	testutil.Eventually(t, func() bool { return opens(t, h) > 0 }, 5*time.Second)
	before := opens(t, h)
	require.NoError(t, runCommand(t, h, id, "git.refresh", nil, nil))
	testutil.Eventually(t, func() bool { return opens(t, h) > before }, 5*time.Second)
}

func TestReviewReopensWhenEditorStarts(t *testing.T) {
	repo := branchRepo(t)
	manifest := strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim","echoResults":{"editor-nvim.review-open":{"mode":"native"}},`, 1)
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, manifest))
	review(t, h, id, nil)
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go"))
	write(t, filepath.Join(repo, "e.go"), "e\n")
	require.NoError(t, runCommand(t, h, id, "git.refresh", nil, nil))
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go", "e.go"))
	assert.Equal(t, 1, opens(t, h))

	payload, err := json.Marshal(map[string]string{"projectId": id, "root": t.TempDir()})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "editor.started", "editor-nvim", payload)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool { return opens(t, h) == 2 }, 5*time.Second)
}

func moduleEditor(t *testing.T, extra string) string {
	t.Helper()
	results := `"echoResults":{"editor-nvim.review-open":{"mode":"module"},"editor-nvim.review-entries":[{"path":"a.go","status":"M","additions":1,"deletions":1,"reviewed":false,"comments":0},{"path":"c.go","status":"A","additions":1,"deletions":0,"reviewed":false,"comments":0}],"editor-nvim.review-select":null,"editor-nvim.review-reviewed":{"reviewed":true}}`
	return testutil.EchoPluginDirAs(t, strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim",`+results+extra+`,`, 1))
}

func commandArgs(t *testing.T, h *testutil.PluginHarness, command string) []json.RawMessage {
	t.Helper()
	events, err := h.Bus.Read(t.Context(), "echo.command", 0, 1000)
	require.NoError(t, err)
	var out []json.RawMessage
	for _, ev := range events {
		var call struct {
			ID   string          `json:"id"`
			Args json.RawMessage `json:"args"`
		}
		require.NoError(t, json.Unmarshal(ev.Payload, &call))
		if call.ID == command {
			out = append(out, call.Args)
		}
	}
	return out
}

func reviewAction(t *testing.T, h *testutil.PluginHarness, id, action string, value any) error {
	t.Helper()
	return h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": "git.review", "nodeId": "review", "action": action, "value": value}, nil)
}

func TestReviewMarksTheStateTheUserSaw(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo, moduleEditor(t, ""))
	review(t, h, id, nil)
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go"))
	require.NoError(t, reviewAction(t, h, id, "reviewed", "a.go"))
	args := commandArgs(t, h, "editor-nvim.review-reviewed")
	require.Len(t, args, 1)
	assert.JSONEq(t, `{"path":"a.go","reviewed":true}`, string(args[0]))
}

func TestReviewClosedByTheUserReopensOnlyOnAListAction(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo, moduleEditor(t, ""))
	review(t, h, id, nil)
	require.NoError(t, runCommand(t, h, id, "git.review-filter", map[string]bool{"tests": true}, nil))
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go"))
	require.Equal(t, 1, opens(t, h))

	payload, err := json.Marshal(map[string]string{"projectId": id, "root": t.TempDir(), "kind": "closed"})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "review.changed", "editor-nvim", payload)
	require.NoError(t, err)
	write(t, filepath.Join(repo, "x_test.go"), "x\n")
	require.NoError(t, runCommand(t, h, id, "git.refresh", nil, nil))
	h.WaitSlot(t, id, reviewSlot, func(n sdui.Node) bool {
		files, header := reviewView(t, n)
		return slices.Equal(files, []string{"a.go", "c.go"}) && header["progress"] == "2 files · 1 hidden · 0/2 reviewed"
	})
	assert.Equal(t, 1, opens(t, h), "a refresh after the user closed the review does not reopen it")

	require.NoError(t, reviewAction(t, h, id, "select", "a.go"))
	assert.Equal(t, 2, opens(t, h))
}

func TestReviewShowsBlockedEditor(t *testing.T) {
	repo := branchRepo(t)
	const blocked = "Neovim is waiting for input — press Enter in the editor"
	h, id := openReady(t, repo, moduleEditor(t, `,"echoErrors":{"editor-nvim.review-entries":"`+blocked+`"}`))
	review(t, h, id, nil)
	h.WaitSlot(t, id, reviewSlot, func(n sdui.Node) bool {
		_, header := reviewView(t, n)
		return header != nil && header["error"] == blocked
	})
}

func TestReviewDropsStoredShellGlobs(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, editorManifest))
	stored := map[string]any{"reviews": map[string]any{repo: map[string]any{"base": "main", "filters": map[string]any{"tests": true, "glob": "$(touch x)"}}}, "filters": map[string]any{"glob": "`touch x`"}}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NoError(t, h.KV.Set(t.Context(), "git", "review/"+id, raw))
	restartGit(t, h)
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go"))
	_, header := reviewView(t, n)
	chips, _ := header["chips"].([]any)
	var ids []string
	for _, c := range chips {
		ids = append(ids, fmt.Sprint(c.(map[string]any)["id"]))
	}
	assert.Equal(t, []string{"tests", "generated"}, ids)
}

func TestReviewRefsFuzzy(t *testing.T) {
	repo := committedRepo(t)
	git(t, repo, "branch", "develop")
	git(t, repo, "branch", "feature/device")
	tools := filepath.Join(repo, ".worktrees", "devtools")
	git(t, repo, "worktree", "add", "-q", "-b", "devtools", tools)
	h, id := openReady(t, repo)

	var refs []struct {
		Ref   string `json:"ref"`
		Kind  string `json:"kind"`
		Label string `json:"label"`
	}
	require.NoError(t, runCommand(t, h, id, "git.review-refs", map[string]string{"query": "dev"}, &refs))
	var got []string
	for _, r := range refs {
		got = append(got, r.Kind+" "+r.Label+" "+r.Ref)
	}
	assert.Equal(t, []string{"worktree devtools " + tools, "branch develop develop", "branch devtools devtools", "branch feature/device feature/device"}, got)

	require.NoError(t, runCommand(t, h, id, "git.review-refs", map[string]string{"query": "main"}, &refs))
	assert.Empty(t, refs, "the head worktree and its branch are not base candidates")
}

func TestReviewBranchHeadWithoutCheckout(t *testing.T) {
	repo := branchRepo(t)
	git(t, repo, "checkout", "-q", "main")
	write(t, filepath.Join(repo, "u.go"), "u\n")
	sha := git(t, repo, "rev-parse", "feature/x")
	mergeBase := git(t, repo, "merge-base", "main", "feature/x")
	manifest := strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim","echoResults":{"editor-nvim.review-open":{"mode":"native"},"editor-nvim.diff":null},`, 1)
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, manifest))

	review(t, h, id, map[string]string{"base": "main", "head": "feature/x"})
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go"))
	_, header := reviewView(t, n)
	assert.Equal(t, "feature/x", header["head"])
	assert.Equal(t, "feature/x", header["headRef"])
	assert.Equal(t, true, header["readOnly"])
	assert.Positive(t, header["started"])
	assert.JSONEq(t, fmt.Sprintf(`{"cwd":%q,"base":%q,"excludes":[]}`, repo, mergeBase+".."+sha), string(commandArgs(t, h, "editor-nvim.review-open")[0]))
	require.NoError(t, reviewAction(t, h, id, "select", "a.go"))
	assert.JSONEq(t, fmt.Sprintf(`{"path":%q,"base":%q,"target":%q}`, filepath.Join(repo, "a.go"), "rev:"+mergeBase, "rev:"+sha), string(commandArgs(t, h, "editor-nvim.diff")[0]))
	assert.Equal(t, "main", git(t, repo, "branch", "--show-current"))
	assert.Equal(t, repo, firstRoot(t, h, id))

	var refs []struct {
		Ref string `json:"ref"`
	}
	require.NoError(t, runCommand(t, h, id, "git.review-refs", map[string]string{"query": ""}, &refs))
	var got []string
	for _, r := range refs {
		got = append(got, r.Ref)
	}
	assert.Equal(t, []string{repo, "main"}, got)

	git(t, repo, "switch", "-q", "feature/x")
	require.NoError(t, runCommand(t, h, id, "git.refresh", nil, nil))
	n = h.WaitSlot(t, id, reviewSlot, func(n sdui.Node) bool {
		_, header := reviewView(t, n)
		return header != nil && header["readOnly"] == nil
	})
	_, header = reviewView(t, n)
	assert.Equal(t, repo, header["headRef"])
	require.NoError(t, reviewAction(t, h, id, "select", "c.go"))
	diffs := commandArgs(t, h, "editor-nvim.diff")
	assert.Contains(t, string(diffs[len(diffs)-1]), `"target":"worktree"`)
}

func TestReviewLiteralBaseWithBranchHead(t *testing.T) {
	repo := branchRepo(t)
	tip := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "-q", "main")
	write(t, filepath.Join(repo, "m.go"), "m\n")
	git(t, repo, "add", "m.go")
	git(t, repo, "commit", "-qm", "main moved")
	sha := git(t, repo, "rev-parse", "HEAD")
	manifest := strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim","echoResults":{"editor-nvim.review-open":{"mode":"native"}},`, 1)
	h, id := openReady(t, repo, testutil.EchoPluginDirAs(t, manifest))

	review(t, h, id, map[string]string{"base": "commit:" + sha, "head": "feature/x", "baseLabel": "before turn 2"})
	n := h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go", "m.go"))
	_, header := reviewView(t, n)
	assert.Equal(t, "before turn 2", header["base"])
	assert.Equal(t, true, header["readOnly"])
	assert.JSONEq(t, fmt.Sprintf(`{"cwd":%q,"base":%q,"excludes":[]}`, repo, sha+".."+tip), string(commandArgs(t, h, "editor-nvim.review-open")[0]))
}

func TestReviewErrors(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo)
	assert.ErrorContains(t, runCommand(t, h, id, "git.review", map[string]string{"base": "gone"}, nil), "unknown branch gone")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review", map[string]string{"head": "nope"}, nil), "unknown branch nope")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review-swap", nil, nil), "no review in progress")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review", map[string]string{"base": "--output=x"}, nil), "invalid ref")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review-filter", map[string]string{"glob": "`touch x`"}, nil), "glob may only contain")
}

func TestReviewStartsGrowAcrossRestartsAndMainShowsWithoutEditor(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo)
	h.WaitSlot(t, id, "git.main", func(n sdui.Node) bool { return n.Type == "empty" && n.Props["text"] != "" })
	started := func(after float64) float64 {
		var got float64
		h.WaitSlot(t, id, reviewSlot, func(n sdui.Node) bool {
			_, header := reviewView(t, n)
			got, _ = header["started"].(float64)
			return got > after
		})
		return got
	}
	review(t, h, id, nil)
	first := started(0)
	review(t, h, id, nil)
	second := started(first)
	restartGit(t, h)
	h.WaitSlot(t, id, reviewSlot, func(n sdui.Node) bool {
		_, header := reviewView(t, n)
		return header != nil && header["started"] == nil
	})
	review(t, h, id, nil)
	started(second)
}

func TestCorruptStoredReviewIgnored(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo)
	require.NoError(t, h.KV.Set(t.Context(), "git", "review/"+id, json.RawMessage(`"corrupt"`)))
	restartGit(t, h)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })
	review(t, h, id, nil)
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go", "d.go"))
}

func TestGitSectionFollowsWorktree(t *testing.T) {
	repo := newRepo(t, "a.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	wt := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", wt, "-b", "feat")
	write(t, filepath.Join(wt, "a.go"), "two\n")
	h, id := open(t, repo)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })

	_, err := h.Svc.SetWorktree(t.Context(), id, repo, wt)
	require.NoError(t, err)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool {
		return len(groups(t, n)["Changes (1)"]) == 1
	})
}

type worktreeEntry struct {
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Head        string `json:"head"`
	Main        bool   `json:"main"`
	Current     bool   `json:"current"`
	DefaultBase string `json:"defaultBase"`
}

type opResult struct {
	Done   bool   `json:"done"`
	Error  string `json:"error"`
	Path   string `json:"path"`
	Notice string `json:"notice"`
}

type sessionEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func committedRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t, "a.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	return repo
}

func openReady(t *testing.T, repo string, dirs ...string) (*testutil.PluginHarness, string) {
	t.Helper()
	h, id := open(t, repo, dirs...)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })
	return h, id
}

func runCommand(t *testing.T, h *testutil.PluginHarness, id, command string, args, out any) error {
	t.Helper()
	return h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "projectId": id, "args": args}, out)
}

func listWorktrees(t *testing.T, h *testutil.PluginHarness, id string) []worktreeEntry {
	t.Helper()
	var list []worktreeEntry
	require.NoError(t, runCommand(t, h, id, "git.worktrees", nil, &list))
	return list
}

func firstRoot(t *testing.T, h *testutil.PluginHarness, id string) string {
	t.Helper()
	p, err := h.Svc.GetProject(t.Context(), id)
	require.NoError(t, err)
	return p.Roots[0]
}

func startOp(t *testing.T, h *testutil.PluginHarness, id, command string, args any) (string, error) {
	t.Helper()
	var started struct {
		ID string `json:"id"`
	}
	err := runCommand(t, h, id, command, args, &started)
	return started.ID, err
}

func awaitOp(t *testing.T, h *testutil.PluginHarness, id, command string, args any) opResult {
	t.Helper()
	op, err := startOp(t, h, id, command, args)
	require.NoError(t, err)
	deadline := time.Now().Add(30 * time.Second)
	var res opResult
	for !res.Done {
		require.True(t, time.Now().Before(deadline), "operation %s did not finish", op)
		require.NoError(t, runCommand(t, h, id, "git.worktree-op-status", map[string]string{"id": op}, &res))
	}
	return res
}

func TestWorktreeList(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	detached := filepath.Join(repo, ".worktrees", "detached")
	git(t, repo, "worktree", "add", "-q", "--detach", detached)
	h, id := openReady(t, repo)

	head := git(t, repo, "rev-parse", "--short", "HEAD")
	list := listWorktrees(t, h, id)
	require.NotEmpty(t, list)
	main := worktreeEntry{Repo: repo, Path: repo, Branch: "main", Head: head, Main: true, Current: true, DefaultBase: "main"}
	assert.Equal(t, main, list[0])
	assert.ElementsMatch(t, []worktreeEntry{
		main,
		{Repo: repo, Path: feat, Branch: "feat", Head: head, DefaultBase: "main"},
		{Repo: repo, Path: detached, Head: head, DefaultBase: "main"},
	}, list)
}

func TestNewWorktree(t *testing.T) {
	repo := committedRepo(t)
	git(t, repo, "branch", "old")
	old := git(t, repo, "rev-parse", "old")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	git(t, repo, "commit", "-qam", "two")
	h, id := openReady(t, repo)
	create := func(branch string) opResult {
		return awaitOp(t, h, id, "git.worktree-new", map[string]string{"branch": branch})
	}
	refused := func(branch string) error {
		_, err := startOp(t, h, id, "git.worktree-new", map[string]string{"branch": branch})
		return err
	}

	xy := filepath.Join(repo, ".worktrees", "x", "y")
	assert.Equal(t, opResult{Done: true, Path: xy}, create("x/y"))
	assert.Equal(t, "x/y", git(t, xy, "symbolic-ref", "--short", "HEAD"))
	assert.Equal(t, xy, firstRoot(t, h, id))
	assert.ErrorContains(t, refused("x/y"), "already exists")
	assert.ErrorContains(t, refused("a..b"), "not a valid branch name")

	write(t, filepath.Join(xy, "a.go"), "three\n")
	git(t, xy, "commit", "-qam", "three")
	assert.Empty(t, create("z").Error)
	assert.Equal(t, git(t, xy, "rev-parse", "HEAD"), git(t, repo, "rev-parse", "z"))

	assert.Empty(t, create("old").Error)
	assert.Equal(t, old, git(t, filepath.Join(repo, ".worktrees", "old"), "rev-parse", "HEAD"))
	exclude, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(exclude), ".worktrees/\n"))
}

func TestNewWorktreeWithoutSwitch(t *testing.T) {
	repo := committedRepo(t)
	h, id := openReady(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "child", "switch": false})
	child := filepath.Join(repo, ".worktrees", "child")
	assert.Equal(t, opResult{Done: true, Path: child}, res)
	assert.Equal(t, repo, firstRoot(t, h, id))
}

func TestNewWorktreeFromOtherWorktree(t *testing.T) {
	repo := committedRepo(t)
	other := filepath.Join(repo, ".worktrees", "other")
	git(t, repo, "worktree", "add", "-q", "-b", "other", other)
	write(t, filepath.Join(other, "a.go"), "other\n")
	git(t, other, "commit", "-qam", "other")
	h, id := openReady(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "child", "from": other})
	assert.Empty(t, res.Error)
	assert.Equal(t, git(t, other, "rev-parse", "HEAD"), git(t, repo, "rev-parse", "child"))

	_, err := startOp(t, h, id, "git.worktree-new", map[string]any{"branch": "bad", "from": t.TempDir()})
	assert.ErrorContains(t, err, "not a worktree")
}

func TestNewWorktreeRefusesSymlink(t *testing.T) {
	repo := committedRepo(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(repo, ".worktrees")))
	h, id := openReady(t, repo)

	_, err = startOp(t, h, id, "git.worktree-new", map[string]string{"branch": "evil"})
	assert.ErrorContains(t, err, "worktrees folder must not be a symlink")
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Equal(t, repo, firstRoot(t, h, id))
}

func TestSwitchMapsSubdir(t *testing.T) {
	repo := newRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "sub"), 0o700))
	write(t, filepath.Join(repo, "sub", "b.go"), "b\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	wt := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", wt)
	folder := filepath.Join(repo, "sub")
	h, id := openReady(t, folder)
	switchTo := func(path string) error {
		return runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": path}, nil)
	}

	require.NoError(t, switchTo(wt))
	assert.Equal(t, filepath.Join(wt, "sub"), firstRoot(t, h, id))
	list := listWorktrees(t, h, id)
	require.Len(t, list, 2)
	assert.False(t, list[0].Current)
	assert.True(t, list[1].Current)
	assert.ErrorContains(t, switchTo(filepath.Join(wt, "sub")), "not a worktree of "+folder)
	require.NoError(t, switchTo(repo))
	assert.Equal(t, folder, firstRoot(t, h, id))
}

func TestRemoveWorktreeRefusals(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	dirty := filepath.Join(repo, ".worktrees", "dirty")
	git(t, repo, "worktree", "add", "-q", "-b", "dirty", dirty)
	write(t, filepath.Join(dirty, "new.go"), "n\n")
	h, id := openReady(t, repo)
	shell := func(owner string) sessionEntry {
		s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{ProjectID: id, Title: owner, Owner: owner, Command: []string{"sh", "-c", "exec cat"}, Cwd: feat})
		require.NoError(t, err)
		return sessionEntry{ID: s.ID, Title: s.Title}
	}
	terminal, editor := shell("terminals"), shell("editor-nvim")

	var users []sessionEntry
	require.NoError(t, runCommand(t, h, id, "git.worktree-sessions", map[string]string{"path": feat}, &users))
	assert.Contains(t, users, terminal)
	assert.NotContains(t, users, editor)

	_, err := startOp(t, h, id, "git.worktree-remove", map[string]string{"path": repo})
	assert.ErrorContains(t, err, "cannot remove the main checkout")
	assert.Equal(t, "worktree has uncommitted changes", awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": dirty}).Error)
	assert.DirExists(t, dirty)

	p, err := h.Svc.CreateProject(t.Context(), "feat", []string{feat})
	require.NoError(t, err)
	_, err = startOp(t, h, p.ID, "git.worktree-remove", map[string]string{"path": feat})
	assert.ErrorContains(t, err, "cannot remove the checkout of the workspace folder")
	assert.DirExists(t, feat)
}

func TestRemoveWorktree(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not in PATH")
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, t.TempDir())
	}
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	h, id := openReady(t, repo, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"), testutil.EchoPluginDirAs(t, agentsManifest))
	surface := func(not string) string {
		n := h.WaitSlot(t, id, editorSlot, func(n sdui.Node) bool { return n.Type == "surface" && n.Props["sessionId"] != not })
		return n.Props["sessionId"].(string)
	}
	first := surface("")
	require.NoError(t, runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": feat}, nil))
	featNvim := surface(first)

	git(t, repo, "worktree", "lock", feat)
	assert.Contains(t, awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": feat}).Error, "locked")
	featNvim = surface(featNvim)
	assert.Equal(t, feat, h.GetSession(t, featNvim).Cwd)
	assert.Equal(t, feat, firstRoot(t, h, id))
	git(t, repo, "worktree", "unlock", feat)

	assert.Equal(t, opResult{Done: true}, awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": feat}))
	assert.NoDirExists(t, feat)
	events, err := h.Bus.Read(t.Context(), "echo.command", 0, 100)
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(events, func(e bus.Event) bool {
		return strings.Contains(string(e.Payload), `"agents.stop-root"`) && strings.Contains(string(e.Payload), `"root":"`+feat+`"`)
	}))
	assert.Equal(t, repo, firstRoot(t, h, id))
	git(t, repo, "rev-parse", "--verify", "-q", "feat")
	testutil.Eventually(t, func() bool {
		s, err := h.Svc.GetSession(t.Context(), featNvim)
		return err != nil || s.State != "running"
	}, 5*time.Second)
}

func TestRemovedWorktreeFallsBack(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	h, id := openReady(t, repo)
	require.NoError(t, runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": feat}, nil))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { rs := roots(t, n); return len(rs) == 1 && rs[0].ID == feat })
	sub, err := h.Bus.Subscribe("project.updated", math.MaxInt64)
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	git(t, repo, "worktree", "remove", feat)

	select {
	case ev := <-sub.C():
		var payload struct {
			Project struct {
				Roots   []string `json:"roots"`
				Missing []string `json:"missing"`
			} `json:"project"`
		}
		require.NoError(t, json.Unmarshal(ev.Payload, &payload))
		assert.Equal(t, []string{repo}, payload.Project.Roots)
		assert.Equal(t, []string{feat}, payload.Project.Missing)
	case <-time.After(5 * time.Second):
		t.Fatal("no project.updated after the worktree was removed")
	}
	assert.Equal(t, repo, firstRoot(t, h, id))
}

func TestSwitchRefusedWhileBusy(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	require.NoError(t, os.MkdirAll(filepath.Dir(hook), 0o700))
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nsleep 2\n"), 0o700))
	write(t, filepath.Join(repo, "a.go"), "two\n")
	git(t, repo, "add", "a.go")
	h, id := openReady(t, repo)
	form := formOf(h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return formOf(n) != "" }))

	h.Event(t, id, sidebarSlot, form, "commit", map[string]string{"message": "slow"})
	err := runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": feat}, nil)
	assert.ErrorContains(t, err, "another git operation is running")
	assert.Equal(t, repo, firstRoot(t, h, id))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return formOf(n) != form })
}

func TestHomeWorkspaceHasNoWorktrees(t *testing.T) {
	for _, key := range []string{"GOPATH", "GOMODCACHE", "GOCACHE"} {
		out, err := exec.Command("go", "env", key).Output()
		require.NoError(t, err)
		t.Setenv(key, strings.TrimSpace(string(out)))
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	git(t, home, "init", "-q")
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	p, err := h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, nil))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })

	assert.Empty(t, listWorktrees(t, h, p.ID))
	assert.ErrorContains(t, runCommand(t, h, p.ID, "git.worktree-switch", map[string]string{"path": home}, nil), "worktrees unavailable in the home workspace")
	_, err = startOp(t, h, p.ID, "git.worktree-new", map[string]string{"branch": "x"})
	assert.ErrorContains(t, err, "worktrees unavailable in the home workspace")
	_, err = startOp(t, h, p.ID, "git.worktree-remove", map[string]string{"path": home})
	assert.ErrorContains(t, err, "worktrees unavailable in the home workspace")
}

type branchEntry struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Remote bool   `json:"remote"`
}

func commitAt(t *testing.T, repo, file, date string) {
	t.Helper()
	t.Setenv("GIT_COMMITTER_DATE", date)
	write(t, filepath.Join(repo, file), date+"\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", file, "--date", date)
}

func clonedRepo(t *testing.T) string {
	t.Helper()
	origin := committedRepo(t)
	git(t, origin, "checkout", "-qb", "remote-only")
	commitAt(t, origin, "r.go", "2022-01-01T00:00:00Z")
	git(t, origin, "checkout", "-q", "main")
	clone, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, clone, "clone", "-q", origin, ".")
	git(t, clone, "config", "user.name", "t")
	git(t, clone, "config", "user.email", "t@t")
	return clone
}

func TestBranchListSkipsWorktreesLocalFirstNewestFirst(t *testing.T) {
	repo := clonedRepo(t)
	for _, b := range []struct{ name, date string }{{"old", "2020-01-01T00:00:00Z"}, {"new", "2024-01-01T00:00:00Z"}} {
		git(t, repo, "checkout", "-qb", b.name, "main")
		commitAt(t, repo, b.name+".go", b.date)
	}
	git(t, repo, "checkout", "-q", "main")
	git(t, repo, "worktree", "add", "-q", "-b", "wt", filepath.Join(repo, ".worktrees", "wt"))
	h, id := openReady(t, repo)

	var list []branchEntry
	require.NoError(t, runCommand(t, h, id, "git.branches", nil, &list))
	assert.Equal(t, []branchEntry{{Repo: repo, Name: "new"}, {Repo: repo, Name: "old"}, {Repo: repo, Name: "origin/remote-only", Remote: true}}, list)
}

func TestBranchSwitch(t *testing.T) {
	repo := clonedRepo(t)
	git(t, repo, "checkout", "-qb", "feat")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	git(t, repo, "commit", "-qam", "feat")
	git(t, repo, "checkout", "-q", "main")
	wt := filepath.Join(repo, ".worktrees", "wt")
	git(t, repo, "worktree", "add", "-q", "-b", "wt", wt)
	h, id := openReady(t, repo, switchEditor(t, `[]`))

	require.Empty(t, awaitOp(t, h, id, "git.branch-switch", map[string]string{"branch": "feat"}).Error)
	assert.Equal(t, "feat", git(t, repo, "branch", "--show-current"))
	assert.JSONEq(t, fmt.Sprintf(`{"root":%q}`, repo), string(commandArgs(t, h, "editor-nvim.checktime")[0]))

	write(t, filepath.Join(repo, "a.go"), "dirty\n")
	assert.Contains(t, awaitOp(t, h, id, "git.branch-switch", map[string]string{"branch": "main"}).Error, "would be overwritten by checkout")
	assert.Equal(t, "feat", git(t, repo, "branch", "--show-current"))
	git(t, repo, "checkout", "--", "a.go")

	require.Empty(t, awaitOp(t, h, id, "git.branch-switch", map[string]string{"branch": "origin/remote-only"}).Error)
	assert.Equal(t, "remote-only", git(t, repo, "branch", "--show-current"))
	assert.Equal(t, "origin/remote-only", git(t, repo, "rev-parse", "--abbrev-ref", "@{upstream}"))

	git(t, repo, "checkout", "-q", "--detach")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "lost")
	res := awaitOp(t, h, id, "git.branch-switch", map[string]string{"branch": "origin/remote-only"})
	require.Empty(t, res.Error)
	assert.Equal(t, "remote-only", git(t, repo, "branch", "--show-current"))
	assert.Contains(t, res.Notice, "you are leaving 1 commit behind")

	require.Empty(t, awaitOp(t, h, id, "git.branch-switch", map[string]string{"branch": "wt"}).Error)
	assert.Equal(t, wt, firstRoot(t, h, id))
	assert.Equal(t, "remote-only", git(t, repo, "branch", "--show-current"))

	_, err := startOp(t, h, id, "git.branch-switch", map[string]string{"branch": "-x"})
	assert.ErrorContains(t, err, "not a valid branch name")
}

func switchEditor(t *testing.T, unsaved string) string {
	t.Helper()
	return testutil.EchoPluginDirAs(t, strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim","echoResults":{"editor-nvim.unsaved":`+unsaved+`,"editor-nvim.checktime":null},`, 1))
}

func TestBranchSwitchRefusesUnsavedBuffers(t *testing.T) {
	repo := clonedRepo(t)
	h, id := openReady(t, repo, switchEditor(t, `["a.go"]`))

	_, err := startOp(t, h, id, "git.branch-switch", map[string]string{"branch": "origin/remote-only"})
	assert.ErrorContains(t, err, "Save or discard unsaved changes first: a.go")
	assert.Equal(t, "main", git(t, repo, "branch", "--show-current"))
}
