package main_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const nightEnv = "CLUBMATE_AGENTS_NIGHT_TEST=1"

var nightModes = map[string]any{"currentModeId": "auto", "availableModes": []map[string]string{{"id": "auto", "name": "Auto"}, {"id": "default", "name": "Manual"}}}

var nightDefaultModes = map[string]any{"currentModeId": "default", "availableModes": []map[string]string{{"id": "auto", "name": "Auto"}, {"id": "default", "name": "Manual"}}}

var nightEcho = []map[string]any{{"echo": true}}

var nonWord = regexp.MustCompile(`[^a-zA-Z0-9]`)

func startNight(t *testing.T, env []string, steps []map[string]any, dirs ...string) *fixture {
	t.Helper()
	return startEnv(t, append(env, nightEnv), map[string]any{"modes": nightModes, "prompt": steps}, dirs...)
}

func (f *fixture) nightSession(t *testing.T, workspace, dir string) string {
	t.Helper()
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.night-spawn", map[string]any{"dir": dir, "kind": "sleep"}, &out))
	require.NotEmpty(t, out.Session)
	return out.Session
}

func rowIDs(raw json.RawMessage) []string {
	var list []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &list)
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.ID)
	}
	return out
}

const (
	sleepMatch   = "digest.md is one"
	improveMatch = "digest/ holds"
)

type nightRig struct {
	*fixture
	workspace, home, memory, sleepGate, improveGate string
}

func startRig(t *testing.T, env []string, steps ...map[string]any) *nightRig {
	t.Helper()
	return startRigIn(t, newRepo(t), env, steps...)
}

func startRigIn(t *testing.T, repo string, env []string, steps ...map[string]any) *nightRig {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	memory := filepath.Join(home, "memory")
	require.NoError(t, os.MkdirAll(memory, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))
	write(t, filepath.Join(memory, "MEMORY.md"), "old")
	raw, err := json.Marshal(map[string]string{"autoMemoryDirectory": memory})
	require.NoError(t, err)
	write(t, filepath.Join(home, ".claude", "settings.json"), string(raw))
	r := &nightRig{home: home, memory: memory, sleepGate: filepath.Join(home, "sleep-gate"), improveGate: filepath.Join(home, "improve-gate")}
	script := slices.Concat(steps, []map[string]any{{"match": sleepMatch, "wait": r.sleepGate}, {"match": improveMatch, "wait": r.improveGate}}, nightEcho)
	r.fixture = startNight(t, append(env, "HOME="+home), script)
	r.workspace = r.fixture.workspace(t, repo)
	selfRepo(t, r.fixture)
	return r
}

func (r *nightRig) enable(t *testing.T) {
	t.Helper()
	r.selfEvent(t, "self-enabled", true)
}

func (r *nightRig) day(t *testing.T, workspace string) {
	t.Helper()
	session := r.newSession(t, workspace)
	r.prompt(t, workspace, session, "work")
	r.waitStatus(t, workspace, session, "done")
}

func (r *nightRig) runNight(t *testing.T) {
	t.Helper()
	testutil.Eventually(t, func() bool { return r.run(t, r.workspace, "agents.night-run", nil, nil) == nil }, wait)
}

func (r *nightRig) nights(t *testing.T, workspace, kind string) []record {
	t.Helper()
	var list []record
	require.NoError(t, r.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	return slices.DeleteFunc(list, func(rec record) bool { return rec.Night != kind })
}

func (r *nightRig) night(t *testing.T, kind string) record {
	t.Helper()
	var got record
	testutil.Eventually(t, func() bool {
		list := r.nights(t, r.workspace, kind)
		if len(list) == 0 {
			return false
		}
		got = list[len(list)-1]
		return true
	}, wait)
	return got
}

func (r *nightRig) declare(t *testing.T, rec record, summary string) {
	t.Helper()
	require.NoError(t, r.runAs(t, r.workspace, rec.ID, "agents.self-declare", map[string]any{"summary": summary, "why": "the user asked twice"}, nil))
}

func (r *nightRig) tidyMemory(t *testing.T, sleep record) {
	t.Helper()
	write(t, filepath.Join(sleep.Worktree, "memory", "0", "MEMORY.md"), "new")
	r.declare(t, sleep, "merged two notes")
}

func (r *nightRig) open(t *testing.T, gate string) {
	t.Helper()
	write(t, gate, "")
}

func (r *nightRig) reviewed(t *testing.T, count int) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		return len(r.reviews(t)) == count
	}, wait)
}

