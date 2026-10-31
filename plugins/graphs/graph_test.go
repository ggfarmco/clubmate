package main

import (
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
	assert.Equal(t, []graphFile{{Name: "review-loop", Title: "Review loop", Description: g.Description, Path: "testdata/.clubmate/graphs/review-loop.json", Nodes: 2}}, listGraphs([]string{"testdata"}))
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
