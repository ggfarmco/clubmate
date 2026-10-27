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
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, under("Staged (1)"))
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { _, ok := find(n, "unstage"); return ok })
	h.Event(t, id, mainSlot, "unstage", "unstage", nil)
	h.WaitSlot(t, id, sidebarSlot, under("Changes (1)"))
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, under("Staged (1)"))

	h.Event(t, id, mainSlot, form, "commit", map[string]string{"message": "m"})
	assert.Equal(t, "m", git(t, repo, "log", "-1", "--format=%s"))
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(roots(t, n)) == 1 && len(groups(t, n)) == 0 })
	h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return formOf(n) != form })

	err := h.Client(t).Call(t.Context(), "ui.event", map[string]any{"projectId": id, "slot": mainSlot, "nodeId": form, "action": "commit", "value": map[string]string{"message": ""}}, nil)
	assert.ErrorContains(t, err, "commit message is required")
}

func TestFreshRepoStageCommit(t *testing.T) {
	repo := newRepo(t, "x")
	h, id := open(t, repo)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Untracked (1)"]) == 1 })

	h.Event(t, id, sidebarSlot, "git", "select", repo+"\x00untracked\x00"+filepath.Join(repo, "x"))
	h.Event(t, id, mainSlot, "stage", "stage", nil)
	h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool { return len(groups(t, n)["Staged (1)"]) == 1 })
	form := formOf(h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return formOf(n) != "" }))
	h.Event(t, id, mainSlot, form, "commit", map[string]string{"message": "first"})
	assert.Equal(t, "first", git(t, repo, "log", "-1", "--format=%s"))
}
