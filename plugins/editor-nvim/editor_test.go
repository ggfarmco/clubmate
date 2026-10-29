package main_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/neovim/go-client/nvim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	stubDiffview = `local function log(entry)
  local l = vim.g.dv_log or {}
  l[#l + 1] = entry
  vim.g.dv_log = l
end
vim.api.nvim_create_user_command("DiffviewOpen", function(ctx) if not vim.g.dv_fail then vim.cmd.tabnew() end; log(ctx.args) end, { nargs = "*" })
vim.api.nvim_create_user_command("DiffviewClose", function() vim.cmd.tabclose(); log("close") end, {})`
	stubSmartSplits = `_G.orig = function() vim.g.se = 1 end
package.loaded["smart-splits"] = { config = { at_edge = _G.orig } }`
	navDesc       = "clubmate navigate"
	mainSlot      = "editor-nvim.main"
	statusSlot    = "editor-nvim.status"
	restartWait   = 15 * time.Second
	blockedBudget = 3 * time.Second
)

type setup struct {
	missingFirst bool
	initLua      string
}

type editor struct {
	h         *testutil.PluginHarness
	projectID string
	repo      string
	sessionID string
	sock      string
}

func TestOpenFileAtLine(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, filepath.Join("-dir with space", "файл.go"))
	require.NoError(t, e.open(t, path, 3))
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool {
		return eval[string](t, v, "expand('%:p')") == path && eval[int](t, v, "line('.')") == 3
	}, 5*time.Second)
	e.waitBuffer(t, path)
}

func TestOpenWithColumn(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("0123456789\n", 5)), 0o600))
	v := dial(t, e.sock)
	for _, tc := range []struct{ col, want int }{{5, 4}, {99, 9}} {
		require.NoError(t, e.openAt(t, path, 3, tc.col))
		testutil.Eventually(t, func() bool {
			var pos [2]int
			require.NoError(t, v.Call("nvim_win_get_cursor", &pos, 0))
			return pos == [2]int{3, tc.want}
		}, 5*time.Second)
	}
}

func TestQuickfix(t *testing.T) {
	e := startEditor(t)
	a, b := e.file(t, "a.go"), e.file(t, "b.go")
	v := dial(t, e.sock)
	c := e.h.Client(t)
	quickfix := func(items ...map[string]any) error {
		args := map[string]any{"title": "Search: foo", "items": items}
		return c.Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.quickfix", "projectId": e.projectID, "agent": true, "args": args}, nil)
	}
	require.ErrorContains(t, quickfix(map[string]any{"path": "a.go", "line": 1}), "absolute")
	drain(c)
	long := map[string]any{"path": "/" + strings.Repeat("a", 4000), "line": 1}
	require.ErrorContains(t, quickfix(slices.Repeat([]map[string]any{long}, 600)...), "MiB")
	drain(c)
	require.NoError(t, quickfix(map[string]any{"path": a, "line": 2, "col": 3, "text": "line"}, map[string]any{"path": b, "line": 4}))
	testutil.Eventually(t, func() bool {
		return eval[map[string]any](t, v, "getqflist({'title': 1, 'size': 1})")["size"] == int64(2)
	}, 5*time.Second)
	assert.Equal(t, "Search: foo", eval[string](t, v, "getqflist({'title': 1}).title"))
	const entries = "map(getqflist(), {_, e -> [fnamemodify(bufname(e.bufnr), ':p'), e.lnum, e.col, e.text]})"
	assert.Equal(t, [][]any{{a, int64(2), int64(3), "line"}, {b, int64(4), int64(0), ""}}, eval[[][]any](t, v, entries))
	assert.Equal(t, []any{int64(1), ""}, eval[[]any](t, v, "[getqflist({'winid': 1}).winid > 0, &buftype]"))
	n, ok := nextFocus(c, 5*time.Second)
	require.True(t, ok, "no ui.focus notification")
	var focus struct {
		Section string `json:"section"`
		Slot    string `json:"slot"`
	}
	require.NoError(t, json.Unmarshal(n.Params, &focus))
	assert.Equal(t, "editor-nvim", focus.Section)
	assert.Equal(t, mainSlot, focus.Slot)
}

