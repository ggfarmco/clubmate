package main_test

import (
	"cmp"
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
	stubDiffview  = `vim.api.nvim_create_user_command("DiffviewOpen", function(ctx) vim.g.dv_log = vim.list_extend(vim.g.dv_log or {}, { ctx.args }) end, { nargs = "*" })`
	mainSlot      = "editor-nvim.main"
	statusSlot    = "editor-nvim.status"
	restartWait   = 15 * time.Second
	blockedBudget = 3 * time.Second
)

type setup struct {
	missingFirst bool
	initLua      string
	link         bool
	sub          string
	plugins      []string
	theme        string
}

type editor struct {
	h         *testutil.PluginHarness
	projectID string
	top       string
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
}

func TestSelectionReturnsVisualRange(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o600))
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "expand('%:p')") == path }, 5*time.Second)
	err := e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.selection", "projectId": e.projectID}, nil)
	assert.ErrorContains(t, err, "no selection")
	require.NoError(t, v.Command("call setpos(\"'<\", [0, 1, 3, 0])"))
	require.NoError(t, v.Command("call setpos(\"'>\", [0, 2, 2, 0])"))
	var got struct {
		Path      string `json:"path"`
		StartLine int    `json:"startLine"`
		EndLine   int    `json:"endLine"`
		Text      string `json:"text"`
	}
	e.run(t, "selection", nil, &got)
	assert.Equal(t, path, got.Path)
	assert.Equal(t, 1, got.StartLine)
	assert.Equal(t, 2, got.EndLine)
	assert.Equal(t, "pha\nbe", got.Text)
}

func TestChecktimeReloadsBuffer(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "expand('%:p')") == path }, 5*time.Second)
	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0o600))
	e.run(t, "checktime", map[string]string{"root": e.repo}, nil)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "getline(1)") == "changed" }, 5*time.Second)
}

func TestChecktimeKeepsModifiedBuffer(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "expand('%:p')") == path }, 5*time.Second)
	require.NoError(t, v.Command("normal! Iunsaved "))
	require.NoError(t, os.WriteFile(path, []byte("changed\n"), 0o600))
	e.run(t, "checktime", map[string]string{"root": e.repo}, nil)
	assert.False(t, eval[bool](t, v, "nvim_get_mode().blocking"))
	assert.Equal(t, "unsaved line", eval[string](t, v, "getline(1)"))
}

func TestChecktimeLoadsFileCreatedAfterOpen(t *testing.T) {
	e := startEditor(t)
	path := filepath.Join(e.repo, "new.txt")
	require.NoError(t, e.open(t, e.file(t, "a.go"), 1))
	v := dial(t, e.sock)
	require.NoError(t, v.Command("edit "+path))
	require.NoError(t, os.WriteFile(path, []byte("created\n"), 0o600))
	e.run(t, "checktime", map[string]string{"root": e.repo}, nil)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "getline(1)") == "created" }, 5*time.Second)
	assert.False(t, eval[bool](t, v, "nvim_get_mode().blocking"))
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
	require.ErrorContains(t, e.open(t, path, 2), "Neovim is waiting for input")
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

