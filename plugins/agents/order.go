package main

import (
	"cmp"
	"context"
	"slices"
	"strings"
)

const (
	startLayout    = "20060102T150405.000000000Z"
	homeListGroup  = "0"
	otherListGroup = "1"
)

func startedFirst(x, y record) int {
	return cmp.Or(x.CreatedAt.Compare(y.CreatedAt), strings.Compare(x.ID, y.ID))
}

func listKey(rec record) string {
	return rec.CreatedAt.UTC().Format(startLayout) + " " + rec.ID
}

func groupKeys(recs []record, home string) map[string]string {
	keys := map[string]string{}
	for _, rec := range recs {
		if _, ok := keys[rec.Worktree]; ok {
			continue
		}
		if rec.Worktree == home && home != "" {
			keys[rec.Worktree] = homeListGroup
			continue
		}
		keys[rec.Worktree] = otherListGroup + rec.CreatedAt.UTC().Format(startLayout) + " " + rec.Worktree
	}
	return keys
}

func setOrder(infos []sessionInfo, home string) {
	recs := make([]record, 0, len(infos))
	for _, info := range infos {
		if info.Kind == "" {
			recs = append(recs, info.record)
		}
	}
	groups := groupKeys(recs, home)
	for i := range infos {
		infos[i].ListKey = listKey(infos[i].record)
		if infos[i].Kind == "" {
			infos[i].ListGroupKey = groups[infos[i].Worktree]
		}
	}
}

func (a *app) home(ctx context.Context, workspaceID string) string {
	workspace, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	home, _, _ := workRoot(workspace)
	return home
}

func sidebarWindow(recs []record) []record {
	return recs[max(0, len(recs)-maxSidebarRows):]
}

func newestPick(recs []record) string {
	for _, rec := range slices.Backward(recs) {
		if !rec.restricted() && rec.GateFor == nil {
			return rec.ID
		}
	}
	return ""
}
