package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/cli"
	"clubmate/internal/project"
)

type posted struct {
	Workspace string `json:"workspace"`
	Session   string `json:"session"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Kind      string `json:"kind"`
}

func lastPosted(t *testing.T, b *bus.Bus) posted {
	t.Helper()
	events, err := b.Read(t.Context(), "notify.posted", 0, 100)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	var p posted
	require.NoError(t, json.Unmarshal(events[len(events)-1].Payload, &p))
	return p
}

func createWorkspace(t *testing.T) string {
	t.Helper()
	var p project.Project
	require.NoError(t, json.Unmarshal(run(t, "project", "create", "demo", "--json"), &p))
	return p.ID
}

func TestNotifyResolvesWorkspaceAndSession(t *testing.T) {
	b := startCore(t)
	here, there := createWorkspace(t), createWorkspace(t)
	sid := strings.TrimSpace(string(run(t, "session", "create", "--project", here, "--", "true")))
	t.Setenv("CLUBMATE_PROJECT_ID", here)
	t.Setenv("CLUBMATE_SESSION_ID", sid)

	run(t, "notify", "--kind", "done", "Build", "finished")
	assert.Equal(t, posted{Workspace: here, Session: sid, Message: "Build finished", Kind: "done"}, lastPosted(t, b))

	run(t, "notify", "--workspace", there, "--title", "CI", "red")
	assert.Equal(t, posted{Workspace: there, Title: "CI", Message: "red", Kind: "info"}, lastPosted(t, b))

	t.Setenv("CLUBMATE_PROJECT_ID", "")
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"notify", "hi"})
	assert.ErrorContains(t, root.Execute(), "no workspace: pass --workspace")
}

func TestNotifyReadsStdinAndTruncates(t *testing.T) {
	b := startCore(t)
	t.Setenv("CLUBMATE_PROJECT_ID", createWorkspace(t))
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetIn(strings.NewReader(strings.Repeat("я", 1500) + "\n"))
	root.SetArgs([]string{"notify", "--kind", "needs-input", "--title", strings.Repeat("t", 200), "-"})
	require.NoError(t, root.Execute(), stderr.String())
	got := lastPosted(t, b)
	assert.Len(t, []rune(got.Message), 1000)
	assert.Len(t, []rune(got.Title), 120)
	assert.True(t, strings.HasPrefix(got.Message, "яя"))
	assert.Equal(t, "needs-input", got.Kind)
}

func TestNotifyRejectsUnknownKind(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetArgs([]string{"notify", "--kind", "urgent", "hi"})
	assert.ErrorContains(t, root.Execute(), `unknown kind "urgent"`)
}