func TestDiffviewOpensCheckpoint(t *testing.T) {
	e := startEditorIn(t, setup{initLua: stubDiffview, sub: "repo dir"})
	v := dial(t, e.sock)
	c := e.h.Client(t)
	sha := strings.Repeat("0123456789", 4)
	drain(c)
	e.run(t, "diffview", map[string]any{"cwd": e.repo, "rev": sha}, nil)
	want := eval[string](t, v, "'-C' . fnameescape('"+e.repo+"') . ' "+sha+" --untracked-scoped'")
	testutil.Eventually(t, func() bool { return slices.Equal([]string{want}, eval[[]string](t, v, "get(g:, 'dv_log', [])")) }, 5*time.Second)
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

func TestDiffviewRejectsRange(t *testing.T) {
	e := startEditorIn(t, setup{initLua: stubDiffview})
	v := dial(t, e.sock)
	err := e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.diffview", "projectId": e.projectID, "args": map[string]any{"cwd": e.repo, "rev": "a...b"}}, nil)
	require.ErrorContains(t, err, "rev must be a commit sha")
	assert.Equal(t, 0, eval[int](t, v, "exists('g:dv_log')"))
}

func TestDiffviewSwitchesWorktree(t *testing.T) {
	e, wt := startWorktreeEditor(t, setup{initLua: stubDiffview})
	sha := strings.TrimSpace(gitIn(t, e.top, "rev-parse", "HEAD"))
	e.run(t, "diffview", map[string]any{"cwd": wt, "rev": sha}, nil)
	p, err := e.h.Svc.GetProject(t.Context(), e.projectID)
	require.NoError(t, err)
	assert.Equal(t, wt, p.Roots[0])
	feat := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	v := dial(t, e.h.GetSession(t, feat.Props["sessionId"].(string)).Command[2])
	testutil.Eventually(t, func() bool { return len(eval[[]string](t, v, "get(g:, 'dv_log', [])")) == 1 }, 5*time.Second)
	assert.Equal(t, 0, eval[int](t, dial(t, e.sock), "exists('g:dv_log')"))
}

func TestDiffviewMissingShowsOpenFailed(t *testing.T) {
	e := startEditor(t)
	e.run(t, "diffview", map[string]any{"cwd": e.repo, "rev": "abcdef0"}, nil)
	e.h.WaitSlot(t, e.projectID, statusSlot, func(n sdui.Node) bool { return hasBadge(n, "open failed: Command not found: DiffviewOpen") })
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

func TestSwitchKeepsNvimPerRoot(t *testing.T) {
	e, wt := startWorktreeEditor(t, setup{})
	pid := eval[int](t, dial(t, e.sock), "getpid()")
	feat := e.setWorktree(t, wt)
	assert.Equal(t, wt, eval[string](t, dial(t, feat.Command[2]), "getcwd()"))
	e.setWorktree(t, "")
	assert.Equal(t, pid, eval[int](t, dial(t, e.sock), "getpid()"))
	assert.ElementsMatch(t, []string{e.sessionID, feat.ID}, e.running(t))
}

func TestOpenRoutesToOwningWorktree(t *testing.T) {
	e, wt := startWorktreeEditor(t, setup{link: true, sub: "sub"})
	path := filepath.Join(e.top, ".worktrees", "feat", "sub", "a.go")
	require.NoError(t, e.open(t, path, 1))
	p, err := e.h.Svc.GetProject(t.Context(), e.projectID)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wt, "sub"), p.Roots[0])
	feat := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	v := dial(t, e.h.GetSession(t, feat.Props["sessionId"].(string)).Command[2])
	real, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "resolve(expand('%:p'))") == real }, 5*time.Second)
	assert.Equal(t, 0, eval[int](t, dial(t, e.sock), "bufexists('"+path+"')"))
}

func TestOpenIgnoresForeignWorktreeList(t *testing.T) {
	e, _ := startWorktreeEditor(t, setup{})
	foreign, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	out, err := exec.Command("git", "-C", foreign, "init", "-q").CombinedOutput()
	require.NoError(t, err, string(out))
	entry := filepath.Join(foreign, ".git", "worktrees", "x")
	require.NoError(t, os.MkdirAll(entry, 0o700))
	for name, content := range map[string]string{"gitdir": filepath.Join(e.repo, ".git") + "\n", "commondir": "../..\n", "HEAD": "ref: refs/heads/main\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(entry, name), []byte(content), 0o600))
	}
	path := filepath.Join(foreign, "x.go")
	require.NoError(t, os.WriteFile(path, []byte("x\n"), 0o600))
	require.NoError(t, e.open(t, path, 1))
	p, err := e.h.Svc.GetProject(t.Context(), e.projectID)
	require.NoError(t, err)
	assert.Equal(t, e.repo, p.Roots[0])
}

