package main

import (
	"context"
	"slices"
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
	out := map[string]workspaceActivity{}
	for _, id := range a.workspaceIDs() {
		if act := activityOf(a.infos(id)); act != (workspaceActivity{}) {
			out[id] = act
		}
	}
	return out, nil
}

func (a *app) workspaceIDs() []string {
	a.mu.Lock()
	all := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		all = append(all, s)
	}
	a.mu.Unlock()
	ids := a.runNeeds.workspaces()
	for _, s := range all {
		if id := s.snapshot().Workspace; id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
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
