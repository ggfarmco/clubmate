package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

func TestLandingWordsFollowTheRound(t *testing.T) {
	t.Parallel()
	findings := []string{"a.go:1", "b.go:2", "c.go:3"}
	for _, tc := range []struct {
		name      string
		st        landReview
		reviewing bool
		want      landingView
	}{
		{"first round", landReview{Cap: 2}, true, landingView{Round: 1, Of: 2, Verdict: "reviewing", Words: "review…", Tone: "accent", Badge: "reviewing", BadgeTone: "accent"}},
		{"second round", landReview{Cap: 2, Rounds: 1, Last: gateChanges, Findings: findings}, true, landingView{Round: 2, Of: 2, Verdict: "reviewing", Words: "review 2/2…", Tone: "accent", Badge: "reviewing", BadgeTone: "accent"}},
		{"changes", landReview{Cap: 2, Rounds: 1, Last: gateChanges, Findings: findings}, false, landingView{Round: 1, Of: 2, Verdict: "changes", Findings: 3, Words: "3 changes", Tone: "plain", Badge: "3 changes"}},
		{"approved after changes", landReview{Cap: 2, Rounds: 1, Last: gateApproved, Findings: findings}, false, landingView{Round: 2, Of: 2, Verdict: "approved", Words: "approved", Tone: "success", Badge: "approved", BadgeTone: "success"}},
		{"land anyway", landReview{Cap: 2, Rounds: 2, Last: gateApproved, Findings: findings}, false, landingView{Round: 2, Of: 2, Verdict: "approved", Words: "approved", Tone: "success", Badge: "approved", BadgeTone: "success"}},
		{"asking", landReview{Cap: 2, Rounds: 2, Last: gateChanges, Findings: findings[:1], Request: "lr_1"}, false, landingView{Round: 2, Of: 2, Verdict: "changes", Findings: 1, Asking: true, Words: "asking", Tone: "warning", Badge: "1 change"}},
	} {
		tc.st.Into, tc.st.Reviewer, tc.want.Into, tc.want.Reviewer = "main", "agt_r", "main", "agt_r"
		got, ok := landingOf(tc.st, tc.reviewing, nil)
		require.True(t, ok, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
	_, ok := landingOf(landReview{Rounds: 1}, false, nil)
	assert.False(t, ok)
}

func TestReviewerRoundCountsAnApprovalAsItsOwnRound(t *testing.T) {
	t.Parallel()
	running := record{GateFor: &gateFor{Old: "a", Tree: "b"}}
	done := record{GateFor: &gateFor{Old: "a", Tree: "b", Done: true}}
	assert.Equal(t, gateRound{Round: 2, Label: "Round 2 review", Verdict: "reviewing"}, reviewRound(running, landReview{Rounds: 1, Last: gateChanges}))
	assert.Equal(t, gateRound{Round: 2, Label: "Round 2 review", Verdict: "approved"}, reviewRound(done, landReview{Rounds: 1, Last: gateApproved}))
	assert.Equal(t, gateRound{Round: 1, Label: "Round 1 review", Verdict: "changes"}, reviewRound(done, landReview{Rounds: 1, Last: gateChanges}))
}

func TestReviewersHaveNoRowUnlessTheyAskOrNothingLinksThem(t *testing.T) {
	t.Parallel()
	now := time.Now()
	lander := record{ID: "agt_l", Worktree: "/r/a", Agent: agentClaude, Title: "Fix login", Status: statusRunning, UpdatedAt: now,
		LandReviews: []landReview{{Into: "main", Source: "fix/login", Reviewer: "agt_ask"}, {Into: "dev", Source: "fix/login", Reviewer: "agt_err"}, {Into: "rel", Source: "fix/login", Reviewer: "agt_hid"}}}
	gf := func(into string, done bool) *gateFor {
		return &gateFor{Lander: "agt_l", Into: into, Source: "fix/login", Old: "a", Tree: "b", Done: done}
	}
	review := func(id, into, status string, done bool) record {
		return record{ID: id, Worktree: "/r/b", Agent: agentClaude, Title: "Land review · " + id, Status: status, UpdatedAt: now, GateFor: gf(into, done)}
	}
	recs := []record{
		lander,
		review("agt_ask", "main", statusWaiting, false),
		review("agt_sup", "main", statusDone, true),
		review("agt_old", "main", statusClosed, true),
		review("agt_err", "dev", statusError, false),
		review("agt_hid", "rel", statusDone, true),
	}
	rowsOf := func(recs []record) []sessionRow {
		nest := reviewNests(recs)
		rows := make([]sessionRow, 0, len(recs))
		for _, rec := range recs {
			row := sessionRow{Rec: rec, Branch: "main"}
			if n, ok := nest[rec.ID]; ok {
				row.Nest = &n
			}
			rows = append(rows, row)
		}
		return rows
	}
	rows := rowsOf(recs)
	rows[0].Landing = &landingView{Words: "review…", Tone: "accent"}

	tree := sidebarView(rows, "", map[string]bool{}, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, sidebarPlus{}, nil)
	require.NoError(t, internalsdui.Validate(tree))
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Equal(t, []string{"agt_l"}, ids(items))
	assert.Equal(t, []string{"review…", "accent"}, []string{items[0].Detail, items[0].DetailTone})
	require.True(t, items[0].Expanded)
	assert.Equal(t, []string{"agt_ask", "agt_sup", "agt_err"}, ids(items[0].Children))
	asks, sup, failed := items[0].Children[0], items[0].Children[1], items[0].Children[2]
	assert.Equal(t, []string{"Round 1 review", "shield", "warning", "asks you", "warning"}, []string{asks.Label, asks.Icon, asks.Tone, asks.Detail, asks.DetailTone})
	assert.Equal(t, "Land review", sup.Label)
	assert.Equal(t, []string{"Round 1 review", "danger"}, []string{failed.Label, failed.Tone})

	tabs := nodesOf(tree, "tabs")[0].Props["items"].([]sdui.Item)
	assert.Equal(t, []string{"1", "1", "1"}, []string{tabs[0].Badge, tabs[1].Badge, tabs[3].Badge})

	alone := sidebarView(rowsOf([]record{review("agt_x", "rel", statusDone, true)})[0:0], "", nil, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, sidebarPlus{}, nil)
	hidden := sidebarView(rowsOf([]record{lander, review("agt_hid", "rel", statusClosed, true)})[1:], "", nil, now, "/r/a", nil, false, filterAll, sidebarSearch{}, 0, sidebarPlus{}, nil)
	assert.Equal(t, alone, hidden)
}

func TestNodesOfTheLandingsRunHaveNoRow(t *testing.T) {
	t.Parallel()
	lander := record{ID: "agt_l", LandReviews: []landReview{{Into: "main", Source: "fix", Reviewer: "agt_v", Run: "gr_2"}}}
	node := func(id, run string) record {
		return record{ID: id, Status: statusRunning, GateFor: &gateFor{Lander: "agt_l", Into: "main", Source: "fix", Old: "a", Tree: "b", Run: run}}
	}

	nest := reviewNests([]record{lander, node("agt_v", "gr_2"), node("agt_lint", "gr_2"), node("agt_old", "gr_1")})

	assert.False(t, nest["agt_lint"].Shown)
	assert.True(t, nest["agt_lint"].Tracked)
	assert.True(t, nest["agt_old"].Shown)
}

func TestLanderHeaderCarriesTheLanding(t *testing.T) {
	t.Parallel()
	rec := record{ID: "agt_l", Worktree: "/r/a", Agent: agentClaude, Title: "Fix login", Status: statusRunning}
	landing := landingView{Round: 1, Of: 2, Verdict: gateChanges, Findings: 3, Words: "3 changes", Tone: "plain", Badge: "3 changes", Into: "main", Reviewer: "agt_r"}
	tree := mainView(screen{View: transcriptView{Session: rec}, Row: sessionRow{Landing: &landing}, Now: time.Now(), Composer: "agt_l/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	assert.NotContains(t, nodesOf(tree, "header")[0].Props["meta"], "changes")
	line := findNode(tree, "agt_l/landing")
	require.NotNil(t, line)
	assert.Equal(t, []string{"Landing into main", "round 1 of 2"}, texts(*line))
	assert.Equal(t, "3 changes", nodesOf(*line, "badge")[0].Props["text"])
	button := nodesOf(*line, "button")[0]
	assert.Equal(t, []any{"Open review", actLandReview, "opt+r"}, []any{button.Props["label"], button.Props["action"], button.Props["shortcut"]})
}

func TestReviewerHeaderNamesItsRoundAndLander(t *testing.T) {
	t.Parallel()
	rec := record{ID: "agt_r", Worktree: "/r/a", Agent: agentClaude, Title: "Land review · fix/login → main", Status: statusWaiting, Notify: notifyNeedsInput}
	head := &reviewHead{Round: gateRound{Round: 1, Label: "Round 1 review", Verdict: "reviewing"}, Title: "Fix login", Into: "main", Source: "fix/login", Badge: "reviewing", Tone: "accent"}
	tree := mainView(screen{View: transcriptView{Session: rec}, Row: sessionRow{Notify: notifyNeedsInput}, GateReview: head, Back: "Agents / main / Fix login / Round 1 review", Now: time.Now(), Composer: "agt_r/composer/0"})
	require.NoError(t, internalsdui.Validate(tree))

	header := nodesOf(tree, "header")[0]
	assert.Equal(t, "Round 1 review", header.Props["title"])
	assert.Equal(t, "of Fix login · fix/login → main", header.Props["meta"])
	assert.Equal(t, []sdui.HeaderBadge{{Text: "reviewing", Tone: "accent"}, {Text: "asks you", Tone: "warning"}}, header.Props["badges"])
	back := nodesOf(tree, "button")[0]
	assert.Equal(t, []any{actBack, "cmd+["}, []any{back.Props["action"], back.Props["shortcut"]})
}

func TestDoneReviewerHeaderNamesTheHoldsRoundAndGate(t *testing.T) {
	t.Parallel()
	lander := record{ID: "agt_l", Title: "Planner", Held: &heldTurn{Gate: "gt_1", Key: "k2", Run: "run_1", Round: 2, Last: gateChanges, Findings: []string{"a.md:1", "b.md:2"}}}
	rec := record{ID: "agt_r", GateFor: &gateFor{Gate: "gt_1", Point: gatePointTurn, Key: "k1", Lander: "agt_l", Run: "run_1", Done: true}}

	logged := func() (gateVerdict, bool) { return gateVerdict{Round: 3, Outcome: gateApproved}, true }

	head := reviewHeadOf(&lander, rec, "Done · plans and specs", logged)
	released := lander
	released.Held = nil
	after := reviewHeadOf(&released, rec, "Done · plans and specs", logged)

	assert.Equal(t, gateRound{Round: 2, Label: "Round 2 review", Verdict: gateChanges}, head.Round)
	assert.Equal(t, []string{"of Planner", "Done · plans and specs"}, head.meta())
	assert.Equal(t, "2 changes", head.Badge)
	assert.Equal(t, gateRound{Round: 3, Label: "Round 3 review", Verdict: gateApproved}, after.Round)
	assert.Equal(t, []string{"of Planner", "Done · plans and specs"}, after.meta())
}

func TestGatePageShowsTheLandingUnderReview(t *testing.T) {
	t.Parallel()
	now := time.Now()
	since := now.Add(-3 * time.Minute)
	row := gateRow{Name: "clubmate", Landings: []gateLanding{{Lander: "agt_l", Title: "Fix login", Into: "main", Source: "fix/login", State: gateStateReview, Round: 1, Reviewer: "agt_r", Since: since}}}
	sc := gateScreen{Row: row, Sessions: map[string]bool{"agt_l": true, "agt_r": true}, Lines: map[string]string{"agt_r": "Reading the callers."}, Now: now, Loc: time.UTC}
	tree := gateMain(sc)
	require.NoError(t, internalsdui.Validate(tree))

	card := findNode(tree, gateLiveID)
	require.NotNil(t, card)
	assert.Equal(t, []string{"Round 1 · reviewing", "fix/login → main · 3 min", "Reading the callers.", gateLiveNote}, texts(*card))
	buttons := nodesOf(*card, "button")
	require.Len(t, buttons, 2)
	assert.Equal(t, []any{"agt_r/" + actGateReview, "opt+r"}, []any{buttons[0].ID, buttons[0].Props["shortcut"]})
	assert.Equal(t, "agt_l/"+actGateLanding, buttons[1].ID)
}

func TestApprovedVerdictOpensItsReview(t *testing.T) {
	t.Parallel()
	now := time.Now()
	v := gateVerdict{Outcome: gateApproved, By: "reviewer", Into: "main", Round: 1, Reviewer: "agt_r", At: now}
	sc := gateScreen{History: []gateVerdict{v}, Picked: gateRowID(now, "agt_r"), Sessions: map[string]bool{"agt_r": true}, Now: now, Loc: time.UTC}
	card, ok := findingsCard(sc, sc.Picked)
	require.True(t, ok)
	button := nodesOf(card, "button")[0]
	assert.Equal(t, []any{"agt_r/" + actGateReview, "opt+r"}, []any{button.ID, button.Props["shortcut"]})
}

func TestBackFromAReviewerGoesWhereItWasOpened(t *testing.T) {
	t.Parallel()
	rec := record{ID: "agt_r", GateFor: &gateFor{Lander: "agt_l"}}
	assert.Equal(t, "gate:gt_1", reviewBack(rec, reviewTrail{Reviewer: "agt_r", From: "gate:gt_1"}))
	assert.Equal(t, "agt_l", reviewBack(rec, reviewTrail{Reviewer: "agt_other", From: "gate:gt_1"}))
}

func findNode(n sdui.Node, id string) *sdui.Node {
	if n.ID == id {
		return &n
	}
	for _, c := range n.Children {
		if found := findNode(c, id); found != nil {
			return found
		}
	}
	return nil
}

func TestGateRunAskRowsUnderItsLander(t *testing.T) {
	t.Parallel()
	now := time.Now()
	lander := record{ID: "agt_l", Worktree: "/r/a", Agent: agentClaude, Title: "Fix login", Status: statusDone, UpdatedAt: now}
	ask := runNeed{Run: "gr_1", Kind: needStep, Text: "where wants to run pwd", Since: now}
	row := sessionRow{Rec: lander, Branch: "main", Landing: &landingView{Round: 1, Words: "asking", Tone: "warning", Asking: true, Ask: ask.Text, Run: ask.Run}, GateAsk: &ask}

	tree := sidebarView([]sessionRow{row}, "", map[string]bool{}, now, "/r/a", nil, false, filterNeeds, sidebarSearch{}, 0, sidebarPlus{}, nil)

	require.NoError(t, internalsdui.Validate(tree))
	items := nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	require.Equal(t, []string{"agt_l"}, ids(items))
	assert.Equal(t, []any{"asking", "warning", true}, []any{items[0].Detail, items[0].DetailTone, items[0].Expanded})
	require.Len(t, items[0].Children, 1)
	asks := items[0].Children[0]
	assert.Equal(t, []string{"run:gr_1", "Round 1 review", "shield", "warning", "where wants to run pwd", "warning"}, []string{asks.ID, asks.Label, asks.Icon, asks.Tone, asks.Detail, asks.DetailTone})
	assert.Equal(t, "1", nodesOf(tree, "tabs")[0].Props["items"].([]sdui.Item)[1].Badge)
}