func TestStopRootRefusesUnsaved(t *testing.T) {
	e, wt := startWorktreeEditor(t, setup{})
	pid := eval[int](t, dial(t, e.sock), "getpid()")
	feat := e.setWorktree(t, wt)
	path := filepath.Join(wt, "a.go")
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, feat.Command[2])
	testutil.Eventually(t, func() bool { return eval[string](t, v, "expand('%:p')") == path }, 5*time.Second)
	require.NoError(t, v.Command("normal! ix"))
	e.setWorktree(t, "")
	run := func(id string, args any) error {
		return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": id, "projectId": e.projectID, "args": args}, nil)
	}
	require.ErrorContains(t, run("editor-nvim.restart", nil), "unsaved buffers: .worktrees/feat/a.go")
	require.ErrorContains(t, run("editor-nvim.stop-root", map[string]any{"root": wt}), "unsaved buffers:")
	require.NoError(t, run("editor-nvim.stop-root", map[string]any{"root": wt, "force": true}))
	testutil.Eventually(t, func() bool { return slices.Equal([]string{e.sessionID}, e.running(t)) }, 5*time.Second)
	assert.Equal(t, pid, eval[int](t, dial(t, e.sock), "getpid()"))
}

func TestUnsavedListsModifiedBuffersWithoutStopping(t *testing.T) {
	e := startEditor(t)
	path := e.file(t, "a.go")
	require.NoError(t, e.open(t, path, 1))
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "expand('%:p')") == path }, 5*time.Second)
	unsaved := func() []string {
		var names []string
		require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.unsaved", "projectId": e.projectID, "args": map[string]string{"root": e.repo}}, &names))
		return names
	}
	assert.Empty(t, unsaved())
	require.NoError(t, v.Command("normal! ix"))
	assert.Equal(t, []string{"a.go"}, unsaved())
	assert.Equal(t, []string{e.sessionID}, e.running(t))
}

func TestStopRootBlocksStartUntilReleased(t *testing.T) {
	e, wt := startWorktreeEditor(t, setup{})
	feat := e.setWorktree(t, wt)
	run := func(id string, args any) error {
		return e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": id, "projectId": e.projectID, "args": args}, nil)
	}

	require.NoError(t, run("editor-nvim.stop-root", map[string]any{"root": wt}))
	testutil.Eventually(t, func() bool { return slices.Equal([]string{e.sessionID}, e.running(t)) }, 5*time.Second)
	require.ErrorContains(t, e.open(t, filepath.Join(wt, "a.go"), 1), "the worktree is being removed")
	assert.Equal(t, []string{e.sessionID}, e.running(t))

	require.NoError(t, run("editor-nvim.release-root", map[string]any{"root": wt}))
	e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		id, _ := n.Props["sessionId"].(string)
		return n.Type == "surface" && id != "" && id != feat.ID && e.h.GetSession(t, id).Cwd == wt
	})
}

func startWorktreeEditor(t *testing.T, s setup) (*editor, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	e := startEditorIn(t, s)
	e.file(t, "a.go")
	gitIn(t, e.top, "init", "-q")
	gitIn(t, e.top, "add", ".")
	gitIn(t, e.top, "commit", "-qm", "init")
	gitIn(t, e.top, "worktree", "add", "-q", filepath.Join(e.top, ".worktrees", "feat"), "-b", "feat")
	var trees []string
	for line := range strings.Lines(gitIn(t, e.top, "worktree", "list", "--porcelain")) {
		if tree, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			trees = append(trees, tree)
		}
	}
	require.Len(t, trees, 2)
	return e, trees[1]
}

