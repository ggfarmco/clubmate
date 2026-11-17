package main

import (
	"cmp"
	"slices"
	"time"
)

const (
	groupNeedsYou = "needsYou"
	groupRunning  = "running"
	groupOpen     = "open"
	groupPaused   = "paused"
	groupDone     = "done"
	groupClosed   = "closed"
	groupService  = "service"
)

var groupOrder = []string{groupNeedsYou, groupRunning, groupOpen, groupPaused, groupDone, groupClosed, groupService}

func ownGroup(info sessionInfo) string {
	switch {
	case info.closed():
		return groupClosed
	case info.NeedsYou:
		return groupNeedsYou
	case info.Service:
		return groupService
	case info.limitPaused():
		return groupPaused
	case info.Status == statusRunning:
		return groupRunning
	case info.OpenCount > 0 && info.Status == statusDone:
		return groupOpen
	}
	return groupDone
}

func setGroups(infos []sessionInfo) {
	index := make(map[string]int, len(infos))
	for i, info := range infos {
		index[info.ID] = i
	}
	kids := map[int][]int{}
	for i, info := range infos {
		parent := statusParent(info.record)
		if info.Kind == kindRun {
			parent = cmp.Or(parent, info.RowUnder)
		}
		if p, ok := index[parent]; ok && p != i {
			kids[p] = append(kids[p], i)
		}
	}
	visiting := map[int]bool{}
	var lift func(i int) string
	lift = func(i int) string {
		if infos[i].Group != "" {
			return infos[i].Group
		}
		visiting[i] = true
		own := ownGroup(infos[i])
		best := slices.Index(groupOrder, own)
		var until int64
		if own == groupPaused {
			until = infos[i].LimitWait.At.Truncate(time.Minute).Unix()
		}
		for _, k := range kids[i] {
			if visiting[k] {
				continue
			}
			g := lift(k)
			if g == groupNeedsYou || (g == groupRunning || g == groupOpen || g == groupPaused) && own != groupService && !infos[k].record.isAside() {
				best = min(best, slices.Index(groupOrder, g))
			}
			if g == groupPaused && own != groupService && (until == 0 || *infos[k].PausedUntil < until) {
				until = *infos[k].PausedUntil
			}
		}
		infos[i].Group = groupOrder[best]
		if infos[i].Group == groupPaused {
			infos[i].PausedUntil = &until
		}
		return infos[i].Group
	}
	for i := range infos {
		infos[i].Paused = infos[i].limitPaused()
		infos[i].Active = !infos[i].Service && infos[i].Kind == "" && (infos[i].Status == statusRunning || infos[i].Status == statusWaiting)
		clear(visiting)
		lift(i)
	}
}

func (info sessionInfo) limitPaused() bool {
	return info.Status == statusError && info.LimitWait.pausing()
}
