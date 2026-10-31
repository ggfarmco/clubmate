package rpc_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/testutil"
)

func TestBroadcastRunsEveryHandler(t *testing.T) {
	h, p := startHandlers(t, "")
	args := map[string]string{"root": "/r"}

	require.NoError(t, h.Client(t).Call(t.Context(), "command.broadcast", map[string]any{"topic": "x.removing", "projectId": p.ID, "args": args}, nil))

	events, err := h.Bus.Read(t.Context(), "echo.command", 0, 100)
	require.NoError(t, err)
	var ran []string
	for _, ev := range events {
		var call struct {
			ID   string            `json:"id"`
			Args map[string]string `json:"args"`
		}
		require.NoError(t, json.Unmarshal(ev.Payload, &call))
		assert.Equal(t, args, call.Args)
		ran = append(ran, call.ID)
	}
	slices.Sort(ran)
	assert.Equal(t, []string{"alpha.on-removing", "beta.on-removing"}, ran)
}

func TestBroadcastRefusalCarriesHandlerMessage(t *testing.T) {
	h, p := startHandlers(t, `"echoErrors":{"beta.on-removing":"unsaved buffers: a.go"},`)

	err := h.Client(t).Call(t.Context(), "command.broadcast", map[string]any{"topic": "x.removing", "projectId": p.ID}, nil)

	require.ErrorIs(t, err, rpc.ErrPluginError)
	assert.EqualError(t, err, "unsaved buffers: a.go")
}

func startHandlers(t *testing.T, betaCanned string) (*testutil.PluginHarness, project.Project) {
	t.Helper()
	h := testutil.NewHarness(t)
	p, err := h.Svc.CreateProject(t.Context(), "demo", nil)
	require.NoError(t, err)
	handler := func(id string) string {
		return fmt.Sprintf(`{"id":"%s.on-removing","title":"On","hidden":true,"handles":["x.removing"]},{"id":"%s.other","title":"Other","hidden":true}`, id, id)
	}
	alpha := `{"id":"alpha","name":"alpha","version":"0.1.0","protocol":1,"command":["echo"],"echoResults":{"alpha.on-removing":{}},"contributes":{"commands":[` + handler("alpha") + `]}}`
	beta := `{"id":"beta","name":"beta","version":"0.1.0","protocol":1,"command":["echo"],` + betaCanned + `"echoResults":{"beta.on-removing":{}},"contributes":{"commands":[` + handler("beta") + `]}}`
	h.Start(t, testutil.EchoPluginDirAs(t, alpha), testutil.EchoPluginDirAs(t, beta))
	testutil.Eventually(t, func() bool {
		return !slices.ContainsFunc(h.Sup.Statuses(), func(s plugin.Status) bool { return s.State != "running" })
	}, 5*time.Second)
	return h, p
}