func (e *editor) setWorktree(t *testing.T, path string) session.Session {
	t.Helper()
	_, err := e.h.Svc.SetWorktree(t.Context(), e.projectID, e.repo, path)
	require.NoError(t, err)
	want := cmp.Or(path, e.repo)
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		id, _ := n.Props["sessionId"].(string)
		return n.Type == "surface" && id != "" && e.h.GetSession(t, id).Cwd == want
	})
	return e.h.GetSession(t, main.Props["sessionId"].(string))
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
		dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "clubmate-dev")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "init.lua"), []byte(s.initLua), 0o600))
	}
	t.Setenv("SHELL", "/bin/sh")
	h := testutil.NewHarness(t)
	if s.theme != "" {
		_, err := h.Bus.Publish(t.Context(), "theme.changed", "test", json.RawMessage(s.theme))
		require.NoError(t, err)
	}
	h.Start(t, append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json")}, s.plugins...)...)
	top, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	if s.link {
		link := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink(top, link))
		top = link
	}
	repo := filepath.Join(top, s.sub)
	require.NoError(t, os.MkdirAll(repo, 0o700))
	repos := []string{repo}
	if s.missingFirst {
		repos = []string{t.TempDir(), repo}
	}
	p, err := h.Svc.CreateProject(t.Context(), "demo", repos)
	require.NoError(t, err)
	if s.missingFirst {
		require.NoError(t, os.RemoveAll(repos[0]))
	}
	e := &editor{h: h, projectID: p.ID, top: top, repo: repo}
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
	require.GreaterOrEqual(t, len(sess.Command), 3)
	require.Equal(t, "--listen", sess.Command[len(sess.Command)-2])
	e.sock = sess.Command[len(sess.Command)-1]
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

func (e *editor) run(t *testing.T, name string, args, res any) {
	t.Helper()
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim." + name, "projectId": e.projectID, "args": args}, res))
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
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

func TestLaunchSetsAppNameAndDistro(t *testing.T) {
	system, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim not in PATH")
	}
	res := t.TempDir()
	bundled := filepath.Join(res, "nvim", "bin", "nvim")
	tools := filepath.Join(res, "tools")
	distroInit := filepath.Join(res, "nvim-distro", "init.lua")
	require.NoError(t, os.MkdirAll(filepath.Dir(bundled), 0o700))
	require.NoError(t, os.Symlink(system, bundled))
	require.NoError(t, os.Mkdir(tools, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(distroInit), 0o700))
	require.NoError(t, os.WriteFile(distroInit, []byte("vim.g.distro = 1"), 0o600))
	t.Setenv("CLUBMATE_RESOURCES", res)
	t.Setenv("VIMRUNTIME", "/nonexistent")
	t.Setenv("NVIM_APPNAME", "elsewhere")
	e := startEditor(t)
	assert.Equal(t, []string{bundled, "-u", distroInit, "--listen", e.sock}, e.h.GetSession(t, e.sessionID).Command)
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[int](t, v, "v:vim_did_enter") == 1 }, 5*time.Second)
	assert.Equal(t, 1, eval[int](t, v, "g:distro"))
	assert.False(t, strings.HasPrefix(eval[string](t, v, "$VIMRUNTIME"), "/nonexistent"))
	assert.Equal(t, "clubmate-dev", eval[string](t, v, "$NVIM_APPNAME"))
	assert.True(t, strings.HasSuffix(eval[string](t, v, "$PATH"), ":"+tools))
}

func TestBundledFallsBackToPath(t *testing.T) {
	res := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(res, "nvim-distro"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(res, "nvim-distro", "init.lua"), nil, 0o600))
	t.Setenv("CLUBMATE_RESOURCES", res)
	e := startEditor(t)
	want, err := exec.LookPath("nvim")
	require.NoError(t, err)
	assert.Equal(t, []string{want, "--listen", e.sock}, e.h.GetSession(t, e.sessionID).Command)
}

