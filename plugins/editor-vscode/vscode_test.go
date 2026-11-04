package main_test

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const (
	mainSlot     = "editor-vscode.main"
	statusSlot   = "editor-vscode.status"
	settingsSlot = "editor-vscode.settings"
	restartWait  = 15 * time.Second
)

const fakeCodeScript = `#!/bin/sh
[ "$1" = serve-web ] || exit 2
while [ $# -gt 0 ]; do [ "$1" = --cli-data-dir ] && cli=$2; shift; done
if [ -e "$(dirname "$0")/offline" ]; then echo 'network down' >&2; exit 1; fi
sleep 1
bin="$cli/serve-web/c1/bin/code-server"
mkdir -p "$(dirname "$bin")"
cat > "$bin" <<'EOS'
` + fakeServerScript + `
EOS
chmod +x "$bin"
echo "Web UI available at http://127.0.0.1:1/?tkn=x"
sleep 60
`

const fakeServerScript = `#!/bin/sh
marks="$(dirname "$0")"
while [ $# -gt 0 ]; do [ "$1" = --port ] && port=$2; shift; done
echo x >> "$marks/starts"
echo "Web UI available at http://127.0.0.1:$port?tkn=x"
if [ -e "$FAKECODE_DIR/crash" ]; then sleep 0.3; exit 1; fi
sleep 60`

type vscodeEditor struct {
	h           *testutil.PluginHarness
	workspaceID string
	top         string
	code        string
}

type setup struct {
	accepted bool
	bare     bool
	marks    []string
	seed     func(h *testutil.PluginHarness)
}

func startVSCode(t *testing.T, s setup) *vscodeEditor {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	code := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(code, "code"), []byte(fakeCodeScript), 0o755))
	t.Setenv("PATH", code+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKECODE_DIR", code)
	for _, m := range s.marks {
		require.NoError(t, os.WriteFile(filepath.Join(code, m), nil, 0o600))
	}
	h := testutil.NewHarness(t)
	settings, err := json.Marshal(map[string]any{"accepted": s.accepted})
	require.NoError(t, err)
	require.NoError(t, h.KV.Set(t.Context(), "editor-vscode", "settings", settings))
	require.NoError(t, h.KV.Set(t.Context(), "core", "plugin.enabled.editor-vscode", json.RawMessage("true")))
	if s.seed != nil {
		s.seed(h)
	}
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/editor-vscode", "plugins/editor-vscode/plugin.json"))
	if s.bare {
		return &vscodeEditor{h: h, code: code}
	}
	top, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{top})
	require.NoError(t, err)
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	return &vscodeEditor{h: h, workspaceID: p.ID, top: top, code: code}
}

func (e *vscodeEditor) state() string {
	return filepath.Join(e.h.Dirs.PluginState("editor-vscode"), "vscode")
}

func (e *vscodeEditor) webview(t *testing.T, ok func(url.Values) bool) url.Values {
	t.Helper()
	var q url.Values
	e.h.WaitSlotWithin(t, e.workspaceID, mainSlot, restartWait, func(n sdui.Node) bool {
		raw, _ := n.Props["url"].(string)
		u, err := url.Parse(raw)
		if n.Type != "webview" || n.Props["key"] != "vscode" || err != nil {
			return false
		}
		q = u.Query()
		return ok(q)
	})
	return q
}

func (e *vscodeEditor) waitWebview(t *testing.T) sdui.Node {
	t.Helper()
	return e.h.WaitSlotWithin(t, e.workspaceID, mainSlot, restartWait, func(n sdui.Node) bool { return n.Type == "webview" })
}

func (e *vscodeEditor) event(t *testing.T, slot, id string) {
	t.Helper()
	e.h.Event(t, e.workspaceID, slot, id, id, nil)
}

func (e *vscodeEditor) accepted(t *testing.T) bool {
	t.Helper()
	var s struct {
		Accepted bool `json:"accepted"`
	}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-vscode.settings", "workspaceId": ""}, &s))
	return s.Accepted
}

