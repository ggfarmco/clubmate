package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/api"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

func TestDoneGateMatchesSessionKind(t *testing.T) {
	t.Parallel()
	gates := []gateConfig{{ID: "gt_done", Repo: "/r", Point: gatePointTurn, Sessions: doneSessionsDefault}}
	held := map[string]record{
		"top":       {ID: "a", Worktree: "/r"},
		"child":     {ID: "b", Worktree: "/r", lineage: lineage{ParentSession: "a"}},
		"afk child": {ID: "c", Worktree: "/r", lineage: lineage{ParentSession: "a"}, AFK: true},
		"afk top":   {ID: "d", Worktree: "/r", AFK: true},
	}

	got := map[string]bool{}
	for name, rec := range held {
		got[name] = doneGate(gates, "/r", rec, nil) != nil
	}

	assert.Equal(t, map[string]bool{"top": true, "child": false, "afk child": false, "afk top": true}, got)
}

func TestDoneGateNeverHoldsGateOrRunNodes(t *testing.T) {
	t.Parallel()
	gates := []gateConfig{{ID: "gt_done", Repo: "/r", Point: gatePointTurn, Sessions: []string{doneTop, doneChild, doneAFK}}}
	never := []record{
		{ID: "gate", Worktree: "/r", GateFor: &gateFor{Lander: "x"}},
		{ID: "node", Worktree: "/r", Run: &api.Run{}},
		{ID: "night", Worktree: "/r", Night: "n1"},
		{ID: "service", Worktree: "/r", SystemPrompt: "you are a voice"},
		{ID: "aside", Worktree: "/r", lineage: lineage{ParentSession: "a"}, Aside: &asideInfo{}},
	}

	for _, rec := range never {
		assert.Nil(t, doneGate(gates, "/r", rec, nil), rec.ID)
	}
}

func TestGateSinceKeptOnlyWhereADoneGateHoldsTheKind(t *testing.T) {
	t.Parallel()
	top := record{ID: "a", Worktree: "/r"}
	child := record{ID: "b", Worktree: "/r", lineage: lineage{ParentSession: "a"}}
	gates := []gateConfig{
		{ID: "gt_land", Repo: "/r", Point: gatePointLand, Into: gateIntoAny},
		{ID: "gt_paused", Repo: "/r", Point: gatePointTurn, Sessions: []string{doneChild}, Paused: true},
		{ID: "gt_done", Repo: "/r", Point: gatePointTurn, Sessions: doneSessionsDefault},
	}

	assert.True(t, doneGated(gates, "/r", top))
	assert.False(t, doneGated(gates, "/r", child))
	assert.False(t, doneGated(gates, "/other", top))
}

func TestLandGateAddTakesAnEmptyDoneMatch(t *testing.T) {
	t.Parallel()
	g, err := gateMatchOf(gateAddArgs{Sessions: []string{}, Wrote: []string{}})

	require.NoError(t, err)
	assert.Equal(t, []string{gatePointLand, gateIntoAny}, []string{g.Point, g.Into})
}

func TestDoneGateWroteMatchesThroughSymlink(t *testing.T) {
	t.Parallel()
	root := unitRepo(t)
	notes := unitRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(notes, "superpowers", "plans"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o700))
	require.NoError(t, os.Symlink(filepath.Join(notes, "superpowers"), filepath.Join(root, "docs", "superpowers")))
	sha, err := checkpoint(t.Context(), root, "s1", 1)
	require.NoError(t, err)
	plan := filepath.Join(root, "docs", "superpowers", "plans", "x.md")
	require.NoError(t, os.WriteFile(plan, []byte("plan\n"), 0o600))
	s := unitSession(t, record{ID: "s1", Worktree: root, GateSince: sha}, cpLine(1, sha), editLine(1, "w1", plan))
	anyTurn := gateConfig{ID: "gt_any", Repo: root, Point: gatePointTurn, Sessions: doneSessionsDefault}
	plans := gateConfig{ID: "gt_plans", Repo: root, Point: gatePointTurn, Sessions: doneSessionsDefault, Wrote: []string{"**/plans/*.md"}}
	specs := gateConfig{ID: "gt_specs", Repo: root, Point: gatePointTurn, Sessions: doneSessionsDefault, Wrote: []string{"**/specs/*.md"}}

	wrote := (&app{}).turnWrote(t.Context(), s, snapTree(t, root))

	assert.Equal(t, []string{filepath.Join(notes, "superpowers", "plans", "x.md")}, wrote)
	assert.Equal(t, "gt_plans", doneGate([]gateConfig{anyTurn, specs, plans}, root, s.snapshot(), wrote).ID)
	assert.Nil(t, doneGate([]gateConfig{specs}, root, s.snapshot(), wrote))
}

