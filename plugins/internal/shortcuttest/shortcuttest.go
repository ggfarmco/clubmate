package shortcuttest

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/pkg/sdui"
)

func RequireDeclared(t testing.TB, views ...sdui.Node) {
	t.Helper()
	raw, err := os.ReadFile(plugin.ManifestFile)
	require.NoError(t, err)
	var m plugin.Manifest
	require.NoError(t, json.Unmarshal(raw, &m))
	declared := make([]string, 0, len(m.Contributes.Shortcuts))
	for _, s := range m.Contributes.Shortcuts {
		declared = append(declared, s.Keys)
	}
	raw, err = json.Marshal(views)
	require.NoError(t, err)
	var tree any
	require.NoError(t, json.Unmarshal(raw, &tree))
	used := map[string]bool{}
	collect(tree, used)
	require.NotEmpty(t, used, "views use no shortcuts")
	for keys := range used {
		assert.True(t, slices.Contains(declared, keys), "shortcut %q is used by a view but not declared in %s", keys, plugin.ManifestFile)
	}
}

func collect(v any, used map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if s, ok := child.(string); ok && k == "shortcut" {
				used[s] = true
				continue
			}
			collect(child, used)
		}
	case []any:
		for _, child := range v {
			collect(child, used)
		}
	}
}
