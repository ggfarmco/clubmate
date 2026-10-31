package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentSubcommandsRunAgentsCommands(t *testing.T) {
	data, err := os.ReadFile("../../plugins/agents/plugin.json")
	require.NoError(t, err)
	var real struct {
		Contributes struct {
			Commands []json.RawMessage `json:"commands"`
		} `json:"contributes"`
	}
	require.NoError(t, json.Unmarshal(data, &real))
	manifest, err := json.Marshal(map[string]any{"id": "agents", "name": "agents", "version": "0.1.0", "protocol": 1, "command": []string{"echo"}, "echoAll": true, "contributes": map[string]any{"commands": real.Contributes.Commands}})
	require.NoError(t, err)
	startEchoPlugin(t, string(manifest))
	t.Setenv("CLUBMATE_AGENT_SESSION", "agt_parent")
	cwd, err := os.Getwd()
	require.NoError(t, err)

	tests := map[string]struct {
		args []string
		want map[string]any
	}{
		"spawn":        {[]string{"spawn", "fix it", "--branch", "fix/x", "--title", "Fix"}, map[string]any{"prompt": "fix it", "branch": "fix/x", "title": "Fix"}},
		"tell":         {[]string{"tell", "agt_1", "go on"}, map[string]any{"session": "agt_1", "text": "go on"}},
		"wait":         {[]string{"wait", "agt_1", "agt_2", "--timeout", "30"}, map[string]any{"sessions": []any{"agt_1", "agt_2"}, "timeout": 30.0}},
		"output":       {[]string{"output", "agt_1", "--from-turn", "2"}, map[string]any{"session": "agt_1", "fromTurn": 2.0}},
		"sessions":     {[]string{"sessions"}, map[string]any{}},
		"cancel":       {[]string{"cancel", "agt_1"}, map[string]any{"session": "agt_1"}},
		"blame file":   {[]string{"blame", "agent.go", "-L", "2,5"}, map[string]any{"cwd": cwd, "path": filepath.Join(cwd, "agent.go"), "startLine": 2.0, "endLine": 5.0}},
		"blame commit": {[]string{"blame", "HEAD~1"}, map[string]any{"cwd": cwd, "commit": "HEAD~1"}},
	}
	for name, tc := range tests {
		var out struct {
			Args   map[string]any `json:"args"`
			Caller string         `json:"caller"`
		}
		require.NoError(t, json.Unmarshal(run(t, append([]string{"agent"}, tc.args...)...), &out), name)
		assert.Equal(t, tc.want, out.Args, name)
		assert.Equal(t, "agt_parent", out.Caller, name)
	}
}
