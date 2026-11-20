package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
)

const (
	agentsEcho  = `{"id":"agents","name":"Agents","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"agents.search","title":"Search","hidden":true}]},"echoDelays":{"agents.search":100},"echoResults":{"agents.search":[{"session":"agt_1","title":"Plan review","status":"running","worktree":"/r/.worktrees/plan-ui"},{"session":"agt_2","title":"","status":"done","worktree":""}]}}`
	backlogEcho = `{"id":"backlog","name":"Backlog","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"backlog.list","title":"List","hidden":true}]},"echoResults":{"backlog.list":{"tasks":[%s]}}}`
	wikiEcho    = `{"id":"wiki","name":"Wiki","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"wiki.search","title":"Search","hidden":true}]},"echoResults":{"wiki.search":{"hits":[{"page":"notes/plan.md","title":"Plan","snippet":"x"}]}}}`
	docsEcho    = `{"id":"docs","name":"Docs","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"docs.list","title":"List","hidden":true}]},"echoResults":{"docs.list":{"docs":[{"path":"/r/docs/PLAN.md","rel":"docs/PLAN.md"},{"path":"/r/README.md","rel":"README.md"},{"path":"/plan/notes.md","rel":"/plan/notes.md"}]}}}`
)

type searchItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Line   string `json:"line"`
	Target string `json:"target"`
}

type searchGroup struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Total int          `json:"total"`
	Items []searchItem `json:"items"`
}

type sourceError struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type workspaceResult struct {
	Groups []searchGroup `json:"groups"`
	Errors []sourceError `json:"errors"`
}

func TestWorkspaceSearchGroupsInOrder(t *testing.T) {
	folder := tempDir(t)
	wiki := tempDir(t)
	t.Setenv("CLUBMATE_WIKI_DIR", wiki)
	write(t, wiki, "proj/.wiki.json", fmt.Sprintf(`{"checkout":%q}`, folder))
	h, id := startWorkspace(t, folder, testutil.EchoPluginDirAs(t, agentsEcho, tasksEcho(1), wikiEcho, docsEcho))

	got := searchWorkspace(t, h, id, map[string]any{"query": "plan"})

	assert.Equal(t, workspaceResult{Groups: []searchGroup{
		{ID: "sessions", Title: "Sessions", Total: 2, Items: []searchItem{
			{Kind: "session", ID: "agt_1", Title: "Plan review", Line: "plan-ui · running", Target: "agt_1"},
			{Kind: "session", ID: "agt_2", Title: "agt_2", Line: "done", Target: "agt_2"},
		}},
		{ID: "tasks", Title: "Tasks", Total: 1, Items: []searchItem{{Kind: "task", ID: "1", Title: "Task 1", Line: "#1 · todo", Target: "1"}}},
		{ID: "wiki", Title: "Wiki pages", Total: 1, Items: []searchItem{{Kind: "wiki", ID: "proj/notes/plan.md", Title: "Plan", Line: "proj", Target: filepath.Join(wiki, "proj", "notes", "plan.md")}}},
		{ID: "docs", Title: "Docs", Total: 1, Items: []searchItem{{Kind: "doc", ID: "/r/docs/PLAN.md", Title: "PLAN.md", Line: "docs/PLAN.md", Target: "/r/docs/PLAN.md"}}},
	}}, got)
}

func TestWorkspaceSearchLimitsAndTotal(t *testing.T) {
	h, id := startWorkspace(t, tempDir(t), testutil.EchoPluginDirAs(t, tasksEcho(5)))

	got := searchWorkspace(t, h, id, map[string]any{"query": "task"})

	assert.Equal(t, []string{"1", "2", "3"}, taskIDs(t, got))
	assert.Equal(t, 5, got.Groups[0].Total)
	assert.Equal(t, []string{"1", "2", "3", "4"}, taskIDs(t, searchWorkspace(t, h, id, map[string]any{"query": "task", "limit": 4})))
}

