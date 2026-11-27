package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

func TestDoneTreeClosesItselfUnlessItAsksOrOwnsJobs(t *testing.T) {
	t.Parallel()
	const after = 300 * time.Millisecond
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, map[string]any{"resume": true, "prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, newRepo(t))
	root := f.newSession(t, workspace)
	kid := f.newChild(t, workspace, root, map[string]any{"prompt": "one"}).Session
	askingKid := f.newChild(t, workspace, root, map[string]any{"prompt": "Which one?"}).Session
	testutil.Eventually(t, func() bool { return f.session(t, workspace, askingKid).Asks }, wait)
	asking := f.newSession(t, workspace)
	scheduled := f.newSession(t, workspace)
	require.NoError(t, f.runAs(t, workspace, scheduled, "cron.add", map[string]any{"name": "ci", "command": "agents.prompt", "args": `{"session":"` + scheduled + `","text":"Check CI"}`, "in": "10h"}, nil))
	f.prompt(t, workspace, root, "done")
	f.prompt(t, workspace, asking, "Ship it?")
	f.prompt(t, workspace, scheduled, "done")

	closed := f.waitStatus(t, workspace, root, "closed")
	assert.True(t, closed.AutoClosed)
	assert.Equal(t, "done", closed.ClosedStatus)
	assert.True(t, f.waitStatus(t, workspace, kid, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, askingKid, "closed").AutoClosed)
	assert.Equal(t, "clubmate", closeActor(t, f.logLines(t, root)))
	f.waitStatus(t, workspace, asking, "done")
	f.waitStatus(t, workspace, scheduled, "running")
	testutil.Never(t, func() bool {
		return f.session(t, workspace, asking).Status != "done" || f.session(t, workspace, scheduled).Status != "running"
	}, 3*after)

	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": kid}, nil))
	f.prompt(t, workspace, kid, "two")
	assert.True(t, f.waitStatus(t, workspace, kid, "closed").AutoClosed)

	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": root}, nil))
	resumed := f.session(t, workspace, root)
	assert.False(t, resumed.AutoClosed)
	assert.Empty(t, resumed.ClosedStatus)
}

func TestNodeOfStoppedRunClosesItself(t *testing.T) {
	t.Parallel()
	const after = 300 * time.Millisecond
	repo := newRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".clubmate", "graphs"), 0o755))
	write(t, filepath.Join(repo, ".clubmate", "graphs", "solo.json"), `{"title":"Solo","nodes":[{"id":"one","role":"One","start":true,"prompt":"done"},{"id":"two","role":"Two","prompt":"done"}],"edges":[{"from":"one","to":"two","on":"error"}]}`)
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, map[string]any{"prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, repo)
	type run struct {
		ID    string `json:"id"`
		Nodes []struct {
			Session string `json:"session"`
		} `json:"nodes"`
	}
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.graph-list", map[string]any{}, nil) == nil }, restartWait)
	var stopped, deleted, running run
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "solo"}, &stopped))
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "solo"}, &deleted))
	require.NoError(t, f.run(t, workspace, "agents.graph-run", map[string]any{"graph": "solo"}, &running))
	f.waitStatus(t, workspace, running.Nodes[0].Session, "done")
	f.waitStatus(t, workspace, stopped.Nodes[0].Session, "done")
	f.waitStatus(t, workspace, deleted.Nodes[0].Session, "done")
	require.NoError(t, f.run(t, workspace, "agents.graph-stop", map[string]any{"id": stopped.ID}, nil))
	require.NoError(t, f.run(t, workspace, "agents.graph-delete", map[string]any{"id": deleted.ID}, nil))

	assert.True(t, f.waitStatus(t, workspace, stopped.Nodes[0].Session, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, stopped.Nodes[1].Session, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, deleted.Nodes[0].Session, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, deleted.Nodes[1].Session, "closed").AutoClosed)
	f.waitStatus(t, workspace, running.Nodes[1].Session, "idle")
	testutil.Never(t, func() bool {
		return f.session(t, workspace, running.Nodes[0].Session).Status != "done" || f.session(t, workspace, running.Nodes[1].Session).Status != "idle"
	}, 3*after)
}

func closeActor(t *testing.T, lines []map[string]json.RawMessage) string {
	t.Helper()
	var actor string
	for _, l := range lines {
		if string(l["kind"]) == `"session/close"` {
			require.NoError(t, json.Unmarshal(l["actor"], &actor))
		}
	}
	return actor
}

