package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	sidebarSlot    = "editor-nvim.sidebar"
	editorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","icon":"chevron.left.forwardslash.chevron.right","order":10}]}}`
)

func items(t *testing.T, n sdui.Node) []sdui.TreeItem {
	t.Helper()
	require.Equal(t, "scroll", n.Type)
	require.Len(t, n.Children, 1)
	raw, err := json.Marshal(n.Children[0].Props["items"])
	require.NoError(t, err)
	var out []sdui.TreeItem
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func breadcrumb(t *testing.T, n sdui.Node) []string {
	t.Helper()
	raw, err := json.Marshal(n.Children[0].Props["breadcrumb"])
	require.NoError(t, err)
	var crumbs []sdui.Item
	require.NoError(t, json.Unmarshal(raw, &crumbs))
	ids := make([]string, len(crumbs))
	for i, c := range crumbs {
		ids[i] = c.ID
	}
	return ids
}

func focused(t *testing.T, want ...string) func(sdui.Node) bool {
	return func(n sdui.Node) bool { return n.Type == "scroll" && assert.ObjectsAreEqual(want, breadcrumb(t, n)) }
}

func startFocusTree(t *testing.T) (*testutil.PluginHarness, string, string) {
	t.Helper()
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/files", "plugins/files/plugin.json"),
		testutil.EchoPluginDirAs(t, editorManifest),
	)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "a", "b", "c"), 0o700))
	for _, f := range []string{"a/q.go", "a/b/y.go", "a/b/c/x.go", "z.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(repo, f), nil, 0o600))
	}
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, nil))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return n.Type == "scroll" })
	h.Event(t, p.ID, sidebarSlot, "files", "focus", filepath.Join(repo, "a", "b"))
	return h, p.ID, repo
}

func TestFocusFolder(t *testing.T) {
	h, id, repo := startFocusTree(t)
	dir := filepath.Join(repo, "a", "b")

	tree := h.WaitSlot(t, id, sidebarSlot, focused(t, repo, filepath.Join(repo, "a"), dir))
	ids := make([]string, 0, 2)
	for _, it := range items(t, tree) {
		ids = append(ids, it.ID)
	}
	assert.Equal(t, []string{filepath.Join(dir, "c"), filepath.Join(dir, "y.go")}, ids)

	var pid int
	for _, st := range h.Sup.Statuses() {
		if st.ID == "files" {
			pid = st.PID
		}
	}
	require.NotZero(t, pid)
	require.NoError(t, unix.Kill(pid, unix.SIGKILL))
	testutil.Eventually(t, func() bool {
		pids := h.Sup.PIDs()
		return len(pids) > 0 && !slices.Contains(pids, pid)
	}, 15*time.Second)
	h.WaitSlot(t, id, sidebarSlot, focused(t, repo, filepath.Join(repo, "a"), dir))
}

func TestRevealOutsideFocusMovesItUp(t *testing.T) {
	h, id, repo := startFocusTree(t)
	h.WaitSlot(t, id, sidebarSlot, focused(t, repo, filepath.Join(repo, "a"), filepath.Join(repo, "a", "b")))
	file := filepath.Join(repo, "a", "q.go")

	payload, err := json.Marshal(map[string]string{"projectId": id, "path": file})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "editor.buffer", "editor-nvim", payload)
	require.NoError(t, err)

	tree := h.WaitSlot(t, id, sidebarSlot, focused(t, repo, filepath.Join(repo, "a")))
	assert.Equal(t, file, tree.Children[0].Props["selected"])
}

func TestRootsChangeClearsFocus(t *testing.T) {
	h, id, repo := startFocusTree(t)
	h.WaitSlot(t, id, sidebarSlot, focused(t, repo, filepath.Join(repo, "a"), filepath.Join(repo, "a", "b")))
	other, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	_, err = h.Svc.SetWorktree(t.Context(), id, repo, other)
	require.NoError(t, err)

	tree := h.WaitSlot(t, id, sidebarSlot, func(n sdui.Node) bool {
		list := items(t, n)
		return len(list) == 1 && list[0].ID == other
	})
	assert.Empty(t, breadcrumb(t, tree))
}

func find(list []sdui.TreeItem, id string) (sdui.TreeItem, bool) {
	for _, it := range list {
		if it.ID == id {
			return it, true
		}
		if got, ok := find(it.Children, id); ok {
			return got, true
		}
	}
	return sdui.TreeItem{}, false
}

