package macui_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSwitcherClosesAWorkspaceByItsCrossAndByDelete(t *testing.T) {
	in := start(t, "macui-switcher-close", "")
	other := filepath.Join(filepath.Dir(in.repo), "other")
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, exec.Command("git", "-C", other, "init", "-q", "-b", "main").Run())
	var opened struct {
		ID string `json:"id"`
	}
	in.json(&opened, "workspace", "open", other)
	require.NotEmpty(t, opened.ID)
	in.eventually("the second workspace is shown", func() bool {
		w := in.snapshot().main()
		return w != nil && strings.HasPrefix(w.Title, "other") && strings.Contains(w.Responder, "TerminalSurfaceView")
	})

	in.cm("ui", "present", "switcher")
	in.eventually("both workspaces have a tile and the switcher takes the keys", func() bool {
		w := in.snapshot().main()
		return w.has("switcher#"+in.workspace) && w.has("switcher#"+opened.ID) && strings.Contains(w.Responder, "SwitcherKeyView")
	})
	w := in.snapshot().main()
	require.True(t, w.has("switcher.close#"+opened.ID), "the highlighted tile shows its cross at rest")
	require.False(t, w.has("switcher.close#"+in.workspace), "a tile neither highlighted nor hovered hides its cross")
	in.pointer("hover", "switcher#"+in.workspace)
	in.eventually("hovering a tile shows its cross", func() bool {
		return in.snapshot().main().has("switcher.close#" + in.workspace)
	})
	in.pointer("click", "switcher.close#"+in.workspace)
	in.eventually("the other workspace's cross closes it and the switcher stays", func() bool {
		w := in.snapshot().main()
		return !w.has("switcher#"+in.workspace) && w.has("switcher#"+opened.ID)
	})

	in.key("delete")
	in.eventually("delete closes the highlighted workspace", func() bool {
		w := in.snapshot().main()
		return !w.has("switcher#"+opened.ID) && w.has("switcher#open")
	})
}