func (e *vscodeEditor) publish(t *testing.T, topic, payload string) {
	t.Helper()
	_, err := e.h.Bus.Publish(t.Context(), topic, "test", json.RawMessage(payload))
	require.NoError(t, err)
}

func serverPID(t *testing.T, state string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(state, "server.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	return pid
}

func addWorktree(t *testing.T, top string) string {
	t.Helper()
	gitIn(t, top, "init", "-q")
	gitIn(t, top, "commit", "-q", "--allow-empty", "-m", "init")
	gitIn(t, top, "worktree", "add", "-q", filepath.Join(top, ".worktrees", "feat"), "-b", "feat")
	var trees []string
	for line := range strings.Lines(gitIn(t, top, "worktree", "list", "--porcelain")) {
		if tree, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			trees = append(trees, tree)
		}
	}
	require.Len(t, trees, 2)
	return trees[1]
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
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

func hasButton(n sdui.Node, id string) bool {
	if n.Type == "button" && n.ID == id {
		return true
	}
	return slices.ContainsFunc(n.Children, func(c sdui.Node) bool { return hasButton(c, id) })
}

func hasEmpty(n sdui.Node, text string) bool {
	return n.Type == "empty" && n.Props["text"] == text
}

func nodeText(n sdui.Node) string {
	s, _ := n.Props["text"].(string)
	for _, c := range n.Children {
		s += " " + nodeText(c)
	}
	return s
}

func TestVSCodeConsentFirst(t *testing.T) {
	e := startVSCode(t, setup{})
	other, err := e.h.Svc.CreateWorkspace(t.Context(), "other", []string{t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": other.ID}, nil))
	for _, id := range []string{e.workspaceID, other.ID} {
		e.h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return hasButton(n, "vscode-accept") })
	}
	e.h.WaitSlot(t, e.workspaceID, statusSlot, func(n sdui.Node) bool { return strings.Contains(nodeText(n), "VS Code") })
	for _, slot := range []string{mainSlot, statusSlot} {
		params := map[string]any{"workspaceId": e.workspaceID, "slot": slot, "nodeId": "vscode-accept", "action": "vscode-accept", "agent": true}
		assert.Error(t, e.h.Client(t).Call(t.Context(), "ui.event", params, nil))
	}
	assert.False(t, e.accepted(t))
	assert.NoDirExists(t, filepath.Join(e.state(), "cli"))

	e.event(t, mainSlot, "vscode-accept")

	for _, id := range []string{e.workspaceID, other.ID} {
		e.h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return hasEmpty(n, "Downloading the VS Code server…") })
	}
	q := e.webview(t, func(url.Values) bool { return true })
	assert.Equal(t, e.top, q.Get("folder"))
	assert.True(t, e.accepted(t))
}

func TestAcceptInSettingsDownloadsWithoutWorkspace(t *testing.T) {
	e := startVSCode(t, setup{bare: true})
	e.h.WaitSlot(t, "", settingsSlot, func(n sdui.Node) bool { return hasButton(n, "vscode-accept") })
	params := map[string]any{"workspaceId": "", "slot": settingsSlot, "nodeId": "vscode-accept", "action": "vscode-accept", "agent": true}
	assert.Error(t, e.h.Client(t).Call(t.Context(), "ui.event", params, nil))
	assert.False(t, e.accepted(t))

	e.event(t, settingsSlot, "vscode-accept")

	e.h.WaitSlotWithin(t, "", settingsSlot, restartWait, func(n sdui.Node) bool {
		return strings.Contains(nodeText(n), "Downloaded.") && hasButton(n, "import-extensions") && !hasButton(n, "vscode-accept")
	})
	assert.True(t, e.accepted(t))
	assert.FileExists(t, filepath.Join(e.state(), "cli", "serve-web", "c1", "bin", "code-server"))
}

