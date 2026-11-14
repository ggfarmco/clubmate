package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/landgate"
)

func TestGateItemSaysTheGateState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	last := func(outcome, by string) *gateVerdict {
		return &gateVerdict{Outcome: outcome, By: by, At: now.Add(-12 * time.Minute)}
	}
	cases := []struct {
		name   string
		row    gateRow
		icon   string
		detail string
		tone   string
		badge  string
	}{
		{"approved", gateRow{State: gateStateActive, Last: last(gateApproved, landgate.ByReviewer)}, gateOKIcon, "approved · 12m", "success", ""},
		{"changes", gateRow{State: gateStateActive, Last: last(gateChanges, landgate.ByReviewer)}, gateWarnIcon, "changes · 12m", "warning", ""},
		{"docs only", gateRow{State: gateStateActive, Last: last(gateApproved, landgate.ByDocs)}, gateIcon, "docs only · 12m", "", ""},
		{"reviewing", gateRow{State: gateStateReview, Last: last(gateApproved, landgate.ByReviewer)}, gateIcon, gateStateReview, "warning", ""},
		{"paused", gateRow{gateConfig: gateConfig{Paused: true}, State: gateStatePaused}, gatePausedIcon, gateStatePaused, "", ""},
		{"needs you", gateRow{State: gateStateNeedsMe}, gateWarnIcon, "", "", "needs you"},
		{"never landed", gateRow{State: gateStateActive}, gateIcon, gateNoLandings, "", ""},
	}
	for _, tc := range cases {
		item := gateItem(tc.row, now)
		badge := ""
		if len(item.Badges) > 0 {
			badge = item.Badges[0].Text
		}
		assert.Equal(t, []string{tc.icon, tc.detail, tc.tone, tc.badge}, []string{item.Icon, item.Detail, item.DetailTone, badge}, tc.name)
	}
}

func TestGateDetailListsLiveLandingsFirstAndLinksOnlySessionsHere(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	older := gateVerdict{Outcome: gateChanges, By: landgate.ByReviewer, Title: "Rate-limit notifications", Into: "main", Round: 1, Reviewer: "agt_gone", Findings: []string{"notify.go:212 — resets on restart"}, Deferred: []string{"rename the knob"}, At: now.Add(-40 * time.Minute)}
	newer := gateVerdict{Outcome: gateApproved, By: landgate.ByReviewer, Title: "Rate-limit notifications", Into: "main", Round: 2, Reviewer: "agt_here", At: now.Add(-20 * time.Minute)}
	sc := gateScreen{
		Row: gateRow{
			gateConfig: gateConfig{ID: "gt_1", Repo: "/src/clubmate", Rounds: 2},
			Name:       "clubmate", State: gateStateReview, Last: &newer,
			Landings: []gateLanding{{Lander: "agt_lander", Title: "Migrate storage", Into: "main", Source: "feat/storage", State: gateStateReview, Round: 1, Reviewer: "agt_rev", Since: now.Add(-3 * time.Minute)}},
		},
		Repo:     gateRepo{Primary: "/src/clubmate", Branch: "main"},
		History:  []gateVerdict{newer, older},
		Picked:   gateRowID(older.At, older.Reviewer),
		Sessions: map[string]bool{"agt_here": true, "agt_rev": true},
		Now:      now,
		Loc:      time.UTC,
	}

	tree := gateMain(sc)

	require.NoError(t, internalsdui.Validate(tree))
	table := nodesOf(tree, "table")[0]
	rows := table.Props["rows"].([]sdui.TableRow)
	require.Len(t, rows, 3)
	var verdicts, links []string
	for _, r := range rows {
		verdicts = append(verdicts, r.Cells[1].Text)
		links = append(links, r.Cells[2].Label)
	}
	assert.Equal(t, []string{"reviewing", "approved", "changes"}, verdicts)
	assert.Equal(t, []string{"Open review", "Open review", ""}, links)
	assert.Equal(t, "Rate-limit notifications · round 1 · 1 finding", rows[2].Cells[2].Text)
	assert.Equal(t, sc.Picked, table.Props["selected"])
	card := nodesOf(tree, "card")
	require.Len(t, card, 1)
	assert.Equal(t, "warning", card[0].Props["tone"])
	assert.Contains(t, textsOf(card[0]), "· notify.go:212 — resets on restart")
	assert.Contains(t, textsOf(card[0]), "Deferred: rename the knob")
}

func TestGateSheetRefusesARepoThatHasAGate(t *testing.T) {
	t.Parallel()
	repos := []gateRepo{{Path: "/src/clubmate", Primary: "/src/clubmate", Branch: "main"}, {Path: "/src/roots", Primary: "/src/roots", Branch: "master"}}
	d := gateDraft{Repo: "/src/clubmate", Rounds: 2, Repos: repos, Gated: map[string]bool{"/src/clubmate": true}}

	taken := gateSheetView(d)
	d.Repo = "/src/roots"
	free := gateSheetView(d)

	require.NoError(t, internalsdui.Validate(sdui.VStack(taken)))
	assert.Equal(t, gateRepoTaken, nodesOf(taken, "select")[0].Props["error"])
	assert.Equal(t, true, saveButton(t, taken).Props["disabled"])
	assert.Nil(t, saveButton(t, free).Props["disabled"])
	assert.Contains(t, textsOf(free), "/src/roots · lands into master")
	assert.Empty(t, nodesOf(free, "toggle"))
}

func TestGateEditSheetLocksRepoAndKind(t *testing.T) {
	t.Parallel()
	sheet := gateSheetView(gateDraft{Edit: "gt_1", Name: "clubmate", Repo: "/src/clubmate", Rounds: 3, Paused: true})

	require.NoError(t, internalsdui.Validate(sdui.VStack(sheet)))
	assert.Equal(t, "Edit gate · clubmate", sheet.Props["title"])
	selects := nodesOf(sheet, "select")
	require.Len(t, selects, 1)
	assert.Equal(t, "3", selects[0].Props["value"])
	assert.Equal(t, true, nodesOf(sheet, "toggle")[0].Props["value"])
}

func TestSessionRowShowsTheGateStateOnlyWhileIdle(t *testing.T) {
	t.Parallel()
	now := time.Now()
	idle := sessionItem(sessionRow{Rec: record{ID: "agt_1", Status: statusIdle, UpdatedAt: now}, Gate: gateChanges}, 0, now)
	running := sessionItem(sessionRow{Rec: record{ID: "agt_2", Status: statusRunning, UpdatedAt: now}, Gate: gateChanges}, 0, now)

	assert.Equal(t, []string{"changes", "warning"}, []string{idle.Detail, idle.DetailTone})
	assert.NotContains(t, running.Detail, "changes")
}

func saveButton(t *testing.T, n sdui.Node) sdui.Node {
	t.Helper()
	for _, b := range nodesOf(n, "button") {
		if b.ID == gateSheetID+"/save" {
			return b
		}
	}
	t.Fatal("no save button")
	return sdui.Node{}
}