func (r *nightRig) fullNight(t *testing.T) (record, record) {
	t.Helper()
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	r.tidyMemory(t, sleep)
	r.open(t, r.sleepGate)
	improve := r.night(t, "improve")
	write(t, filepath.Join(improve.Worktree, "prompt.md"), "be brief")
	r.declare(t, improve, "shorter replies")
	r.open(t, r.improveGate)
	r.reviewed(t, 1)
	return sleep, improve
}

func (r *nightRig) reviews(t *testing.T) []notePost {
	t.Helper()
	return slices.DeleteFunc(r.notes(t), func(n notePost) bool { return n.Title != "Night review" })
}

func (r *nightRig) projectMemory(t *testing.T, repo string) string {
	t.Helper()
	escaped := strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			return c
		}
		return '-'
	}, repo)
	dir := filepath.Join(r.home, ".claude", "projects", escaped, "memory")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	write(t, filepath.Join(dir, "MEMORY.md"), "old")
	return dir
}

func (r *nightRig) gone(t *testing.T, dir string) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(dir)
		return errors.Is(err, os.ErrNotExist)
	}, wait)
}

func (r *nightRig) noImprove(t *testing.T) {
	t.Helper()
	testutil.Never(t, func() bool { return len(r.nights(t, r.workspace, "improve")) > 0 }, time.Second)
}

func (r *nightRig) selfFile(t *testing.T, name string) string {
	t.Helper()
	return readFile(t, filepath.Join(r.h.Dirs.PluginState("agents"), "self", name))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func lastAgentText(entries []entry) string {
	for _, en := range slices.Backward(entries) {
		if en.Kind == "agent" {
			return en.Text
		}
	}
	return ""
}

func TestNightRunsSleepThenImprove(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)

	r.runNight(t)

	sleep := r.night(t, "sleep")
	assert.Contains(t, readFile(t, filepath.Join(sleep.Worktree, "digest.md")), "work")
	workspace, err := r.h.Svc.GetWorkspace(t.Context(), r.workspace)
	require.NoError(t, err)
	created := r.waitCall(t, "session/new", func(c fakeCall) bool { return strings.Contains(string(c.Params), sleep.Worktree) })
	var params struct {
		AdditionalDirectories []string `json:"additionalDirectories"`
	}
	require.NoError(t, json.Unmarshal(created.Params, &params))
	assert.Equal(t, workspace.Roots(), params.AdditionalDirectories)
	assert.Empty(t, r.nights(t, r.workspace, "improve"))
	r.tidyMemory(t, sleep)
	r.open(t, r.sleepGate)

	improve := r.night(t, "improve")
	assert.Equal(t, "new", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	assert.Equal(t, "closed", r.waitStatus(t, r.workspace, sleep.ID, "closed").Status)
	assert.NoDirExists(t, sleep.Worktree)
	assert.NoDirExists(t, sleep.Worktree+".git")
	assert.FileExists(t, filepath.Join(improve.Worktree, "digest", "demo.md"))
	assert.NoFileExists(t, filepath.Join(improve.Worktree, ".git"))
	assert.DirExists(t, filepath.Join(r.h.Dirs.PluginState("agents"), "self", "memory-patches"))
	assert.NoDirExists(t, filepath.Join(improve.Worktree, "memory-patches"))
	write(t, filepath.Join(improve.Worktree, "prompt.md"), "be brief")
	r.declare(t, improve, "shorter replies")
	r.open(t, r.improveGate)

	r.reviewed(t, 1)
	assert.Equal(t, "be brief", r.selfFile(t, "prompt.md"))
	view := r.transcript(t, r.workspace, improve.ID)
	report := lastAgentText(view.Entries)
	assert.Contains(t, report, "shorter replies")
	assert.Contains(t, report, "merged two notes")
	assert.Contains(t, report, "+be brief")
	assert.Equal(t, 2, r.session(t, r.workspace, improve.ID).OpenCount)
	assert.Contains(t, r.selfFile(t, filepath.Join("reports", time.Now().Format(time.DateOnly)+".md")), "+be brief")
	assert.Contains(t, r.notes(t), notePost{Workspace: r.workspace, Session: improve.ID, Title: "Night review", Message: "2 changes", Kind: "done"})
	assert.Contains(t, r.settingsTree(t), "self-reset")
}

func TestOfferUndoesWithoutATurn(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	_, improve := r.fullNight(t)
	prompts := len(r.calls(t, "session/prompt"))
	items := r.openEntry(t, r.workspace, improve.ID).Items
	require.Len(t, items, 2)

	require.NoError(t, r.run(t, r.workspace, "agents.open-answer", map[string]any{"session": improve.ID, "offers": []string{items[0].ID, items[1].ID}}, nil))

	assert.Empty(t, r.selfFile(t, "prompt.md"))
	assert.Equal(t, "old", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	assert.Equal(t, 0, r.session(t, r.workspace, improve.ID).OpenCount)
	testutil.Never(t, func() bool { return len(r.calls(t, "session/prompt")) > prompts }, 500*time.Millisecond)
}

func TestUndoneMemoryReachesTheNextSleep(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	first, improve := r.fullNight(t)
	items := r.openEntry(t, r.workspace, improve.ID).Items
	require.NoError(t, r.run(t, r.workspace, "agents.open-answer", map[string]any{"session": improve.ID, "offers": []string{items[1].ID}}, nil))
	require.NoError(t, os.Remove(r.sleepGate))

	r.day(t, r.workspace)
	r.runNight(t)

	var sleep record
	testutil.Eventually(t, func() bool {
		sleep = r.night(t, "sleep")
		return sleep.ID != first.ID
	}, wait)
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(sleep.Worktree, "rejected.md"))
		return err == nil
	}, wait)
	rejected := readFile(t, filepath.Join(sleep.Worktree, "rejected.md"))
	assert.Contains(t, rejected, "merged two notes")
	assert.Contains(t, rejected, "+new")
	assert.NotContains(t, rejected, "shorter replies")
	r.waitCall(t, "session/prompt", func(c fakeCall) bool {
		return strings.Contains(promptText(t, c), "rejected.md lists cleanups the user undid in this project. Don't repeat them.")
	})
	r.open(t, r.sleepGate)
}

