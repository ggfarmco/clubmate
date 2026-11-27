package main_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const queueEditWarning = "[Clubmate landing queue] a.go is in the queued landing of \"A\" (a) into main. An edit here makes it wait for your change and may make it rebase. Prefer to make this change in your own worktree."

func startQueueResumable(t *testing.T, steps []func(repo string) map[string]any) queueSetup {
	t.Helper()
	repo := newRepo(t)
	script := []map[string]any{{"match": "Clubmate land review", "wait": filepath.Join(t.TempDir(), "release")}}
	for _, step := range steps {
		script = append(script, step(repo))
	}
	f := startEnv(t, nil, map[string]any{"resume": true, "prompt": script}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	q := queueSetup{f: f, repo: repo, workspace: f.workspace(t, repo)}
	require.NoError(t, f.run(t, q.workspace, "agents.gate-add", map[string]any{"repo": repo}, nil))
	return q
}

func editStep(match, call, path string) map[string]any {
	return map[string]any{"match": match, "update": map[string]any{
		"sessionUpdate": "tool_call", "toolCallId": call, "title": "Edit", "kind": "edit", "status": "in_progress",
		"locations": []map[string]any{{"path": path}}, "rawInput": map[string]any{"file_path": path},
	}}
}

func (f *fixture) lastAttachDir(t *testing.T, session string) string {
	t.Helper()
	calls := f.allCalls(t)
	for i := len(calls) - 1; i >= 0; i-- {
		c := calls[i]
		if slices.Contains([]string{"session/new", "session/resume", "session/fork"}, c.Method) && strings.Contains(string(c.Params), session) {
			return hookMetaOf(t, c).dir
		}
	}
	return ""
}

func queueNoteExists(dir, call string) bool {
	_, err := os.Stat(filepath.Join(dir, "findings", call+".queue"))
	return err == nil
}

func (f *fixture) waitFinding(t *testing.T, dir, call string) string {
	t.Helper()
	var text string
	testutil.Eventually(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(dir, "findings", call+".queue"))
		text = string(raw)
		return err == nil
	}, wait)
	return text
}

func TestEditOfQueuedFileInTargetWarnsOnce(t *testing.T) {
	t.Parallel()
	q := startQueueResumable(t, []func(string) map[string]any{
		func(repo string) map[string]any { return editStep("first-edit", "e1", filepath.Join(repo, "a.go")) },
		func(repo string) map[string]any { return editStep("second-edit", "e2", filepath.Join(repo, "a.go")) },
		func(repo string) map[string]any { return editStep("third-edit", "e3", filepath.Join(repo, "c.go")) },
	})
	session := q.f.newSession(t, q.workspace)
	a, c := q.lander(t, "A", "a.go"), q.lander(t, "C", "c.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, c), "queued")

	q.f.prompt(t, q.workspace, session, "first-edit")
	q.f.waitStatus(t, q.workspace, session, "done")
	dir := q.f.lastAttachDir(t, session)
	require.NotEmpty(t, dir)
	assert.Equal(t, queueEditWarning, q.f.waitFinding(t, dir, "e1"))

	q.f.restartPlugin(t, q.workspace)

	q.f.prompt(t, q.workspace, session, "second-edit")
	q.f.waitStatus(t, q.workspace, session, "done")
	q.f.prompt(t, q.workspace, session, "third-edit")
	assert.Contains(t, q.f.waitFinding(t, dir, "e3"), "c.go is in the queued landing of \"C\" (c)")
	testutil.Never(t, func() bool { return queueNoteExists(dir, "e2") }, time.Second)
}

func TestEditOutsideTargetGetsNoWarning(t *testing.T) {
	t.Parallel()
	var target string
	q := startQueueWith(t, true, func(repo string) []map[string]any {
		target = filepath.Join(repo, "a.go")
		return []map[string]any{editStep("own-edit", "e1", "a.go"), editStep("target-edit", "e2", target)}
	})
	a := q.lander(t, "A", "a.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	b := q.lander(t, "B", "b.go")
	dir := q.f.lastAttachDir(t, b.id)
	require.NotEmpty(t, dir)

	q.f.prompt(t, q.workspace, b.id, "own-edit")
	q.f.waitStatus(t, q.workspace, b.id, "done")
	q.f.prompt(t, q.workspace, b.id, "target-edit")

	assert.Equal(t, queueEditWarning, q.f.waitFinding(t, dir, "e2"))
	testutil.Never(t, func() bool { return queueNoteExists(dir, "e1") }, time.Second)
}
