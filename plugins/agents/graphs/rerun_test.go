package graphs

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func rerun(t *testing.T, a *app, id string) (any, error) {
	t.Helper()
	return a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "agents.graph-rerun", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + id + `"}`)})
}

func TestGraphRerun(t *testing.T) {
	a := testApp(t, &fakeCore{})
	r := runWith(t, a, "review-loop", "fix the flaky test")
	_, err := rerun(t, a, r.ID)
	assert.ErrorContains(t, err, "the run is still running")
	_, err = a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "agents.graph-stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	out, err := rerun(t, a, r.ID)
	require.NoError(t, err)
	again := out.(*Run)
	assert.NotEqual(t, r.ID, again.ID)
	assert.Equal(t, []string{r.Input, r.Dir, r.Graph}, []string{again.Input, again.Dir, again.Graph})
}

func TestDefinitionWaitsTrustAfterARefusedRun(t *testing.T) {
	a := testApp(t, &fakeCore{})
	waits := func() bool {
		defs := (&Engine{a: a}).Definitions(t.Context(), "ws1")
		i := slices.IndexFunc(defs, func(d Definition) bool { return d.Name == "gate" })
		require.GreaterOrEqual(t, i, 0)
		assert.True(t, defs[i].Untrusted || !defs[i].WaitsTrust)
		return defs[i].WaitsTrust
	}
	assert.False(t, waits())
	assert.ErrorContains(t, agentRun(t, a, "gate"), "untrusted")
	assert.True(t, waits())
	require.NoError(t, trustGraph(t, a, "gate"))
	assert.False(t, waits())
}

func TestGraphRerunIsTheUsersOnly(t *testing.T) {
	a := testApp(t, &fakeCore{})
	r := runWith(t, a, "review-loop", "x")
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "agents.graph-stop", WorkspaceID: "ws1", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	require.NoError(t, err)
	_, err = a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "agents.graph-rerun", WorkspaceID: "ws1", Caller: "agt_boss", Args: json.RawMessage(`{"id":"` + r.ID + `"}`)})
	assert.Error(t, err)
}

func TestRunningIsUnknownUntilTheEngineStarted(t *testing.T) {
	assert.Nil(t, New(Deps{}).Running("ws1"))
}