func TestNightLogsGoWithTheirSandbox(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	projects := filepath.Join(r.home, ".claude", "projects")
	logDir := func(path string) string { return filepath.Join(projects, nonWord.ReplaceAllString(path, "-")) }
	log := func(dir string) {
		require.NoError(t, os.MkdirAll(dir, 0o700))
		write(t, filepath.Join(dir, "s.jsonl"), "digest")
	}
	log(logDir(sleep.Worktree))
	real, err := filepath.EvalSymlinks(sleep.Worktree)
	require.NoError(t, err)
	log(logDir(real))
	other := filepath.Join(projects, "-Users-me-work")
	log(other)
	r.open(t, r.sleepGate)
	improve := r.night(t, "improve")
	log(logDir(improve.Worktree))
	r.open(t, r.improveGate)
	r.reviewed(t, 1)

	assert.NoDirExists(t, logDir(sleep.Worktree))
	assert.NoDirExists(t, logDir(real))
	assert.DirExists(t, logDir(improve.Worktree), "the Improve session stays until the next night")
	assert.DirExists(t, other)

	r.day(t, r.workspace)
	r.runNight(t)
	r.gone(t, logDir(improve.Worktree))
	assert.DirExists(t, other)
}

func TestDeclareRefusedOnceTheNightApplied(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	_, improve := r.fullNight(t)
	write(t, filepath.Join(improve.Worktree, "prompt.md"), "later")

	err := r.runAs(t, r.workspace, improve.ID, "agents.self-declare", map[string]any{"summary": "too late"}, nil)

	assert.ErrorContains(t, err, "this night's changes are already applied")
}

func TestNightEmptyDigestStartsNothing(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	_, improve := r.fullNight(t)
	started := len(r.calls(t, "session/new"))

	r.runNight(t)

	testutil.Never(t, func() bool { return len(r.calls(t, "session/new")) > started }, time.Second)
	assert.Equal(t, 2, r.session(t, r.workspace, improve.ID).OpenCount)
	assert.DirExists(t, improve.Worktree)
}

