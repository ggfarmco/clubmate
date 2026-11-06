package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	editorSlot         = "editor-nvim.main"
	editorManifest     = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"sections":[{"id":"editor-nvim","title":"Code","order":10}],"commands":[{"id":"editor-nvim.checktime","title":"Checktime","hidden":true,"handles":["git.worktree.switched"]},{"id":"editor-nvim.guard-switch","title":"Guard Switch","hidden":true,"handles":["git.worktree.switching"]}]}}`
	readyWait          = 15 * time.Second
	stopEditorManifest = `{"id":"editor-nvim","name":"Code","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"editor-nvim.stop-root","title":"Stop Root","hidden":true,"handles":["git.worktree.removing"]},{"id":"editor-nvim.release-root","title":"Release Root","hidden":true,"handles":["git.worktree.released"]}]},"echoResults":{"editor-nvim.stop-root":{},"editor-nvim.release-root":{}}}`
	agentsManifest     = `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"agents.stop-root","title":"Stop Worktree Agents","hidden":true,"handles":["git.worktree.vacating"]}]},"echoResults":{"agents.stop-root":{}}}`
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
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	waitReady(t, h)
	return h, p.ID
}

func waitReady(t *testing.T, h *testutil.PluginHarness) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		events, err := h.Bus.Read(t.Context(), "git.ready", 0, 1)
		_, running := h.Sup.Conn("git")
		return err == nil && len(events) > 0 && running
	}, readyWait)
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

type worktreeEntry struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Head    string `json:"head"`
	Main    bool   `json:"main"`
	Current bool   `json:"current"`
}

type opResult struct {
	Done   bool   `json:"done"`
	Error  string `json:"error"`
	Path   string `json:"path"`
	Notice string `json:"notice"`
	Note   string `json:"note"`
}

type sessionEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type outcomeEntry struct {
	Workspace string       `json:"workspace"`
	ID        string       `json:"id"`
	Message   string       `json:"message"`
	Confirm   *promptEntry `json:"confirm"`
}

type promptEntry struct {
	Title   string          `json:"title"`
	Detail  string          `json:"detail"`
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args"`
}

func committedRepo(t *testing.T) string {
	t.Helper()
	repo := newRepo(t, "a.go")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	return repo
}

func runCommand(t *testing.T, h *testutil.PluginHarness, id, command string, args, out any) error {
	t.Helper()
	return h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "workspaceId": id, "args": args}, out)
}

func listWorktrees(t *testing.T, h *testutil.PluginHarness, id string) []worktreeEntry {
	t.Helper()
	var list []worktreeEntry
	require.NoError(t, runCommand(t, h, id, "git.worktrees", nil, &list))
	return list
}

func firstRoot(t *testing.T, h *testutil.PluginHarness, id string) string {
	t.Helper()
	p, err := h.Svc.GetWorkspace(t.Context(), id)
	require.NoError(t, err)
	return p.Folders[0].Root()
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
	return waitOp(t, h, id, op)
}

func waitOp(t *testing.T, h *testutil.PluginHarness, id, op string) opResult {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var res opResult
		require.NoError(t, runCommand(t, h, id, "git.worktree-op-status", map[string]string{"id": op}, &res))
		if res.Done {
			return res
		}
		require.True(t, time.Now().Before(deadline), "operation %s did not finish", op)
		time.Sleep(20 * time.Millisecond)
	}
}

func outcomes(t *testing.T, h *testutil.PluginHarness) []outcomeEntry {
	t.Helper()
	events, err := h.Bus.Read(t.Context(), "git.outcome", 0, 1000)
	require.NoError(t, err)
	out := make([]outcomeEntry, 0, len(events))
	for _, ev := range events {
		var o outcomeEntry
		require.NoError(t, json.Unmarshal(ev.Payload, &o))
		out = append(out, o)
	}
	return out
}

func outcomeOf(t *testing.T, h *testutil.PluginHarness, op string) outcomeEntry {
	t.Helper()
	i := slices.IndexFunc(outcomes(t, h), func(o outcomeEntry) bool { return o.ID == op })
	require.GreaterOrEqual(t, i, 0, "no git.outcome for %s", op)
	return outcomes(t, h)[i]
}

