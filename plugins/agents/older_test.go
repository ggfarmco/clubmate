package main

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

func TestASessionTreeIsOlderOnlyWhenEveryRowOfItsUnitIs(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cutoff := now.Add(-time.Hour)
	old := now.Add(-2 * time.Hour)
	closed := func(id, parent string) record {
		return record{ID: id, Status: statusClosed, UpdatedAt: old, lineage: lineage{ParentSession: parent}}
	}
	reviewer := func(id, lander string) record {
		r := closed(id, "")
		r.GateFor = &gateFor{Lander: lander}
		return r
	}
	tests := []struct {
		name  string
		recs  []record
		runs  map[string]bool
		older []string
	}{
		{name: "closed tree with its reviewer", recs: []record{closed("root", ""), closed("kid", "root"), reviewer("rev", "kid")}, older: []string{"kid", "rev", "root"}},
		{name: "open parent", recs: []record{{ID: "root", Status: statusDone, UpdatedAt: old}, closed("kid", "root")}},
		{name: "one recent row", recs: []record{closed("root", ""), func() record { r := closed("kid", "root"); r.UpdatedAt = now; return r }()}},
		{name: "open items", recs: []record{closed("root", ""), func() record { r := closed("kid", "root"); r.OpenCount = 1; return r }()}},
		{name: "open task", recs: []record{func() record {
			r := closed("root", "")
			r.Task = &taskRecord{Repos: []taskRepo{{Path: "/r", State: landWaits}}}
			return r
		}(), closed("kid", "root")}},
		{name: "live run", recs: []record{closed("root", ""), func() record { r := closed("kid", "root"); r.Run = &api.Run{ID: "gr_1"}; return r }()}, runs: map[string]bool{"gr_1": true}},
		{name: "ended run", recs: []record{func() record { r := closed("root", ""); r.Run = &api.Run{ID: "gr_1"}; return r }()}, older: []string{"root"}},
		{name: "reviewer under a live lander", recs: []record{{ID: "lander", Status: statusRunning, UpdatedAt: old}, reviewer("rev", "lander"), closed("other", "")}, older: []string{"other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			infos := make([]sessionInfo, 0, len(tt.recs))
			for _, rec := range tt.recs {
				infos = append(infos, sessionInfo{record: rec})
			}
			setOlder(infos, cutoff, tt.runs)
			var got []string
			for _, info := range infos {
				if info.Older {
					got = append(got, info.ID)
				}
			}
			slices.Sort(got)
			assert.Equal(t, tt.older, got)
		})
	}
}

func TestOlderPagesHoldWholeUnitsNewestFirstAndKeepTheirPlaceWhenAUnitEnters(t *testing.T) {
	t.Parallel()
	t0 := time.Now().Add(-time.Hour)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	row := func(id, parent string, m int) sessionInfo {
		return sessionInfo{record: record{ID: id, Status: statusClosed, UpdatedAt: at(m), lineage: lineage{ParentSession: parent}}}
	}
	infos := []sessionInfo{row("a", "", 1), row("a1", "a", 5), row("b", "", 4), row("c", "", 3), row("d", "", 2), {record: record{ID: "e", Status: statusDone, UpdatedAt: at(9)}}}
	hidden := row("h", "", 8)
	hidden.Hidden = true
	infos = append(infos, hidden)
	setOlder(infos, at(10), nil)
	ids := func(rows []sessionInfo) []string {
		out := []string{}
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}

	first, next, count, err := olderPage(infos, "", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "a1", "b"}, ids(first))
	assert.Equal(t, 4, count)
	assert.Equal(t, 4, olderCount(pluginsdk.CommandRun{}, infos))

	infos = append(infos, row("z", "", 7))
	setOlder(infos, at(10), nil)
	second, last, _, err := olderPage(infos, next, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "d"}, ids(second))
	assert.Empty(t, last)

	_, _, _, err = olderPage(infos, "nope", 2)
	assert.ErrorIs(t, err, errOlderCursor)
}

func TestAUnitWithAHiddenRootIsNotShownAsOlder(t *testing.T) {
	t.Parallel()
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	recs := []record{
		{ID: "plain", Status: statusClosed, UpdatedAt: old},
		{ID: "reviewer", Status: statusClosed, UpdatedAt: old, GateFor: &gateFor{Lander: "gone"}},
		{ID: "child", Status: statusClosed, UpdatedAt: old, lineage: lineage{ParentSession: "reviewer"}},
	}
	assert.Equal(t, map[string]string{"plain": "plain"}, shownUnits(recs, now.Add(-time.Hour), nil))
}