func TestBufferEventReveals(t *testing.T) {
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/files", "plugins/files/plugin.json"),
		testutil.EchoPluginDirAs(t, editorManifest),
	)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	nested := filepath.Join(repo, "x", "y")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	file := filepath.Join(nested, "z.go")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return n.Type == "scroll" })

	payload, err := json.Marshal(map[string]string{"projectId": p.ID, "path": file})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "editor.buffer", "editor-nvim", payload)
	require.NoError(t, err)

	tree := h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return len(n.Children) == 1 && n.Children[0].Props["selected"] == file })
	it, ok := find(items(t, tree), nested)
	require.True(t, ok)
	assert.True(t, it.Expanded)
	assert.Equal(t, "x / y", it.Label)

	created := filepath.Join(nested, "new.go")
	require.NoError(t, os.WriteFile(created, nil, 0o600))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool {
		_, ok := find(items(t, n), created)
		return ok
	})
}

func TestGitBadgesFollowRepoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/files", "plugins/files/plugin.json"),
		testutil.EchoPluginDirAs(t, editorManifest),
	)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	dir := filepath.Join(repo, "sub")
	require.NoError(t, os.Mkdir(dir, 0o700))
	file := filepath.Join(dir, "a.txt")
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(file, []byte("one\n"), 0o600))
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	untracked := filepath.Join(repo, "u.txt")
	require.NoError(t, os.WriteFile(untracked, nil, 0o600))
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	badge := func(id, want string) func(sdui.Node) bool {
		return func(n sdui.Node) bool {
			it, ok := find(items(t, n), id)
			return ok && it.Badge == want
		}
	}
	h.WaitSlot(t, p.ID, sidebarSlot, badge(untracked, "U"))

	require.NoError(t, os.WriteFile(file, []byte("two\n"), 0o600))
	h.WaitSlot(t, p.ID, sidebarSlot, badge(dir, "•"))
	git("commit", "-qam", "two")
	h.WaitSlot(t, p.ID, sidebarSlot, badge(dir, ""))
}

func TestCollapseOthers(t *testing.T) {
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/files", "plugins/files/plugin.json"),
		testutil.EchoPluginDirAs(t, editorManifest),
	)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	deep := filepath.Join(repo, "a", "b", "c")
	file := filepath.Join(deep, "x.go")
	require.NoError(t, os.MkdirAll(deep, 0o700))
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	for _, d := range []string{"d", "e"} {
		require.NoError(t, os.MkdirAll(filepath.Join(repo, d, "sub"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(repo, d, "f.go"), nil, 0o600))
	}
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return n.Type == "scroll" })
	for _, d := range []string{deep, filepath.Join(repo, "d"), filepath.Join(repo, "e")} {
		h.Event(t, p.ID, sidebarSlot, "files", "expand", d)
	}
	payload, err := json.Marshal(map[string]string{"projectId": p.ID, "path": file})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "editor.buffer", "editor-nvim", payload)
	require.NoError(t, err)
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return len(n.Children) == 1 && n.Children[0].Props["selected"] == file })

	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "files.collapse-others", "projectId": p.ID}, nil))

	tree := h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool {
		it, ok := find(items(t, n), filepath.Join(repo, "d"))
		return ok && !it.Expanded
	})
	for dir, want := range map[string]bool{repo: true, deep: true, filepath.Join(repo, "e"): false} {
		it, ok := find(items(t, tree), dir)
		require.True(t, ok, dir)
		assert.Equal(t, want, it.Expanded, dir)
	}
}

func TestTreeFollowsWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	h := testutil.NewPluginHarness(t,
		testutil.BuildPluginDir(t, "clubmate/plugins/files", "plugins/files/plugin.json"),
		testutil.EchoPluginDirAs(t, editorManifest),
	)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o600))
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	wt := filepath.Join(repo, ".worktrees", "feat")
	git("worktree", "add", "-q", wt, "-b", "feat")
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, nil))
	rootIs := func(want string) func(sdui.Node) bool {
		return func(n sdui.Node) bool {
			list := items(t, n)
			return len(list) == 1 && list[0].ID == want
		}
	}
	h.WaitSlot(t, p.ID, sidebarSlot, rootIs(repo))

	_, err = h.Svc.SetWorktree(t.Context(), p.ID, repo, wt)
	require.NoError(t, err)
	h.WaitSlot(t, p.ID, sidebarSlot, rootIs(wt))

	file := filepath.Join(wt, "a.txt")
	payload, err := json.Marshal(map[string]string{"projectId": p.ID, "path": file})
	require.NoError(t, err)
	_, err = h.Bus.Publish(t.Context(), "editor.buffer", "editor-nvim", payload)
	require.NoError(t, err)
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool { return n.Children[0].Props["selected"] == file })

	_, err = h.Svc.SetWorktree(t.Context(), p.ID, repo, "")
	require.NoError(t, err)
	h.WaitSlot(t, p.ID, sidebarSlot, func(n sdui.Node) bool {
		return rootIs(repo)(n) && n.Children[0].Props["selected"] != file
	})
}
