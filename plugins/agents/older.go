package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/graphs"
)

const (
	olderAfter     = 3 * 24 * time.Hour
	olderAfterEnv  = "CLUBMATE_AGENTS_OLDER_AFTER"
	olderPageSize  = 30
	olderPageMax   = 100
	olderCursorSep = "."
)

var errOlderCursor = errors.New("invalid_params: before is not a cursor from agents.sessions-older")

type sessionsOlderArgs struct {
	Before string `json:"before"`
	Limit  int    `json:"limit"`
}

type olderReply struct {
	Sessions []sessionInfo `json:"sessions"`
	Next     string        `json:"next,omitempty"`
	Count    int           `json:"count"`
}

type olderUnit struct {
	root   string
	newest time.Time
	rows   []*sessionInfo
}

func (a *app) sessionsOlderCommand(ctx context.Context, cmd pluginsdk.CommandRun, args sessionsOlderArgs) (any, error) {
	home, err := a.home(ctx, cmd.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("sessions-older: workspace home: %w", err)
	}
	limit := olderPageSize
	if args.Limit > 0 {
		limit = min(args.Limit, olderPageMax)
	}
	infos := visibleInfos(cmd, a.listInfos(ctx, cmd.WorkspaceID, home, nil))
	rows, next, count, err := olderPage(infos, args.Before, limit)
	if err != nil {
		return nil, err
	}
	return olderReply{Sessions: rows, Next: next, Count: count}, nil
}

func setOlder(infos []sessionInfo, cutoff time.Time, liveRuns map[string]bool) {
	recs := make([]*record, 0, len(infos))
	for i := range infos {
		if infos[i].Kind == "" {
			recs = append(recs, &infos[i].record)
		}
	}
	units := olderUnits(recs, cutoff, liveRuns)
	for i := range infos {
		if root, ok := units[infos[i].ID]; ok && infos[i].Kind == "" {
			infos[i].Older, infos[i].olderRoot = true, root
		}
	}
}

func olderPage(infos []sessionInfo, before string, limit int) ([]sessionInfo, string, int, error) {
	after, err := parseOlderCursor(before)
	if err != nil {
		return nil, "", 0, err
	}
	units := olderUnitsOf(infos)
	start := 0
	if before != "" {
		start = len(units)
		for i, u := range units {
			if compareOlder(u, after) > 0 {
				start = i
				break
			}
		}
	}
	end := min(start+limit, len(units))
	rows := []sessionInfo{}
	for _, u := range units[start:end] {
		for _, info := range u.rows {
			rows = append(rows, *info)
		}
	}
	next := ""
	if end < len(units) {
		next = units[end-1].cursor()
	}
	return rows, next, len(units), nil
}

func olderCount(cmd pluginsdk.CommandRun, infos []sessionInfo) int {
	n := 0
	for i := range infos {
		info := &infos[i]
		if info.Older && info.ID == info.olderRoot && !info.Hidden && !hiddenFrom(cmd, info.record) {
			n++
		}
	}
	return n
}

func (a *app) olderInputs(workspaceID string, runs []graphs.RunSnapshot) (time.Time, map[string]bool, bool) {
	if a.olderAfter <= 0 {
		return time.Time{}, nil, false
	}
	if runs == nil {
		runs = a.graphs.Runs(workspaceID)
	}
	return time.Now().Add(-a.olderAfter), liveRunIDs(runs), true
}

func (a *app) setOlderIn(infos []sessionInfo, workspaceID string, runs []graphs.RunSnapshot) {
	if cutoff, live, ok := a.olderInputs(workspaceID, runs); ok {
		setOlder(infos, cutoff, live)
	}
}

func (a *app) olderShown(workspaceID string, recs []record) map[string]string {
	cutoff, live, ok := a.olderInputs(workspaceID, nil)
	if !ok {
		return nil
	}
	return shownUnits(recs, cutoff, live)
}

func shownUnits(recs []record, cutoff time.Time, live map[string]bool) map[string]string {
	units := olderUnits(recordRefs(recs), cutoff, live)
	nest := reviewNests(recs)
	for id, root := range units {
		if n, ok := nest[root]; ok && !n.Shown {
			delete(units, id)
		}
	}
	return units
}

func olderUnits(recs []*record, cutoff time.Time, liveRuns map[string]bool) map[string]string {
	byID := make(map[string]*record, len(recs))
	for _, rec := range recs {
		byID[rec.ID] = rec
	}
	roots := make(map[string]string, len(recs))
	var path []string
	for _, rec := range recs {
		path = path[:0]
		root := ""
		for cur := rec; ; {
			if r, ok := roots[cur.ID]; ok {
				root = r
				break
			}
			if slices.Contains(path, cur.ID) {
				root = cur.ID
				break
			}
			path = append(path, cur.ID)
			up, ok := unitParent(cur, byID)
			if !ok {
				root = cur.ID
				break
			}
			cur = up
		}
		for _, id := range path {
			roots[id] = root
		}
	}
	fresh := map[string]bool{}
	for _, rec := range recs {
		if !rec.expired(cutoff) || rec.OpenCount > 0 || liveRuns[runID(*rec)] {
			fresh[roots[rec.ID]] = true
		}
	}
	out := map[string]string{}
	for id, root := range roots {
		if !fresh[root] {
			out[id] = root
		}
	}
	return out
}

func unitParent(rec *record, byID map[string]*record) (*record, bool) {
	if up, ok := byID[rec.ParentSession]; ok && rec.ParentSession != "" {
		return up, true
	}
	if rec.GateFor == nil {
		return nil, false
	}
	up, ok := byID[rec.GateFor.Lander]
	return up, ok
}

func recordRefs(recs []record) []*record {
	out := make([]*record, len(recs))
	for i := range recs {
		out[i] = &recs[i]
	}
	return out
}

func liveRunIDs(runs []graphs.RunSnapshot) map[string]bool {
	out := make(map[string]bool, len(runs))
	for _, r := range runs {
		out[r.ID] = true
	}
	return out
}

func olderUnitsOf(infos []sessionInfo) []olderUnit {
	index := map[string]int{}
	var units []olderUnit
	for k := range infos {
		info := &infos[k]
		if !info.Older {
			continue
		}
		i, ok := index[info.olderRoot]
		if !ok {
			i = len(units)
			index[info.olderRoot] = i
			units = append(units, olderUnit{root: info.olderRoot})
		}
		units[i].rows = append(units[i].rows, info)
		if info.UpdatedAt.After(units[i].newest) {
			units[i].newest = info.UpdatedAt
		}
	}
	units = slices.DeleteFunc(units, func(u olderUnit) bool {
		return !slices.ContainsFunc(u.rows, func(info *sessionInfo) bool { return info.ID == u.root && !info.Hidden })
	})
	slices.SortFunc(units, compareOlder)
	return units
}

func compareOlder(x, y olderUnit) int {
	return cmp.Or(y.newest.Compare(x.newest), strings.Compare(y.root, x.root))
}

func (u olderUnit) cursor() string {
	return strconv.FormatInt(u.newest.UnixNano(), 10) + olderCursorSep + u.root
}

func parseOlderCursor(s string) (olderUnit, error) {
	if s == "" {
		return olderUnit{}, nil
	}
	nanos, root, ok := strings.Cut(s, olderCursorSep)
	n, err := strconv.ParseInt(nanos, 10, 64)
	if !ok || err != nil || root == "" {
		return olderUnit{}, errOlderCursor
	}
	return olderUnit{root: root, newest: time.Unix(0, n)}, nil
}
