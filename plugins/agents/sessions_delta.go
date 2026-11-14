package main

import (
	"context"
	"slices"
)

type sessionsPage struct {
	Sessions []sessionInfo `json:"sessions"`
	Removed  []string      `json:"removed"`
	Cursor   string        `json:"cursor"`
	Delta    bool          `json:"delta,omitempty"`
}

func (a *app) sessionsPage(ctx context.Context, workspaceID string, infos []sessionInfo, since string) sessionsPage {
	items := make(map[string]any, len(infos))
	for _, info := range infos {
		items[info.ID] = info
	}
	v, err := a.sessionRevs.Observe(ctx, sessionsTracker(workspaceID), items, since)
	logError("sessions delta", err)
	out := sessionsPage{Sessions: infos, Removed: []string{}, Cursor: v.Cursor}
	if v.Full {
		return out
	}
	out.Delta = true
	out.Sessions = slices.DeleteFunc(infos, func(info sessionInfo) bool {
		_, found := slices.BinarySearch(v.Changed, info.ID)
		return !found
	})
	out.Removed = append(out.Removed, v.Removed...)
	return out
}

func sessionsTracker(workspaceID string) string {
	return "sessions/" + workspaceID
}