func TestChangedAskSessionWaitsForReviewBeforeClosing(t *testing.T) {
	t.Parallel()
	const after = 2 * time.Second
	repo := newRepo(t)
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, map[string]any{"prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, repo)
	review := f.newSession(t, workspace)
	merge := f.newSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.config", map[string]any{"session": merge, "id": "finish", "value": "merge"}, nil))
	reviewed := func(n sdui.Node) bool {
		return slices.ContainsFunc(cardsOf(n, "button"), func(b sdui.Node) bool { return b.ID == review+"/reviewed" })
	}
	f.prompt(t, workspace, review, "one")
	f.prompt(t, workspace, merge, "one")
	f.waitStatus(t, workspace, review, "done")
	f.waitStatus(t, workspace, merge, "done")

	write(t, filepath.Join(repo, "new.txt"), "x\n")
	f.prompt(t, workspace, review, "two")
	f.prompt(t, workspace, merge, "two")

	assert.True(t, f.waitStatus(t, workspace, merge, "closed").AutoClosed)
	assert.True(t, f.session(t, workspace, review).Review)
	f.h.Event(t, workspace, "agents.sidebar", review, "select", review)
	f.h.WaitSlot(t, workspace, "agents.main", reviewed)
	testutil.Never(t, func() bool { return f.session(t, workspace, review).Status != "done" }, after)

	require.NoError(t, f.run(t, workspace, "agents.reviewed", map[string]any{"session": review}, nil))
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return !reviewed(n) })
	assert.True(t, f.waitStatus(t, workspace, review, "closed").AutoClosed)
}

const treeCloseAfter = 300 * time.Millisecond

func startDoneTree(t *testing.T, repo string) (*fixture, string, string, func()) {
	t.Helper()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + treeCloseAfter.String()}, map[string]any{"resume": true, "prompt": []map[string]any{{"echo": true}}},
		testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	workspace := f.workspace(t, repo)
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"finish": "merge"}, &out))
	root := out.Session
	var job struct {
		ID string `json:"id"`
	}
	require.NoError(t, f.runAs(t, workspace, root, "cron.add", map[string]any{"name": "hold", "command": "agents.prompt", "args": `{"session":"` + root + `","text":"hold"}`, "in": "10h"}, &job))
	require.NotEmpty(t, job.ID)
	return f, workspace, root, func() {
		t.Helper()
		require.NoError(t, f.runAs(t, workspace, root, "cron.delete", map[string]any{"id": job.ID}, nil))
	}
}

func turnEnds(t *testing.T, f *fixture, session string) int {
	t.Helper()
	n := 0
	for _, l := range f.logLines(t, session) {
		if string(l["kind"]) == `"turn_end"` {
			n++
		}
	}
	return n
}

func (f *fixture) waitTurnEnds(t *testing.T, session string, turns int) {
	t.Helper()
	testutil.Eventually(t, func() bool { return turnEnds(t, f, session) >= turns }, wait)
}

func TestDoneTreeWithAnUnlandedChildBranchStaysOpenUntilItLandsOnOrigin(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	origin := t.TempDir()
	git(t, origin, "init", "-q", "--bare", "-b", "main")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "-q", "origin", "main")
	git(t, repo, "remote", "set-head", "origin", "main")
	f, workspace, root, release := startDoneTree(t, repo)
	kid := f.newChild(t, workspace, root, map[string]any{"branch": "c1", "prompt": "work"})
	f.waitBranchChildDone(t, workspace, kid.Session)
	f.waitTurnEnds(t, root, 1)
	commitIn(t, kid.Worktree, "b.go")
	release()

	testutil.Never(t, func() bool {
		return f.session(t, workspace, root).Status == "closed" || f.session(t, workspace, kid.Session).Status == "closed"
	}, 3*treeCloseAfter)

	git(t, kid.Worktree, "push", "-q", "origin", "c1:main")
	assert.True(t, f.waitStatus(t, workspace, kid.Session, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, root, "closed").AutoClosed)
}

func TestDoneTreeStaysOpenWhileGitCannotReadAChildWorktree(t *testing.T) {
	t.Parallel()
	f, workspace, root, release := startDoneTree(t, newRepo(t))
	kid := f.newChild(t, workspace, root, map[string]any{"branch": "c1", "prompt": "work"})
	f.waitBranchChildDone(t, workspace, kid.Session)
	f.waitTurnEnds(t, root, 1)
	dotGit := filepath.Join(kid.Worktree, ".git")
	saved, err := os.ReadFile(dotGit)
	require.NoError(t, err)
	write(t, dotGit, "gitdir: "+filepath.Join(t.TempDir(), "gone")+"\n")
	release()

	testutil.Never(t, func() bool { return f.session(t, workspace, kid.Session).Status == "closed" }, 3*treeCloseAfter)

	write(t, dotGit, string(saved))
	assert.True(t, f.waitStatus(t, workspace, kid.Session, "closed").AutoClosed)
}

