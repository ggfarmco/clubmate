package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestListAgentDocsFirst(t *testing.T) {
	e := newEnv(t)
	gitRepo(t, e.dir, "")
	e.doc(".gitignore", "plans/\n")
	e.doc("main.go", "")
	a := e.doc("a.md", "")
	r := e.doc(filepath.Join("b", "r.html"), "")
	e.doc(filepath.Join("plans", "ignored.md"), "")
	plan := e.doc(filepath.Join("plans", "p.md"), "")
	z := e.doc("z.md", "")
	e.core.agentDocs = map[string][]string{"": {plan, z}}

	got := e.list("")

	assert.False(t, got.Truncated)
	assert.Equal(t, []string{"plans/p.md", "z.md", "a.md", "b/r.html"}, rels(got))
	assert.Equal(t, []bool{true, true, false, false}, agents(got))
	info, err := os.Stat(r)
	require.NoError(t, err)
	assert.Equal(t, listItem{Path: r, Rel: "b/r.html", Kind: "artifact", Modified: info.ModTime().UTC().Format(time.RFC3339)}, got.Docs[3])
	assert.Equal(t, a, got.Docs[2].Path)
	assert.Equal(t, "markdown", got.Docs[2].Kind)
}

func TestListSkipsDeleted(t *testing.T) {
	e := newEnv(t)
	gone := e.doc("gone.md", "")
	kept := e.doc("kept.md", "")
	outside := filepath.Join(realDir(t), "out.md")
	write(t, outside, "")
	e.core.agentDocs = map[string][]string{"s1": {gone, outside, kept}}
	require.NoError(t, os.Remove(gone))

	assert.Equal(t, []string{"kept.md"}, rels(e.list("s1")))
}

func TestListCappedAndHomeOnlyAgentDocs(t *testing.T) {
	e := newEnv(t)
	for i := range maxListed + 1 {
		e.doc(fmt.Sprintf("d%04d.md", i), "")
	}
	e.core.agentDocs = map[string][]string{"": {filepath.Join(e.dir, "d1999.md")}}

	got := e.list("")
	assert.Len(t, got.Docs, maxListed)
	assert.True(t, got.Truncated)
	assert.Equal(t, []string{"d1999.md", "d0000.md"}, rels(got)[:2])

	e.core.ws.Home = true
	home := e.list("")
	assert.Equal(t, []listItem{{Path: filepath.Join(e.dir, "d1999.md"), Rel: "d1999.md", Kind: "markdown", Modified: got.Docs[0].Modified, Agent: true}}, home.Docs)
	assert.False(t, home.Truncated)
}

func TestListIfNoneMatch(t *testing.T) {
	e := newEnv(t)
	a := e.doc("a.md", "")
	first := e.list("")
	require.NotEmpty(t, first.Etag)
	again := func() any {
		res, err := e.run(pluginsdk.CommandRun{ID: "docs.list", Args: e.args(map[string]any{"ifNoneMatch": first.Etag})})
		require.NoError(t, err)
		return res
	}

	assert.Equal(t, unchangedResult{Unchanged: true, Etag: first.Etag}, again())

	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(a, later, later))
	got, ok := again().(listResult)
	require.True(t, ok)
	assert.NotEqual(t, first.Etag, got.Etag)
	assert.Equal(t, []string{"a.md"}, rels(got))
}

func TestListSessionOnly(t *testing.T) {
	e := newEnv(t)
	e.doc("other.md", "")
	mine := e.doc("mine.md", "")
	e.core.agentDocs = map[string][]string{"s1": {mine}}

	got := e.list("s1")

	assert.Equal(t, []string{"mine.md"}, rels(got))
	assert.Equal(t, []map[string]string{{"session": "s1"}}, e.core.docsArgs)
}

func (e *env) list(session string) listResult {
	e.t.Helper()
	args := map[string]any{}
	if session != "" {
		args["session"] = session
	}
	res, err := e.run(pluginsdk.CommandRun{ID: "docs.list", Args: e.args(args)})
	require.NoError(e.t, err)
	got, ok := res.(listResult)
	require.True(e.t, ok)
	return got
}

func (c *fakeCore) runAgentDocs(raw json.RawMessage) (any, error) {
	var args map[string]string
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	c.docsArgs = append(c.docsArgs, args)
	docs := []map[string]string{}
	for _, path := range c.agentDocs[args["session"]] {
		docs = append(docs, map[string]string{"path": path, "session": "s1", "at": "2026-11-01T09:00:00Z"})
	}
	return map[string]any{"docs": docs}, nil
}

func rels(r listResult) []string {
	out := make([]string, len(r.Docs))
	for i, d := range r.Docs {
		out[i] = d.Rel
	}
	return out
}

func agents(r listResult) []bool {
	out := make([]bool, len(r.Docs))
	for i, d := range r.Docs {
		out[i] = d.Agent
	}
	return out
}
