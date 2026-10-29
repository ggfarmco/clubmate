package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/cli"
	"clubmate/internal/testutil"
)

type echoed struct {
	Plugin string         `json:"plugin"`
	Args   map[string]any `json:"args"`
}

func startEcho(t *testing.T, command string) {
	t.Helper()
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	h.Start(t, testutil.EchoPluginDirAs(t, fmt.Sprintf(`{"id":"alpha","name":"alpha","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[%s]}}`, command)))
	testutil.Eventually(t, func() bool { return h.Sup.Statuses()[0].State == "running" }, 5*time.Second)
	h.Client(t)
	t.Setenv("CLUBMATE_STATE_DIR", h.Dirs.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", h.Dirs.Runtime)
	t.Setenv("CLUBMATE_PROJECT_ID", p.ID)
}

func TestCLIOpenUsesCapability(t *testing.T) {
	startEcho(t, `{"id":"alpha.say","title":"Open","provides":["editor.open"],"args":{"properties":{"path":{"type":"string"},"line":{"type":"integer"}},"required":["path"]}}`)
	var out echoed
	require.NoError(t, json.Unmarshal(run(t, "open", "/tmp/x.go:3"), &out))
	assert.Equal(t, echoed{Plugin: "alpha", Args: map[string]any{"path": "/tmp/x.go", "line": 3.0}}, out)
}

func TestCommandRunCoercesKeyValues(t *testing.T) {
	startEcho(t, `{"id":"alpha.say","title":"Say","args":{"properties":{"n":{"type":"integer"},"ok":{"type":"boolean"},"tags":{"type":"array","items":{"type":"string"}},"s":{"type":"string"}}}}`)
	var out echoed
	require.NoError(t, json.Unmarshal(run(t, "command", "run", "alpha.say", "n=3", "ok=true", "tags=a,b", "s=x=y"), &out))
	assert.Equal(t, map[string]any{"n": 3.0, "ok": true, "tags": []any{"a", "b"}, "s": "x=y"}, out.Args)
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"command", "run", "alpha.say", "zz=1"})
	assert.ErrorContains(t, root.Execute(), `unknown arg "zz"`)
}
