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
	"clubmate/plugins/internal/landgate"
)

const turnText = "Your turn to land into main"

type queueSetup struct {
	f         *fixture
	repo      string
	workspace string
}

type queueLander struct {
	id       string
	worktree string
	branch   string
}

type queueView struct {
	ID       string           `json:"id"`
	Repo     string           `json:"repo"`
	Into     string           `json:"into"`
	NeedsYou bool             `json:"needsYou"`
	Entries  []queueEntryView `json:"entries"`
}

type queueEntryView struct {
	ID      string   `json:"id"`
	Session string   `json:"session"`
	Title   string   `json:"title"`
	Source  string   `json:"source"`
	Mode    string   `json:"mode"`
	State   string   `json:"state"`
	Place   int      `json:"place"`
	Files   []string `json:"files"`
	Why     string   `json:"why"`
	Skips   int      `json:"skips"`
	Marks   []struct {
		ID string `json:"id"`
	} `json:"marks"`
	NeedsYou bool `json:"needsYou"`
}

func startQueue(t *testing.T, gated bool, env ...string) queueSetup {
	t.Helper()
	return startQueueWith(t, gated, nil, env...)
}

func startQueueWith(t *testing.T, gated bool, steps func(repo string) []map[string]any, env ...string) queueSetup {
	t.Helper()
	repo := newRepo(t)
	script := []map[string]any{{"match": "Clubmate land review", "wait": filepath.Join(t.TempDir(), "release")}}
	if steps != nil {
		script = append(script, steps(repo)...)
	}
	f := startEnv(t, env, map[string]any{"prompt": script}, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
	q := queueSetup{f: f, repo: repo, workspace: f.workspace(t, repo)}
	if gated {
		require.NoError(t, f.run(t, q.workspace, "agents.gate-add", map[string]any{"repo": repo}, nil))
	}
	return q
}

func (q queueSetup) lander(t *testing.T, title, file string) queueLander {
	t.Helper()
	return q.landerWith(t, title, file, title+"\n", "merge")
}

func (q queueSetup) landerFinish(t *testing.T, title, file, finish string) queueLander {
	t.Helper()
	return q.landerWith(t, title, file, title+"\n", finish)
}

func (q queueSetup) landerWith(t *testing.T, title, file, content, finish string) queueLander {
	t.Helper()
	branch := strings.ToLower(title)
	var kid child
	require.NoError(t, q.f.run(t, q.workspace, "agents.new", map[string]any{"title": title, "branch": branch, "prompt": "work", "finish": finish}, &kid))
	q.f.waitBranchChildDone(t, q.workspace, kid.Session)
	write(t, filepath.Join(kid.Worktree, file), content)
	git(t, kid.Worktree, "add", ".")
	git(t, kid.Worktree, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "work on "+file)
	return queueLander{id: kid.Session, worktree: kid.Worktree, branch: branch}
}

func (q queueSetup) land(t *testing.T, l queueLander) error {
	t.Helper()
	return q.f.runAs(t, q.workspace, l.id, "git.land", map[string]any{"path": l.worktree, "message": "feat: " + l.branch}, nil)
}

func (q queueSetup) landIndex(t *testing.T, session string) error {
	t.Helper()
	return q.f.runAs(t, q.workspace, session, "git.land", map[string]any{"path": q.repo, "mode": "index", "message": "feat: staged"}, nil)
}

func (q queueSetup) queue(t *testing.T) queueView {
	t.Helper()
	var out struct {
		Queues []queueView `json:"queues"`
	}
	require.NoError(t, q.f.run(t, q.workspace, "agents.land-queue", map[string]any{}, &out))
	if len(out.Queues) == 0 {
		return queueView{}
	}
	return out.Queues[0]
}

func (f *fixture) clearQueues(t *testing.T, workspace string) {
	t.Helper()
	var out struct {
		Queues []queueView `json:"queues"`
	}
	require.NoError(t, f.run(t, workspace, "agents.land-queue", map[string]any{}, &out))
	for _, qv := range out.Queues {
		for _, e := range qv.Entries {
			require.NoError(t, f.run(t, workspace, "agents.land-queue-remove", map[string]any{"queue": qv.ID, "entry": e.ID}, nil))
		}
	}
}

func (q queueSetup) order(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, e := range q.queue(t).Entries {
		out = append(out, e.Session)
	}
	return out
}

func (q queueSetup) turns(t *testing.T, l queueLander) []string {
	t.Helper()
	agent := q.f.session(t, q.workspace, l.id).AgentSessionID
	if agent == "" {
		return nil
	}
	return q.f.promptsTo(t, agent, turnText)
}

func (q queueSetup) waitTurn(t *testing.T, l queueLander) string {
	t.Helper()
	head := git(t, q.repo, "rev-parse", "main")
	testutil.Eventually(t, func() bool {
		turns := q.turns(t, l)
		return len(turns) == 1 && strings.Contains(turns[0], "Its head is now "+head)
	}, wait)
	return head
}

func (q queueSetup) approve(t *testing.T, l queueLander) {
	t.Helper()
	q.approveOn(t, l, "")
}

func (q queueSetup) approveOn(t *testing.T, l queueLander, tree string) {
	t.Helper()
	q.verdictOn(t, l, tree, "approved")
}

func (q queueSetup) verdictOn(t *testing.T, l queueLander, tree, outcome string, findings ...string) {
	t.Helper()
	var r record
	testutil.Eventually(t, func() bool {
		var list []record
		require.NoError(t, q.f.run(t, q.workspace, "agents.sessions", map[string]any{}, &list))
		i := slices.IndexFunc(list, func(r record) bool {
			return r.GateFor != nil && r.GateFor.Lander == l.id && r.GateFor.Verdict && r.Status != "closed" && (tree == "" || r.GateFor.Tree == tree)
		})
		if i >= 0 {
			r = list[i]
		}
		return i >= 0
	}, wait)
	require.NoError(t, q.f.runAs(t, q.workspace, r.ID, "agents.land-verdict", map[string]any{"tree": r.GateFor.Tree, "outcome": outcome, "findings": findings}, nil))
}

func (q queueSetup) toldReview(t *testing.T, l queueLander, what string) int {
	t.Helper()
	return len(q.f.promptsTo(t, q.f.session(t, q.workspace, l.id).AgentSessionID, "Land review of "+l.branch+" → main"+what))
}

func (q queueSetup) speculate(t *testing.T) {
	t.Helper()
	require.NoError(t, q.f.h.Client(t).Call(t.Context(), "ui.event", map[string]any{"workspaceId": "", "slot": "agents.settings", "nodeId": "land-queue-speculate", "action": "land-queue-speculate", "value": true}, nil))
}

func (e queueEntryView) ahead() bool {
	return slices.ContainsFunc(e.Marks, func(m struct {
		ID string `json:"id"`
	}) bool {
		return m.ID == "ahead"
	})
}

func (q queueSetup) reviewedAhead(t *testing.T, l queueLander) bool {
	t.Helper()
	var list []record
	require.NoError(t, q.f.run(t, q.workspace, "agents.sessions", map[string]any{}, &list))
	return slices.ContainsFunc(list, func(r record) bool { return r.GateFor != nil && r.GateFor.Lander == l.id })
}

func (q queueSetup) stage(t *testing.T, file string) {
	t.Helper()
	write(t, filepath.Join(q.repo, file), file+"\n")
	git(t, q.repo, "add", file)
}

func (q queueSetup) mark(t *testing.T) string {
	t.Helper()
	common, err := filepath.EvalSymlinks(filepath.Join(q.repo, ".git"))
	require.NoError(t, err)
	return filepath.Join(landgate.QueueDir(filepath.Join(q.f.h.Dirs.PluginState("agents"), "githooks"), common), "refs", "heads", "main")
}

func (q queueSetup) headBlocked(t *testing.T, a queueLander) {
	t.Helper()
	write(t, filepath.Join(q.repo, "a.go"), "dirty\n")
	err := q.land(t, a)
	require.ErrorContains(t, err, "queued: place 2 of 2 into main")
	entries := q.queue(t).Entries
	require.Len(t, entries, 2)
	assert.Equal(t, "dirty", entries[0].Mode)
	assert.Equal(t, a.id, entries[1].Session)
	assert.Equal(t, "blocked", entries[1].State)
	assert.Contains(t, entries[1].Why, "a.go")
}

func TestSecondLanderIsQueuedBehindHead(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")

	err := q.land(t, b)

	require.ErrorContains(t, err, `queued: place 2 of 2 into main; ahead: "A" (a). End your turn; Clubmate prompts you when it is your turn.`)
}

func TestQueueWithoutGateStillOrders(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	q.stage(t, "c.go")
	require.ErrorContains(t, q.land(t, a), "staged changes")
	require.ErrorContains(t, q.land(t, b), `queued: place 2 of 2 into main; ahead: "A" (a)`)
	git(t, q.repo, "reset", "-q")

	require.NoError(t, q.land(t, a))

	q.waitTurn(t, b)
	assert.Equal(t, []string{b.id}, q.order(t))
}

func TestHeadLandingPromptsNext(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")
	q.approve(t, a)

	require.NoError(t, q.land(t, a))

	q.waitTurn(t, b)
	require.ErrorContains(t, q.land(t, b), "review pending")
}

func TestClosedHeadLeavesQueue(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")

	q.f.close(t, q.workspace, a.id)

	q.waitTurn(t, b)
	assert.Equal(t, []string{b.id}, q.order(t))
	testutil.Never(t, func() bool { return len(q.turns(t, b)) > 1 }, time.Second)
}

func TestQueuedTopLevelSessionStaysOpenUntilItLeavesTheQueue(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	script := []map[string]any{{"match": "Clubmate land review", "wait": filepath.Join(t.TempDir(), "release")}}
	after := testutil.Timeout(time.Second)
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=" + after.String()}, map[string]any{"prompt": script},
		testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	q := queueSetup{f: f, repo: repo, workspace: f.workspace(t, repo)}
	require.NoError(t, f.run(t, q.workspace, "agents.gate-add", map[string]any{"repo": repo}, nil))
	a := q.lander(t, "A", "x.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	b := q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, b), "queued")

	testutil.Never(t, func() bool { return f.session(t, q.workspace, b.id).Status == "closed" }, 3*after)

	entry := q.queue(t).Entries[1]
	require.Equal(t, b.id, entry.Session)
	require.NoError(t, f.run(t, q.workspace, "agents.land-queue-remove", map[string]any{"queue": q.queue(t).ID, "entry": entry.ID}, nil))
	assert.True(t, f.waitStatus(t, q.workspace, b.id, "closed").AutoClosed)
}

func TestSilentHeadIsPassedAfterStuckTime(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	q := startQueueWith(t, false, func(string) []map[string]any {
		return []map[string]any{{"match": "hold", "wait": hold}}
	}, "CLUBMATE_AGENTS_QUEUE_LOOP=50ms", "CLUBMATE_AGENTS_QUEUE_STUCK="+testutil.Timeout(time.Second).String())
	h, a, b := q.lander(t, "H", "h.go"), q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	q.f.prompt(t, q.workspace, h.id, "hold")
	q.f.waitStatus(t, q.workspace, h.id, "running")
	q.stage(t, "c.go")
	require.ErrorContains(t, q.land(t, h), "staged changes")
	require.ErrorContains(t, q.land(t, a), "queued")
	require.ErrorContains(t, q.land(t, b), "queued")
	git(t, q.repo, "reset", "-q")
	require.NoError(t, q.land(t, h))
	q.waitTurn(t, a)

	q.waitTurn(t, b)

	entries := q.queue(t).Entries
	require.Len(t, entries, 2)
	assert.Equal(t, []string{b.id, a.id}, []string{entries[0].Session, entries[1].Session})
	assert.Equal(t, 1, entries[1].Skips)
}

func TestHeadUnderReviewIsNotPassed(t *testing.T) {
	t.Parallel()
	stuck := testutil.Timeout(time.Second)
	q := startQueue(t, true, "CLUBMATE_AGENTS_QUEUE_LOOP=50ms", "CLUBMATE_AGENTS_QUEUE_STUCK="+stuck.String())
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")

	require.ErrorContains(t, q.land(t, b), "queued")
	testutil.Never(t, func() bool { return !slices.Equal(q.order(t), []string{a.id, b.id}) }, 3*stuck)
	q.approve(t, a)

	q.f.restartPlugin(t, q.workspace)

	testutil.Never(t, func() bool { return !slices.Equal(q.order(t), []string{a.id, b.id}) }, stuck/2)
	assert.Equal(t, "ready", q.queue(t).Entries[1].State)
}

func TestQueueIntoRemovedBranchDrains(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true, "CLUBMATE_AGENTS_QUEUE_LOOP=50ms")
	a := q.lander(t, "A", "x.go")
	rel := filepath.Join(q.repo, ".worktrees", "rel")
	git(t, q.repo, "worktree", "add", "-q", "-b", "rel", rel, "main")
	require.ErrorContains(t, q.f.runAs(t, q.workspace, a.id, "git.land", map[string]any{"path": a.worktree, "into": "rel", "message": "feat: a"}, nil), "review pending")
	require.Equal(t, "rel", q.queue(t).Into)

	git(t, q.repo, "worktree", "remove", "--force", rel)
	git(t, q.repo, "branch", "-D", "rel")

	testutil.Eventually(t, func() bool { return q.queue(t).ID == "" }, wait)
	agent := q.f.session(t, q.workspace, a.id).AgentSessionID
	testutil.Eventually(t, func() bool {
		return len(q.f.promptsTo(t, agent, "into rel was dropped because its target is gone")) == 1
	}, wait)
}

func TestEntryWithNothingToLandLeaves(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true, "CLUBMATE_AGENTS_QUEUE_LOOP=50ms")
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")

	git(t, b.worktree, "reset", "-q", "--hard", "main")

	testutil.Eventually(t, func() bool { return slices.Equal(q.order(t), []string{a.id}) }, wait)
}