func TestWorktreeList(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	t.Setenv("GIT_COMMITTER_DATE", time.Now().Add(time.Hour).Format(time.RFC3339))
	git(t, feat, "commit", "-q", "--allow-empty", "-m", "feat")
	detached := filepath.Join(repo, ".worktrees", "detached")
	git(t, repo, "worktree", "add", "-q", "--detach", detached)
	h, id := open(t, repo)

	head := git(t, repo, "rev-parse", "--short", "HEAD")
	featHead := git(t, feat, "rev-parse", "--short", "HEAD")
	assert.Equal(t, []worktreeEntry{
		{Repo: repo, Path: repo, Branch: "main", Head: head, Main: true, Current: true},
		{Repo: repo, Path: feat, Branch: "feat", Head: featHead},
		{Repo: repo, Path: detached, Head: head},
	}, listWorktrees(t, h, id))
}

func TestNewWorktree(t *testing.T) {
	repo := committedRepo(t)
	git(t, repo, "branch", "old")
	old := git(t, repo, "rev-parse", "old")
	write(t, filepath.Join(repo, "a.go"), "two\n")
	git(t, repo, "commit", "-qam", "two")
	h, id := open(t, repo)
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
	h, id := open(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "child", "switch": false})
	child := filepath.Join(repo, ".worktrees", "child")
	assert.Equal(t, opResult{Done: true, Path: child}, res)
	assert.Equal(t, repo, firstRoot(t, h, id))

	var started struct {
		ID string `json:"id"`
	}
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "git.worktree-new", "workspaceId": id, "agent": true, "args": map[string]any{"branch": "agent", "switch": false}}, &started))
	assert.Empty(t, waitOp(t, h, id, started.ID).Error)
	assert.Len(t, outcomes(t, h), 1)
}