func TestNightOffKeepsNothing(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.day(t, r.workspace)

	err := r.run(t, r.workspace, "agents.night-run", nil, nil)

	assert.ErrorContains(t, err, "self-improvement is off")
	assert.Empty(t, r.nights(t, r.workspace, "sleep"))
}

func TestTurningOffMidNightAppliesNothing(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	r.tidyMemory(t, sleep)

	r.selfEvent(t, "self-enabled", false)
	testutil.Eventually(t, func() bool { return strings.Contains(r.settingsTree(t), `"value":false`) }, wait)
	r.open(t, r.sleepGate)

	r.waitStatus(t, r.workspace, sleep.ID, "closed")
	assert.Equal(t, "old", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	r.noImprove(t)
}

func TestRestartMidNight(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	r.tidyMemory(t, sleep)
	prompts := len(r.calls(t, "session/prompt"))

	r.restartPlugin(t, r.workspace)

	r.waitStatus(t, r.workspace, sleep.ID, "closed")
	r.gone(t, sleep.Worktree)
	r.gone(t, sleep.Worktree+".git")
	r.reviewed(t, 1)
	assert.Equal(t, notePost{Workspace: r.workspace, Title: "Night review", Message: "Stopped: Clubmate restarted during the night", Kind: "done"}, r.reviews(t)[0])
	assert.Equal(t, "old", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	testutil.Never(t, func() bool { return len(r.calls(t, "session/prompt")) > prompts }, time.Second)
	assert.Empty(t, r.nights(t, r.workspace, "improve"))

	r.day(t, r.workspace)
	r.runNight(t)
	testutil.Eventually(t, func() bool { return r.night(t, "sleep").ID != sleep.ID }, wait)
}

func TestFailedNightLosesNoDay(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	stopped := r.night(t, "sleep")
	r.restartPlugin(t, r.workspace)
	r.reviewed(t, 1)

	r.runNight(t)

	var sleep record
	testutil.Eventually(t, func() bool {
		sleep = r.night(t, "sleep")
		return sleep.ID != stopped.ID
	}, wait)
	assert.Contains(t, readFile(t, filepath.Join(sleep.Worktree, "digest.md")), "work")
	r.open(t, r.sleepGate)
	r.open(t, r.improveGate)
}

func TestFinishedNightMovesTheDigestOn(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	sleep, _ := r.fullNight(t)

	r.runNight(t)

	testutil.Never(t, func() bool { return r.night(t, "sleep").ID != sleep.ID }, time.Second)
}

func TestRestartAfterSleepLandedStillReports(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	r.tidyMemory(t, r.night(t, "sleep"))
	r.open(t, r.sleepGate)
	improve := r.night(t, "improve")

	r.restartPlugin(t, r.workspace)

	r.reviewed(t, 1)
	assert.Equal(t, "1 change, stopped early", r.reviews(t)[0].Message)
	r.gone(t, improve.Worktree)
	report := r.selfFile(t, filepath.Join("reports", time.Now().Format(time.DateOnly)+".md"))
	assert.Contains(t, report, "- Memory: merged two notes")
	assert.Contains(t, report, "Undo from any chat")
}

func TestSessionFailureEndsNight(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		step map[string]any
	}{
		{"internal", map[string]any{"match": sleepMatch, "fail": "boom"}},
		{"rate limit", map[string]any{"match": sleepMatch, "fail": "slow down", "errorKind": "rate_limited"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := startRig(t, []string{"CLUBMATE_AGENTS_NET_RETRY_AFTER=10ms"}, tc.step)
			r.enable(t)
			r.day(t, r.workspace)

			r.runNight(t)

			sleep := r.night(t, "sleep")
			r.waitStatus(t, r.workspace, sleep.ID, "closed")
			r.gone(t, sleep.Worktree)
			assert.Equal(t, "old", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
			r.noImprove(t)
			assert.Len(t, r.calls(t, "session/prompt"), 2)
			assert.NotContains(t, r.logText(t, sleep.ID), "retrying")
			r.reviewed(t, 1)
			got := r.reviews(t)[0]
			assert.Contains(t, got.Message, "Stopped: a night session failed")
			assert.Empty(t, got.Session)
			assert.NoFileExists(t, filepath.Join(r.h.Dirs.PluginState("agents"), "self", "reports", time.Now().Format(time.DateOnly)+".md"))
		})
	}
}

func TestSessionLimitEndsOnlyThatSession(t *testing.T) {
	t.Parallel()
	r := startRig(t, []string{"CLUBMATE_AGENTS_NIGHT_SESSION_LIMIT=5s"})
	r.enable(t)
	r.day(t, r.workspace)
	r.open(t, r.improveGate)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	r.tidyMemory(t, sleep)

	improve := r.night(t, "improve")

	r.reviewed(t, 1)
	assert.Equal(t, "closed", r.session(t, r.workspace, sleep.ID).Status)
	assert.Equal(t, "old", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	assert.Contains(t, lastAgentText(r.transcript(t, r.workspace, improve.ID).Entries), "demo: a night session reached its time limit")
	assert.Equal(t, "Nothing landed, see the report", r.reviews(t)[0].Message)
}

func TestImproveAtItsLimitStillReports(t *testing.T) {
	t.Parallel()
	r := startRig(t, []string{"CLUBMATE_AGENTS_NIGHT_SESSION_LIMIT=3s"})
	r.enable(t)
	r.day(t, r.workspace)
	r.open(t, r.sleepGate)
	r.runNight(t)
	improve := r.night(t, "improve")
	write(t, filepath.Join(improve.Worktree, "prompt.md"), "be brief")
	r.declare(t, improve, "shorter replies")

	r.reviewed(t, 1)

	assert.Equal(t, "Nothing landed, see the report", r.reviews(t)[0].Message)
	assert.Empty(t, r.selfFile(t, "prompt.md"))
	assert.Contains(t, lastAgentText(r.transcript(t, r.workspace, improve.ID).Entries), "Night review: a night session reached its time limit")
	assert.Equal(t, "closed", r.session(t, r.workspace, improve.ID).Status)
}

func TestStoppedAfterSleepLandedStillReports(t *testing.T) {
	t.Parallel()
	older := newRepo(t)
	r := startRigIn(t, older, nil, map[string]any{"match": older, "fail": "boom"})
	require.NoError(t, os.Remove(filepath.Join(r.home, ".claude", "settings.json")))
	r.projectMemory(t, older)
	newer := newRepo(t)
	memory := r.projectMemory(t, newer)
	first := r.workspace
	r.workspace = r.fixture.workspace(t, newer)
	r.enable(t)
	r.day(t, first)
	r.day(t, r.workspace)
	r.runNight(t)
	r.tidyMemory(t, r.night(t, "sleep"))

	r.open(t, r.sleepGate)

	r.reviewed(t, 1)
	assert.Equal(t, notePost{Workspace: r.workspace, Title: "Night review", Message: "1 change, stopped early", Kind: "done"}, r.reviews(t)[0])
	assert.Equal(t, "new", readFile(t, filepath.Join(memory, "MEMORY.md")))
	assert.Contains(t, r.selfFile(t, filepath.Join("reports", time.Now().Format(time.DateOnly)+".md")), "merged two notes")
	r.noImprove(t)
}

func TestCancelledSleepAppliesNothing(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	r.tidyMemory(t, sleep)

	require.NoError(t, r.run(t, r.workspace, "agents.cancel", map[string]any{"session": sleep.ID}, nil))

	r.waitStatus(t, r.workspace, sleep.ID, "closed")
	assert.Equal(t, "old", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	r.noImprove(t)
}

func TestNightLimitEndsTheNight(t *testing.T) {
	t.Parallel()
	r := startRig(t, []string{"CLUBMATE_AGENTS_NIGHT_LIMIT=5s"})
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")

	r.waitStatus(t, r.workspace, sleep.ID, "closed")

	r.noImprove(t)
	r.reviewed(t, 1)
	assert.Equal(t, "Stopped: the night reached its time limit", r.reviews(t)[0].Message)
}

func TestMovedMemoryIsSkippedAndReported(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	sleep := r.night(t, "sleep")
	r.tidyMemory(t, sleep)
	write(t, filepath.Join(r.memory, "MEMORY.md"), "the user's own edit")
	r.open(t, r.sleepGate)

	improve := r.night(t, "improve")
	r.open(t, r.improveGate)

	r.reviewed(t, 1)
	assert.Equal(t, "the user's own edit", readFile(t, filepath.Join(r.memory, "MEMORY.md")))
	assert.Contains(t, lastAgentText(r.transcript(t, r.workspace, improve.ID).Entries), "memory changed during the night")
}

func TestNightSleepsOncePerMemoryDir(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	older := r.workspace
	r.workspace = r.fixture.workspace(t, newRepo(t))
	r.day(t, older)

	r.fullNight(t)

	assert.Len(t, slices.Concat(r.nights(t, r.workspace, "sleep"), r.nights(t, older, "sleep")), 1)
}

func TestNextNightDeletesPrevious(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil)
	sleep, improve := r.fullNight(t)
	write(t, filepath.Join(improve.Worktree, "left-over"), "x")

	r.day(t, r.workspace)
	r.runNight(t)

	r.reviewed(t, 2)
	assert.Empty(t, r.session(t, r.workspace, sleep.ID).ID)
	assert.Empty(t, r.session(t, r.workspace, improve.ID).ID)
	assert.NoFileExists(t, filepath.Join(improve.Worktree, "left-over"))
	assert.NoDirExists(t, filepath.Join(improve.Worktree, "reports"))
	assert.Equal(t, "Nothing to change tonight.", lastAgentText(r.transcript(t, r.workspace, r.night(t, "improve").ID).Entries))
	assert.FileExists(t, filepath.Join(r.h.Dirs.PluginState("agents"), "self", "reports", time.Now().Format(time.DateOnly)+".md"))
}

func TestReportCountsRefusedRequests(t *testing.T) {
	t.Parallel()
	r := startRig(t, nil, map[string]any{"match": improveMatch, "permission": map[string]any{
		"toolCall": map[string]any{"toolCallId": "t1", "title": "Write /outside"},
		"options":  []map[string]any{{"optionId": "no", "name": "No", "kind": "reject_once"}},
	}})
	r.enable(t)
	r.day(t, r.workspace)
	r.runNight(t)
	r.open(t, r.sleepGate)
	r.open(t, r.improveGate)

	r.reviewed(t, 1)

	assert.Equal(t, "Nothing landed, see the report", r.reviews(t)[0].Message)
	assert.Contains(t, lastAgentText(r.transcript(t, r.workspace, r.night(t, "improve").ID).Entries), "- Refused: 1")
}

func TestNightPermissionIsRejectedAtOnce(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, []map[string]any{{"permission": map[string]any{
		"toolCall": map[string]any{"toolCallId": "t1", "title": "Write /outside"},
		"options":  []map[string]any{{"optionId": "yes", "name": "Yes", "kind": "allow_once"}, {"optionId": "no", "name": "No", "kind": "reject_once"}},
	}}})
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())

	f.waitStatus(t, workspace, night, "done")

	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"selected","optionId":"no"}}`, string(got.Params))
	assert.NotContains(t, f.logText(t, night), "session/request_permission")
}

func TestNightElicitationIsDeclinedAtOnce(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, []map[string]any{{"elicitation": askScript(false)}})
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())

	f.waitStatus(t, workspace, night, "done")

	got := f.waitCall(t, "elicitation_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"action":"decline"}`, string(got.Params))
	assert.NotContains(t, f.logText(t, night), "elicitation/create")
}

