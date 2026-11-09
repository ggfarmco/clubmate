package macui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
