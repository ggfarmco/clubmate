package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot = "git.sidebar"
	mainSlot    = "git.main"
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

func open(t *testing.T, repo string) (*testutil.PluginHarness, string) {
	t.Helper()
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
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