func TestLandingIntoAnotherBranchLeavesTheFirstQueue(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")
	git(t, q.repo, "worktree", "add", "-q", "-b", "rel", filepath.Join(q.repo, ".worktrees", "rel"), "main")

	require.ErrorContains(t, q.f.runAs(t, q.workspace, b.id, "git.land", map[string]any{"path": b.worktree, "into": "rel", "message": "feat: b"}, nil), "review pending")

	var out struct {
		Queues []queueView `json:"queues"`
	}
	require.NoError(t, q.f.run(t, q.workspace, "agents.land-queue", map[string]any{}, &out))
	byInto := map[string][]string{}
	for _, qv := range out.Queues {
		for _, e := range qv.Entries {
			byInto[qv.Into] = append(byInto[qv.Into], e.Session)
		}
	}
	assert.Equal(t, map[string][]string{"main": {a.id}, "rel": {b.id}}, byInto)
}

func TestUserLandingMovesHeadAndPrompts(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true, "CLUBMATE_AGENTS_QUEUE_LOOP=50ms")
	a := q.lander(t, "A", "x.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	user := filepath.Join(q.repo, ".worktrees", "user")
	git(t, q.repo, "worktree", "add", "-q", "-b", "user", user)
	write(t, filepath.Join(user, "u.go"), "user\n")
	git(t, user, "add", ".")
	git(t, user, "commit", "-qm", "user work")

	require.NoError(t, q.f.run(t, q.workspace, "git.land", map[string]any{"path": user, "message": "feat: user"}, nil))

	q.waitTurn(t, a)
	assert.Equal(t, []string{a.id}, q.order(t))
}

