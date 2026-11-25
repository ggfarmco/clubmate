package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const neighboursHead = "[Clubmate neighbours]"

type overlapRow struct {
	ID       string `json:"id"`
	Overlaps []struct {
		Session string   `json:"session"`
		Branch  string   `json:"branch"`
		Files   []string `json:"files"`
	} `json:"overlaps"`
}

func startNeighbours(t *testing.T, gates ...string) *fixture {
	t.Helper()
	steps := []map[string]any{
		{"match": "touch-a", "run": "echo x >> a.go"},
		{"match": "commit-a", "run": "echo x >> a.go && git commit -qam x"},
	}
	for _, g := range gates {
		steps = append(steps, map[string]any{"match": "hold-" + filepath.Base(g), "wait": g})
	}
	return startEnv(t, []string{"CLUBMATE_AGENTS_TOUCHED_AGE=0s"}, map[string]any{"resume": true, "prompt": steps}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
}

func (f *fixture) topLevel(t *testing.T, workspace, branch, prompt string) child {
	t.Helper()
	var out child
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"branch": branch, "title": branch, "prompt": prompt}, &out))
	f.waitStatus(t, workspace, out.Session, "done")
	return out
}

func gitOut(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func allBlocks(t *testing.T, c fakeCall) string {
	t.Helper()
	var p struct {
		Prompt []struct {
			Text string `json:"text"`
		} `json:"prompt"`
	}
	require.NoError(t, json.Unmarshal(c.Params, &p))
	var out []string
	for _, b := range p.Prompt {
		out = append(out, b.Text)
	}
	return strings.Join(out, "\n")
}

func (f *fixture) neighbourBlocks(t *testing.T, agentID string) []string {
	t.Helper()
	var out []string
	for _, c := range f.calls(t, "session/prompt") {
		if !strings.Contains(string(c.Params), agentID) {
			continue
		}
		text := allBlocks(t, c)
		if i := strings.Index(text, neighboursHead); i >= 0 {
			out = append(out, text[i:])
		} else {
			out = append(out, "")
		}
	}
	return out
}

func (f *fixture) overlapsOf(t *testing.T, workspace, session string) []string {
	t.Helper()
	var rows []overlapRow
	require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &rows))
	var out []string
	for _, r := range rows {
		if r.ID != session {
			continue
		}
		for _, o := range r.Overlaps {
			out = append(out, o.Session+":"+strings.Join(o.Files, ","))
		}
	}
	return out
}

func TestNeighboursTellOrchestratorOncePerPair(t *testing.T) {
	t.Parallel()
	gate := filepath.Join(t.TempDir(), "one")
	f := startNeighbours(t, gate)
	workspace := f.workspace(t, newRepo(t))
	parent := f.newSession(t, workspace)
	f.prompt(t, workspace, parent, "plan")
	parentID := f.waitStatus(t, workspace, parent, "done").AgentSessionID

	kidOne := f.newChild(t, workspace, parent, map[string]any{"branch": "one", "title": "One", "prompt": "touch-a hold-one"})
	one := kidOne.Session
	testutil.Eventually(t, func() bool { return gitOut(kidOne.Worktree, "status", "--porcelain") != "" }, wait)
	two := f.newChild(t, workspace, parent, map[string]any{"branch": "two", "title": "Two", "prompt": "touch-a"}).Session
	f.waitStatus(t, workspace, two, "done")

	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, `Child "Two"`)) == 1 }, wait)
	assert.Contains(t, f.promptsTo(t, parentID, `Child "Two"`)[0], `It changes files that open session "One" (`+one+", branch one) changes too: a.go.")
	testutil.Eventually(t, func() bool {
		return slices.Equal(f.overlapsOf(t, workspace, one), []string{two + ":a.go"}) && slices.Equal(f.overlapsOf(t, workspace, two), []string{one + ":a.go"})
	}, wait)

	write(t, gate, "")
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, `Child "One"`)) == 1 }, wait)
	assert.NotContains(t, f.promptsTo(t, parentID, `Child "One"`)[0], "It changes files")

	f.newChild(t, workspace, parent, map[string]any{"branch": "three", "title": "Three", "prompt": "touch-a"})
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, `Child "Three"`)) == 1 }, wait)
	assert.Contains(t, f.promptsTo(t, parentID, `Child "Three"`)[0], `"Two" (`+two+", branch two) changes too: a.go.")
}

