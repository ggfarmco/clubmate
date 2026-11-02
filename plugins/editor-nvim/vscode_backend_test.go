package main_test

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const fakeCodeScript = `#!/bin/sh
[ "$1" = serve-web ] || exit 2
while [ $# -gt 0 ]; do [ "$1" = --cli-data-dir ] && cli=$2; shift; done
if [ -e "$(dirname "$0")/offline" ]; then echo 'network down' >&2; exit 1; fi
sleep 1
bin="$cli/serve-web/c1/bin/code-server"
mkdir -p "$(dirname "$bin")"
cat > "$bin" <<'EOS'
#!/bin/sh
marks="$(dirname "$0")"
while [ $# -gt 0 ]; do [ "$1" = --port ] && port=$2; shift; done
echo x >> "$marks/starts"
echo "Web UI available at http://127.0.0.1:$port?tkn=x"
if [ -e "$FAKECODE_DIR/crash" ]; then sleep 0.3; exit 1; fi
sleep 60
EOS
chmod +x "$bin"
echo "Web UI available at http://127.0.0.1:1/?tkn=x"
sleep 60
`

type vscodeEditor struct {
	*editor
	code string
}

func fakeCode(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "code"), []byte(fakeCodeScript), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKECODE_DIR", dir)
	return dir
}

func startVSCode(t *testing.T, accepted bool, marks ...string) *vscodeEditor {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("SHELL", "/bin/sh")
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, t.TempDir())
	}
	code := fakeCode(t)
	for _, m := range marks {
		require.NoError(t, os.WriteFile(filepath.Join(code, m), nil, 0o600))
	}
	h := testutil.NewHarness(t)
	settings, err := json.Marshal(map[string]any{"editor": "vscode", "vscodeAccepted": accepted})
	require.NoError(t, err)
	require.NoError(t, h.KV.Set(t.Context(), "editor-nvim", "settings", settings))
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/editor-nvim", "plugins/editor-nvim/plugin.json"))
	top, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{top})
	require.NoError(t, err)
	e := &editor{h: h, workspaceID: p.ID, top: top, repo: top}
	t.Cleanup(func() { e.killAll(t) })
	require.NoError(t, h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": p.ID}, nil))
	return &vscodeEditor{editor: e, code: code}
}

func (e *vscodeEditor) state() string {
	return filepath.Join(e.h.Dirs.PluginState("editor-nvim"), "vscode")
}

func (e *vscodeEditor) webview(t *testing.T, ok func(url.Values) bool) url.Values {
	t.Helper()
	var q url.Values
	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool {
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
	return e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool { return n.Type == "webview" })
}

func (e *vscodeEditor) event(t *testing.T, slot, id string) {
	t.Helper()
	e.h.Event(t, e.workspaceID, slot, id, id, nil)
}

