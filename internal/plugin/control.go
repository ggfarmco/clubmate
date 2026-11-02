package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"clubmate/internal/kv"
	"clubmate/internal/rpc"
)

const (
	prefsNamespace   = "core"
	enabledPrefix    = "plugin.enabled."
	defaultPrefix    = "capability.default."
	editorMigrated   = "migrated.editor-vscode"
	editorCapability = "editor.open"
	nvimPlugin       = "editor-nvim"
	vscodePlugin     = "editor-vscode"
	settingsKey      = "settings"
	vscodePortKey    = "vscode.port"
)

type Control struct {
	sup    *Supervisor
	router *Router
	store  *kv.Store

	enabled map[string]bool
}

func NewControl(ctx context.Context, store *kv.Store, manifests []Manifest) (*Control, error) {
	c := &Control{store: store, enabled: map[string]bool{}}
	if err := c.migrateEditor(ctx); err != nil {
		return nil, err
	}
	for _, m := range manifests {
		on, err := c.loadEnabled(ctx, m)
		if err != nil {
			return nil, err
		}
		c.enabled[m.ID] = on
	}
	return c, nil
}

func (c *Control) Attach(sup *Supervisor, router *Router) error {
	c.sup, c.router = sup, router
	keys, err := c.store.List(context.Background(), prefsNamespace, defaultPrefix)
	if err != nil {
		return fmt.Errorf("load capability defaults: %w", err)
	}
	for _, key := range keys {
		var id string
		if _, err := c.get(context.Background(), prefsNamespace, key, &id); err != nil {
			return fmt.Errorf("load capability defaults: %w", err)
		}
		router.SetDefault(strings.TrimPrefix(key, defaultPrefix), id)
	}
	return nil
}

func (c *Control) StartEnabled(m Manifest) bool {
	return c.enabled[m.ID]
}

func (c *Control) Enabled(id string) bool {
	return slices.ContainsFunc(c.sup.Statuses(), func(st Status) bool { return st.ID == id && st.Enabled })
}

func (c *Control) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if !slices.ContainsFunc(c.sup.Statuses(), func(st Status) bool { return st.ID == id }) {
		return fmt.Errorf("%w: %s", ErrUnknownPlugin, id)
	}
	if err := c.set(ctx, prefsNamespace, enabledPrefix+id, enabled); err != nil {
		return err
	}
	return c.sup.SetEnabled(ctx, id, enabled)
}

func (c *Control) Providers(capability string) rpc.CapabilityProviders {
	return c.router.Providers(capability)
}

func (c *Control) SetDefault(ctx context.Context, capability, pluginID string) error {
	i := slices.IndexFunc(c.router.Providers(capability).Providers, func(p rpc.CapabilityProvider) bool { return p.Plugin == pluginID })
	switch {
	case i < 0:
		return fmt.Errorf("%w: %s does not provide %s", rpc.ErrInvalidParams, pluginID, capability)
	case !c.Enabled(pluginID):
		return fmt.Errorf("%w: %s", ErrPluginDisabled, pluginID)
	}
	if err := c.set(ctx, prefsNamespace, defaultPrefix+capability, pluginID); err != nil {
		return err
	}
	c.router.SetDefault(capability, pluginID)
	c.sup.changed(pluginID, c.Running(pluginID))
	return nil
}

func (c *Control) Running(id string) bool {
	return slices.ContainsFunc(c.sup.Statuses(), func(st Status) bool { return st.ID == id && st.State == StateRunning })
}

func (c *Control) loadEnabled(ctx context.Context, m Manifest) (bool, error) {
	on := m.DefaultEnabled()
	if _, err := c.get(ctx, prefsNamespace, enabledPrefix+m.ID, &on); err != nil {
		return false, fmt.Errorf("load enabled %s: %w", m.ID, err)
	}
	return on, nil
}

func (c *Control) migrateEditor(ctx context.Context) error {
	var done bool
	if _, err := c.get(ctx, prefsNamespace, editorMigrated, &done); err != nil || done {
		return err
	}
	var old struct {
		Editor         string `json:"editor"`
		VSCodeAccepted bool   `json:"vscodeAccepted"`
	}
	if _, err := c.get(ctx, nvimPlugin, settingsKey, &old); err != nil {
		return fmt.Errorf("migrate editor: %w", err)
	}
	if old.Editor == "vscode" {
		if err := c.set(ctx, prefsNamespace, enabledPrefix+vscodePlugin, true); err != nil {
			return err
		}
		if err := c.set(ctx, prefsNamespace, defaultPrefix+editorCapability, vscodePlugin); err != nil {
			return err
		}
	}
	if old.VSCodeAccepted {
		if err := c.copyMissing(ctx, vscodePlugin, settingsKey, json.RawMessage(`{"accepted":true}`)); err != nil {
			return err
		}
	}
	port, ok, err := c.store.Get(ctx, nvimPlugin, vscodePortKey)
	if err != nil {
		return err
	}
	if ok {
		if err := c.copyMissing(ctx, vscodePlugin, vscodePortKey, port); err != nil {
			return err
		}
	}
	return c.set(ctx, prefsNamespace, editorMigrated, true)
}

func (c *Control) copyMissing(ctx context.Context, namespace, key string, value json.RawMessage) error {
	_, exists, err := c.store.Get(ctx, namespace, key)
	if err != nil || exists {
		return err
	}
	return c.store.Set(ctx, namespace, key, value)
}

func (c *Control) get(ctx context.Context, namespace, key string, v any) (bool, error) {
	raw, ok, err := c.store.Get(ctx, namespace, key)
	if err != nil || !ok {
		return false, err
	}
	return true, json.Unmarshal(raw, v)
}

func (c *Control) set(ctx context.Context, namespace, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, namespace, key, raw)
}
