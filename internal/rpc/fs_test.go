package rpc_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

type fsChanged struct {
	Watch     string   `json:"watch"`
	ProjectID string   `json:"projectId"`
	Paths     []string `json:"paths"`
	Overflow  bool     `json:"overflow"`
}

func watchRepo(t *testing.T) (*rpc.Client, string, string, string) {
	t.Helper()
	h := testutil.NewHarness(t)
	h.Start(t)
	repo := t.TempDir()
	p, err := h.Svc.CreateProject(t.Context(), "demo", []string{repo})
	require.NoError(t, err)
	c := h.Client(t)
	var w struct {
		Watch string `json:"watch"`
	}
	require.NoError(t, c.Call(t.Context(), "fs.watch", map[string]string{"projectId": p.ID}, &w))
	real, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	return c, p.ID, w.Watch, real
}

func awaitFS(t *testing.T, c *rpc.Client, cond func(fsChanged) bool) fsChanged {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case n := <-c.Notifications():
			if n.Method != "fs.changed" {
				continue
			}
			var ev fsChanged
			require.NoError(t, json.Unmarshal(n.Params, &ev))
			if cond(ev) {
				return ev
			}
		case <-deadline:
			require.FailNow(t, "no matching fs.changed")
		}
	}
}

func TestFSWatchNotifies(t *testing.T) {
	c, projectID, watch, repo := watchRepo(t)
	a := filepath.Join(repo, "a.txt")
	require.NoError(t, os.WriteFile(a, []byte("a"), 0o644))
	ev := awaitFS(t, c, func(ev fsChanged) bool { return slices.Contains(ev.Paths, a) })
	assert.Equal(t, watch, ev.Watch)
	assert.Equal(t, projectID, ev.ProjectID)
	assert.False(t, ev.Overflow)

	require.NoError(t, c.Call(t.Context(), "fs.unwatch", map[string]string{"watch": watch}, nil))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644))
	select {
	case n := <-c.Notifications():
		assert.Failf(t, "unexpected notification", "%s %s", n.Method, n.Params)
	case <-time.After(time.Second):
	}
}

func TestFSWatchOverflow(t *testing.T) {
	c, _, _, repo := watchRepo(t)
	for i := range 1500 {
		require.NoError(t, os.WriteFile(filepath.Join(repo, fmt.Sprintf("f%d.txt", i)), nil, 0o644))
	}
	ev := awaitFS(t, c, func(ev fsChanged) bool { return ev.Overflow })
	assert.Empty(t, ev.Paths)
}