func TestVSCodeFollowsRoot(t *testing.T) {
	e := startVSCode(t, setup{accepted: true})
	wt := addWorktree(t, e.top)
	e.webview(t, func(q url.Values) bool { return q.Get("folder") == e.top })

	_, err := e.h.Svc.SetWorktree(t.Context(), e.workspaceID, e.top, wt)
	require.NoError(t, err)

	e.webview(t, func(q url.Values) bool { return q.Get("folder") == wt })
}

func TestVSCodeOpenAddsPayload(t *testing.T) {
	e := startVSCode(t, setup{accepted: true})
	e.waitWebview(t)
	path := filepath.Join(e.top, "a b.go")
	c := e.h.Client(t)
	drain(c)

	args := map[string]any{"path": path, "line": 12, "focus": true}
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "editor-vscode.open", "workspaceId": e.workspaceID, "args": args}, nil))

	opened := func(q url.Values) bool { return strings.Contains(q.Get("payload"), url.PathEscape("a b.go")+":12") }
	e.webview(t, opened)
	_, ok := nextFocus(c, 5*time.Second)
	assert.True(t, ok, "no ui.focus notification")
	e.publish(t, "ui.opened", `{"workspace":"`+e.workspaceID+`"}`)
	testutil.Never(t, func() bool {
		n := e.waitWebview(t)
		u, err := url.Parse(n.Props["url"].(string))
		return err != nil || !opened(u.Query())
	}, time.Second)
}

func TestRetryAfterDownloadFailure(t *testing.T) {
	e := startVSCode(t, setup{accepted: true, marks: []string{"offline"}})
	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool {
		return hasButton(n, "vscode-retry") && strings.Contains(nodeText(n), "network down")
	})
	require.NoError(t, os.Remove(filepath.Join(e.code, "offline")))

	e.event(t, mainSlot, "vscode-retry")

	e.waitWebview(t)
}

func TestServerExitRestartsOnceThenFails(t *testing.T) {
	e := startVSCode(t, setup{accepted: true})
	e.waitWebview(t)
	starts := filepath.Join(e.state(), "cli", "serve-web", "c1", "bin", "starts")
	require.NoError(t, os.WriteFile(filepath.Join(e.code, "crash"), nil, 0o600))
	require.NoError(t, syscall.Kill(-serverPID(t, e.state()), syscall.SIGKILL))

	e.h.WaitSlotWithin(t, e.workspaceID, mainSlot, restartWait, func(n sdui.Node) bool { return hasButton(n, "vscode-retry") })
	raw, err := os.ReadFile(starts)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(raw), "x"))
}

func TestThemeChangeBumpsRev(t *testing.T) {
	e := startVSCode(t, setup{accepted: true})
	before := e.waitWebview(t)
	rev, _ := before.Props["rev"].(float64)

	e.publish(t, "theme.changed", `{"name":"light","dark":false}`)

	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool {
		r, _ := n.Props["rev"].(float64)
		return n.Type == "webview" && r == rev+1
	})
	raw, err := os.ReadFile(filepath.Join(e.state(), "server", "data", "Machine", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Default Light Modern")
}

func TestAdoptsEditorNvimServer(t *testing.T) {
	var legacy string
	e := startVSCode(t, setup{accepted: true, marks: []string{"offline"}, seed: func(h *testutil.PluginHarness) {
		legacy = filepath.Join(h.Dirs.PluginState("editor-nvim"), "vscode")
		bin := filepath.Join(legacy, "cli", "serve-web", "c1", "bin", "code-server")
		require.NoError(t, os.MkdirAll(filepath.Dir(bin), 0o700))
		require.NoError(t, os.WriteFile(bin, []byte(fakeServerScript), 0o755))
	}})

	e.waitWebview(t)

	assert.NoDirExists(t, legacy)
	assert.FileExists(t, filepath.Join(e.state(), "cli", "serve-web", "c1", "bin", "starts"))
}