func TestQuickfixCapsFiles(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	items := make([]map[string]any, 0, 1003)
	for i := range 1001 {
		items = append(items, map[string]any{"path": filepath.Join(e.repo, "d", strconv.Itoa(i)+".go"), "line": 1})
	}
	items = append(items, map[string]any{"path": filepath.Join(e.repo, "d", "0.go"), "line": 2})
	args := map[string]any{"title": "Search: foo", "items": items}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.quickfix", "projectId": e.projectID, "args": args}, nil))
	start := time.Now()
	eval[int](t, v, "1")
	assert.Less(t, time.Since(start), blockedBudget)
	testutil.Eventually(t, func() bool { return eval[int](t, v, "getqflist({'size': 1}).size") == 1001 }, 20*time.Second)
	assert.Equal(t, "Search: foo (first 1000 files)", eval[string](t, v, "getqflist({'title': 1}).title"))
	assert.Equal(t, []any{"0.go", int64(2)}, eval[[]any](t, v, "[fnamemodify(bufname(getqflist()[-1].bufnr), ':t'), getqflist()[-1].lnum]"))
}

func TestOpenLightFiles(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	require.ErrorContains(t, e.open(t, filepath.Join(e.repo, "a\nb.go"), 1), "line breaks")
	big := e.file(t, filepath.Join("-dir with space", "big файл.go"))
	require.NoError(t, os.WriteFile(big, []byte(strings.Repeat("line\n", 6*1024*1024/5)), 0o600))
	blob := e.file(t, "blob.go")
	require.NoError(t, os.WriteFile(blob, []byte("a\x00b\n"), 0o600))
	for _, path := range []string{big, blob} {
		require.NoError(t, e.open(t, path, 1))
		testutil.Eventually(t, func() bool {
			return eval[string](t, v, "expand('%:p')") == path
		}, 10*time.Second)
		assert.Empty(t, eval[string](t, v, "&filetype"))
		assert.Equal(t, 0, eval[int](t, v, "&swapfile"))
		assert.True(t, eval[bool](t, v, "b:clubmate_light"))
		e.waitBuffer(t, path)
	}
	e.h.WaitSlot(t, e.projectID, statusSlot, func(n sdui.Node) bool { return hasBadge(n, "light") })
	small := e.file(t, "small.go")
	require.NoError(t, e.open(t, small, 1))
	testutil.Eventually(t, func() bool {
		return eval[string](t, v, "expand('%:p')") == small && eval[string](t, v, "&filetype") == "go"
	}, 5*time.Second)
}

func TestOpenFailureShowsInStatus(t *testing.T) {
	e := startEditorIn(t, setup{initLua: "vim.o.hidden = false"})
	first := e.file(t, "a.go")
	require.NoError(t, e.open(t, first, 1))
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "expand('%:p')") == first }, 5*time.Second)
	require.NoError(t, v.Command("normal! ix"))
	require.NoError(t, e.open(t, e.file(t, "b.go"), 1))
	e.h.WaitSlot(t, e.projectID, statusSlot, func(n sdui.Node) bool { return hasBadge(n, "open failed") && hasBadge(n, "E37") })
}

func TestNvimInitsWithoutUI(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[int](t, v, "v:vim_did_enter") == 1 }, 5*time.Second)
}

func TestBlockedNvimTimesOut(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, e.sock)
	_, err := v.Input(`:echo "a\nb"<CR>`)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		m, err := v.Mode()
		require.NoError(t, err)
		return m.Blocking
	}, 5*time.Second)
	start := time.Now()
	require.ErrorContains(t, e.open(t, path, 2), "nvim rpc timed out")
	assert.Less(t, time.Since(start), blockedBudget)
	_, err = v.Input("<CR>")
	require.NoError(t, err)
	require.NoError(t, e.open(t, path, 2))
}