func TestChildWhoseClosedParentWorktreeIsGoneLandsAgainstTheParentsClosedBranch(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	f, workspace, root, _ := startDoneTree(t, repo)
	mid := f.newChild(t, workspace, root, map[string]any{"branch": "feat", "prompt": "mid"})
	f.waitBranchChildDone(t, workspace, mid.Session)
	kid := f.newChild(t, workspace, mid.Session, map[string]any{"branch": "c1", "prompt": "work"})
	f.waitBranchChildDone(t, workspace, kid.Session)
	f.waitTurnEnds(t, mid.Session, 2)
	f.waitStatus(t, workspace, mid.Session, "done")
	commitIn(t, kid.Worktree, "a.go")
	git(t, mid.Worktree, "merge", "-q", "--ff-only", "c1")
	require.NoError(t, f.run(t, workspace, "agents.close", map[string]any{"session": mid.Session}, nil))
	assert.Equal(t, "feat", f.waitStatus(t, workspace, mid.Session, "closed").ClosedBranch)
	f.waitStatus(t, workspace, kid.Session, "closed")
	git(t, repo, "worktree", "remove", "--force", mid.Worktree)
	write(t, filepath.Join(repo, "a.go"), "main\n")
	git(t, repo, "commit", "-qam", "main")
	require.NoError(t, f.run(t, workspace, "agents.resume", map[string]any{"session": kid.Session}, nil))
	f.prompt(t, workspace, kid.Session, "go")

	assert.True(t, f.waitStatus(t, workspace, kid.Session, "closed").AutoClosed)
}

func TestDoneTreeWithAChildInAFolderOutsideGitCloses(t *testing.T) {
	t.Parallel()
	f, workspace, root, release := startDoneTree(t, t.TempDir())
	kid := f.newChild(t, workspace, root, map[string]any{"prompt": "work"})
	f.waitStatus(t, workspace, kid.Session, "done")
	release()

	assert.True(t, f.waitStatus(t, workspace, kid.Session, "closed").AutoClosed)
	assert.True(t, f.waitStatus(t, workspace, root, "closed").AutoClosed)
}

func TestChildWhoseWorktreeWasRemovedStaysOpenUntilItsBranchIsGone(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	f, workspace, root, release := startDoneTree(t, repo)
	kid := f.newChild(t, workspace, root, map[string]any{"branch": "c1", "prompt": "work"})
	f.waitBranchChildDone(t, workspace, kid.Session)
	f.waitTurnEnds(t, root, 1)
	commitIn(t, kid.Worktree, "b.go")
	git(t, repo, "worktree", "remove", "--force", kid.Worktree)
	release()

	testutil.Never(t, func() bool { return f.session(t, workspace, kid.Session).Status == "closed" }, 3*treeCloseAfter)

	git(t, repo, "branch", "-D", "c1")
	assert.True(t, f.waitStatus(t, workspace, kid.Session, "closed").AutoClosed)
}

func TestChildWithNoRecordedBaseLandsAgainstTheBranchItsOpenParentsGoneWorktreeWasMadeOn(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	f, workspace, root, release := startDoneTree(t, repo)
	mid := f.newChild(t, workspace, root, map[string]any{"branch": "feat", "prompt": "mid"})
	f.waitBranchChildDone(t, workspace, mid.Session)
	git(t, mid.Worktree, "branch", "c1")
	kid := f.newChild(t, workspace, mid.Session, map[string]any{"branch": "c1", "prompt": "work"})
	f.waitBranchChildDone(t, workspace, kid.Session)
	f.waitTurnEnds(t, mid.Session, 2)
	f.waitStatus(t, workspace, mid.Session, "done")
	commitIn(t, kid.Worktree, "b.go")
	git(t, mid.Worktree, "merge", "-q", "--ff-only", "c1")
	git(t, repo, "merge", "-q", "--ff-only", "feat")
	git(t, repo, "worktree", "remove", "--force", mid.Worktree)
	release()

	assert.True(t, f.waitStatus(t, workspace, kid.Session, "closed").AutoClosed)
}
