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

	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot    = "git.sidebar"
	editorSlot     = "editor-nvim.main"
	mainSlot       = "git.main"
	reviewSlot     = "git.review"
	editorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","order":10}],"commands":[{"id":"editor-nvim.diff","title":"Diff","hidden":true},{"id":"editor-nvim.review-open","title":"Open Review","hidden":true},{"id":"editor-nvim.review-entries","title":"Review Entries","hidden":true},{"id":"editor-nvim.review-select","title":"Select","hidden":true},{"id":"editor-nvim.review-reviewed","title":"Reviewed","hidden":true},{"id":"editor-nvim.review-next","title":"Next","hidden":true},{"id":"editor-nvim.review-excludes","title":"Excludes","hidden":true},{"id":"editor-nvim.review-close","title":"Close Branch Review","hidden":true}]}}`
	restartWait    = 15 * time.Second
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
	if n.Type != "scroll" || len(n.Children) != 1 {
		return nil
	}
	raw, err := json.Marshal(n.Children[0].Props["items"])
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

func find(n sdui.Node, id string) (sdui.Node, bool) {
	if n.ID == id {
		return n, true
	}
	for _, c := range n.Children {
		if got, ok := find(c, id); ok {
			return got, true
		}
	}
	return sdui.Node{}, false
}

func committed(t *testing.T, h *testutil.PluginHarness, id, repo, form, message string) {
	t.Helper()
	h.Event(t, id, mainSlot, form, "commit", map[string]string{"message": message})
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return formOf(n) != form })
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

	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00changes\x00"+filepath.Join(repo, "b.go"))
	main := h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { _, ok := find(n, "stage"); return ok })
	assert.Contains(t, main.Children[1].Children[0].Props["text"], "+two")
	form := formOf(main)
	stale := h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": mainSlot, "nodeId": repo + "\x00changes\x00" + filepath.Join(repo, "a.go"), "action": "discard", "value": nil}, nil)
	assert.ErrorContains(t, stale, "the file is no longer selected")
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, under("Staged (1)"))
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { _, ok := find(n, "unstage"); return ok })
	h.Event(t, id, mainSlot, "unstage", "unstage", nil)
	h.WaitSlot(t, id, sidebarSlot, under("Changes (1)"))
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, under("Staged (1)"))

	committed(t, h, id, repo, form, "m")
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 && len(groups(t, n)) == 0 })

	err := h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": mainSlot, "nodeId": form, "action": "commit", "value": map[string]string{"message": ""}}, nil)
	assert.ErrorContains(t, err, "commit message is required")
}

func TestFreshRepoStageCommit(t *testing.T) {
	repo := newRepo(t, "x")
	h, id := open(t, repo)
	untracked := func(n sdui.Node) bool { return len(groups(t, n)["Untracked (1)"]) == 1 }
	staged := func(n sdui.Node) bool { return len(groups(t, n)["Staged (1)"]) == 1 }
	tree := h.WaitSlot(t, id, sidebarSlot, untracked)
	assert.Equal(t, "main", roots(t, tree)[0].Detail)

	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00untracked\x00"+filepath.Join(repo, "x"))
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, staged)
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { _, ok := find(n, "unstage"); return ok })
	h.Event(t, id, mainSlot, "unstage", "unstage", nil)
	h.WaitSlot(t, id, sidebarSlot, untracked)
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, staged)
	committed(t, h, id, repo, formOf(h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return formOf(n) != "" })), "first")
}

func TestUntrackedDirListsFiles(t *testing.T) {
	repo := newRepo(t)
	require.NoError(t, os.Mkdir(filepath.Join(repo, "newpkg"), 0o700))
	write(t, filepath.Join(repo, "newpkg", "a.go"), "a\n")
	write(t, filepath.Join(repo, "newpkg", "b.go"), "b\n")
	h, id := open(t, repo)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Untracked (1)"]) == 1 })

	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00untracked\x00"+filepath.Join(repo, "newpkg"))
	main := h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { _, ok := find(n, "stage"); return ok })
	body := main.Children[1].Children[0]
	assert.Equal(t, "mono", body.Props["style"])
	assert.Equal(t, "newpkg/a.go\nnewpkg/b.go", body.Props["text"])
	_, ok := find(main, "open-diff")
	assert.False(t, ok)
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
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Changes (6)"]) == 6 })
	discard := func(leaf string) error {
		return h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": mainSlot, "nodeId": leaf, "action": "discard", "value": nil}, nil)
	}

	kept := map[string]string{"f.go": "keep\n", "d.go": "cee\n", "a1/u": "u\n", "d": "file\n", "n/z": "z\n"}
	for _, name := range []string{"f.go", "d.go", "a1", "d/x", "n"} {
		leaf := repo + "\x00changes\x00" + filepath.Join(repo, name)
		h.Event(t, id, sidebarSlot, "git", "select", leaf)
		main := h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return showing(n, name) })
		_, ok := find(main, leaf)
		assert.False(t, ok, name)
		assert.ErrorContains(t, discard(leaf), "cannot discard "+name+": ", name)
	}
	for name, content := range kept {
		got, err := os.ReadFile(filepath.Join(repo, name))
		require.NoError(t, err)
		assert.Equal(t, content, string(got), name)
	}
	assert.DirExists(t, filepath.Join(nested, ".git"))

	modified := repo + "\x00changes\x00" + filepath.Join(repo, "b.go")
	h.Event(t, id, sidebarSlot, "git", "select", modified)
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { _, ok := find(n, modified); return ok })
	require.NoError(t, discard(modified))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Changes (5)"]) == 5 })
	content, err := os.ReadFile(filepath.Join(repo, "b.go"))
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(content))
}

func showing(n sdui.Node, rel string) bool {
	if n.Type == "text" && n.Props["style"] == "mono" && n.Props["text"] == rel {
		return true
	}
	for _, c := range n.Children {
		if showing(c, rel) {
			return true
		}
	}
	return false
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

func TestReviewClosedByTheUserReopensOnTheNextAction(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo, moduleEditor(t, ""))
	review(t, h, id, nil)
	h.WaitSlot(t, id, reviewSlot, reviewing(t, "a.go", "c.go"))
	require.Equal(t, 1, opens(t, h))

	payload, err := json.Marshal(map[string]string{"projectId": id, "root": t.TempDir(), "kind": "closed"})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "review.changed", "editor-nvim", payload)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		require.NoError(t, reviewAction(t, h, id, "select", "a.go"))
		return opens(t, h) == 2
	}, 5*time.Second)
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

func TestReviewErrors(t *testing.T) {
	repo := branchRepo(t)
	h, id := openReady(t, repo)
	assert.ErrorContains(t, runCommand(t, h, id, "git.review", map[string]string{"base": "gone"}, nil), "unknown branch gone")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review", map[string]string{"head": "nope"}, nil), "nope is not checked out in a worktree")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review-swap", nil, nil), "no review in progress")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review", map[string]string{"base": "--output=x"}, nil), "invalid ref")
	assert.ErrorContains(t, runCommand(t, h, id, "git.review-filter", map[string]string{"glob": "`touch x`"}, nil), "glob may only contain")
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
	Done  bool   `json:"done"`
	Error string `json:"error"`
	Path  string `json:"path"`
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
	h, id := openReady(t, repo, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"))
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
	form := formOf(h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return formOf(n) != "" }))

	h.Event(t, id, mainSlot, form, "commit", map[string]string{"message": "slow"})
	err := runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": feat}, nil)
	assert.ErrorContains(t, err, "another git operation is running")
	assert.Equal(t, repo, firstRoot(t, h, id))
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return formOf(n) != form })
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
