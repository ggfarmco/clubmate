package main

import (
	"context"
	"slices"

	"clubmate/plugins/agents/api"
)

const (
	runsKey   = "#runs"
	dockKey   = "#dock"
	queuesKey = "#queues"
)

type sessionsPage struct {
	Sessions   []sessionInfo `json:"sessions"`
	Removed    []string      `json:"removed"`
	Cursor     string        `json:"cursor"`
	Delta      bool          `json:"delta,omitempty"`
	Runs       []api.RunRow  `json:"runs,omitzero"`
	Dock       []api.DockRow `json:"dock,omitzero"`
	LandQueues []queueRow    `json:"landQueues,omitzero"`
	OlderCount int           `json:"olderCount,omitempty"`
}

func (a *app) sessionsPage(ctx context.Context, workspaceID string, infos []sessionInfo, st graphState, since string, lean, all bool) sessionsPage {
	items := make(map[string]any, len(infos)+2)
	for _, info := range infos {
		items[info.ID] = info
	}
	items[runsKey], items[dockKey], items[queuesKey] = st.runs, st.dock, st.queues
	v, err := a.sessionRevs.Observe(ctx, sessionsTracker(workspaceID), items, since)
	logError("sessions delta", err)
	out := sessionsPage{Sessions: infos, Removed: []string{}, Cursor: v.Cursor, Runs: st.runs, Dock: st.dock, LandQueues: st.queues}
	if v.Full {
		if !all {
			out.Sessions = slices.DeleteFunc(infos, func(info sessionInfo) bool { return info.Older })
		}
		return out
	}
	out.Delta = true
	out.Sessions = slices.DeleteFunc(infos, func(info sessionInfo) bool {
		_, found := slices.BinarySearch(v.Changed, info.ID)
		if found && info.Older && !all {
			out.Removed = append(out.Removed, info.ID)
			return true
		}
		return !found
	})
	out.Removed = append(out.Removed, v.Removed...)
	if !lean {
		return out
	}
	if _, found := slices.BinarySearch(v.Changed, runsKey); !found {
		out.Runs = nil
	}
	if _, found := slices.BinarySearch(v.Changed, dockKey); !found {
		out.Dock = nil
	}
	if _, found := slices.BinarySearch(v.Changed, queuesKey); !found {
		out.LandQueues = nil
	}
	return out
}

func sessionsTracker(workspaceID string) string {
	return "sessions/" + workspaceID
}