func TestPluginRestartReconnects(t *testing.T) {
	e := startEditor(t)
	first := e.file(t, "a.go")
	require.NoError(t, e.open(t, first, 1))
	pid := eval[int](t, dial(t, e.sock), "getpid()")
	plugins := e.h.Sup.PIDs()
	require.Len(t, plugins, 1)
	require.NoError(t, unix.Kill(plugins[0], unix.SIGKILL))
	second := e.file(t, "b.go")
	testutil.Eventually(t, func() bool {
		restarted := e.h.Sup.PIDs()
		return len(restarted) == 1 && restarted[0] != plugins[0] && e.open(t, second, 1) == nil
	}, restartWait)
	v := dial(t, e.sock)
	assert.Equal(t, second, eval[string](t, v, "expand('%:p')"))
	assert.Equal(t, pid, eval[int](t, v, "getpid()"))
	assert.Equal(t, []string{e.sessionID}, e.running(t))
}

func TestExitedShowsRestart(t *testing.T) {
	e := startEditor(t)
	require.NoError(t, e.h.Svc.CheckTransitions(t.Context()))
	v := dial(t, e.sock)
	_ = v.Command("qa!")
	testutil.Eventually(t, func() bool { return e.h.GetSession(t, e.sessionID).State != session.StateRunning }, 5*time.Second)
	require.NoError(t, e.h.Svc.CheckTransitions(t.Context()))
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool { return contains(n, "button") })
	e.h.WaitSlot(t, e.projectID, statusSlot, func(n sdui.Node) bool { return !contains(n, "badge") })
	e.h.Event(t, e.projectID, mainSlot, "restart", "restart", nil)
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	assert.Equal(t, []string{main.Props["sessionId"].(string)}, e.running(t))
}

func TestRestartRefusesUnsavedBuffers(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	require.NoError(t, dial(t, e.sock).Command("normal! ix"))
	restart := func(args any) error {
		return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID, "args": args}, nil)
	}
	require.ErrorContains(t, restart(nil), "unsaved buffers: a.go")
	assert.Equal(t, []string{e.sessionID}, e.running(t))
	require.NoError(t, restart(map[string]any{"force": true}))
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	assert.Equal(t, []string{main.Props["sessionId"].(string)}, e.running(t))
}

func TestRestartFailsClosedWhenNvimBlocks(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	_, err := v.Input(`:echo "a\nb"<CR>`)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		m, err := v.Mode()
		require.NoError(t, err)
		return m.Blocking
	}, 5*time.Second)
	restart := func(args any) error {
		return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID, "args": args}, nil)
	}
	require.ErrorContains(t, restart(nil), "unsaved buffers: unknown (nvim not responding)")
	assert.Equal(t, []string{e.sessionID}, e.running(t))
	require.NoError(t, restart(map[string]any{"force": true}))
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	assert.Equal(t, []string{main.Props["sessionId"].(string)}, e.running(t))
}

func TestEdgeFocusesSidebar(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	c := e.h.Client(t)
	for _, dir := range []string{"h", "l"} {
		drain(c)
		require.NoError(t, v.Command("lua require('clubmate').navigate('"+dir+"')"))
		n, ok := nextFocus(c, 5*time.Second)
		require.True(t, ok, "no ui.focus notification for %s", dir)
		var focus struct {
			Section string `json:"section"`
			Slot    string `json:"slot"`
			Edge    string `json:"edge"`
		}
		require.NoError(t, json.Unmarshal(n.Params, &focus))
		assert.Equal(t, "editor-nvim", focus.Section)
		assert.Equal(t, "editor-nvim.sidebar", focus.Slot)
		assert.Equal(t, dir, focus.Edge)
	}
}

