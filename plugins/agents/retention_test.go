package main_test

import (
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

func TestOldClosedSessionsAreDeletedWithTheirFiles(t *testing.T) {
	t.Parallel()
	const after = time.Second
	f := startEnv(t, []string{"CLUBMATE_AGENTS_DELETE_AFTER=" + after.String()}, map[string]any{"resume": true})
	workspace := f.workspace(t, newRepo(t))
	state := f.h.Dirs.PluginState("agents")
	old, held := f.newSession(t, workspace), f.newSession(t, workspace)
	file := map[string]any{"kind": "file", "name": "a.txt", "data": base64.StdEncoding.EncodeToString([]byte("hi"))}
	require.NoError(t, f.run(t, workspace, "agents.prompt", map[string]any{"session": old, "text": "see", "attachments": []map[string]any{file}}, nil))
	f.waitStatus(t, workspace, old, "done")
	require.DirExists(t, filepath.Join(state, "uploads", old))
	kid := f.newChild(t, workspace, held, map[string]any{}).Session
	f.close(t, workspace, held)
	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": kid}, nil))
	f.close(t, workspace, old)

	testutil.Eventually(t, func() bool { return f.session(t, workspace, old).ID == "" }, wait)
	assert.NoDirExists(t, filepath.Join(state, "uploads", old))
	assert.NoFileExists(t, filepath.Join(state, "sessions", old+".jsonl"))
	time.Sleep(2 * after)
	assert.Equal(t, "closed", f.session(t, workspace, held).Status)
}
