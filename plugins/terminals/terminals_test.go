package main_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/internal/wire"
	"clubmate/pkg/sdui"
)

func TestTerminalsLifecycle(t *testing.T) {
	h := newHarness(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo, t.TempDir()})
	require.NoError(t, err)
	sidebar := h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	assert.Equal(t, "stack", sidebar.Type)
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID}, nil))
	main := h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	sid := main.Props["sessionId"].(string)
	s := h.GetSession(t, sid)
	assert.Equal(t, "terminals", s.Owner)
	assert.Equal(t, "Terminal 1", s.Title)
	assert.Equal(t, repo, s.Cwd)
	h.Event(t, p.ID, "terminals.main", "close", "close", nil)
	h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "empty" })
	h.WaitSessionGone(t, sid)
	other := p.Repos[1]
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID, "args": map[string]string{"cwd": other}}, nil))
	main = h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	assert.Equal(t, other, h.GetSession(t, main.Props["sessionId"].(string)).Cwd)
}

func TestTerminalPathHasTools(t *testing.T) {
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools")
	out := filepath.Join(dir, "path")
	sh := filepath.Join(dir, "sh")
	require.NoError(t, os.Mkdir(tools, 0o700))
	require.NoError(t, os.WriteFile(sh, []byte("#!/bin/sh\nprintf '%s' \"$PATH\" > "+out+"\nexec sleep 30\n"), 0o700))
	t.Setenv("CLUBMATE_RESOURCES", dir)
	t.Setenv("SHELL", sh)
	h := testutil.NewHarness(t)
	h.Start(t, testutil.BuildPluginDir(t, "clubmate/plugins/terminals", "plugins/terminals/plugin.json"))
	p := h.CreateProject(t, "demo")
	h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID}, nil))
	var path []byte
	testutil.Eventually(t, func() bool {
		var err error
		path, err = os.ReadFile(out)
		return err == nil && len(path) > 0
	}, 5*time.Second)
	assert.True(t, strings.HasSuffix(string(path), ":"+tools), string(path))
}

func TestExitedTerminalCanBeRestarted(t *testing.T) {
	h := newHarness(t)
	p := h.CreateProject(t, "demo")
	require.NoError(t, h.Svc.CheckTransitions(t.Context()))
	h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	h.Event(t, p.ID, "terminals.sidebar", "new", "new", nil)
	assert.Same(t, h.Client(t), h.Client(t))
	main := h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	old := main.Props["sessionId"].(string)
	require.NoError(t, h.Svc.KillSession(t.Context(), old))
	testutil.Eventually(t, func() bool { return h.GetSession(t, old).State != session.StateRunning }, 5*time.Second)
	require.NoError(t, h.Svc.CheckTransitions(t.Context()))
	h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return contains(n, "button") })
	h.Event(t, p.ID, "terminals.main", "restart", "restart", nil)
	main = h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" })
	fresh := main.Props["sessionId"].(string)
	assert.NotEqual(t, old, fresh)
	assert.Equal(t, "Terminal 1", h.GetSession(t, fresh).Title)
	h.WaitSessionGone(t, old)
}

func TestNewTerminalFocusesMainSlot(t *testing.T) {
	h := newHarness(t)
	p := h.CreateProject(t, "demo")
	h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "ui.subscribe", map[string]string{"projectId": p.ID}, nil))
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID}, nil))
	deadline := time.After(2 * time.Second)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method != "ui.focus" {
				continue
			}
			var got struct{ Section, Slot string }
			require.NoError(t, json.Unmarshal(n.Params, &got))
			assert.Equal(t, "terminals", got.Section)
			assert.Equal(t, "terminals.main", got.Slot)
			return
		case <-deadline:
			t.Fatal("no ui.focus notification")
		}
	}
}

func TestTerminalBusy(t *testing.T) {
	h := newHarness(t)
	p := h.CreateProject(t, "demo")
	h.WaitSlot(t, p.ID, "terminals.sidebar", func(n sdui.Node) bool { return contains(n, "empty") })
	c := h.Client(t)
	require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "terminals.new", "projectId": p.ID}, nil))
	sid := h.WaitSlot(t, p.ID, "terminals.main", func(n sdui.Node) bool { return n.Type == "surface" }).Props["sessionId"].(string)
	t.Cleanup(func() {
		_ = c.Call(context.Background(), "command.run", map[string]any{"id": "terminals.close", "projectId": p.ID, "args": map[string]string{"sessionId": sid}}, nil)
		h.WaitSessionGone(t, sid)
	})
	busy := func(want bool, process string) func() bool {
		return func() bool {
			var got struct {
				Busy    bool   `json:"busy"`
				Process string `json:"process"`
			}
			err := c.Call(t.Context(), "command.run", map[string]any{"id": "terminals.busy", "projectId": p.ID, "args": map[string]string{"sessionId": sid}}, &got)
			return err == nil && got.Busy == want && got.Process == process
		}
	}
	testutil.Eventually(t, busy(false, ""), 5*time.Second)
	conn, err := net.Dial("unix", h.Dirs.SessionSocket(sid))
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, wire.WriteFrame(conn, wire.Frame{Type: wire.FrameData, Payload: []byte("sleep 100\n")}))
	testutil.Eventually(t, busy(true, "sleep"), 5*time.Second)
}

func newHarness(t *testing.T) *testutil.PluginHarness {
	t.Helper()
	return testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/terminals", "plugins/terminals/plugin.json"))
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