func TestNightSessionHiddenFromAgents(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, nightEcho)
	workspace := f.workspace(t, newRepo(t))
	other := f.newSession(t, workspace)
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "done")

	args := map[string]any{"session": night}
	assert.Error(t, f.runAs(t, workspace, other, "agents.transcript", args, nil))
	assert.Error(t, f.runAs(t, workspace, other, "agents.export", map[string]any{"session": night, "dir": filepath.Join(t.TempDir(), "x")}, nil))
	assert.Error(t, f.runAs(t, workspace, other, "agents.wait", map[string]any{"sessions": []string{night}, "timeout": 1}, nil))

	var list json.RawMessage
	require.NoError(t, f.runAs(t, workspace, other, "agents.sessions", map[string]any{}, &list))
	assert.Contains(t, rowIDs(list), other)
	assert.NotContains(t, rowIDs(list), night)
	var page struct {
		Sessions json.RawMessage `json:"sessions"`
	}
	require.NoError(t, f.runAs(t, workspace, other, "agents.sessions", map[string]any{"since": ""}, &page))
	assert.NotContains(t, rowIDs(page.Sessions), night)
	var hits []struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.runAs(t, workspace, other, "agents.search", map[string]any{"query": "Night"}, &hits))
	assert.Empty(t, hits)

	require.NoError(t, f.runAs(t, workspace, night, "agents.transcript", args, nil))
	require.NoError(t, f.run(t, workspace, "agents.transcript", args, nil))
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
	assert.Contains(t, rowIDs(list), night)
	require.NoError(t, f.run(t, workspace, "agents.search", map[string]any{"query": "Night"}, &hits))
	assert.Len(t, hits, 1)
}

