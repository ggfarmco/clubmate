package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/paths"
	"clubmate/pkg/contract"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
)

func TestInstallRetriesAfterInterruptedNpm(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
echo run >> "` + calls + `"
dir="$3/node_modules/` + claudePackage + `"
mkdir -p "$dir"
printf '{"version":"` + claudeVersion + `","bin":{"` + claudeBin + `":"dist/index.js"}}' > "$dir/package.json"
exit "${FAKE_NPM_EXIT:-0}"
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prefix := filepath.Join(t.TempDir(), claudeDir)
	var notes []bool
	note := func(on bool) { notes = append(notes, on) }

	t.Setenv("FAKE_NPM_EXIT", "1")
	_, err := installClaude(context.Background(), prefix, os.Environ(), note)
	require.Error(t, err)

	t.Setenv("FAKE_NPM_EXIT", "0")
	got, err := installClaude(context.Background(), prefix, os.Environ(), note)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(prefix, "node_modules", filepath.FromSlash(claudePackage), "dist", "index.js"), got)

	_, err = installClaude(context.Background(), prefix, os.Environ(), note)
	require.NoError(t, err)
	runs, err := os.ReadFile(calls)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(runs), "run"))
	assert.Equal(t, []bool{true, false, true, false}, notes)
}

func TestAgentEnvDropsCoreAndPluginVars(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	defaults, err := paths.Defaults()
	require.NoError(t, err)
	custom := t.TempDir()
	t.Setenv("CLUBMATE_STATE_DIR", defaults.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", custom)
	for _, name := range []string{"CLUBMATE_PLUGIN_ID", "CLUBMATE_PLUGIN_STATE_DIR", "CLUBMATE_PLUGIN_RUNTIME_DIR", "CLUBMATE_FLAVOR", "CLUBMATE_RESOURCES"} {
		t.Setenv(name, "x")
	}

	env := agentEnv("prj_1")

	assert.Contains(t, env, "CLUBMATE_RUNTIME_DIR="+custom)
	assert.Contains(t, env, contract.WorkspaceEnv+"=prj_1")
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		assert.NotContains(t, []string{"CLUBMATE_STATE_DIR", "CLUBMATE_PLUGIN_ID", "CLUBMATE_PLUGIN_STATE_DIR", "CLUBMATE_PLUGIN_RUNTIME_DIR", "CLUBMATE_FLAVOR", "CLUBMATE_RESOURCES"}, name)
	}
}

func TestAgentEnvAppendsTools(t *testing.T) {
	res := t.TempDir()
	tools := filepath.Join(res, "tools")
	require.NoError(t, os.Mkdir(tools, 0o700))
	t.Setenv("CLUBMATE_RESOURCES", res)
	t.Setenv("PATH", "/a:"+tools+":/b")
	assert.Contains(t, agentEnv(""), "PATH=/a:/b:"+tools)
}

func TestChromeFollowsClaudeCodeDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	assert.False(t, chromeByDefault())
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"claudeInChromeDefaultEnabled":true}`), 0o600))
	assert.True(t, chromeByDefault())
}

func TestChromeOnlyForUnrestrictedSessions(t *testing.T) {
	assert.Equal(t, map[string]*string{"chrome": nil}, sessionMeta(record{ID: "a"}, "", selfParts{}, true, false).ClaudeCode.Options.ExtraArgs)
	assert.Nil(t, sessionMeta(record{ID: "a", SystemPrompt: "serve"}, "", selfParts{}, true, false).ClaudeCode.Options.ExtraArgs)
	assert.Nil(t, sessionMeta(record{ID: "a"}, "", selfParts{}, false, false).ClaudeCode.Options.ExtraArgs)
}

func TestChromeOnlyForSessionsTheUserStarted(t *testing.T) {
	a := &app{registry: newRegistry()}
	a.sessions["mine"] = newSession(record{ID: "mine"}, nil, true, nil)
	a.sessions["cron"] = newSession(record{ID: "cron", CronJob: "cj"}, nil, true, nil)

	assert.True(t, a.userStarted(record{ID: "c", lineage: lineage{ParentSession: "mine"}}))
	assert.False(t, a.userStarted(record{ID: "c", lineage: lineage{ParentSession: "cron"}}))
	assert.False(t, a.userStarted(record{ID: "c", lineage: lineage{ParentSession: "gone"}}))
	assert.False(t, a.userStarted(record{ID: "t", Trigger: "tr"}))
	assert.False(t, a.userStarted(record{ID: "s", StartedBy: "agt_x"}))
	assert.False(t, a.userStarted(record{ID: "g", Run: &api.Run{ID: "r"}}))
}

func TestLeanProfileForAgentStartedSessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"enabledPlugins":{"b@m":true,"off@m":false,"a@m":["x"]}}`), 0o600))
	assert.Equal(t, []string{"a@m", "b@m"}, userPlugins())

	for _, rec := range []record{{ID: "c", ParentSession: "p"}, {ID: "n", Run: &api.Run{ID: "r"}}, {ID: "g", GateFor: &gateFor{}}} {
		opts := sessionMeta(rec, "", selfParts{}, false, false).ClaudeCode.Options
		require.True(t, rec.lean(), rec.ID)
		leanOptions(&opts, userPlugins())
		assert.Equal(t, "1", opts.Env[advisorOffEnv], rec.ID)
		assert.Equal(t, &acp.ClaudeCodeSettings{EnabledPlugins: map[string]bool{"a@m": false, "b@m": false}, DisableClaudeAiConnectors: true}, opts.Settings, rec.ID)
		assert.True(t, opts.StrictMcpConfig, rec.ID)
	}

	byHand := sessionMeta(record{ID: "c", ParentSession: "p"}, "opus", selfParts{}, false, false).ClaudeCode.Options
	assert.Equal(t, "opus", byHand.Settings.AdvisorModel)
	assert.NotContains(t, byHand.Env, advisorOffEnv)

	for _, rec := range []record{{ID: "top"}, {ID: "aside", ParentSession: "p", Aside: &asideInfo{Kept: true}}, {ID: "voice", ParentSession: "p", SystemPrompt: "serve"}} {
		assert.False(t, rec.lean(), rec.ID)
		assert.NotContains(t, sessionMeta(rec, "", selfParts{}, false, false).ClaudeCode.Options.Env, advisorOffEnv, rec.ID)
	}
}

func TestLeanSessionGetsClubmateMCPWhenRegistered(t *testing.T) {
	own := []acp.McpServer{{Name: mcpName, Command: "clubmate"}}
	p := &agentProc{launch: launch{registered: true}, mcp: own}
	assert.Empty(t, p.mcpFor(record{ID: "top"}))
	assert.Equal(t, own, p.mcpFor(record{ID: "c", ParentSession: "p"}))
}