func TestEdgeAfterPluginRestart(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	require.NoError(t, v.Command("lua _G.cached = require('clubmate')"))
	plugins := e.h.Sup.PIDs()
	require.Len(t, plugins, 1)
	require.NoError(t, unix.Kill(plugins[0], unix.SIGKILL))
	c := e.h.Client(t)
	testutil.Eventually(t, func() bool {
		restarted := e.h.Sup.PIDs()
		if len(restarted) != 1 || restarted[0] == plugins[0] {
			return false
		}
		drain(c)
		if v.Command("lua _G.cached.navigate('h')") != nil {
			return false
		}
		_, ok := nextFocus(c, 200*time.Millisecond)
		return ok
	}, restartWait)
}

func TestHookCreatesNoKeymaps(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	ref, err := nvim.NewChildProcess(nvim.ChildProcessArgs("--embed", "--headless"), nvim.ChildProcessLogf(func(string, ...any) {}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ref.Close() })
	for _, n := range []*nvim.Nvim{v, ref} {
		testutil.Eventually(t, func() bool { return eval[int](t, n, "v:vim_did_enter") == 1 }, 5*time.Second)
	}
	const lhs = "map(nvim_get_keymap('n'), {_, m -> m.lhs})"
	assert.Empty(t, eval[string](t, v, "maparg('<C-H>', 'n')"))
	assert.Equal(t, eval[string](t, ref, "maparg('<C-L>', 'n')"), eval[string](t, v, "maparg('<C-L>', 'n')"))
	assert.ElementsMatch(t, eval[[]string](t, ref, lhs), eval[[]string](t, v, lhs))
}

func TestNavigationOverlayToggle(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	keys := []string{"<C-h>", "<C-j>", "<C-k>", "<C-l>", "<C-р>", "<C-о>", "<C-л>", "<C-д>"}
	descs := func(v *nvim.Nvim) []string {
		out := make([]string, 0, len(keys))
		for _, k := range keys {
			out = append(out, eval[string](t, v, "get(maparg('"+k+"', 'n', 0, 1), 'desc', '')"))
		}
		return out
	}
	enabled := slices.Repeat([]string{navDesc}, len(keys))
	e.navigation(t, true)
	assert.Equal(t, enabled, descs(v))
	e.navigation(t, false)
	assert.NotContains(t, descs(v), navDesc)
	assert.Equal(t, ":help CTRL-L-default", eval[string](t, v, "get(maparg('<C-l>', 'n', 0, 1), 'desc', '')"))
	e.navigation(t, true)
	assert.Equal(t, enabled, descs(v))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID, "args": map[string]any{"force": true}}, nil))
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	restarted := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return slices.Equal(enabled, descs(restarted)) }, 5*time.Second)
}

func TestNavigationKeepsUserMappings(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	require.NoError(t, v.Command("nnoremap <C-j> <Cmd>let g:mine = 1<CR>"))
	require.NoError(t, v.Command(`lua vim.keymap.set("n", "<C-h>", "<C-w>h", { desc = "Move to left window" })`))
	const move = "filter(maparg('<C-h>', 'n', 0, 1), {k -> index(['rhs', 'desc', 'noremap'], k) >= 0})"
	before := eval[map[string]any](t, v, move)
	assert.Equal(t, []string{"<C-j>"}, e.navigation(t, true))
	assert.Equal(t, navDesc, eval[string](t, v, "get(maparg('<C-h>', 'n', 0, 1), 'desc', '')"))
	e.navigation(t, false)
	assert.Equal(t, before, eval[map[string]any](t, v, move))
	assert.Equal(t, "<Cmd>let g:mine = 1<CR>", eval[string](t, v, "maparg('<C-j>', 'n')"))
}