func TestNightWithoutDefaultModeNeverPrompts(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{nightEnv}, map[string]any{
		"modes":  map[string]any{"currentModeId": "auto", "availableModes": []map[string]string{{"id": "auto", "name": "Auto"}}},
		"prompt": nightEcho,
	})
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "error")

	f.prompt(t, workspace, night, "again")

	testutil.Never(t, func() bool { return len(f.calls(t, "session/prompt")) > 0 }, time.Second)
	assert.Equal(t, "error", f.session(t, workspace, night).Status)
}

func (f *fixture) suspendedNight(t *testing.T) (string, string) {
	t.Helper()
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "done")
	testutil.Eventually(t, func() bool { return f.session(t, workspace, night).Suspended }, wait)
	return workspace, night
}

func (f *fixture) sequence(t *testing.T, methods ...string) []string {
	t.Helper()
	data, err := os.ReadFile(f.fakeLog)
	require.NoError(t, err)
	var out []string
	for line := range strings.Lines(string(data)) {
		if !strings.HasSuffix(line, "\n") {
			break
		}
		var c fakeCall
		require.NoError(t, json.Unmarshal([]byte(line), &c))
		if slices.Contains(methods, c.Method) {
			out = append(out, c.Method)
		}
	}
	return out
}

func TestNightSessionStaysInItsMode(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, nightEcho)
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "done")

	assert.ErrorContains(t, f.run(t, workspace, "agents.mode", map[string]any{"session": night, "mode": "auto"}, nil), "only in default mode")
	assert.ErrorContains(t, f.run(t, workspace, "agents.handoff", map[string]any{"session": night, "note": "go on"}, nil), "can't be handed off")
	assert.ErrorContains(t, f.run(t, workspace, "agents.config", map[string]any{"session": night, "id": "advisor", "value": "opus"}, nil), "settings don't change")
	assert.Empty(t, f.calls(t, "session/set_mode")[1:])
}