func TestNewWorktreeFromOtherWorktree(t *testing.T) {
	repo := committedRepo(t)
	other := filepath.Join(repo, ".worktrees", "other")
	git(t, repo, "worktree", "add", "-q", "-b", "other", other)
	write(t, filepath.Join(other, "a.go"), "other\n")
	git(t, other, "commit", "-qam", "other")
	h, id := open(t, repo)

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
	h, id := open(t, repo)

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
	h, id := open(t, folder)
	switchTo := func(path string) error {
		return runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": path}, nil)
	}

	require.NoError(t, switchTo(wt))
	assert.Equal(t, filepath.Join(wt, "sub"), firstRoot(t, h, id))
	list := listWorktrees(t, h, id)
	require.Len(t, list, 2)
	assert.Equal(t, wt, list[0].Path)
	assert.True(t, list[0].Current)
	assert.False(t, list[1].Current)
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
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, stopEditorManifest))
	shell := func(owner string) sessionEntry {
		s, err := h.Svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: id, Title: owner, Owner: owner, Command: []string{"sh", "-c", "exec cat"}, Cwd: feat})
		require.NoError(t, err)
		return sessionEntry{ID: s.ID, Title: s.Title}
	}
	terminal, editor := shell("terminals"), shell("editor-nvim")

	var users []sessionEntry
	require.NoError(t, runCommand(t, h, id, "git.worktree-sessions", map[string]string{"path": feat}, &users))
	assert.Contains(t, users, terminal)
	assert.NotContains(t, users, editor)
	var prompt promptEntry
	require.NoError(t, runCommand(t, h, id, "git.worktree-remove-prompt", map[string]string{"path": feat}, &prompt))
	assert.Equal(t, "Remove worktree feat? Its branch is kept.", prompt.Title)
	assert.Equal(t, "Terminals still in it: terminals", prompt.Detail)
	assert.Equal(t, "git.worktree-remove", prompt.Command)
	assert.JSONEq(t, fmt.Sprintf(`{"repo":%q,"path":%q}`, repo, feat), string(prompt.Args))

	assert.ErrorContains(t, runCommand(t, h, id, "git.worktree-remove-prompt", map[string]string{"path": repo}, nil), "cannot remove the main checkout")
	_, err := startOp(t, h, id, "git.worktree-remove", map[string]string{"path": repo})
	assert.ErrorContains(t, err, "cannot remove the main checkout")
	assert.Equal(t, "worktree has uncommitted changes", awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": dirty}).Error)
	assert.DirExists(t, dirty)

	p, err := h.Svc.CreateWorkspace(t.Context(), "feat", []string{feat})
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
	h, id := open(t, repo, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"), testutil.EchoPluginDirAs(t, agentsManifest))
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
		return strings.Contains(string(e.Payload), `"agents.stop-root"`) && strings.Contains(string(e.Payload), `"path":"`+feat+`"`)
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
	git(t, repo, "worktree", "add", "-q", "-b", "feature", feat)
	h, id := open(t, repo)
	require.NoError(t, runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": feat}, nil))
	require.Equal(t, feat, firstRoot(t, h, id))

	require.NoError(t, os.RemoveAll(feat))

	testutil.Eventually(t, func() bool { return len(outcomes(t, h)) > 0 }, 10*time.Second)
	assert.Equal(t, []outcomeEntry{{Workspace: id, Message: "Worktree feature no longer exists — back on main"}}, outcomes(t, h))
	p, err := h.Svc.GetWorkspace(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []string{repo}, p.Paths())
	assert.Empty(t, p.Folders[0].Worktree)
}

func TestInPlaceSwitchPublishesChanged(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	sub, err := h.Bus.Subscribe("git.changed", math.MaxInt64)
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	deadline := time.Now().Add(10 * time.Second)
	for n := 0; ; n++ {
		require.True(t, time.Now().Before(deadline), "no git.changed after an in-place switch")
		git(t, repo, "switch", "-q", "-c", fmt.Sprintf("b%d", n))
		select {
		case ev := <-sub.C():
			var payload struct {
				Workspace string `json:"workspace"`
			}
			require.NoError(t, json.Unmarshal(ev.Payload, &payload))
			assert.Equal(t, id, payload.Workspace)
			return
		case <-time.After(time.Second):
		}
	}
}

func TestSwitchRefusedWhileBusy(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	require.NoError(t, os.MkdirAll(filepath.Dir(hook), 0o700))
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nsleep 2\n"), 0o700))
	h, id := open(t, repo)
	op, err := startOp(t, h, id, "git.worktree-new", map[string]any{"branch": "slow", "switch": false})
	require.NoError(t, err)

	err = runCommand(t, h, id, "git.worktree-switch", map[string]string{"path": feat}, nil)
	assert.ErrorContains(t, err, "another git operation is running")
	assert.Equal(t, repo, firstRoot(t, h, id))
	assert.Empty(t, waitOp(t, h, id, op).Error)
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
	waitReady(t, h)

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
	h, id := open(t, repo)

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
	h, id := open(t, repo, switchEditor(t, ""))

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
	op, err := startOp(t, h, id, "git.branch-switch", map[string]string{"branch": "origin/remote-only"})
	require.NoError(t, err)
	res := waitOp(t, h, id, op)
	require.Empty(t, res.Error)
	assert.Equal(t, "remote-only", git(t, repo, "branch", "--show-current"))
	assert.Contains(t, res.Notice, "you are leaving 1 commit behind")
	assert.Equal(t, res.Notice, outcomeOf(t, h, op).Message)

	require.Empty(t, awaitOp(t, h, id, "git.branch-switch", map[string]string{"branch": "wt"}).Error)
	assert.Equal(t, wt, firstRoot(t, h, id))
	assert.Equal(t, "remote-only", git(t, repo, "branch", "--show-current"))

	_, err = startOp(t, h, id, "git.branch-switch", map[string]string{"branch": "-x"})
	assert.ErrorContains(t, err, "not a valid branch name")
}

func switchEditor(t *testing.T, refusal string) string {
	t.Helper()
	canned := `"echoResults":{"editor-nvim.guard-switch":null,"editor-nvim.checktime":null},`
	if refusal != "" {
		canned += fmt.Sprintf(`"echoErrors":{"editor-nvim.guard-switch":%q},`, refusal)
	}
	return testutil.EchoPluginDirAs(t, strings.Replace(editorManifest, `{"id":"editor-nvim",`, `{"id":"editor-nvim",`+canned, 1))
}

func TestBranchSwitchRefusesUnsavedBuffers(t *testing.T) {
	repo := clonedRepo(t)
	h, id := open(t, repo, switchEditor(t, "Save or discard unsaved changes first: a.go"))

	_, err := startOp(t, h, id, "git.branch-switch", map[string]string{"branch": "origin/remote-only"})
	assert.ErrorContains(t, err, "Save or discard unsaved changes first: a.go")
	assert.Equal(t, "main", git(t, repo, "branch", "--show-current"))
}

type approver struct {
	asked  atomic.Int32
	answer atomic.Bool
	last   atomic.Pointer[confirmRequest]
}

type confirmRequest struct {
	Title  string            `json:"title"`
	Args   map[string]string `json:"args"`
	Body   string            `json:"body"`
	Plugin bool              `json:"plugin"`
}

func newApprover(t *testing.T, h *testutil.PluginHarness) *approver {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	a := &approver{}
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		var p confirmRequest
		if err := json.Unmarshal(*req.Params, &p); err != nil {
			return nil, err
		}
		a.last.Store(&p)
		a.asked.Add(1)
		return map[string]bool{"approved": a.answer.Load()}, nil
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() {
		conn.Close()
		server.Close()
	})
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"ui.confirm"}}, nil))
	return a
}

func hookRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := committedRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate"), 0o755))
	for name, content := range files {
		write(t, filepath.Join(repo, ".clubmate", name), content)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "hooks")
	return repo
}

func TestWorktreeSetupHook(t *testing.T) {
	repo := hookRepo(t, map[string]string{
		"setup.sh":         "cat .env > setup-ran # \u202enur\r\n",
		"worktree-include": "# secrets\n.env\nconf/local.env\nmissing\nlink\n../escape\n",
		"a\n──── fake":     "──── .clubmate/setup.sh\n",
	})
	write(t, filepath.Join(repo, ".env"), "SECRET=1\n")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "conf"), 0o755))
	write(t, filepath.Join(repo, "conf", "local.env"), "LOCAL=1\n")
	require.NoError(t, os.Symlink(filepath.Join(repo, ".env"), filepath.Join(repo, "link")))
	h, id := open(t, repo)
	a := newApprover(t, h)
	create := func(branch string) (opResult, string) {
		res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": branch, "switch": false})
		return res, filepath.Join(repo, ".worktrees", branch)
	}

	long := "denied-" + strings.Repeat("x", 150)
	res, denied := create(long)
	assert.Empty(t, res.Error)
	assert.Contains(t, res.Notice, "Skipped .clubmate/setup.sh: denied by user")
	assert.Contains(t, res.Notice, `"../escape" is not a path inside the repo`)
	assert.Contains(t, res.Notice, "link: not a regular file")
	assert.NoFileExists(t, filepath.Join(denied, "setup-ran"))
	assert.FileExists(t, filepath.Join(denied, "conf", "local.env"))
	assert.NoFileExists(t, filepath.Join(denied, "link"))
	asked := a.last.Load()
	forged := `\u{2500}\u{2500}\u{2500}\u{2500}`
	body := "──── .clubmate/setup.sh\ncat .env > setup-ran # \\u{202e}nur\\u{d}\n" +
		"\n──── .clubmate/a\\u{a}" + forged + " fake\n" + forged + " .clubmate/setup.sh\n" +
		"\n──── .clubmate/worktree-include\n# secrets\n.env\nconf/local.env\nmissing\nlink\n../escape\n"
	assert.Equal(t, confirmRequest{Title: "Run .clubmate/setup.sh", Args: asked.Args, Body: body, Plugin: true}, *asked)
	assert.Equal(t, "all of .clubmate/ (3 entries, shown below)", asked.Args["trusts"])
	assert.True(t, strings.HasPrefix(asked.Args["worktree"], "…"))
	assert.True(t, strings.HasSuffix(asked.Args["worktree"], "/.worktrees/"+long))
	assert.Equal(t, 200, utf8.RuneCountInString(asked.Args["worktree"]))

	a.answer.Store(true)
	for _, branch := range []string{"trusted", "again"} {
		res, dir := create(branch)
		assert.Empty(t, res.Error)
		data, err := os.ReadFile(filepath.Join(dir, "setup-ran"))
		require.NoError(t, err)
		assert.Equal(t, "SECRET=1\n", string(data))
	}
	assert.EqualValues(t, 2, a.asked.Load())

	write(t, filepath.Join(repo, ".clubmate", "helper.sh"), "true\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "helper")
	res, _ = create("helper")
	assert.Empty(t, res.Error)
	assert.EqualValues(t, 3, a.asked.Load())
}

