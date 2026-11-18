package main

import (
	"cmp"
	"context"
	"maps"
	"path/filepath"
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
	firstNode := map[string]record{}
	for _, rec := range recs {
		if run := runID(rec); run != "" {
			if first, ok := firstNode[run]; !ok || startedFirst(rec, first) < 0 {
				firstNode[run] = rec
			}
		}
	}
	for i := range infos {
		infos[i].ListKey = listKey(infos[i].record)
		switch first, ok := firstNode[infos[i].ID]; {
		case infos[i].Kind == "":
			infos[i].ListGroupKey = groups[infos[i].Worktree]
		case infos[i].Kind == kindRun && ok:
			infos[i].ListKey, infos[i].ListGroupKey, infos[i].Worktree = listKey(first), groups[first.Worktree], first.Worktree
		}
	}
}

func setGroupLabels(infos []sessionInfo, label func(worktree string) string) {
	labels := map[string]string{}
	for _, info := range infos {
		if _, ok := labels[info.Worktree]; !ok && info.ListGroupKey != "" && info.Night == "" {
			labels[info.Worktree] = label(info.Worktree)
		}
	}
	for i, info := range infos {
		if info.ListGroupKey != "" {
			infos[i].ListGroupLabel = cmp.Or(labels[info.Worktree], filepath.Base(info.Worktree))
		}
	}
}

func setListFilters(infos []sessionInfo, needs map[string]runNeed) {
	var recs []record
	for _, info := range infos {
		if info.Kind == "" {
			recs = append(recs, info.record)
		}
	}
	nest := reviewNests(recs)
	rows := make([]sessionRow, 0, len(recs))
	index := make(map[string]int, len(infos))
	for i, info := range infos {
		index[info.ID] = i
		if info.Kind != "" {
			continue
		}
		row := sessionRow{Rec: info.record, Children: info.ChildStatus, Landing: info.Landing}
		row.attachNeeds(needs, nest)
		rows = append(rows, row)
	}
	roots, kids := sidebarTree(rows)
	tallyFilters(roots, markRoots(roots, kids), append(slices.Sorted(maps.Keys(needs)), gateEntries(infos)...), func(id, filter string, n int) {
		i, ok := index[id]
		if !ok {
			return
		}
		infos[i].ListFilters = append(infos[i].ListFilters, filter)
		if filter == filterOpen {
			infos[i].ListOpen += n
		}
	})
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

func (row *sessionRow) attachNeeds(needs map[string]runNeed, nest map[string]reviewerNest) {
	if n, ok := needs[runID(row.Rec)]; ok {
		row.RunNeed = &n
	}
	if l := row.Landing; l != nil && l.Run != "" {
		if n, ok := needs[l.Run]; ok {
			row.GateAsk = &n
		}
	}
	if n, ok := nest[row.Rec.ID]; ok {
		row.Nest = &n
	}
}