func TestThemeFileWrittenAndPushed(t *testing.T) {
	e := startEditor(t)
	v := dial(t, e.sock)
	require.NoError(t, v.ExecLua(`package.loaded["clubmate.theme"] = { apply = function(t) vim.g.got = vim.list_extend(vim.g.got or {}, { t.name }) end }`, nil))
	bad := themePayload("Bad", 15)
	good := themePayload("Dracula", 16)
	e.publish(t, "theme.changed", bad)
	e.publish(t, "theme.changed", good)
	testutil.Eventually(t, func() bool { return len(eval[[]string](t, v, "get(g:, 'got', [])")) > 0 }, 5*time.Second)
	assert.Equal(t, []string{"Dracula"}, eval[[]string](t, v, "g:got"))
	got, err := os.ReadFile(e.themeFile())
	require.NoError(t, err)
	assert.JSONEq(t, good, string(got))
}

func TestThemePublishedBeforeStartIsWritten(t *testing.T) {
	theme := themePayload("Catppuccin Mocha", 16)
	e := startEditorIn(t, setup{theme: theme})
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(e.themeFile())
		return err == nil
	}, 5*time.Second)
	got, err := os.ReadFile(e.themeFile())
	require.NoError(t, err)
	assert.JSONEq(t, theme, string(got))
}

func TestThemeAppliedOnAttach(t *testing.T) {
	e := startEditorIn(t, setup{
		theme:   themePayload("Catppuccin Mocha", 16),
		initLua: `package.loaded["clubmate.theme"] = { apply = function(t) vim.g.applied = t.name end }`,
	})
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[string](t, v, "get(g:, 'applied', '')") == "Catppuccin Mocha" }, 5*time.Second)
}

func TestLaunchPassesThemeFile(t *testing.T) {
	e := startEditor(t)
	assert.Empty(t, eval[string](t, dial(t, e.sock), "$CLUBMATE_THEME_FILE"))
	e.publish(t, "theme.changed", themePayload("Dracula", 16))
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(e.themeFile())
		return err == nil
	}, 5*time.Second)
	e.run(t, "restart", nil, nil)
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	cmd := e.h.GetSession(t, main.Props["sessionId"].(string)).Command
	assert.Equal(t, e.themeFile(), eval[string](t, dial(t, cmd[len(cmd)-1]), "$CLUBMATE_THEME_FILE"))
}

func themePayload(name string, colors int) string {
	palette, _ := json.Marshal(slices.Repeat([]string{"#112233"}, colors))
	return `{"name":"` + name + `","dark":true,"background":"#1f1f28","foreground":"#dcd7ba","palette":` + string(palette) + `}`
}

func (e *editor) publish(t *testing.T, topic, payload string) {
	t.Helper()
	_, err := e.h.Bus.Publish(t.Context(), topic, "test", json.RawMessage(payload))
	require.NoError(t, err)
}

func (e *editor) themeFile() string {
	return filepath.Join(e.h.Dirs.PluginState("editor-nvim"), "theme.json")
}

func TestImportStartsAgentSession(t *testing.T) {
	home := importHome(t)
	res := t.TempDir()
	t.Setenv("CLUBMATE_RESOURCES", res)
	e := startEditorIn(t, setup{plugins: []string{fakeAgents(t)}})
	c := e.h.Client(t)
	drain(c)
	e.run(t, "import-config", nil, nil)
	calls := agentCalls(t, e)
	require.Len(t, calls, 1)
	assert.Equal(t, "Import nvim config", calls[0].Title)
	assert.Contains(t, calls[0].Prompt, filepath.Join(home, ".config", "nvim"))
	assert.Contains(t, calls[0].Prompt, filepath.Join(home, ".config", "clubmate-dev"))
	assert.Contains(t, calls[0].Prompt, filepath.Join(res, "nvim-distro", "skill", "SKILL.md"))
	n, ok := nextFocus(c, 5*time.Second)
	require.True(t, ok, "no ui.focus notification")
	var focus struct {
		Section string `json:"section"`
	}
	require.NoError(t, json.Unmarshal(n.Params, &focus))
	assert.Equal(t, "agents", focus.Section)
}

