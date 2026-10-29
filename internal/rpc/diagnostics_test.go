package rpc_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/diagnostics"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
)

func TestDiagnosticsBundle(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	srv, _ := testutil.NewRPCServerIn(t, dirs)
	srv.SetPluginList(func() any {
		return []map[string]any{{"id": "files", "state": "running", "restarts": 2, "lastError": "boom"}}
	})
	l, err := net.Listen("unix", dirs.CoreSocket())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, l) }()
	c, err := rpc.Dial(ctx, dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() {
		c.Close()
		cancel()
		assert.NoError(t, <-done)
	})

	var coreLog strings.Builder
	for i := 0; coreLog.Len() <= 3*diagnostics.LogTailMax/2; i++ {
		fmt.Fprintf(&coreLog, "line %07d %s\n", i, strings.Repeat("x", 80))
	}
	require.NoError(t, os.WriteFile(dirs.CoreLog(), []byte(coreLog.String()), 0o600))
	require.NoError(t, os.WriteFile(dirs.PluginLog("files"), []byte("plugin says hi\n"), 0o600))
	var p project.Project
	require.NoError(t, c.Call(t.Context(), "project.create", map[string]any{"name": "demo", "repos": []string{t.TempDir()}}, &p))
	var s session.Session
	require.NoError(t, c.Call(t.Context(), "session.create", map[string]any{"projectId": p.ID, "command": []string{"sh", "-c", "echo secret-output; exec cat"}}, &s))

	require.Eventually(t, func() bool {
		var snap session.Snapshot
		return c.Call(t.Context(), "session.read", map[string]any{"id": s.ID}, &snap) == nil && strings.Contains(snap.Text, "secret-output")
	}, 5*time.Second, 20*time.Millisecond)

	var res diagnostics.Result
	require.NoError(t, c.Call(t.Context(), "diagnostics.collect", map[string]any{"extra": map[string][]byte{"settings.json": []byte(`{"a":1}`)}}, &res))
	assert.Equal(t, dirs.DiagnosticsDir(), filepath.Dir(res.Path))
	files := readZip(t, res.Path)
	for name, data := range files {
		assert.NotContains(t, data, "secret-output", name)
	}
	names := make([]string, 0, len(res.Files))
	for _, f := range res.Files {
		names = append(names, f.Name)
		assert.Equal(t, int64(len(files[f.Name])), f.Bytes, f.Name)
	}
	assert.ElementsMatch(t, []string{"info.json", "logs/core.log", "logs/plugins/files.log", "plugins.json", "workspaces.json", "sessions.json", "settings.json", "README.txt"}, names)

	tail := files["logs/core.log"]
	assert.LessOrEqual(t, len(tail), diagnostics.LogTailMax)
	assert.True(t, strings.HasPrefix(tail, "line "))
	assert.True(t, strings.HasSuffix(coreLog.String(), tail))
	assert.Equal(t, "plugin says hi\n", files["logs/plugins/files.log"])
	assert.Contains(t, files["plugins.json"], `"lastError": "boom"`)
	assert.Contains(t, files["workspaces.json"], p.ID)
	assert.Equal(t, `{"a":1}`, files["settings.json"])
	assert.Contains(t, files["README.txt"], "logs/core.log")

	var info map[string]any
	require.NoError(t, json.Unmarshal([]byte(files["info.json"]), &info))
	assert.NotEmpty(t, info["macOS"])
	assert.NotEmpty(t, info["collectedAt"])

	var sessions []map[string]any
	require.NoError(t, json.Unmarshal([]byte(files["sessions.json"]), &sessions))
	require.Len(t, sessions, 1)
	keys := make([]string, 0, len(sessions[0]))
	for k := range sessions[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	assert.Equal(t, []string{"created", "cwd", "exitCode", "id", "owner", "state", "title", "workspace"}, keys)
	assert.Equal(t, s.ID, sessions[0]["id"])

	var again diagnostics.Result
	require.NoError(t, c.Call(t.Context(), "diagnostics.collect", map[string]any{}, &again))
	assert.NotEqual(t, res.Path, again.Path)

	err = c.Call(t.Context(), "diagnostics.collect", map[string]any{"extra": map[string][]byte{"env.txt": []byte("x")}}, &res)
	var remote *rpc.RemoteError
	require.ErrorAs(t, err, &remote)
	assert.Contains(t, remote.Message, `unknown file "env.txt"`)
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)
		out[f.Name] = string(data)
	}
	return out
}
