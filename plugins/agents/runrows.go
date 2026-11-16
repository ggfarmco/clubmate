package main

import (
	"regexp"
	"strconv"
	"strings"

	"clubmate/plugins/agents/api"
	"clubmate/plugins/agents/graphs"
)

const (
	runRunning   = "running"
	runAsks      = "asks"
	runEnded     = "ended"
	runStopped   = "stopped"
	runFailed    = "failed"
	runEmptied   = "emptied"
	homeGroup    = "group"
	homeClosed   = "closed"
	graphRunning = "running"
	startFailed  = "start failed:"
)

var reportedEnd = regexp.MustCompile(`^.+ reported (.+)$`)

func endKind(reason string) (string, string) {
	switch {
	case strings.HasPrefix(reason, startFailed):
		return runFailed, runFailed
	case reportedEnd.MatchString(reason):
		return runEnded, reportedEnd.FindStringSubmatch(reason)[1]
	}
	return runStopped, runStopped
}

func runRows(runs []graphs.RunSnapshot, infos map[string]sessionInfo) []api.RunRow {
	out := make([]api.RunRow, 0, len(runs))
	for _, r := range runs {
		out = append(out, runRow(r, infos))
	}
	return out
}

func runRow(r graphs.RunSnapshot, infos map[string]sessionInfo) api.RunRow {
	row := api.RunRow{ID: r.ID, Graph: r.Graph, Title: r.Title, Number: r.Number, Sessions: []string{}, Home: homeGroup}
	var open []sessionInfo
	var waiting *graphs.SnapshotNode
	var working string
	for i, n := range r.Nodes {
		info, ok := infos[n.Session]
		if n.Session == "" || !ok {
			continue
		}
		row.Sessions = append(row.Sessions, n.Session)
		if info.closed() {
			continue
		}
		open = append(open, info)
		if info.NeedsYou && waiting == nil {
			waiting = &r.Nodes[i]
		}
		if info.Status == statusRunning && info.Activity != nil && working == "" {
			working = info.Activity.Title
		}
	}
	if len(open) > 0 {
		row.Worktree = open[0].Worktree
	} else {
		row.Worktree = r.Dir
	}
	switch {
	case r.Status == graphRunning && r.Need != nil:
		row.State, row.Detail, row.NeedsYou, row.Request = runAsks, "asks you", true, r.Need.Request
	case r.Status == graphRunning && waiting != nil:
		row.State, row.Detail, row.Pointer = runRunning, "→ "+waiting.Role, waiting.ID
	case r.Status == graphRunning:
		row.State, row.Detail = runRunning, "turn "+strconv.Itoa(r.Turns)
		if working != "" {
			row.Detail = working
		}
	case len(row.Sessions) == 0:
		row.State, row.Detail = runEmptied, "no sessions"
	default:
		row.State, row.Detail = endKind(r.Reason)
	}
	if r.Status != graphRunning && len(open) == 0 {
		row.Home = homeClosed
	}
	row.Group, row.Status = runGroup(row)
	return row
}

func runGroup(row api.RunRow) (string, string) {
	switch {
	case row.NeedsYou:
		return groupNeedsYou, statusWaiting
	case row.State == runRunning:
		return groupRunning, statusRunning
	case row.State == runEmptied || row.Home == homeClosed:
		return groupClosed, statusClosed
	case row.State == runFailed:
		return groupDone, statusError
	}
	return groupDone, statusDone
}
