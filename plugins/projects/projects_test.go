package main_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func listItems(n sdui.Node) []sdui.Item {
	for _, c := range n.Children {
		if c.Type != "list" {
			continue
		}
		var items []sdui.Item
		for _, raw := range c.Props["items"].([]any) {
			m := raw.(map[string]any)
			items = append(items, sdui.Item{ID: m["id"].(string), Label: m["label"].(string)})
		}
		return items
	}
	return nil
}

func formID(n sdui.Node) string {
	for _, c := range n.Children {
		if c.Type == "form" {
			return c.ID
		}
	}
	return ""
}

func opens(c *rpc.Client) int {
	n := 0
	for {
		select {
		case note := <-c.Notifications():
			if note.Method == "project.open" {
				n++
			}
		default:
			return n
		}
	}
}

func TestCreateAndDeleteProject(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/projects", "plugins/projects/plugin.json"))
	repo := filepath.Join(t.TempDir(), "alpha")
	require.NoError(t, os.Mkdir(repo, 0o755))
	initial := h.WaitSlot(t, "", "projects.main", func(n sdui.Node) bool { return n.Type == "stack" })

	h.Event(t, "", "projects.main", "create", "create", map[string]any{"name": "", "repos": []string{repo}})
	projects, err := h.Svc.ListProjects(t.Context())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "alpha", projects[0].Name)
	id := projects[0].ID
	h.WaitSlot(t, "", "projects.main", func(n sdui.Node) bool {
		items := listItems(n)
		return len(items) == 1 && items[0].ID == id && items[0].Label == "alpha" && formID(n) != formID(initial)
	})

	require.NoError(t, h.Client(t).Call(t.Context(), "client.register", map[string][]string{"handles": {"project.open"}}, nil))
	h.Event(t, "", "projects.main", "projects", "select", id)
	assert.Zero(t, opens(h.Client(t)))
	h.Event(t, "", "projects.main", "open", "open", nil)
	assert.Equal(t, 1, opens(h.Client(t)))
	h.Event(t, "", "projects.main", "delete", "delete", nil)
	h.WaitSlot(t, "", "projects.main", func(n sdui.Node) bool { return len(listItems(n)) == 0 })
	projects, err = h.Svc.ListProjects(t.Context())
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestDeleteReturnsImmediately(t *testing.T) {
	h := testutil.NewPluginHarness(t, testutil.BuildPluginDir(t, "clubmate/plugins/projects", "plugins/projects/plugin.json"))
	p := h.CreateProject(t, "doomed")
	h.WaitSlot(t, "", "projects.main", func(n sdui.Node) bool { return len(listItems(n)) == 1 })
	h.Event(t, "", "projects.main", "projects", "select", p.ID)
	start := time.Now()
	h.Event(t, "", "projects.main", "delete", "delete", nil)
	assert.Less(t, time.Since(start), time.Second)
	testutil.Eventually(t, func() bool {
		projects, err := h.Svc.ListProjects(t.Context())
		return err == nil && len(projects) == 0
	}, 10*time.Second)
}
