package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	editorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","order":10}],"commands":[{"id":"editor-nvim.review","title":"Review Branch","hidden":true},{"id":"editor-nvim.review-close","title":"Close Branch Review","hidden":true}]}}`
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

func reviewBranch(t *testing.T, h *testutil.PluginHarness, id string) {
	t.Helper()
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "git.review-branch", "projectId": id}, nil))
}

func reviewing(t *testing.T, label string, n int) func(sdui.Node) bool {
	return func(tree sdui.Node) bool {
		rs := roots(t, tree)
		return len(rs) == 1 && len(rs[0].Children) > 0 && rs[0].Children[0].Label == label && rs[0].Children[0].Expanded && len(rs[0].Children[0].Children) == n
	}
}

func badgeText(n sdui.Node) string {
	if n.Type == "badge" {
		text, _ := n.Props["text"].(string)
		return text
	}
	for _, c := range n.Children {
		if text := badgeText(c); text != "" {
			return text
		}
	}
	return ""
}

func reviewFormID(t *testing.T, h *testutil.PluginHarness, id string) string {
	t.Helper()
	form := ""
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool {
		for _, c := range n.Children {
			for _, f := range c.Children {
				if f.Type == "form" {
					form = f.ID
				}
			}
		}
		return form != ""
	})
	return form
}

func TestReviewGroup(t *testing.T) {
	repo := branchRepo(t)
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, editorManifest))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })

	reviewBranch(t, h, id)
	tree := h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (3)", 3))
	var leaves []string
	for _, l := range roots(t, tree)[0].Children[0].Children {
		leaves = append(leaves, l.ID+"|"+l.Label+"|"+l.Badge)
	}
	assert.Equal(t, []string{
		repo + "\x00review\x00" + filepath.Join(repo, "a.go") + "|a.go|M",
		repo + "\x00review\x00" + filepath.Join(repo, "c.go") + "|c.go|A",
		repo + "\x00review\x00" + filepath.Join(repo, "d.go") + "|d.go|A",
	}, leaves)

	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00review\x00"+filepath.Join(repo, "a.go"))
	main := h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return showing(n, "a.go") })
	assert.Contains(t, main.Children[1].Children[0].Props["text"], "+two")

	write(t, filepath.Join(repo, "b.go"), "b\n")
	git(t, repo, "add", "b.go")
	git(t, repo, "commit", "-qm", "b")
	start := time.Now()
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (4)", 4))
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestReviewHead(t *testing.T) {
	repo := newRepo(t, "a.go", "b.go", "d.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	git(t, repo, "checkout", "-qb", "feature/x")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	write(t, filepath.Join(repo, "n.go"), "n\n")
	require.NoError(t, os.Remove(filepath.Join(repo, "d.go")))
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-qm", "feature")
	git(t, repo, "checkout", "-q", "main")
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, editorManifest))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })

	h.Event(t, id, mainSlot, reviewFormID(t, h, id), "review", map[string]string{"base": "main", "head": "feature/x"})
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main...feature/x (3)", 3))

	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00review\x00"+filepath.Join(repo, "a.go"))
	main := h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return showing(n, "a.go") })
	assert.Contains(t, main.Children[1].Children[0].Props["text"], "+two")
	content, err := os.ReadFile(filepath.Join(repo, "a.go"))
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(content))

	wt := filepath.Join(t.TempDir(), "wt")
	git(t, repo, "worktree", "add", "-q", wt, "feature/x")
	write(t, filepath.Join(wt, "b.go"), "two\n")
	git(t, wt, "commit", "-qam", "b")
	start := time.Now()
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main...feature/x (4)", 4))
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestReviewErrors(t *testing.T) {
	repo := branchRepo(t)
	h, id := open(t, repo)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })
	reviewBranch(t, h, id)
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (3)", 3))

	form := reviewFormID(t, h, id)
	h.Event(t, id, mainSlot, form, "review", map[string]string{"base": "main", "head": "nope"})
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return badgeText(n) == "unknown branch nope" })
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (3)", 3))
	h.Event(t, id, mainSlot, form, "review", map[string]string{"base": "gone", "head": "HEAD"})
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return badgeText(n) == "unknown branch gone" })
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (3)", 3))

	git(t, repo, "checkout", "-q", "--orphan", "o")
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return badgeText(n) == "unknown branch HEAD" })
	git(t, repo, "commit", "-qm", "orphan")
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return badgeText(n) == "no merge base with main" })
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (3)", 3))
}

func TestReviewPersists(t *testing.T) {
	repo := branchRepo(t)
	h, id := open(t, repo)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })
	h.Event(t, id, mainSlot, reviewFormID(t, h, id), "review", map[string]string{"base": "main", "head": "feature/x"})
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main...feature/x (3)", 3))

	pids := h.Sup.PIDs()
	require.Len(t, pids, 1)
	require.NoError(t, syscall.Kill(pids[0], syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		restarted := h.Sup.PIDs()
		return len(restarted) == 1 && restarted[0] != pids[0]
	}, restartWait)
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main...feature/x (3)", 3))
}

func TestCorruptStoredReviewIgnored(t *testing.T) {
	repo := branchRepo(t)
	h, id := open(t, repo)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })
	require.NoError(t, h.KV.Set(t.Context(), "git", "review/"+id, json.RawMessage(`"corrupt"`)))

	pids := h.Sup.PIDs()
	require.Len(t, pids, 1)
	require.NoError(t, syscall.Kill(pids[0], syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		restarted := h.Sup.PIDs()
		return len(restarted) == 1 && restarted[0] != pids[0]
	}, restartWait)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 })
	reviewBranch(t, h, id)
	h.WaitSlot(t, id, sidebarSlot, reviewing(t, "Review: main (3)", 3))
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
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Head    string `json:"head"`
	Main    bool   `json:"main"`
	Current bool   `json:"current"`
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
	main := worktreeEntry{Repo: repo, Path: repo, Branch: "main", Head: head, Main: true, Current: true}
	assert.Equal(t, main, list[0])
	assert.ElementsMatch(t, []worktreeEntry{
		main,
		{Repo: repo, Path: feat, Branch: "feat", Head: head},
		{Repo: repo, Path: detached, Head: head},
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

	assert.Equal(t, opResult{Done: true}, awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": feat}))
	assert.NoDirExists(t, feat)
	assert.Equal(t, repo, firstRoot(t, h, id))
	git(t, repo, "rev-parse", "--verify", "-q", "feat")
	testutil.Eventually(t, func() bool {
		s, err := h.Svc.GetSession(t.Context(), featNvim)
		return err != nil || s.State != "running"
	}, 5*time.Second)
}