func TestSecondIndexEntryRefused(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	x, y := q.f.newSession(t, q.workspace), q.f.newSession(t, q.workspace)
	q.stage(t, "d.go")
	require.ErrorContains(t, q.landIndex(t, x), "review pending")
	q.stage(t, "e.go")

	err := q.landIndex(t, y)

	require.ErrorContains(t, err, "another session's change is staged in "+q.repo+"; wait for it to land, then stage yours")
	assert.Equal(t, []string{x}, q.order(t))
}

func TestIndexEntryGoesFirst(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a := q.lander(t, "A", "x.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	c := q.f.newSession(t, q.workspace)
	q.stage(t, "d.go")

	require.ErrorContains(t, q.landIndex(t, c), "review pending")

	entries := q.queue(t).Entries
	require.Len(t, entries, 2)
	assert.Equal(t, []string{c, a.id}, []string{entries[0].Session, entries[1].Session})
	assert.Equal(t, []string{"index", "head"}, []string{entries[0].Mode, entries[0].State})
	assert.Equal(t, 2, entries[1].Place)
	move := map[string]any{"queue": q.queue(t).ID, "entry": entries[1].ID, "to": 1}
	require.ErrorContains(t, q.f.run(t, q.workspace, "agents.land-queue-move", move, nil), "stay before branch entries")
}

func TestQueueSurvivesRestart(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false, "CLUBMATE_AGENTS_QUEUE_LOOP=50ms")
	h, a, b, c := q.lander(t, "H", "h.go"), q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go"), q.lander(t, "C", "z.go")
	q.stage(t, "s.go")
	require.ErrorContains(t, q.land(t, h), "staged changes")
	for _, l := range []queueLander{a, b, c} {
		require.ErrorContains(t, q.land(t, l), "queued")
	}
	git(t, q.repo, "reset", "-q")
	require.NoError(t, q.land(t, h))
	q.waitTurn(t, a)
	before := q.queue(t).Entries

	q.f.restartPlugin(t, q.workspace)

	assert.Equal(t, before, q.queue(t).Entries)
	assert.FileExists(t, q.mark(t))
	testutil.Never(t, func() bool { return len(q.turns(t, a)) > 1 }, time.Second)
	assert.Equal(t, []string{a.id, b.id, c.id}, q.order(t))
}