func TestWorktreeHooksTooLargeAreSkippedUnasked(t *testing.T) {
	repo := hookRepo(t, map[string]string{"setup.sh": "touch setup-ran\n", "blob": strings.Repeat("x", 64<<10)})
	h, id := open(t, repo)
	a := newApprover(t, h)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "feat", "switch": false})
	assert.Equal(t, "Skipped .clubmate/setup.sh: .clubmate/ holds more than 64 KiB to review", res.Notice)
	assert.NoFileExists(t, filepath.Join(repo, ".worktrees", "feat", "setup-ran"))
	assert.Zero(t, a.asked.Load())
}

func TestWorktreeSetupFailureRemovesWorktree(t *testing.T) {
	repo := hookRepo(t, map[string]string{"setup.sh": "echo boom; exit 3\n"})
	h, id := open(t, repo)
	newApprover(t, h).answer.Store(true)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "feat", "switch": false})
	assert.Contains(t, res.Error, ".clubmate/setup.sh failed: exit status 3: boom")
	assert.NoDirExists(t, filepath.Join(repo, ".worktrees", "feat"))
	git(t, repo, "rev-parse", "--verify", "-q", "feat")
}

func TestWorktreeTeardownFailureStillRemoves(t *testing.T) {
	repo := hookRepo(t, map[string]string{"teardown.sh": "echo bye; exit 1\n"})
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, stopEditorManifest, agentsManifest))
	testutil.Eventually(t, func() bool {
		return !slices.ContainsFunc(h.Sup.Statuses(), func(s plugin.Status) bool { return s.State != "running" })
	}, readyWait)
	newApprover(t, h).answer.Store(true)

	res := awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": feat})
	assert.Equal(t, opResult{Done: true, Notice: ".clubmate/teardown.sh failed: exit status 1: bye"}, res)
	assert.NoDirExists(t, feat)
}

func TestWorktreeRemoveRefusalStopsBeforeVacating(t *testing.T) {
	repo := committedRepo(t)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feature", feat)
	editor := strings.Replace(stopEditorManifest, `"echoResults"`, `"echoErrors":{"editor-nvim.stop-root":"unsaved buffers: a.go"},"echoResults"`, 1)
	h, id := open(t, repo, testutil.EchoPluginDirAs(t, editor, agentsManifest))
	testutil.Eventually(t, func() bool {
		return !slices.ContainsFunc(h.Sup.Statuses(), func(s plugin.Status) bool { return s.State != "running" })
	}, readyWait)

	op, err := startOp(t, h, id, "git.worktree-remove", map[string]string{"path": feat})
	require.NoError(t, err)
	assert.Equal(t, opResult{Done: true, Error: "unsaved buffers: a.go"}, waitOp(t, h, id, op))
	asked := outcomeOf(t, h, op)
	assert.Empty(t, asked.Message)
	require.NotNil(t, asked.Confirm)
	assert.Equal(t, "Worktree feature has unsaved buffers: a.go. Remove it and discard the changes?", asked.Confirm.Title)
	assert.Equal(t, "git.worktree-remove", asked.Confirm.Command)
	assert.JSONEq(t, fmt.Sprintf(`{"repo":%q,"path":%q,"force":true}`, repo, feat), string(asked.Confirm.Args))

	op, err = startOp(t, h, id, "git.worktree-remove", asked.Confirm.Args)
	require.NoError(t, err)
	assert.Equal(t, opResult{Done: true, Error: "unsaved buffers: a.go"}, waitOp(t, h, id, op))
	assert.Equal(t, outcomeEntry{Workspace: id, ID: op, Message: "unsaved buffers: a.go"}, outcomeOf(t, h, op))
	assert.DirExists(t, feat)
	assert.JSONEq(t, fmt.Sprintf(`{"path":%q,"root":%q,"force":true}`, feat, feat), string(commandArgs(t, h, "editor-nvim.stop-root")[1]))
	assert.Empty(t, commandArgs(t, h, "agents.stop-root"))
	assert.Len(t, commandArgs(t, h, "editor-nvim.release-root"), 2)
}