func TestNightResumeSetsDefaultBeforePrompt(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{nightEnv, "CLUBMATE_AGENTS_SUSPEND_AFTER=" + suspendAfter.String()}, map[string]any{
		"resume": true,
		"modes":  nightDefaultModes,
		"prompt": []map[string]any{
			{"match": "two", "permission": map[string]any{
				"toolCall": map[string]any{"toolCallId": "t1", "title": "Write /outside"},
				"options":  []map[string]any{{"optionId": "no", "name": "No", "kind": "reject_once"}},
			}},
			{"echo": true},
		},
	})
	workspace, night := f.suspendedNight(t)

	f.prompt(t, workspace, night, "two")

	f.waitCall(t, "session/prompt", func(c fakeCall) bool { return promptText(t, c) == "two" })
	assert.Equal(t, []string{"session/prompt", "session/resume", "session/set_mode", "session/prompt"}, f.sequence(t, "session/prompt", "session/resume", "session/set_mode"))
	resumed := string(f.calls(t, "session/resume")[0].Params)
	assert.Contains(t, resumed, `"tools":["Read","Grep","Glob","Edit","Write"]`)
	assert.Contains(t, resumed, `"permissions":{"allow":`)
	got := f.waitCall(t, "permission_response", func(fakeCall) bool { return true })
	assert.JSONEq(t, `{"outcome":{"outcome":"selected","optionId":"no"}}`, string(got.Params))
}