func TestNavigationRespectsSmartSplits(t *testing.T) {
	e := startEditorIn(t, setup{initLua: stubSmartSplits})
	v := dial(t, e.sock)
	c := e.h.Client(t)
	e.navigation(t, true)
	assert.Empty(t, eval[string](t, v, "get(maparg('<C-h>', 'n', 0, 1), 'desc', '')"))
	drain(c)
	atEdge := func(dir string) {
		require.NoError(t, v.Command(`lua package.loaded["smart-splits"].config.at_edge({ direction = "`+dir+`" })`))
	}
	atEdge("left")
	assert.Equal(t, 0, eval[int](t, v, "get(g:, 'se', 0)"))
	n, ok := nextFocus(c, 5*time.Second)
	require.True(t, ok, "no ui.focus notification")
	var focus struct {
		Edge string `json:"edge"`
	}
	require.NoError(t, json.Unmarshal(n.Params, &focus))
	assert.Equal(t, "h", focus.Edge)
	atEdge("right")
	assert.Equal(t, 1, eval[int](t, v, "get(g:, 'se', 0)"))
	e.navigation(t, false)
	assert.True(t, eval[bool](t, v, `luaeval('package.loaded["smart-splits"].config.at_edge == _G.orig')`))
}

func TestDiffOpensSideBySide(t *testing.T) {
	e := startEditor(t)
	a := e.file(t, "a.go")
	c := filepath.Join(e.repo, "c.go")
	require.NoError(t, os.WriteFile(c, []byte("x\r\ny\r\n"), 0o600))
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", e.repo, "-c", "core.autocrlf=false"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	git("init", "-q", "-b", "trunk")
	git("add", "a.go", "c.go")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "a")
	require.NoError(t, os.WriteFile(a, []byte("changed\n"), 0o600))
	require.NoError(t, os.WriteFile(c, []byte("x\r\nz\r\n"), 0o600))
	b := filepath.Join(e.repo, "b.go")
	require.NoError(t, os.WriteFile(b, []byte("new\n"), 0o600))
	git("add", "b.go")
	v := dial(t, e.sock)
	for _, tc := range []struct {
		name, path, base, target, left, right string
		leftLines, rightLines                 []string
	}{
		{"worktree", a, "index", "worktree", "clubmate://index/a.go", a, slices.Repeat([]string{"line"}, 5), []string{"changed"}},
		{"staged", b, "head", "index", "clubmate://head/b.go", "clubmate://index/b.go", []string{""}, []string{"new"}},
		{"crlf", c, "index", "worktree", "clubmate://index/c.go", c, []string{"x", "y"}, []string{"x", "z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"path": tc.path, "base": tc.base, "target": tc.target}
			require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.diff", "projectId": e.projectID, "args": args}, nil))
			assert.Equal(t, 2, eval[int](t, v, "winnr('$')"))
			assert.Equal(t, []int{1, 1}, eval[[]int](t, v, "[getwinvar(1, '&diff'), getwinvar(2, '&diff')]"))
			assert.Equal(t, []string{tc.left, tc.right}, eval[[]string](t, v, "[nvim_buf_get_name(winbufnr(1)), nvim_buf_get_name(winbufnr(2))]"))
			assert.Equal(t, tc.leftLines, eval[[]string](t, v, "getbufline(winbufnr(1), 1, '$')"))
			assert.Equal(t, tc.rightLines, eval[[]string](t, v, "getbufline(winbufnr(2), 1, '$')"))
		})
	}
	tabs := eval[int](t, v, "tabpagenr('$')")
	args := map[string]any{"path": a, "base": "index", "target": "worktree"}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.diff", "projectId": e.projectID, "args": args}, nil))
	assert.Equal(t, tabs, eval[int](t, v, "tabpagenr('$')"))
	assert.Equal(t, 1, eval[int](t, v, "len(filter(range(1, tabpagenr('$')), {_, t -> index(tabpagebuflist(t), bufnr('"+a+"')) >= 0}))"))
	e.h.WaitSlot(t, e.projectID, statusSlot, func(n sdui.Node) bool {
		return slices.ContainsFunc(n.Children, func(c sdui.Node) bool { return c.Props["text"] == "trunk" })
	})
}