func TestQueueMoveIsUserOnly(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")
	qv := q.queue(t)
	move := map[string]any{"queue": qv.ID, "entry": qv.Entries[1].ID, "to": 1}

	require.ErrorContains(t, q.f.runFrom(t, q.workspace, a.id, "agents.land-queue-move", move, nil), "only the user")
	require.NoError(t, q.f.run(t, q.workspace, "agents.land-queue-move", move, nil))

	assert.Equal(t, []string{b.id, a.id}, q.order(t))
}

func TestMirrorFileTracksQueue(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a := q.lander(t, "A", "x.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	assert.FileExists(t, q.mark(t))
	q.approve(t, a)

	require.NoError(t, q.land(t, a))

	_, err := os.Stat(q.mark(t))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDirtyOverlapBlocksHeadAndNamesOwner(t *testing.T) {
	t.Parallel()
	q := startQueueWith(t, false, func(repo string) []map[string]any {
		edit := map[string]any{"sessionUpdate": "tool_call", "toolCallId": "e1", "title": "Edit a.go", "kind": "edit", "status": "completed", "locations": []map[string]any{{"path": filepath.Join(repo, "a.go")}}}
		return []map[string]any{{"match": "edit a.go", "update": edit}}
	})
	var s child
	require.NoError(t, q.f.run(t, q.workspace, "agents.new", map[string]any{"title": "S", "prompt": "edit a.go"}, &s))
	q.f.waitStatus(t, q.workspace, s.Session, "done")
	a := q.lander(t, "A", "a.go")

	q.headBlocked(t, a)

	assert.Equal(t, s.Session, q.queue(t).Entries[0].Session)
	agent := q.f.session(t, q.workspace, s.Session).AgentSessionID
	testutil.Eventually(t, func() bool { return len(q.f.promptsTo(t, agent, "git_land with mode index")) == 1 }, wait)
	assert.False(t, q.queue(t).NeedsYou)
}

func TestHumanDirtyOverlapNeedsYou(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	a := q.lander(t, "A", "a.go")

	q.headBlocked(t, a)

	qv := q.queue(t)
	assert.Empty(t, qv.Entries[0].Session)
	assert.True(t, qv.Entries[0].NeedsYou)
	assert.True(t, qv.NeedsYou)
	remove := map[string]any{"queue": qv.ID, "entry": qv.Entries[0].ID}
	require.ErrorContains(t, q.f.run(t, q.workspace, "agents.land-queue-remove", remove, nil), "stays until its files are committed")
}

func TestDisjointDirtyFilesDoNotBlock(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	a := q.lander(t, "A", "a.go")
	write(t, filepath.Join(q.repo, "c.go"), "dirty\n")

	require.NoError(t, q.land(t, a))

	assert.Equal(t, "A", git(t, q.repo, "show", "main:a.go"))
}

func TestEntryWaitIsLogged(t *testing.T) {
	t.Parallel()
	q := startQueue(t, false)
	h, b := q.lander(t, "H", "h.go"), q.lander(t, "B", "y.go")
	q.stage(t, "c.go")
	require.ErrorContains(t, q.land(t, h), "staged changes")
	require.ErrorContains(t, q.land(t, b), "queued")
	git(t, q.repo, "reset", "-q")
	require.NoError(t, q.land(t, h))
	q.waitTurn(t, b)

	require.NoError(t, q.land(t, b))

	row := q.f.outcome(t, q.workspace, b.id, func(r outcomeRow) bool { return r.Landing != nil })
	assert.Positive(t, row.Landing.QueueWait)
}

func TestSpeculativeVerdictLandsAfterHead(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	q.speculate(t)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")
	q.approve(t, a)
	q.approve(t, b)
	testutil.Eventually(t, func() bool { e := q.queue(t).Entries; return len(e) == 2 && e[1].ahead() }, wait)
	assert.Zero(t, q.toldReview(t, b, ": approved"))
	q.f.restartPlugin(t, q.workspace)
	require.True(t, q.queue(t).Entries[1].ahead())

	require.NoError(t, q.land(t, a))

	testutil.Eventually(t, func() bool { return slices.Equal(q.order(t), []string{b.id}) }, wait)
	require.NoError(t, q.land(t, b))
	assert.Equal(t, "B", git(t, q.repo, "show", "main:y.go"))
}

func TestSpeculationVoidWhenHeadChanges(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	q.speculate(t)
	write(t, filepath.Join(q.repo, "s.go"), "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	git(t, q.repo, "add", ".")
	git(t, q.repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "s")
	a := q.landerWith(t, "A", "s.go", "one\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "merge")
	b := q.landerWith(t, "B", "s.go", "1\n2\n3\n4\n5\n6\n7\n8\n9\nten\n", "merge")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")
	q.approve(t, b)
	qv := q.queue(t)
	require.NoError(t, q.f.run(t, q.workspace, "agents.land-queue-remove", map[string]any{"queue": qv.ID, "entry": qv.Entries[0].ID}, nil))
	q.waitTurn(t, b)

	require.ErrorContains(t, q.land(t, b), "review pending")

	q.approveOn(t, b, git(t, q.repo, "merge-tree", "--write-tree", "main", "b"))
	gate := q.f.gates(t, q.workspace)[0].ID
	var h gateHistory
	require.NoError(t, q.f.run(t, q.workspace, "agents.gate-verdicts", map[string]any{"id": gate}, &h))
	last := h.Verdicts[0]
	assert.Equal(t, [3]string{b.id, "reviewer", "base-moved"}, [3]string{last.Lander, last.By, last.Reason})
	require.NoError(t, q.land(t, b))
}

func TestSpeculationOffByDefault(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")

	require.ErrorContains(t, q.land(t, b), "queued")

	testutil.Never(t, func() bool { return q.reviewedAhead(t, b) }, time.Second)
	assert.False(t, q.queue(t).Entries[1].ahead())
}

func TestHeadWithOpenReviewAheadGetsItsVerdict(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	q.speculate(t)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")
	testutil.Eventually(t, func() bool { return q.reviewedAhead(t, b) }, wait)
	q.approve(t, a)
	require.NoError(t, q.land(t, a))
	q.waitTurn(t, b)
	require.Error(t, q.land(t, b))

	q.approve(t, b)

	testutil.Eventually(t, func() bool { return q.toldReview(t, b, ": approved") == 1 }, wait)
	require.NoError(t, q.land(t, b))
}

func TestChangesAheadAreSentAtOnce(t *testing.T) {
	t.Parallel()
	q := startQueue(t, true)
	q.speculate(t)
	a, b := q.lander(t, "A", "x.go"), q.lander(t, "B", "y.go")
	require.ErrorContains(t, q.land(t, a), "review pending")
	require.ErrorContains(t, q.land(t, b), "queued")

	q.verdictOn(t, b, "", "changes", "y.go:1 — breaks")

	testutil.Eventually(t, func() bool { return q.toldReview(t, b, " asks for changes") == 1 }, wait)
}
