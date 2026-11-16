package macui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func TestSectionMenuNew(t *testing.T) {
	app := start(t, "macui-new", "")
	for _, tc := range []struct{ section, title string }{
		{"triggers", "New trigger"},
		{"cron", "New cron job"},
	} {
		t.Run(tc.section, func(t *testing.T) {
			in := app.at(t)
			in.focusSection(tc.section, tc.section+".sidebar")
			before := in.sessions()
			assert.Equal(t, []string{"menu New"}, in.key("cmd+n"))
			added := in.added(before)
			in.eventually("the session is titled "+tc.title, func() bool {
				for _, s := range in.sessions() {
					if s.ID == added.ID {
						return s.Title == tc.title
					}
				}
				return false
			})
		})
	}
}

func TestAgentsMenuKeys(t *testing.T) {
	app := start(t, "macui-agents", "")
	app.focusSection("agents", "agents.sidebar")

	t.Run("cmd+n from the sessions tree", func(t *testing.T) {
		in := app.at(t)
		before := in.sessions()
		assert.Equal(t, []string{"menu New"}, in.key("cmd+n"))
		in.focusIn(in.added(before).ID)
	})
	t.Run("cmd+n with the caret in another prompt", func(t *testing.T) {
		in := app.at(t)
		in.key("h", "i")
		before := in.sessions()
		in.key("cmd+n")
		in.focusIn(in.added(before).ID)
	})
	t.Run("cmd+n with Context open", func(t *testing.T) {
		in := app.at(t)
		routes := in.key("cmd+i")
		require.Len(t, routes, 1)
		assert.NotContains(t, routes[0], "responder")
		before := in.sessions()
		in.key("cmd+n")
		in.focusIn(in.added(before).ID)
	})
	t.Run("ctrl+cmd+down and up step sessions", func(t *testing.T) {
		in := app.at(t)
		first := focusedSession(in.snapshot().key())
		require.NotEmpty(t, first)
		assert.Equal(t, []string{"menu Next Session"}, in.key("ctrl+cmd+down"))
		in.eventually("another session's prompt has the caret", func() bool {
			next := focusedSession(in.snapshot().key())
			return next != "" && next != first
		})
		assert.Equal(t, []string{"menu Previous Session"}, in.key("ctrl+cmd+up"))
		in.focusIn(first)
	})
	t.Run("cmd+shift+i reaches its shifted menu item", func(t *testing.T) {
		in := app.at(t)
		assert.Equal(t, []string{"menu Notifications", "menu Notifications"}, in.key("cmd+shift+i", "cmd+shift+i"))
	})
	t.Run("cmd+n in Terminals opens a terminal", func(t *testing.T) {
		in := app.at(t)
		in.focusSection("terminals", "terminals.sidebar")
		before, sessions := in.terminals(), len(in.sessions())
		assert.Equal(t, []string{"menu New over terminal"}, in.key("cmd+n"))
		in.eventually("a terminal is added", func() bool { return in.terminals() == before+1 })
		assert.Len(t, in.sessions(), sessions)
	})
	t.Run("agents.new from Terminals lands in the new prompt", func(t *testing.T) {
		in := app.at(t)
		before := in.sessions()
		in.cm("command", "run", "agents.new")
		in.focusIn(in.added(before).ID)
	})
}

func TestCloseSessionAsksWhileTheTurnRuns(t *testing.T) {
	release := filepath.Join(root, "build", "mac-ui", "macui-close-busy-release")
	require.NoError(t, os.RemoveAll(release))
	in := start(t, "macui-close-busy", fmt.Sprintf(`{"prompt": [{"match": "Hold", "wait": %q}]}`, release))
	in.focusSection("agents", "agents.sidebar")
	before := in.sessions()
	in.cm("command", "run", "agents.new", "prompt=Hold")
	id := in.added(before).ID
	in.focusIn(id)
	statusOf := func() string {
		list := in.sessions()
		if i := slices.IndexFunc(list, func(s session) bool { return s.ID == id }); i >= 0 {
			return list[i].Status
		}
		return ""
	}
	status := func(want string) func() bool { return func() bool { return statusOf() == want } }
	in.eventually("the turn runs", status("running"))

	assert.Equal(t, []string{"menu Close Session"}, in.key("cmd+opt+delete"))
	in.eventually("the close alert is the key window", func() bool { return in.alert() != nil })
	assert.Equal(t, []string{"alert"}, in.key("esc"))
	in.eventually("esc keeps the session", func() bool { return in.alert() == nil })
	assert.Equal(t, []string{"menu Close Terminal"}, in.key("cmd+w"))
	testutil.Never(t, func() bool { return statusOf() != "running" }, testutil.Timeout(time.Second))

	in.cm("ui", "menu", "sessions#"+id, "Close Session", "--context")
	in.eventually("the row menu's close alert is the key window", func() bool { return in.alert() != nil })
	assert.Equal(t, []string{"alert"}, in.key("esc"))
	in.eventually("esc keeps the session", func() bool { return in.alert() == nil })
	testutil.Never(t, func() bool { return statusOf() != "running" }, testutil.Timeout(time.Second))

	require.NoError(t, os.WriteFile(release, nil, 0o600))
	in.eventually("the turn ends", status("done"))
	in.focusIn(id)
	in.key("cmd+opt+delete")
	in.eventually("an idle session closes without asking", status("closed"))
}

func TestCloseSessionAsksWhileATerminalRunsAProcess(t *testing.T) {
	in := start(t, "macui-close-terminal", "")
	in.focusSection("terminals", "terminals.sidebar")
	ids := func() []string {
		var list []struct {
			ID          string `json:"id"`
			Owner       string `json:"owner"`
			WorkspaceID string `json:"workspaceId"`
		}
		in.json(&list, "session", "list")
		var out []string
		for _, s := range list {
			if s.Owner == "terminals" && s.WorkspaceID == in.workspace {
				out = append(out, s.ID)
			}
		}
		return out
	}
	before := ids()
	in.key("cmd+n")
	var id string
	in.eventually("a terminal is added", func() bool {
		for _, s := range ids() {
			if !slices.Contains(before, s) {
				id = s
				return true
			}
		}
		return false
	})
	in.cm("session", "send", id, "sleep 600", "--enter")
	in.eventually("the terminal is busy", func() bool {
		var busy struct {
			Busy bool `json:"busy"`
		}
		in.json(&busy, "command", "run", "terminals.busy", "sessionId="+id)
		return busy.Busy
	})

	assert.Equal(t, []string{"menu Close Session over terminal"}, in.key("cmd+opt+delete"))
	in.eventually("the close alert is the key window", func() bool { return in.alert() != nil })
	assert.Equal(t, []string{"alert"}, in.key("esc"))
	in.eventually("esc keeps the terminal", func() bool { return in.alert() == nil })
	testutil.Never(t, func() bool { return !slices.Contains(ids(), id) }, testutil.Timeout(time.Second))
}
