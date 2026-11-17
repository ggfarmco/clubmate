package main

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShareLeavesOutAWindowThatRoundsToZero(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000, 0)
	s := shareState{Windows: map[string]*shareWindow{
		windowFiveHour: {ResetsAt: 2_000, Base: 0.10, Last: 0.11, Total: 1_000, Sessions: map[string]float64{"tiny": 1, "big": 999}},
		windowSevenDay: {ResetsAt: 2_000, Base: 0.10, Last: 0.20, Total: 2, Sessions: map[string]float64{"tiny": 1, "big": 1}},
	}}

	assert.Equal(t, map[string]float64{windowSevenDay: 5}, s.of([]string{"tiny"}, now))
}

func TestShareAddsUpChildrenAsidesAndReviewersIntoTheParent(t *testing.T) {
	t.Parallel()
	recs := []record{
		{ID: "parent"},
		{ID: "child", lineage: lineage{ParentSession: "parent"}},
		{ID: "grandchild", lineage: lineage{ParentSession: "child"}},
		{ID: "aside", lineage: lineage{ParentSession: "parent"}},
		{ID: "reviewer", GateFor: &gateFor{Lander: "parent"}},
		{ID: "other"},
	}
	resets := time.Now().Add(time.Hour).Unix()
	var changed []string
	u := usageTracker{
		share: shareState{Windows: map[string]*shareWindow{
			windowFiveHour: {ResetsAt: resets, Base: 0.10, Last: 0.20, Total: 10, Sessions: map[string]float64{"child": 2, "grandchild": 1, "aside": 1, "reviewer": 1, "other": 5}},
		}},
		tree:    func() spendTree { return spendTreeOf(recs) },
		onShare: func(ids []string) { changed = append(changed, ids...) },
	}
	u.shown = u.share.keys(time.Now(), u.lineage())

	assert.Equal(t, map[string]float64{windowFiveHour: 5}, u.sessionShare(u.lineage().of("parent")))
	assert.Equal(t, map[string]float64{windowFiveHour: 3}, u.sessionShare(u.lineage().of("child")))

	u.observe("agent-child", "child", []byte(`{"sessionUpdate":"usage_update","cost":{"amount":2,"currency":"USD"}}`))
	assert.Contains(t, changed, "parent")

	u.fold("aside", "parent")
	recs = slices.Delete(recs, 3, 4)
	assert.InDelta(t, 5.8, u.sessionShare(u.lineage().of("parent"))[windowFiveHour], 0.001)
}