func TestImportRefused(t *testing.T) {
	home := importHome(t)
	e := startEditorIn(t, setup{plugins: []string{fakeAgents(t)}})
	p, err := e.h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)
	err = e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.import-config", "projectId": p.ID}, nil)
	assert.ErrorContains(t, err, "open a project workspace first")
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".config", "nvim")))
	err = e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.import-config", "projectId": e.projectID}, nil)
	assert.ErrorContains(t, err, "no ~/.config/nvim to import")
	assert.Empty(t, agentCalls(t, e))
}

func TestImportOfferedOnce(t *testing.T) {
	importHome(t)
	distroWith(t, `package.loaded["clubmate.import"] = { offer = function() vim.g.offered = (vim.g.offered or 0) + 1 end }`)
	e := startEditor(t)
	v := dial(t, e.sock)
	testutil.Eventually(t, func() bool { return eval[int](t, v, "get(g:, 'offered', 0)") == 1 }, 5*time.Second)
	e.run(t, "restart", nil, nil)
	main := e.h.WaitSlot(t, e.projectID, mainSlot, func(n sdui.Node) bool {
		return n.Type == "surface" && n.Props["sessionId"] != e.sessionID
	})
	cmd := e.h.GetSession(t, main.Props["sessionId"].(string)).Command
	w := dial(t, cmd[len(cmd)-1])
	testutil.Eventually(t, func() bool { return eval[int](t, w, "v:vim_did_enter") == 1 }, 5*time.Second)
	testutil.Never(t, func() bool { return eval[int](t, w, "get(g:, 'offered', 0)") > 0 }, time.Second)
}

func TestImportEventStartsSession(t *testing.T) {
	importHome(t)
	distroWith(t, `package.loaded["clubmate.import"] = { offer = function() vim.schedule(function() vim.rpcnotify(require("clubmate").chan, "clubmate", { event = "import" }) end) end }`)
	e := startEditorIn(t, setup{plugins: []string{fakeAgents(t)}})
	testutil.Eventually(t, func() bool { return len(agentCalls(t, e)) == 1 }, 5*time.Second)
}

type agentsNew struct {
	Prompt string `json:"prompt"`
	Title  string `json:"title"`
}

func importHome(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"GOPATH", "GOMODCACHE", "GOCACHE"} {
		out, err := exec.Command("go", "env", key).Output()
		require.NoError(t, err)
		t.Setenv(key, strings.TrimSpace(string(out)))
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "nvim"), 0o700))
	t.Setenv("HOME", home)
	return home
}

func distroWith(t *testing.T, initLua string) {
	t.Helper()
	system, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("nvim not in PATH")
	}
	res := t.TempDir()
	bundled := filepath.Join(res, "nvim", "bin", "nvim")
	require.NoError(t, os.MkdirAll(filepath.Dir(bundled), 0o700))
	require.NoError(t, os.Symlink(system, bundled))
	require.NoError(t, os.MkdirAll(filepath.Join(res, "nvim-distro"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(res, "nvim-distro", "init.lua"), []byte(initLua), 0o600))
	t.Setenv("CLUBMATE_RESOURCES", res)
}

func fakeAgents(t *testing.T) string {
	t.Helper()
	return testutil.EchoPluginDirAs(t, `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"agents","title":"Agents","icon":"sparkles","order":25}],"commands":[{"id":"agents.new","title":"New Agent Session","hidden":true}]},"echoResults":{"agents.new":{"session":"agt_1","worktree":"/x"}}}`)
}

func agentCalls(t *testing.T, e *editor) []agentsNew {
	t.Helper()
	events, err := e.h.Bus.Read(t.Context(), "echo.command", 0, 100)
	require.NoError(t, err)
	var out []agentsNew
	for _, ev := range events {
		var call struct {
			ID   string    `json:"id"`
			Args agentsNew `json:"args"`
		}
		require.NoError(t, json.Unmarshal(ev.Payload, &call))
		if call.ID == "agents.new" {
			out = append(out, call.Args)
		}
	}
	return out
}