func TestWorkspaceSearchOneGroupInFull(t *testing.T) {
	h, id := startWorkspace(t, tempDir(t), testutil.EchoPluginDirAs(t, tasksEcho(5)))

	got := searchWorkspace(t, h, id, map[string]any{"query": "task", "group": "tasks"})

	assert.Empty(t, got.Errors)
	assert.Equal(t, []string{"1", "2", "3", "4", "5"}, taskIDs(t, got))
}

func TestWorkspaceSearchNumberPinsTask(t *testing.T) {
	h, id := startWorkspace(t, tempDir(t), testutil.BuildPluginDir(t, "clubmate/plugins/backlog", "plugins/backlog/plugin.json"))
	testutil.WaitEvents(t, h.Bus, "backlog.ready", func(events []bus.Event) bool { return len(events) > 0 })
	for _, title := range []string{"Follow up on 2", "Search by number"} {
		require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "backlog.add", "workspaceId": id, "args": map[string]any{"title": title}}, nil))
	}

	assert.Equal(t, []string{"2", "1"}, taskIDs(t, searchWorkspace(t, h, id, map[string]any{"query": "2"})))
}

func TestWorkspaceSearchMissingSourceIsSkipped(t *testing.T) {
	failing := strings.Replace(docsEcho, `"echoResults"`, `"echoErrors":{"docs.list":"boom"},"echoResults"`, 1)
	h, id := startWorkspace(t, tempDir(t), testutil.EchoPluginDirAs(t, tasksEcho(1), failing))

	got := searchWorkspace(t, h, id, map[string]any{"query": "task"})

	assert.Equal(t, []string{"tasks"}, groupIDs(got))
	errs := map[string]string{}
	for _, e := range got.Errors {
		errs[e.ID] = e.Error
	}
	assert.ElementsMatch(t, []string{"sessions", "wiki", "docs"}, mapKeys(errs))
	assert.Contains(t, errs["docs"], "boom")
}

func TestWorkspaceSearchHomeSkipsWiki(t *testing.T) {
	h, _ := startWorkspace(t, tempDir(t))
	home, err := h.Svc.HomeWorkspace(t.Context())
	require.NoError(t, err)

	got := searchWorkspace(t, h, home.ID, map[string]any{"query": "plan"})

	assert.ElementsMatch(t, []string{"sessions", "tasks", "docs"}, errorIDs(got))
}

func startWorkspace(t *testing.T, folder string, pluginDirs ...string) (*testutil.PluginHarness, string) {
	t.Helper()
	dirs := append([]string{testutil.BuildPluginDir(t, "clubmate/plugins/search", "plugins/search/plugin.json")}, pluginDirs...)
	h := testutil.NewPluginHarness(t, dirs...)
	p, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{folder})
	require.NoError(t, err)
	ids := []string{"search"}
	for _, dir := range pluginDirs {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			ids = append(ids, e.Name())
		}
	}
	testutil.Eventually(t, func() bool {
		for _, id := range ids {
			if _, ok := h.Sup.Conn(id); !ok {
				return false
			}
		}
		return true
	}, 5*time.Second)
	return h, p.ID
}

func searchWorkspace(t *testing.T, h *testutil.PluginHarness, workspaceID string, args map[string]any) workspaceResult {
	t.Helper()
	var out workspaceResult
	require.NoError(t, h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "search.workspace", "workspaceId": workspaceID, "args": args}, &out))
	return out
}

func tasksEcho(n int) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"id":%d,"title":"Task %d","status":"todo"}`, i+1, i+1)
	}
	return fmt.Sprintf(backlogEcho, strings.Join(rows, ","))
}

func taskIDs(t *testing.T, r workspaceResult) []string {
	t.Helper()
	for _, g := range r.Groups {
		if g.ID != "tasks" {
			continue
		}
		ids := make([]string, 0, len(g.Items))
		for _, it := range g.Items {
			ids = append(ids, it.ID)
		}
		return ids
	}
	require.Fail(t, "no tasks group", "%+v", r)
	return nil
}

func groupIDs(r workspaceResult) []string {
	ids := make([]string, 0, len(r.Groups))
	for _, g := range r.Groups {
		ids = append(ids, g.ID)
	}
	return ids
}

func errorIDs(r workspaceResult) []string {
	ids := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		ids = append(ids, e.ID)
	}
	return ids
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