func TestReviewReplacesTab(t *testing.T) {
	e := startEditorIn(t, setup{initLua: stubDiffview})
	repo := e.dir(t, "repo dir")
	v := dial(t, e.sock)
	assert.Equal(t, "diffview", e.review(t, repo, "feature/x", ""))
	e.review(t, repo, "feature/x", "feature/y")
	want := []string{openLog(t, v, repo, "feature/x", "HEAD"), "close", openLog(t, v, repo, "feature/x", "feature/y")}
	assert.Equal(t, want, eval[[]string](t, v, "g:dv_log"))
	assert.Equal(t, 2, eval[int](t, v, "tabpagenr('$')"))
}

func TestReviewFailedOpenRecordsNoTab(t *testing.T) {
	e := startEditorIn(t, setup{initLua: stubDiffview})
	v := dial(t, e.sock)
	require.NoError(t, v.SetVar("dv_fail", true))
	assert.Equal(t, "builtin", e.review(t, e.repo, "feature/x", ""))
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.review-close", "projectId": e.projectID}, nil))
	assert.Equal(t, []string{openLog(t, v, e.repo, "feature/x", "HEAD")}, eval[[]string](t, v, "g:dv_log"))
	assert.Equal(t, 1, eval[int](t, v, "tabpagenr('$')"))
}

func TestReviewWithoutDiffview(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	assert.Equal(t, "builtin", e.review(t, e.repo, "main", ""))
	assert.Equal(t, 1, eval[int](t, v, "tabpagenr('$')"))
}

func TestDiffRevBase(t *testing.T) {
	e := startEditor(t)
	a := filepath.Join(e.repo, "a.go")
	require.NoError(t, os.WriteFile(a, []byte("one\n"), 0o600))
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", e.repo}, args...)...).Output()
		require.NoError(t, err)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "a.go")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "a")
	sha := git("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(a, []byte("two\n"), 0o600))
	v := dial(t, e.sock)
	diff := func() {
		args := map[string]any{"path": a, "base": "rev:" + sha, "target": "worktree"}
		require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.diff", "projectId": e.projectID, "args": args}, nil))
	}
	diff()
	tabs := eval[int](t, v, "tabpagenr('$')")
	assert.Equal(t, []string{"clubmate://" + sha[:7] + "/a.go", a}, eval[[]string](t, v, "[nvim_buf_get_name(winbufnr(1)), nvim_buf_get_name(winbufnr(2))]"))
	assert.Equal(t, []string{"one"}, eval[[]string](t, v, "getbufline(winbufnr(1), 1, '$')"))
	assert.Equal(t, []int{1, 1}, eval[[]int](t, v, "[getwinvar(1, '&diff'), getwinvar(2, '&diff')]"))
	diff()
	assert.Equal(t, tabs, eval[int](t, v, "tabpagenr('$')"))
}

