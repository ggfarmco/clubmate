package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/kv"
	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
)

func TestDisabledPluginIsHiddenUntilEnabled(t *testing.T) {
	h := testutil.NewHarness(t)
	p := h.CreateWorkspace(t, "demo")
	editor := func(id, extra string) string {
		return fmt.Sprintf(`{"id":%q,"name":%q,"version":"0.1.0","protocol":1,"command":["echo"]%s,"contributes":{"sections":[{"id":%q,"title":%q}],"commands":[{"id":"%s.say","title":"Say","provides":["editor.open"]}]}}`, id, id, extra, id, id, id)
	}
	root := testutil.EchoPluginDirAs(t, editor("alpha", ""), editor("beta", `,"enabledByDefault":false`))
	h.Start(t, root)
	c := h.Client(t)
	state := func(id string) plugin.Status {
		i := slices.IndexFunc(h.Sup.Statuses(), func(st plugin.Status) bool { return st.ID == id })
		return h.Sup.Statuses()[i]
	}
	testutil.Eventually(t, func() bool { return state("alpha").State == plugin.StateRunning }, 5*time.Second)
	sections := func() []string {
		var list []internalsdui.Section
		require.NoError(t, c.Call(t.Context(), "ui.sections", struct{}{}, &list))
		var ids []string
		for _, s := range list {
			ids = append(ids, s.ID)
		}
		return ids
	}
	opener := func() string {
		var out struct {
			Plugin string `json:"plugin"`
		}
		require.NoError(t, c.Call(t.Context(), "command.run", map[string]any{"id": "editor.open", "workspaceId": p.ID}, &out))
		return out.Plugin
	}
	providers := func() rpc.CapabilityProviders {
		var out rpc.CapabilityProviders
		require.NoError(t, c.Call(t.Context(), "capability.providers", map[string]string{"name": "editor.open"}, &out))
		return out
	}

	beta := state("beta")
	assert.Equal(t, []any{plugin.StateDisabled, 0, false}, []any{beta.State, beta.PID, beta.Enabled})
	assert.Equal(t, []string{"alpha"}, sections())
	assert.ErrorIs(t, c.Call(t.Context(), "command.run", map[string]any{"id": "beta.say", "workspaceId": p.ID}, nil), rpc.ErrNotFound)
	assert.ErrorIs(t, c.Call(t.Context(), "plugin.restart", map[string]string{"id": "beta"}, nil), rpc.ErrPluginUnavailable)
	assert.ErrorIs(t, c.Call(t.Context(), "capability.set-default", map[string]string{"name": "editor.open", "plugin": "beta"}, nil), rpc.ErrPluginUnavailable)
	assert.Equal(t, rpc.CapabilityProviders{Default: "alpha", Providers: []rpc.CapabilityProvider{{Plugin: "alpha", Name: "alpha", Enabled: true}, {Plugin: "beta", Name: "beta"}}}, providers())

	require.NoError(t, c.Call(t.Context(), "plugin.set-enabled", map[string]any{"id": "beta", "enabled": true}, nil))
	testutil.Eventually(t, func() bool { return state("beta").State == plugin.StateRunning }, 5*time.Second)
	assert.Equal(t, []string{"alpha", "beta"}, sections())
	assert.Equal(t, "alpha", opener())
	require.NoError(t, c.Call(t.Context(), "capability.set-default", map[string]string{"name": "editor.open", "plugin": "beta"}, nil))
	assert.Equal(t, "beta", opener())
	assert.Equal(t, "beta", providers().Default)

	require.NoError(t, c.Call(t.Context(), "plugin.set-enabled", map[string]any{"id": "alpha", "enabled": false}, nil))
	alpha := state("alpha")
	assert.Equal(t, []any{plugin.StateDisabled, 0, false}, []any{alpha.State, alpha.PID, alpha.Enabled})
	assert.Equal(t, []string{"beta"}, sections())
	require.NoError(t, c.Call(t.Context(), "plugin.set-enabled", map[string]any{"id": "beta", "enabled": false}, nil))
	assert.ErrorIs(t, c.Call(t.Context(), "command.run", map[string]any{"id": "editor.open", "workspaceId": p.ID}, nil), rpc.ErrNotFound)

	manifests, errs := plugin.Discover([]string{root})
	require.Empty(t, errs)
	reloaded, err := plugin.NewControl(t.Context(), h.KV, manifests)
	require.NoError(t, err)
	assert.False(t, reloaded.StartEnabled(manifests[0]))
	assert.False(t, reloaded.StartEnabled(manifests[1]))
}

func TestControlMigratesVSCodeChoice(t *testing.T) {
	store, err := kv.Open(filepath.Join(t.TempDir(), "plugins.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	require.NoError(t, store.Set(ctx, "editor-nvim", "settings", json.RawMessage(`{"editor":"vscode","vscodeAccepted":true}`)))
	require.NoError(t, store.Set(ctx, "editor-nvim", "vscode.port", json.RawMessage(`41234`)))
	off := false
	vscode := plugin.Manifest{ID: "editor-vscode", EnabledByDefault: &off}

	control, err := plugin.NewControl(ctx, store, []plugin.Manifest{vscode})
	require.NoError(t, err)

	assert.True(t, control.StartEnabled(vscode))
	def, _, err := store.Get(ctx, "core", "capability.default.editor.open")
	require.NoError(t, err)
	assert.JSONEq(t, `"editor-vscode"`, string(def))
	accepted, _, err := store.Get(ctx, "editor-vscode", "settings")
	require.NoError(t, err)
	assert.JSONEq(t, `{"accepted":true}`, string(accepted))
	port, _, err := store.Get(ctx, "editor-vscode", "vscode.port")
	require.NoError(t, err)
	assert.JSONEq(t, `41234`, string(port))
}