func TestNightResumeWithoutDefaultNeverPrompts(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{nightEnv, "CLUBMATE_AGENTS_SUSPEND_AFTER=" + suspendAfter.String()}, map[string]any{"resume": true, "failSetMode": true, "modes": nightDefaultModes, "prompt": nightEcho})
	workspace, night := f.suspendedNight(t)

	f.prompt(t, workspace, night, "two")
	f.waitStatus(t, workspace, night, "error")
	f.prompt(t, workspace, night, "three")

	testutil.Never(t, func() bool { return len(f.calls(t, "session/prompt")) > 1 }, time.Second)
	assert.Equal(t, "error", f.session(t, workspace, night).Status)
}

func TestNightQuestionDoesNotNeedYou(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, []map[string]any{{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Merge these two?"}}}})
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())

	got := f.waitStatus(t, workspace, night, "done")

	assert.False(t, got.NeedsYou)
	assert.Equal(t, "service", got.Group)
}

func TestNightOfferRefusesTheBacklog(t *testing.T) {
	t.Parallel()
	f := startNight(t, nil, nightEcho, backlogDir(t))
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "done")
	f.postItems(t, workspace, night, offer("undo it"))

	err := f.run(t, workspace, "agents.open-backlog", map[string]any{"session": night}, nil)

	assert.ErrorContains(t, err, "backlog")
	assert.Equal(t, 1, f.session(t, workspace, night).OpenCount)
}

func TestNightOffersSkipAging(t *testing.T) {
	t.Parallel()
	f := startNight(t, []string{"CLUBMATE_AGENTS_OPEN_AFTER=1ms"}, nightEcho, backlogDir(t))
	workspace := f.workspace(t, newRepo(t))
	night := f.nightSession(t, workspace, t.TempDir())
	f.waitStatus(t, workspace, night, "done")
	f.postItems(t, workspace, night, offer("undo it"))
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": night}, nil))

	session := f.doneSession(t, workspace)
	f.postItems(t, workspace, session, offer("add a screen"))
	testutil.Eventually(t, func() bool { return f.session(t, workspace, session).OpenCount == 0 }, wait)

	assert.Equal(t, 1, f.session(t, workspace, night).OpenCount)
}

func TestNightSessionRunsNoGitInItsSandbox(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	bin, calls := t.TempDir(), filepath.Join(t.TempDir(), "git.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$PWD $*\" >> %q\nexec %q \"$@\"\n", calls, real)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700))
	sandbox := t.TempDir()
	git(t, sandbox, "init", "-q")
	f := startNight(t, []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}, []map[string]any{
		{"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Edit", "kind": "edit", "status": "completed"}},
	})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	night := f.nightSession(t, workspace, sandbox)
	f.waitStatus(t, workspace, night, "done")
	f.h.WaitSlot(t, workspace, "agents.sidebar", func(n sdui.Node) bool {
		raw, err := json.Marshal(n)
		return err == nil && strings.Contains(string(raw), "Service")
	})

	require.NoError(t, f.run(t, workspace, "agents.export", map[string]any{"session": night, "dir": filepath.Join(t.TempDir(), "x")}, nil))
	require.NoError(t, f.run(t, workspace, "agents.delete", map[string]any{"session": night}, nil))

	ordinary := f.newSession(t, workspace)
	f.prompt(t, workspace, ordinary, "control")
	f.waitStatus(t, workspace, ordinary, "done")
	var log string
	testutil.Eventually(t, func() bool {
		data, err := os.ReadFile(calls)
		log = string(data)
		return err == nil && strings.Contains(log, repo)
	}, wait)

	assert.NotContains(t, log, sandbox)
	assert.Empty(t, git(t, sandbox, "for-each-ref", "refs/clubmate"))
}

func TestNightSpawnNeedsTheTestSwitch(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{"prompt": nightEcho})
	workspace := f.workspace(t, newRepo(t))

	err := f.run(t, workspace, "agents.night-spawn", map[string]any{"dir": t.TempDir(), "kind": "sleep"}, nil)

	require.Error(t, err)
	assert.Empty(t, f.calls(t, "session/new"))
}
