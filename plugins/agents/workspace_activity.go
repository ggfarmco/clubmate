package main

import (
	"context"
	"time"

	"clubmate/pkg/pluginsdk"
)

type workspaceActivity struct {
	NeedsYou     int       `json:"needsYou,omitempty"`
	Running      int       `json:"running,omitempty"`
	Failed       int       `json:"failed,omitempty"`
	LastActiveAt time.Time `json:"lastActiveAt,omitzero"`
}

func (a *app) activityCommand(context.Context, pluginsdk.CommandRun) (any, error) {
	byWS := a.listByWorkspace()
	for _, id := range a.runNeeds.workspaces() {
		if _, ok := byWS[id]; !ok {
			byWS[id] = nil
		}
	}
	out := map[string]workspaceActivity{}
	for id, recs := range byWS {
		if act := activityOf(a.activityInfos(id, recs)); act != (workspaceActivity{}) {
			out[id] = act
		}
	}
	return out, nil
}

func (a *app) activityInfos(workspaceID string, recs []record) []sessionInfo {
	nest := reviewNests(recs)
	out := make([]sessionInfo, 0, len(recs))
	for _, rec := range recs {
		out = append(out, baseInfo(rec, nest))
	}
	out = append(out, a.runRows(workspaceID, recs, nil)...)
	for _, n := range trustNeedsOf(recs) {
		out = append(out, trustInfo(workspaceID, n))
	}
	setGroups(out)
	return out
}

func activityOf(infos []sessionInfo) workspaceActivity {
	index := make(map[string]bool, len(infos))
	for _, info := range infos {
		index[info.ID] = true
	}
	var out workspaceActivity
	for _, info := range infos {
		if info.Service {
			continue
		}
		if info.NeedsYou {
			out.NeedsYou++
		}
		if info.Group == groupRunning && !index[groupParent(info)] {
			out.Running++
		}
		if info.Kind != "" {
			continue
		}
		if info.Status == statusError && !info.limitPaused() && !info.Hidden {
			out.Failed++
		}
		if info.UpdatedAt.After(out.LastActiveAt) {
			out.LastActiveAt = info.UpdatedAt.UTC()
		}
	}
	return out
}
