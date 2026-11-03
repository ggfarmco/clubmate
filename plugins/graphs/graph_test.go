package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	require.Len(t, files, 6)
	assert.Equal(t, []string{"gate", "nightly", "pr-walkthrough", "red-team", "review-gate", "review-loop"}, []string{files[0].Name, files[1].Name, files[2].Name, files[3].Name, files[4].Name, files[5].Name})
	assert.Equal(t, graphFile{Name: "pr-walkthrough", Title: "PR walkthrough", Description: files[2].Description, Path: "builtin/pr-walkthrough.json", Nodes: files[2].Nodes, Builtin: true}, files[2])
	assert.Equal(t, graphFile{Name: "review-loop", Title: "Review loop", Description: g.Description, Path: "testdata/.clubmate/graphs/review-loop.json", Nodes: 2}, files[5])
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
	require.Len(t, files, 3)
	assert.Equal(t, "Mine", files[1].Title)
	assert.False(t, files[1].Builtin)
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

func TestStepGraphParses(t *testing.T) {
	g, _, err := loadGraph([]string{"testdata"}, "gate")
	require.NoError(t, err)
	tests := g.node("tests")
	assert.True(t, tests.step())
	assert.False(t, g.node("coder").step())
	assert.Equal(t, []string{"pass", "fail", "flaky"}, tests.outcomeWords())
	assert.True(t, g.check("tests"))
	assert.False(t, g.check("merge"))
	assert.True(t, g.owned(g.Edges[0]))
	assert.True(t, g.owned(g.Edges[4]))
	assert.False(t, g.owned(g.Edges[5]))
	assert.Equal(t, 10*time.Minute, tests.timeout())
	assert.Equal(t, 50, g.maxSteps())
	assert.Equal(t, "CODE_REVIEW", envName("code-review"))
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
		{"step with a prompt", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","prompt":"p"}],"edges":[]}`, "one of prompt, run or command"},
		{"run and command", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","command":"notify.post"}],"edges":[]}`, "one of prompt, run or command"},
		{"start step", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","start":true}],"edges":[]}`, "a step can't be a start node"},
		{"step outcomes", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","outcomes":["ok"]}],"edges":[]}`, "use exits"},
		{"exit code 0", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","exits":{"0":"ok"}}],"edges":[]}`, "exit code 1–255"},
		{"exit code word", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","exits":{"abc":"ok"}}],"edges":[]}`, "exit code 1–255"},
		{"exits word pass", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","exits":{"3":"pass"}}],"edges":[]}`, "pass and fail are built in"},
		{"timeout too long", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","timeout":9000}],"edges":[]}`, "timeout: between 1 and 7200"},
		{"maxSteps too high", `{"maxSteps":900,"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[]}`, "maxSteps: between 1 and 500"},
		{"maxFires too high", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[{"from":"a","to":"a","maxFires":51}]}`, "maxFires: between 1 and 50"},
		{"asks from a step", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x"}],"edges":[{"from":"s","to":"a","on":"asks"}]}`, "a step doesn't ask"},
		{"text into a step", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x"}],"edges":[{"from":"a","to":"s","text":"hi"}]}`, "a step takes no message"},
		{"output from an agent", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[{"from":"a","to":"a","text":"{{payload.output}}"}]}`, "payload.output is only in edges from a step"},
		{"reply from a step", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x"}],"edges":[{"from":"s","to":"a","text":"{{payload.reply}}"}]}`, "payload.reply is only in edges from an agent"},
		{"in a step", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x"},{"id":"t","run":"y","in":"s"}],"edges":[]}`, "in: main or an agent node id"},
		{"in unknown", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","in":"zz"}],"edges":[]}`, "in: main or an agent node id"},
		{"args without command", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"x","args":{"a":"b"}}],"edges":[]}`, "args: only with command"},
		{"placeholder in run", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","run":"echo {{input}}"}],"edges":[]}`, "run: no placeholders"},
		{"bad args placeholder", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s","command":"notify.post","args":{"message":"{{input}}"}}],"edges":[]}`, "args: {{input}}"},
		{"nine agent nodes", `{"nodes":[{"id":"a0","prompt":"p","start":true},{"id":"a1","prompt":"p","start":true},{"id":"a2","prompt":"p","start":true},{"id":"a3","prompt":"p","start":true},{"id":"a4","prompt":"p","start":true},{"id":"a5","prompt":"p","start":true},{"id":"a6","prompt":"p","start":true},{"id":"a7","prompt":"p","start":true},{"id":"a8","prompt":"p","start":true}],"edges":[]}`, "at most 8 agent nodes"},
		{"seventeen nodes", `{"nodes":[{"id":"a","prompt":"p","start":true},{"id":"s0","run":"x"},{"id":"s1","run":"x"},{"id":"s2","run":"x"},{"id":"s3","run":"x"},{"id":"s4","run":"x"},{"id":"s5","run":"x"},{"id":"s6","run":"x"},{"id":"s7","run":"x"},{"id":"s8","run":"x"},{"id":"s9","run":"x"},{"id":"s10","run":"x"},{"id":"s11","run":"x"},{"id":"s12","run":"x"},{"id":"s13","run":"x"},{"id":"s14","run":"x"},{"id":"s15","run":"x"}],"edges":[]}`, "at most 16 nodes"},
		{"placeholder of an unknown node", `{"nodes":[{"id":"a","prompt":"p","start":true}],"edges":[{"from":"a","to":"a","text":"{{node.b}}"}]}`, "{{node.b}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGraph("g", []byte(tt.graph))
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