func TestTurnWroteSpansTurnsSinceRelease(t *testing.T) {
	t.Parallel()
	root := unitRepo(t)
	old := filepath.Join(root, "old.md")
	require.NoError(t, os.WriteFile(old, []byte("old\n"), 0o600))
	before, err := checkpoint(t.Context(), root, "s1", 1)
	require.NoError(t, err)
	since, err := checkpoint(t.Context(), root, "s1", 2)
	require.NoError(t, err)
	plan := filepath.Join(root, "plans", "a.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(plan), 0o700))
	require.NoError(t, os.WriteFile(plan, []byte("plan\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "scratch.txt"), []byte("x\n"), 0o600))
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	spec := filepath.Join(outside, "specs", "b.md")
	s := unitSession(t, record{ID: "s1", Worktree: root, GateSince: since},
		cpLine(1, before), editLine(1, "w0", old), `{"turn":1,"kind":"turn_end","payload":{"stopReason":"end_turn"}}`,
		cpLine(2, since), editLine(2, "w1", plan), `{"turn":2,"kind":"turn_end","payload":{"error":"boom"}}`,
		cpLine(3, since),
		`{"turn":3,"kind":"session/update","payload":{"sessionUpdate":"subagent_update","sessionId":"sub-1","title":"spec","state":{"state":"running"}}}`,
		`{"turn":3,"agentSession":"sub-1",`+strings.TrimPrefix(editLine(3, "w2", spec), `{"turn":3,`),
		`{"turn":3,"kind":"turn_end","payload":{"stopReason":"end_turn"}}`,
	)
	want := []string{plan, filepath.Join(root, "scratch.txt"), spec}
	slices.Sort(want)

	assert.Equal(t, want, (&app{}).turnWrote(t.Context(), s, snapTree(t, root)))
}

func TestTurnWroteAfterAReviewedDoneSkipsItsTurnsEarlierEdits(t *testing.T) {
	t.Parallel()
	root := unitRepo(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	reviewed, later := filepath.Join(outside, "reviewed.md"), filepath.Join(outside, "later.md")
	cp, err := checkpoint(t.Context(), root, "s1", 1)
	require.NoError(t, err)
	base, err := reviewedCheckpoint(t.Context(), record{ID: "s1", Worktree: root}, snapTree(t, root))
	require.NoError(t, err)
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	stamped := func(line, ts string) string { return `{"ts":"` + ts + `",` + strings.TrimPrefix(line, "{") }
	s := unitSession(t, record{ID: "s1", Worktree: root, GateSince: base, GateAt: at},
		stamped(cpLine(1, cp), "2026-11-17T11:00:00Z"), stamped(editLine(1, "w1", reviewed), "2026-11-17T11:30:00Z"),
		stamped(editLine(1, "w2", later), "2026-11-17T12:30:00Z"))

	assert.Equal(t, []string{later}, (&app{}).turnWrote(t.Context(), s, snapTree(t, root)))
}

func TestAcceptedHoldCountsEditsFromWhenItsTreeWasTaken(t *testing.T) {
	t.Parallel()
	root := unitRepo(t)
	tree := snapTree(t, root)
	taken := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

	base := (&app{}).releasedBase(t.Context(), record{ID: "s1", Worktree: root}, heldTurn{Tree: tree, TreeAt: taken, Since: taken.Add(-time.Hour)}, landgate.ByUser)

	require.NotNil(t, base)
	got, err := gitrepo.TreeOf(t.Context(), root, base.since)
	require.NoError(t, err)
	assert.Equal(t, []any{tree, taken}, []any{got, base.at})
}

func snapTree(t *testing.T, root string) string {
	t.Helper()
	tree, _, err := gitrepo.Snapshot(t.Context(), root)
	require.NoError(t, err)
	return tree
}

func unitRepo(t *testing.T) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	return repo
}

func unitSession(t *testing.T, rec record, lines ...string) *session {
	t.Helper()
	path := filepath.Join(t.TempDir(), rec.ID+".jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return newSession(rec, newEventLog(path), false, func(string, bool) {})
}

func cpLine(turn int, sha string) string {
	n := strconv.Itoa(turn)
	return `{"turn":` + n + `,"kind":"checkpoint","payload":{"turn":` + n + `,"sha":"` + sha + `"}}`
}

func editLine(turn int, id, path string) string {
	return `{"turn":` + strconv.Itoa(turn) + `,"kind":"session/update","payload":{"sessionUpdate":"tool_call","toolCallId":"` + id + `","kind":"edit","status":"completed","locations":[{"path":"` + path + `"}]}}`
}

func TestDoneGateHoldsWhenTheWorktreeCannotBeSnapshotted(t *testing.T) {
	t.Parallel()
	plans := gateConfig{ID: "gt_plans", Repo: "/r", Point: gatePointTurn, Sessions: doneSessionsDefault, Wrote: []string{"**/plans/*.md"}}
	rec := record{ID: "a", Worktree: "/r"}

	assert.Nil(t, heldGate([]gateConfig{plans}, "/r", rec, nil, true))
	assert.Equal(t, "gt_plans", heldGate([]gateConfig{plans}, "/r", rec, nil, false).ID)
}

func TestUnmovedHoldKeepsItsPointer(t *testing.T) {
	t.Parallel()
	prior := &heldTurn{Gate: "gt_a", Key: "k", Tree: "t", Run: "gr_a", Round: 1}

	assert.Same(t, prior, nextHold(prior, "gt_a", "k", "t", time.Now()))
}

func TestHeldCardTitleAfterTheThirdKeyMove(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "The files kept changing during Fable review", heldCardTitle(heldTurn{Graph: "Fable review", Moves: heldMovesMax}))
}

func TestUnreadHoldKeepsItsGateAfterARestart(t *testing.T) {
	t.Parallel()
	repo := unitRepo(t)
	dir := t.TempDir()
	(&app{stateDir: dir}).keepLastGates([]gateConfig{{ID: "gt_done", Repo: repo, Point: gatePointTurn, Sessions: doneSessionsDefault}})
	rec := record{ID: "a", Worktree: repo}
	down := errors.New("storage down")
	gateOf := func(p donePlan) string {
		if p.held == nil {
			return ""
		}
		return p.held.Gate
	}

	restarted := &app{stateDir: dir}
	unknown := &app{stateDir: t.TempDir()}
	tried := []time.Time{time.Now(), time.Now()}

	assert.Equal(t, "gt_done", gateOf(restarted.unreadPlan(t.Context(), nil, rec, restarted.lastGatesRead(), false, down)))
	kept := unknown.unreadPlan(t.Context(), &heldTurn{Gate: "gt_old", Round: 1, Unread: true, Tried: tried}, rec, unknown.lastGatesRead(), false, down).held
	require.NotNil(t, kept)
	assert.Equal(t, []string{"gt_old", "", ""}, []string{kept.Gate, kept.Graph, kept.Name})
	assert.NotEmpty(t, kept.Request)
}

func TestUnreadHoldOnlyUnderAGateOfTheSessionsRepo(t *testing.T) {
	t.Parallel()
	gated, other := unitRepo(t), unitRepo(t)
	linked := filepath.Join(t.TempDir(), "wt")
	out, err := exec.Command("git", "-C", gated, "worktree", "add", "-q", "-b", "feat", linked).CombinedOutput()
	require.NoError(t, err, string(out))
	gates := []gateConfig{{ID: "gt_done", Repo: gated, Point: gatePointTurn, Sessions: doneSessionsDefault}}
	bad := errors.New("not a git repository")
	a := &app{}

	forged := filepath.Join(t.TempDir(), "mysub")
	require.NoError(t, os.MkdirAll(forged, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(forged, ".git"), []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0o600))

	assert.Nil(t, a.unreadPlan(t.Context(), nil, record{ID: "a", Worktree: other}, &gates, true, bad).held)
	h := a.unreadPlan(t.Context(), nil, record{ID: "b", Worktree: linked}, &gates, true, bad).held
	require.NotNil(t, h)
	assert.Equal(t, []string{"gt_done", filepath.Base(gated)}, []string{h.Gate, h.ReadRepo})
	h = a.unreadPlan(t.Context(), nil, record{ID: "c", Worktree: forged}, &gates, true, bad).held
	require.NotNil(t, h)
	assert.Equal(t, "mysub", h.ReadRepo)
	assert.Nil(t, a.unreadPlan(t.Context(), nil, record{ID: "d", Worktree: t.TempDir()}, &gates, false, bad).held)
}

func TestOnlyAnErrorKeepsTheGateOfAHoldingTurn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status string
		held   *heldTurn
		want   string
	}{
		{"error", statusError, &heldTurn{Gate: "gt_1"}, "gt_1"},
		{"stop", statusIdle, &heldTurn{Gate: "gt_1"}, ""},
		{"fix turn", statusError, &heldTurn{Gate: "gt_1", Last: gateChanges}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSession(record{ID: "s", Status: statusRunning, Held: tc.held}, nil, false, func(string, bool) {})

			s.setStatusLocked(tc.status, "")

			assert.Equal(t, tc.want, s.rec.HeldGate)
			assert.Nil(t, s.rec.Held)
		})
	}
}

func TestDeletedGateReleasesItsHoldWithoutReadingTheGatesAgain(t *testing.T) {
	t.Parallel()
	h := scriptedHost(t, func(method, _ string) any {
		if method == "storage.get" {
			return map[string]any{"found": true, "value": "not a gate list"}
		}
		return nil
	})
	_, err := (&app{host: h}).gates(t.Context())
	require.Error(t, err)
	s := newSession(record{ID: "agt_1", Workspace: "ws", Status: statusRunning, Held: &heldTurn{Gate: "gt_1", Key: "k"}}, newEventLog(filepath.Join(t.TempDir(), "s.jsonl")), true, func(string, bool) {})
	s.turned = func(api.Turn) {}
	a := &app{host: h, notes: newNotifier(h, ""), sessions: map[string]*session{"agt_1": s}}

	a.regateHolds(t.Context(), nil, "gt_1", byGateDeleted)

	snap := s.snapshot()
	assert.Nil(t, snap.Held)
	assert.Equal(t, statusDone, snap.Status)
}

func TestFinishReplansOnlyWhenTheCarriedGateChangedUnderAHoldOrCarry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		plan donePlan
		want bool
	}{
		{"hold", donePlan{seen: "gt_1", held: &heldTurn{Gate: "gt_1"}}, true},
		{"cancelled", donePlan{seen: "gt_1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSession(record{ID: "s", Status: statusRunning, HeldGate: "gt_1"}, nil, false, func(string, bool) {})
			s.rec.HeldGate = ""

			held, stale := s.finishLocked(tc.plan)

			assert.Equal(t, tc.want, stale)
			assert.Equal(t, tc.want, held)
			assert.Nil(t, s.rec.Held)
			if tc.want {
				assert.Equal(t, statusRunning, s.rec.Status)
			}
		})
	}
}