func TestDiffRevTarget(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	e := startEditor(t)
	a := filepath.Join(e.repo, "a.go")
	n := filepath.Join(e.repo, "newdir", "n.go")
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", e.repo}, args...)...).Output()
		require.NoError(t, err)
		return strings.TrimSpace(string(out))
	}
	commit := func() string {
		git("add", "-A")
		git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "c")
		return git("rev-parse", "HEAD")
	}
	git("init", "-q")
	require.NoError(t, os.WriteFile(a, []byte("one\n"), 0o600))
	first := commit()
	require.NoError(t, os.WriteFile(a, []byte("two\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Dir(n), 0o700))
	require.NoError(t, os.WriteFile(n, []byte("new\n"), 0o600))
	second := commit()
	git("checkout", "-q", "--detach", first)
	v := dial(t, e.sock)
	diff := func(path string) {
		args := map[string]any{"path": path, "base": "rev:" + first, "target": "rev:" + second}
		require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.diff", "projectId": e.projectID, "args": args}, nil))
	}
	diff(a)
	assert.Equal(t, []string{"clubmate://" + first[:7] + "/a.go", "clubmate://" + second[:7] + "/a.go"}, eval[[]string](t, v, "[nvim_buf_get_name(winbufnr(1)), nvim_buf_get_name(winbufnr(2))]"))
	assert.Equal(t, []string{"one"}, eval[[]string](t, v, "getbufline(winbufnr(1), 1, '$')"))
	assert.Equal(t, []string{"two"}, eval[[]string](t, v, "getbufline(winbufnr(2), 1, '$')"))
	assert.Equal(t, []int{1, 1}, eval[[]int](t, v, "[getwinvar(1, '&diff'), getwinvar(2, '&diff')]"))
	assert.False(t, eval[bool](t, v, "getbufvar(winbufnr(2), '&modifiable')"))
	diff(n)
	diff(n)
	assert.Equal(t, []string{""}, eval[[]string](t, v, "getbufline(winbufnr(1), 1, '$')"))
	assert.Equal(t, []string{"new"}, eval[[]string](t, v, "getbufline(winbufnr(2), 1, '$')"))
	assert.Equal(t, 1, eval[int](t, v, "len(filter(range(1, tabpagenr('$')), {_, t -> len(filter(tabpagebuflist(t), {_, b -> bufname(b) =~# 'n\\.go$'})) > 0}))"))
}

func TestRestartIgnoresScratchBuffers(t *testing.T) {
	e := startEditor(t)
	require.NoError(t, dial(t, e.sock).Command("setlocal buftype=nofile | normal! ix"))
	err := e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID}, nil)
	require.NoError(t, err)
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
}

func TestMissingFirstRepoStartsInNext(t *testing.T) {
	e := startEditorIn(t, setup{missingFirst: true})
	assert.Equal(t, e.repo, e.h.GetSession(t, e.sessionID).Cwd)
}

func startEditor(t *testing.T) *editor {
	t.Helper()
	return startEditorIn(t, setup{})
}