func TestNeighboursLandedChildKeepsItsOverlap(t *testing.T) {
	t.Parallel()
	gates := t.TempDir()
	one, two := filepath.Join(gates, "one"), filepath.Join(gates, "two")
	f := startNeighbours(t, one, two)
	workspace := f.workspace(t, newRepo(t))
	parent := f.topLevel(t, workspace, "feat", "plan")
	parentID := f.agentID(t, workspace, parent.Session)

	kidTwo := f.newChild(t, workspace, parent.Session, map[string]any{"branch": "ktwo", "title": "Two", "prompt": "touch-a hold-two"}).Session
	kidOne := f.newChild(t, workspace, parent.Session, map[string]any{"branch": "kone", "title": "One", "prompt": "commit-a hold-one"})
	testutil.Eventually(t, func() bool { return gitOut(kidOne.Worktree, "log", "--format=%s", "-1") == "x" }, wait)
	testutil.Eventually(t, func() bool { return gitOut(f.session(t, workspace, kidTwo).Worktree, "status", "--porcelain") != "" }, wait)

	var res struct {
		Note string `json:"note"`
	}
	require.NoError(t, f.runAs(t, workspace, kidOne.Session, "git.land", map[string]any{"path": kidOne.Worktree, "into": "feat", "message": "feat: one"}, &res))
	testutil.Eventually(t, func() bool {
		_, err := os.Stat(kidOne.Worktree)
		return os.IsNotExist(err)
	}, wait)
	assert.Contains(t, res.Note, `Open session "Two" (`+kidTwo+", branch ktwo) changes a.go too; its landing can conflict with yours.")

	write(t, one, "")
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, parentID, `Child "One"`)) == 1 }, wait)
	assert.Contains(t, f.promptsTo(t, parentID, `Child "One"`)[0], `It changes files that open session "Two" (`+kidTwo+", branch ktwo) changes too: a.go.")
	write(t, two, "")
}

func TestNeighboursNoteOnLandWithoutGate(t *testing.T) {
	t.Parallel()
	f := startNeighbours(t)
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	other := f.topLevel(t, workspace, "other", "touch-a")
	lander := f.newSession(t, workspace)
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", feat)
	write(t, filepath.Join(feat, "a.go"), "feat\n")
	git(t, feat, "commit", "-qam", "feat")

	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	write(t, hook, "#!/bin/sh\n[ -f .hooked ] && exit 0\ntouch .hooked\necho hook > hooked.txt\ngit add hooked.txt\n")
	require.NoError(t, os.Chmod(hook, 0o700))
	require.ErrorContains(t, f.runAs(t, workspace, lander, "git.land", map[string]any{"path": feat, "message": "feat: a"}, nil), "commit hook changed the committed tree")

	var res struct {
		Note string `json:"note"`
	}
	require.NoError(t, f.runAs(t, workspace, lander, "git.land", map[string]any{"path": feat, "message": "feat: a"}, &res))

	assert.Equal(t, `Open session "other" (`+other.Session+", branch other) changes a.go too; its landing can conflict with yours.", res.Note)
}

func TestNeighboursBlockFromSecondPromptOnlyNewPairs(t *testing.T) {
	t.Parallel()
	f := startNeighbours(t)
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	self := f.topLevel(t, workspace, "self", "touch-a")
	other := f.topLevel(t, workspace, "other", "touch-a")
	selfID := f.agentID(t, workspace, self.Session)
	mb := git(t, repo, "rev-parse", "main")

	f.prompt(t, workspace, self.Session, "next")
	f.waitStatus(t, workspace, self.Session, "done")
	f.restartAgents(t, workspace)
	f.prompt(t, workspace, self.Session, "next")
	f.waitStatus(t, workspace, self.Session, "done")
	write(t, filepath.Join(self.Worktree, "b.go"), "s\n")
	write(t, filepath.Join(other.Worktree, "b.go"), "o\n")
	f.prompt(t, workspace, self.Session, "next")
	f.waitStatus(t, workspace, self.Session, "done")

	got := f.neighbourBlocks(t, selfID)
	require.Len(t, got, 4)
	assert.Empty(t, got[0])
	assert.Equal(t, `[Clubmate neighbours] Open sessions change files that you change: "other" (session `+other.Session+`, branch other): a.go. Before you edit these files, read their version: git -C `+other.Worktree+` diff `+mb+` -- a.go. Do not edit their worktree. If your changes clash, tell the user in your report.`, got[1])
	assert.Empty(t, got[2])
	assert.Contains(t, got[3], `"other" (session `+other.Session+`, branch other): b.go. Before you edit these files, read their version: `+other.Worktree+`/b.go (new, not committed).`)
}

func TestNeighboursSkipChainLandedGoneAndClosed(t *testing.T) {
	t.Parallel()
	f := startNeighbours(t)
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	self := f.topLevel(t, workspace, "self", "touch-a")
	kid := f.newChild(t, workspace, self.Session, map[string]any{"branch": "kid", "title": "kid", "prompt": "touch-a"}).Session
	f.waitStatus(t, workspace, kid, "done")
	selfID := f.agentID(t, workspace, self.Session)
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, selfID, "[Clubmate] Child")) == 1 }, wait)
	f.waitStatus(t, workspace, self.Session, "done")
	landed := f.topLevel(t, workspace, "landed", "commit-a")
	gone := f.topLevel(t, workspace, "gone", "touch-a")
	closed := f.topLevel(t, workspace, "closed", "touch-a")
	live := f.topLevel(t, workspace, "live", "touch-a")
	git(t, repo, "merge", "-q", "--squash", "landed")
	git(t, repo, "commit", "-qm", "landed")
	git(t, repo, "worktree", "remove", "--force", gone.Worktree)
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": closed.Session}, nil))
	f.waitStatus(t, workspace, closed.Session, "closed")

	f.prompt(t, workspace, self.Session, "next")
	f.waitStatus(t, workspace, self.Session, "done")

	got := f.neighbourBlocks(t, selfID)
	last := got[len(got)-1]
	assert.Contains(t, last, live.Session)
	for _, id := range []string{kid, landed.Session, gone.Session, closed.Session} {
		assert.NotContains(t, last, id)
	}
}
