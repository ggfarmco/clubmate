package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
)

func TestRunRowStates(t *testing.T) {
	t.Parallel()
	nodes := []graphs.SnapshotNode{{ID: "coder", Role: "Coder", Session: "agt_c"}, {ID: "reviewer", Role: "Reviewer", Session: "agt_r"}}
	open := map[string]sessionInfo{
		"agt_c": {record: record{ID: "agt_c", Worktree: "/r/wt", Status: statusIdle}},
		"agt_r": {record: record{ID: "agt_r", Worktree: "/r/wt", Status: statusIdle}},
	}
	waiting := map[string]sessionInfo{
		"agt_c": {record: record{ID: "agt_c", Worktree: "/r/wt", Status: statusWaiting}, NeedsYou: true},
		"agt_r": open["agt_r"],
	}
	closed := map[string]sessionInfo{
		"agt_c": {record: record{ID: "agt_c", Worktree: "/r/wt", Status: statusClosed}},
	}
	need := &api.RunNeed{Run: "gr_1", Request: "step-tests"}
	tests := []struct {
		name  string
		run   graphs.RunSnapshot
		infos map[string]sessionInfo
		want  api.RunRow
	}{
		{"asks", graphs.RunSnapshot{Status: "running", Turns: 3, Need: need}, open, api.RunRow{State: "asks", Detail: "asks you", NeedsYou: true, Request: "step-tests", Home: "group"}},
		{"running, a node waits", graphs.RunSnapshot{Status: "running", Turns: 3}, waiting, api.RunRow{State: "running", Detail: "→ Coder", Pointer: "coder", Home: "group"}},
		{"running", graphs.RunSnapshot{Status: "running", Turns: 3}, open, api.RunRow{State: "running", Detail: "turn 3", Home: "group"}},
		{"ended", graphs.RunSnapshot{Status: "stopped", Reason: "reviewer reported approved"}, open, api.RunRow{State: "ended", Detail: "approved", Home: "group"}},
		{"stopped", graphs.RunSnapshot{Status: "stopped", Reason: "stopped"}, closed, api.RunRow{State: "stopped", Detail: "stopped", Home: "closed"}},
		{"failed", graphs.RunSnapshot{Status: "stopped", Reason: "start failed: boom"}, open, api.RunRow{State: "failed", Detail: "failed", Home: "group"}},
		{"emptied", graphs.RunSnapshot{Status: "stopped", Reason: "stopped", Dir: "/r/main"}, map[string]sessionInfo{}, api.RunRow{State: "emptied", Detail: "no sessions", Home: "closed", Worktree: "/r/main"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run.ID, tt.run.Nodes = "gr_1", nodes
			got := runRows([]graphs.RunSnapshot{tt.run}, tt.infos)[0]
			assert.Equal(t, []any{tt.want.State, tt.want.Detail, tt.want.NeedsYou, tt.want.Request, tt.want.Pointer, tt.want.Home}, []any{got.State, got.Detail, got.NeedsYou, got.Request, got.Pointer, got.Home})
			if tt.want.Worktree != "" {
				assert.Equal(t, tt.want.Worktree, got.Worktree)
			}
		})
	}
}

func TestDockRowAttention(t *testing.T) {
	t.Parallel()
	asking := []sessionInfo{{record: record{ID: "agt_v"}, NeedsYou: true}}
	defs := []graphs.Definition{{Name: "nightly", Untrusted: true}, {Name: "review-loop"}}
	rows := dockRows(asking, gateCount{Gated: 2, Asking: 1}, defs)
	assert.Equal(t, []string{"service", "gates", "graphs"}, []string{rows[0].ID, rows[1].ID, rows[2].ID})
	assert.Equal(t, api.DockRow{ID: "service", Label: "Service", Icon: rows[0].Icon, Detail: "asks you", PhoneDetail: "1 asks", Attention: true}, rows[0])
	assert.Equal(t, []any{"1 asking", false}, []any{rows[1].Detail, rows[1].Attention})
	assert.Equal(t, []any{"2", false}, []any{rows[2].Detail, rows[2].Attention})

	defs[0].WaitsTrust = true
	rows = dockRows(nil, gateCount{Gated: 2}, defs)
	assert.Equal(t, []any{"0", false}, []any{rows[0].Detail, rows[0].Attention})
	assert.Equal(t, "2", rows[1].Detail)
	assert.Equal(t, []any{"1 to trust", true}, []any{rows[2].Detail, rows[2].Attention})
}

func TestReviewerNestsUnderLander(t *testing.T) {
	t.Parallel()
	lander := record{ID: "agt_l", LandReviews: []landReview{{Gate: "lg_1", Reviewer: "agt_rev", Rounds: 0}}}
	reviewing := func(string, landReview) bool { return true }
	got := reviewerNesting([]record{lander}, reviewing)
	assert.Equal(t, reviewerNest{Lander: "agt_l", Round: gateRound{Round: 1, Label: "Round 1 review", Verdict: "reviewing"}}, got["agt_rev"])

	lander.LandReviews[0].Rounds, lander.LandReviews[0].Last = 2, gateChanges
	got = reviewerNesting([]record{lander}, func(string, landReview) bool { return false })
	assert.Len(t, got, 1)
	assert.Equal(t, gateRound{Round: 2, Label: "Round 2 review", Verdict: "changes"}, got["agt_rev"].Round)
}

func TestRunRowGroupAndStatus(t *testing.T) {
	t.Parallel()
	open := map[string]sessionInfo{"agt_c": {record: record{ID: "agt_c", Status: statusIdle}}}
	nodes := []graphs.SnapshotNode{{ID: "coder", Role: "Coder", Session: "agt_c"}}
	tests := []struct {
		run         graphs.RunSnapshot
		infos       map[string]sessionInfo
		group, stat string
	}{
		{graphs.RunSnapshot{Status: "running", Need: &api.RunNeed{Request: "cap-1"}}, open, groupNeedsYou, statusWaiting},
		{graphs.RunSnapshot{Status: "running"}, open, groupRunning, statusRunning},
		{graphs.RunSnapshot{Status: "stopped", Reason: "start failed: x"}, open, groupDone, statusError},
		{graphs.RunSnapshot{Status: "stopped", Reason: "stopped"}, map[string]sessionInfo{}, groupClosed, statusClosed},
	}
	for _, tt := range tests {
		tt.run.ID, tt.run.Nodes = "gr_1", nodes
		got := runRow(tt.run, tt.infos)
		assert.Equal(t, []string{tt.group, tt.stat}, []string{got.Group, got.Status}, got.State)
	}
}

func TestReviewerWithoutVerdictIsRoundOne(t *testing.T) {
	t.Parallel()
	lander := record{ID: "agt_l", LandReviews: []landReview{{Gate: "lg_1", Reviewer: "agt_rev"}}}
	got := reviewerNesting([]record{lander}, func(string, landReview) bool { return false })
	assert.Equal(t, "Round 1 review", got["agt_rev"].Round.Label)
}
