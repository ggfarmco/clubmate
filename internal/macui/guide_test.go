package macui_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompactHeaderDrawsTheGuideStripAndItsKey(t *testing.T) {
	in := start(t, "macui-guide-strip", `{}`)
	in.focusSection("agents", "agents.sidebar")
	before := in.sessions()
	in.cm("command", "run", "agents.new", "prompt=Hello")
	id := in.added(before).ID
	in.focusIn(id)

	concept := func(id, title, status, parent string) map[string]any {
		return map[string]any{"id": id, "title": title, "status": status, "parent": parent}
	}
	ledger, err := json.Marshal(map[string]any{"current": "c2", "concepts": []map[string]any{
		concept("c1", "Limiter", "known", ""),
		concept("c2", "Bucket per workspace", "todo", "c1"),
	}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "tour.json")
	require.NoError(t, os.WriteFile(path, ledger, 0o600))
	args, err := json.Marshal(map[string]any{"title": "Bucket per workspace", "check": false, "concept": "c2", "ledger": path})
	require.NoError(t, err)
	in.runAs(id, "command", "run", "--agent", "agents.guide-step", "--args", string(args))

	strip := func(size string) bool { return in.snapshotAt(size).main().has(id + "/strip") }
	in.eventually("the compact header draws the strip while the tour pane is hidden", func() bool { return strip("900x700") })
	shown := func() bool {
		var views []struct {
			Tree json.RawMessage `json:"tree"`
		}
		in.json(&views, "ui", "dump", "--workspace="+in.workspace)
		for _, v := range views {
			if on, ok := stripShown(v.Tree, id+"/strip"); ok {
				return on
			}
		}
		return false
	}
	in.eventually("the plugin shows the tour pane", shown)
	assert.Contains(t, in.key("cmd+shift+m"), "monitor")
	in.eventually("cmd+shift+m from the header's strip hides the tour pane", func() bool { return !shown() })
}

func stripShown(raw json.RawMessage, id string) (bool, bool) {
	var n struct {
		ID       string            `json:"id"`
		Props    map[string]any    `json:"props"`
		Children []json.RawMessage `json:"children"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return false, false
	}
	if n.ID == id {
		on, _ := n.Props["shown"].(bool)
		return on, true
	}
	for _, c := range n.Children {
		if on, ok := stripShown(c, id); ok {
			return on, true
		}
	}
	return false, false
}
