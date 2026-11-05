package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/agents/acp"
)

func TestNightMetaIsConfined(t *testing.T) {
	m := sessionMeta(record{ID: "n", Night: nightSleep, SystemPrompt: "x", Allow: []string{"Edit(//d/**)"}}, "", selfParts{Prompt: "self"})
	o := m.ClaudeCode.Options
	assert.Equal(t, []string{"Read", "Grep", "Glob", "Edit", "Write"}, o.Tools)
	assert.Equal(t, 60, o.MaxTurns)
	assert.Equal(t, []string{"Edit(//d/**)"}, o.Settings.Permissions.Allow)
	assert.Equal(t, []string{}, o.SettingSources)
	assert.Equal(t, "x", m.SystemPrompt.Append)
}

func TestNightAllowCoversSandboxReadsAndDeclare(t *testing.T) {
	assert.Equal(t, []string{"Edit(//s/**)", "Read(//w/**)", "mcp__clubmate__agents_self_declare"}, nightAllow("/s", []string{"/w"}))
}

func TestNightOutcomeTakesRejectOnceOrCancels(t *testing.T) {
	options := []acp.PermissionOption{{OptionID: "yes", Kind: "allow_once"}, {OptionID: "never", Kind: "reject_always"}, {OptionID: "no", Kind: "reject_once"}}
	assert.Equal(t, acp.PermissionOutcome{Outcome: acp.OutcomeSelected, OptionID: "no"}, nightOutcome(options))
	assert.Equal(t, acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}, nightOutcome(options[:2]))
}

func TestNightSpecMustBeConfinable(t *testing.T) {
	good := nightSpec{Kind: nightSleep, Workspace: "w", Dir: "/s", System: "x", Reads: []string{"/w"}}
	for _, bad := range []func(*nightSpec){
		func(s *nightSpec) { s.Kind = "" },
		func(s *nightSpec) { s.Dir = "" },
		func(s *nightSpec) { s.Dir = "s" },
		func(s *nightSpec) { s.Reads = []string{""} },
		func(s *nightSpec) { s.System = "" },
	} {
		spec := good
		bad(&spec)
		_, err := (&app{}).spawnNight(spec)
		assert.ErrorIs(t, err, errNightSpec)
	}
}
