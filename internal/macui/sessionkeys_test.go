package macui_test

import (
	"cmp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionKeys(t *testing.T) {
	in := start(t, "macui-session-keys", askScript)
	in.focusSection("agents", "agents.sidebar")
	before := in.sessions()
	in.cm("command", "run", "agents.new")
	plain := in.added(before)
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Ask two things")
	in.eventually("the asking session waits", func() bool {
		return slices.ContainsFunc(in.sessions(), func(s session) bool { return s.ID == spawned.Session && s.Status == "waiting" })
	})
	numbered := slices.DeleteFunc(in.sessions(), func(s session) bool { return !s.Numbered })
	slices.SortFunc(numbered, func(a, b session) int { return cmp.Compare(a.ListKey, b.ListKey) })
	require.NotEmpty(t, numbered)
	in.cm("command", "run", "agents.reveal", "session="+plain.ID)
	in.focusIn(plain.ID)

	assert.Equal(t, []string{"menu Next Session That Needs You"}, in.key("ctrl+cmd+j"))
	in.focusIn(spawned.Session)
	require.NotEqual(t, spawned.Session, numbered[0].ID)
	assert.Equal(t, []string{"menu Session 1"}, in.key("ctrl+cmd+1"))
	in.focusIn(numbered[0].ID)
	assert.Equal(t, []string{"monitor"}, in.key("ctrl+tab"))
	in.focusIn(spawned.Session)

	in.focusSection("terminals", "terminals.sidebar")
	terminals := in.terminals()
	in.key("cmd+n")
	in.eventually("a terminal is added", func() bool { return in.terminals() == terminals+1 })
	out, err := in.run("--json", "ui", "key", "ctrl+tab")
	require.Error(t, err)
	assert.Contains(t, string(out), "neither a key monitor nor an app command took")
}
