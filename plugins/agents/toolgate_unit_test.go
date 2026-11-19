package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/internal/toolgate"
)

func TestToolGateNames(t *testing.T) {
	t.Parallel()
	gates := []gateConfig{
		{Tool: "Bash", How: toolgate.HowCommand, Text: "git push"},
		{Tool: "Bash", How: toolgate.HowStarts, Text: "make deploy"},
		{Tool: "Bash", How: toolgate.HowContains, Text: "--force"},
		{Tool: "Write", How: toolgate.HowRegex, Text: `\.env$`},
		{Tool: "mcp__clubmate__git_land"},
	}

	var got [][2]string
	for _, g := range gates {
		got = append(got, [2]string{toolGateName(g), toolMatchWords(g)})
	}

	assert.Equal(t, [][2]string{
		{"Tool call · git push", "Bash · a git push command"},
		{"Tool call · make deploy", "Bash · a command that starts with make deploy"},
		{"Tool call · --force", "Bash · the command contains --force"},
		{`Tool call · /\.env$/`, `Write · the input matches /\.env$/`},
		{"Tool call · mcp__clubmate__git_land", "mcp__clubmate__git_land · any call"},
	}, got)
	assert.Equal(t, []string{
		"",
		"git push · inside sh -c, held to be safe",
		"git push · inside bash <<'EOF', held to be safe",
		"git push · inside sh, held to be safe",
		"git push · in a command Clubmate can't parse, held to be safe",
	}, []string{
		toolHeldWhy(gates[0], ""),
		toolHeldWhy(gates[0], "sh -c"),
		toolHeldWhy(gates[0], "bash <<'EOF'"),
		toolHeldWhy(gates[0], "sh"),
		toolHeldWhy(gates[0], toolgate.ViaUnread),
	})
}

func TestToolRulesAreTheRepoActiveToolGatesOldestFirst(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	gates := []gateConfig{
		{ID: "new", Repo: "/r", Point: gatePointTool, Tool: "Bash", How: toolgate.HowCommand, Text: "gh pr create", CreatedAt: at.Add(time.Hour)},
		{ID: "land", Repo: "/r", Point: gatePointLand, Into: gateIntoAny, CreatedAt: at},
		{ID: "paused", Repo: "/r", Point: gatePointTool, Tool: "Write", Paused: true, CreatedAt: at},
		{ID: "other", Repo: "/o", Point: gatePointTool, Tool: "Write", CreatedAt: at},
		{ID: "old", Repo: "/r", Point: gatePointTool, Tool: "Bash", How: toolgate.HowCommand, Text: "git push", CreatedAt: at},
	}

	assert.Equal(t, []toolgate.Rule{
		{ID: "old", Tool: "Bash", How: toolgate.HowCommand, Text: "git push"},
		{ID: "new", Tool: "Bash", How: toolgate.HowCommand, Text: "gh pr create"},
	}, toolRulesOf(gates, "/r"))
}