func startEditorIn(t *testing.T, s setup) *editor {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not in PATH")
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, t.TempDir())
	}
	if s.initLua != "" {
		dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "nvim")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "init.lua"), []byte(s.initLua), 0o600))
	}
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"))
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repos := []string{repo}
	if s.missingFirst {
		repos = []string{t.TempDir(), repo}
	}
	p, err := h.Svc.CreateProject(t.Context(), "demo", repos)
	require.NoError(t, err)
	if s.missingFirst {
		require.NoError(t, os.RemoveAll(repos[0]))
	}
	e := &editor{h: h, projectID: p.ID, repo: repo}
	t.Cleanup(func() { e.killAll(t) })
	var sub struct {
		Subscription string `json:"subscription"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, &sub))
	main := h.WaitSlot(t, p.ID, mainSlot, func(n sdui.Node) bool { return n.Type == "surface" })
	h.WaitSlot(t, p.ID, statusSlot, func(n sdui.Node) bool { return contains(n, "badge") })
	e.sessionID = main.Props["sessionId"].(string)
	sess := h.GetSession(t, e.sessionID)
	assert.Equal(t, "nvim", sess.Title)
	assert.Equal(t, repo, sess.Cwd)
	require.Len(t, sess.Command, 3)
	e.sock = sess.Command[2]
	if s.initLua != "" {
		v := dial(t, e.sock)
		testutil.Eventually(t, func() bool { return eval[int](t, v, "v:vim_did_enter") == 1 }, 5*time.Second)
	}
	return e
}

func (e *editor) file(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(e.repo, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("line\n", 5)), 0o600))
	return path
}

func (e *editor) dir(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(e.repo, rel)
	require.NoError(t, os.MkdirAll(path, 0o700))
	return path
}

func (e *editor) review(t *testing.T, repo, base, head string) string {
	t.Helper()
	var res struct {
		Mode string `json:"mode"`
	}
	args := map[string]any{"repo": repo, "base": base, "head": head, "mergeBase": "0000000"}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.review", "projectId": e.projectID, "args": args}, &res))
	return res.Mode
}

func openLog(t *testing.T, v *nvim.Nvim, repo, base, head string) string {
	t.Helper()
	return eval[string](t, v, "'-C' . fnameescape('"+repo+"') . ' "+base+"..."+head+"'")
}

func (e *editor) open(t *testing.T, path string, line int) error {
	t.Helper()
	return e.openAt(t, path, line, 0)
}

func (e *editor) openAt(t *testing.T, path string, line, col int) error {
	t.Helper()
	args := map[string]any{"path": path, "line": line, "col": col}
	return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.open", "projectId": e.projectID, "args": args}, nil)
}

func (e *editor) navigation(t *testing.T, enabled bool) []string {
	t.Helper()
	var res struct {
		Skipped []string `json:"skipped"`
	}
	args := map[string]any{"enabled": enabled, "side": "left"}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.navigation", "projectId": e.projectID, "args": args}, &res))
	return res.Skipped
}

func (e *editor) running(t *testing.T) []string {
	t.Helper()
	sessions, err := e.h.Svc.ListSessions(t.Context(), session.ListFilter{ProjectID: e.projectID, Owner: "editor-nvim"})
	require.NoError(t, err)
	var ids []string
	for _, s := range sessions {
		if s.State == session.StateRunning {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

func (e *editor) killAll(t *testing.T) {
	ctx := context.Background()
	sessions, err := e.h.Svc.ListSessions(ctx, session.ListFilter{Owner: "editor-nvim"})
	require.NoError(t, err)
	for _, s := range sessions {
		if s.State == session.StateRunning {
			assert.NoError(t, e.h.Svc.KillSession(ctx, s.ID))
		}
	}
}

func dial(t *testing.T, sock string) *nvim.Nvim {
	t.Helper()
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(sock)
		return err == nil
	}, 5*time.Second)
	v, err := nvim.Dial(sock, nvim.DialLogf(func(string, ...any) {}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })
	return v
}

func eval[T any](t *testing.T, v *nvim.Nvim, expr string) T {
	t.Helper()
	var out T
	require.NoError(t, v.Eval(expr, &out))
	return out
}

func drain(c *rpc.Client) {
	for {
		select {
		case <-c.Notifications():
		default:
			return
		}
	}
}

func nextFocus(c *rpc.Client, wait time.Duration) (rpc.Notification, bool) {
	deadline := time.After(wait)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method == "ui.focus" {
				return n, true
			}
		case <-deadline:
			return rpc.Notification{}, false
		}
	}
}

func (e *editor) waitBuffer(t *testing.T, path string) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		events, err := e.h.Bus.Read(t.Context(), "editor.buffer", 0, 100)
		require.NoError(t, err)
		for _, ev := range events {
			var p struct {
				ProjectID string `json:"projectId"`
				Path      string `json:"path"`
			}
			require.NoError(t, json.Unmarshal(ev.Payload, &p))
			if p.ProjectID == e.projectID && p.Path == path {
				return true
			}
		}
		return false
	}, 5*time.Second)
}

func hasBadge(n sdui.Node, sub string) bool {
	if text, _ := n.Props["text"].(string); n.Type == "badge" && strings.Contains(text, sub) {
		return true
	}
	return slices.ContainsFunc(n.Children, func(c sdui.Node) bool { return hasBadge(c, sub) })
}

func contains(n sdui.Node, typ string) bool {
	if n.Type == typ {
		return true
	}
	for _, c := range n.Children {
		if contains(c, typ) {
			return true
		}
	}
	return false
}

func TestNavigationOutlastsUserMappingsAfterRestart(t *testing.T) {
	e := startEditorIn(t, setup{initLua: `vim.keymap.set("n", "<C-h>", "<C-w>h", { desc = "Move to left window" })`})
	e.navigation(t, true)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.restart", "projectId": e.projectID, "args": map[string]any{"force": true}}, nil))
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[int](t, v, "v:vim_did_enter") == 1 }, 5*time.Second)
	testutil.Eventually(t, func() bool {
		return eval[string](t, v, "get(maparg('<C-h>', 'n', 0, 1), 'desc', '')") == navDesc
	}, 5*time.Second)
}
