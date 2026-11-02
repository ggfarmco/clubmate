package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadGraph(t *testing.T) {
	g, _, err := loadGraph([]string{t.TempDir(), "testdata"}, "review-loop")
	require.NoError(t, err)
	assert.Equal(t, "review-loop", g.Name)
	assert.Len(t, g.Nodes, 2)
	assert.Equal(t, onDone, g.Edges[0].On)

	_, builtin, err := loadGraph([]string{t.TempDir()}, "pr-walkthrough")
	require.NoError(t, err)
	assert.True(t, builtin)

	files := listGraphs([]string{"testdata"})
	require.Len(t, files, 2)
	assert.Equal(t, []string{"pr-walkthrough", "review-loop"}, []string{files[0].Name, files[1].Name})
	assert.Equal(t, graphFile{Name: "pr-walkthrough", Title: "PR walkthrough", Description: files[0].Description, Path: "builtin/pr-walkthrough.json", Nodes: files[0].Nodes, Builtin: true}, files[0])
	assert.Equal(t, graphFile{Name: "review-loop", Title: "Review loop", Description: g.Description, Path: "testdata/.clubmate/graphs/review-loop.json", Nodes: 2}, files[1])
}

func TestProjectGraphOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, graphsDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, graphsDir, "pr-walkthrough.json"), []byte(`{"title":"Mine","nodes":[{"id":"a","prompt":"p","start":true}]}`), 0o644))

	g, builtin, err := loadGraph([]string{dir}, "pr-walkthrough")
	require.NoError(t, err)
	assert.Equal(t, "Mine", g.Title)
	assert.False(t, builtin)

	files := listGraphs([]string{dir})
	require.Len(t, files, 1)
	assert.Equal(t, "Mine", files[0].Title)
	assert.False(t, files[0].Builtin)
}

func TestBuiltinGraphsParse(t *testing.T) {
	entries, err := builtinGraphs.ReadDir(builtinDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		data, err := builtinGraphs.ReadFile(builtinPath(strings.TrimSuffix(e.Name(), graphExt)))
		require.NoError(t, err)
		_, err = parseGraph(strings.TrimSuffix(e.Name(), graphExt), data)
		assert.NoError(t, err, e.Name())
	}
}

func TestParseGraphRejects(t *testing.T) {
	tests := []struct {
		name  string
		graph string
		want  string
	}{
		{"unknown field", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edge":[]}`, `unknown field "edge"`},
		{"no start node", `{"nodes":[{"id":"a","prompt":"p"}]}`, "start: true"},
		{"unknown edge target", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[{"from":"a","to":"b"}]}`, `edges[0].to: no node "b"`},
		{"undeclared outcome", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[{"from":"a","to":"a","on":"outcome","outcome":"ok"}]}`, "not among node a's outcomes"},
		{"end that an edge fires on", `{"nodes":[{"id":"a","prompt":"p","start":true,"outcomes":["ok"],"end":["ok"]}],"edges":[{"from":"a","to":"a","on":"outcome","outcome":"ok"}]}`, "ends the run"},
		{"trigger placeholder in a prompt", `{"nodes":[{"id":"a","prompt":"{{payload.x}}","start":true}]}`, "{{payload.x}}"},
		{"placeholder of an unknown node", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[{"from":"a","to":"a","text":"{{node.b}}"}]}`, "{{node.b}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGraph("g", []byte(tt.graph))
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
