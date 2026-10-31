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
	assert.Contains(t, env, projectEnv+"=prj_1")
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