func serverPID(t *testing.T, state string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(state, "server.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	return pid
}

func hasButton(n sdui.Node, id string) bool {
	if n.Type == "button" && n.ID == id {
		return true
	}
	for _, c := range n.Children {
		if hasButton(c, id) {
			return true
		}
	}
	return false
}

func hasEmpty(n sdui.Node, text string) bool {
	return n.Type == "empty" && n.Props["text"] == text
}

func TestVSCodeConsentFirst(t *testing.T) {
	e := startVSCode(t, false)
	other, err := e.h.Svc.CreateWorkspace(t.Context(), "other", []string{t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, e.h.Client(t).Call(t.Context(), "ui.subscribe", map[string]string{"workspaceId": other.ID}, nil))
	for _, id := range []string{e.workspaceID, other.ID} {
		e.h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return hasButton(n, "vscode-accept") })
	}
	require.NoError(t, e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.settings-set", "workspaceId": "", "args": map[string]any{"vscodeAccepted": true}}, nil))
	e.h.WaitSlot(t, e.workspaceID, statusSlot, func(n sdui.Node) bool { return strings.Contains(nodeText(n), "VS Code") })
	for _, slot := range []string{mainSlot, statusSlot} {
		params := map[string]any{"workspaceId": e.workspaceID, "slot": slot, "nodeId": "vscode-accept", "action": "vscode-accept", "agent": true}
		assert.Error(t, e.h.Client(t).Call(t.Context(), "ui.event", params, nil))
	}
	assert.False(t, settingsOf(t, e.h).VSCodeAccepted)
	assert.NoDirExists(t, filepath.Join(e.state(), "cli"))

	e.event(t, mainSlot, "vscode-accept")

	for _, id := range []string{e.workspaceID, other.ID} {
		e.h.WaitSlot(t, id, mainSlot, func(n sdui.Node) bool { return hasEmpty(n, "Downloading the VS Code server…") })
	}
	q := e.webview(t, func(url.Values) bool { return true })
	assert.Equal(t, e.repo, q.Get("folder"))
	assert.True(t, settingsOf(t, e.h).VSCodeAccepted)
}

func TestVSCodeFollowsRoot(t *testing.T) {
	e := startVSCode(t, true)
	require.NoError(t, os.WriteFile(filepath.Join(e.repo, "a.go"), []byte("package a\n"), 0o600))
	wt := addWorktree(t, e.top)
	e.webview(t, func(q url.Values) bool { return q.Get("folder") == e.repo })

	_, err := e.h.Svc.SetWorktree(t.Context(), e.workspaceID, e.repo, wt)
	require.NoError(t, err)

	e.webview(t, func(q url.Values) bool { return q.Get("folder") == wt })
}

func TestVSCodeOpenAddsPayload(t *testing.T) {
	e := startVSCode(t, true)
	e.waitWebview(t)
	path := filepath.Join(e.repo, "a b.go")
	c := e.h.Client(t)
	drain(c)

	require.NoError(t, e.open(t, path, 12))

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

func TestQuickfixShowsNvimThenBack(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not in PATH")
	}
	e := startVSCode(t, true)
	e.waitWebview(t)
	path := e.file(t, "a.go")
	args := map[string]any{"title": "Search", "items": []map[string]any{{"path": path, "line": 1}}}

	e.run(t, "quickfix", args, nil)

	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool { return n.Type == "surface" })
	e.h.WaitSlot(t, e.workspaceID, statusSlot, func(n sdui.Node) bool { return hasButton(n, "back-to-vscode") })
	e.event(t, statusSlot, "back-to-vscode")
	e.waitWebview(t)
}

func TestSelectionNotAvailable(t *testing.T) {
	e := startVSCode(t, true)
	e.waitWebview(t)
	err := e.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "editor-nvim.selection", "workspaceId": e.workspaceID}, nil)
	assert.ErrorContains(t, err, "Not available with VS Code yet")
}

func TestSwitchRerendersOpenWorkspaces(t *testing.T) {
	fakeCode(t)
	e := startEditor(t)

	require.NoError(t, setEditor(t, e.h, "vscode"))

	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool { return hasButton(n, "vscode-accept") })
}

func TestRetryAfterDownloadFailure(t *testing.T) {
	e := startVSCode(t, true, "offline")
	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool {
		return hasButton(n, "vscode-retry") && strings.Contains(nodeText(n), "network down")
	})
	require.NoError(t, os.Remove(filepath.Join(e.code, "offline")))

	e.event(t, mainSlot, "vscode-retry")

	e.waitWebview(t)
}

func TestServerExitRestartsOnceThenFails(t *testing.T) {
	e := startVSCode(t, true)
	e.waitWebview(t)
	starts := filepath.Join(e.state(), "cli", "serve-web", "c1", "bin", "starts")
	require.NoError(t, os.WriteFile(filepath.Join(e.code, "crash"), nil, 0o600))
	require.NoError(t, syscall.Kill(-serverPID(t, e.state()), syscall.SIGKILL))

	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool { return hasButton(n, "vscode-retry") })
	raw, err := os.ReadFile(starts)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(raw), "x"))
}

func TestSwitchToNvimStopsServer(t *testing.T) {
	e := startVSCode(t, true)
	e.waitWebview(t)
	pid := serverPID(t, e.state())

	require.NoError(t, setEditor(t, e.h, "nvim"))

	testutil.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }, restartWait)
	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool { return n.Type == "surface" })
}

func TestThemeChangeBumpsRev(t *testing.T) {
	e := startVSCode(t, true)
	before := e.waitWebview(t)
	rev, _ := before.Props["rev"].(float64)

	e.publish(t, "theme.changed", strings.Replace(themePayload("light", 16), `"dark":true`, `"dark":false`, 1))

	e.h.WaitSlot(t, e.workspaceID, mainSlot, func(n sdui.Node) bool {
		r, _ := n.Props["rev"].(float64)
		return n.Type == "webview" && r == rev+1
	})
	raw, err := os.ReadFile(filepath.Join(e.state(), "server", "data", "Machine", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Default Light Modern")
}

func nodeText(n sdui.Node) string {
	s, _ := n.Props["text"].(string)
	for _, c := range n.Children {
		s += " " + nodeText(c)
	}
	return s
}
