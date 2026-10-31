package sessionhost_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/sessionhost"
)

const oldMeta = `{"id":"ses_1","projectId":"prj_1","title":"t","owner":"terminals","command":["zsh","-l"],"cwd":"/w","hostPid":10,"childPid":11,"pgid":11,"hostStart":"2026-10-31T10:00:00Z","startedAt":"2026-10-31T10:00:01Z","exitCode":3,"exitedAt":"2026-10-31T10:05:00Z","proto":1}`

func TestMetaFileFormatIsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ses_1.json")
	require.NoError(t, os.WriteFile(path, []byte(oldMeta), 0o600))
	m, err := sessionhost.ReadMeta(path)
	require.NoError(t, err)
	code, exitedAt := 3, time.Date(2026, 10, 31, 10, 5, 0, 0, time.UTC)
	assert.Equal(t, sessionhost.Meta{
		ID:        "ses_1",
		ProjectID: "prj_1",
		Title:     "t",
		Owner:     "terminals",
		Command:   []string{"zsh", "-l"},
		Cwd:       "/w",
		HostPID:   10,
		ChildPID:  11,
		PGID:      11,
		HostStart: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC),
		StartedAt: time.Date(2026, 10, 31, 10, 0, 1, 0, time.UTC),
		ExitCode:  &code,
		ExitedAt:  &exitedAt,
		Proto:     1,
	}, m)
	require.NoError(t, sessionhost.WriteMeta(path, m))
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	var got, want map[string]any
	require.NoError(t, json.Unmarshal(written, &got))
	require.NoError(t, json.Unmarshal([]byte(oldMeta), &want))
	assert.Equal(t, want, got)
}
