package main

import "slices"

const (
	groupNeedsYou = "needsYou"
	groupRunning  = "running"
	groupOpen     = "open"
	groupDone     = "done"
	groupClosed   = "closed"
	groupService  = "service"
)

var groupOrder = []string{groupNeedsYou, groupRunning, groupOpen, groupDone, groupClosed, groupService}

func ownGroup(info sessionInfo) string {
	switch {
	case info.closed():
		return groupClosed
	case info.NeedsYou:
		return groupNeedsYou
	case info.Service:
		return groupService
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
		if p, ok := index[info.ParentSession]; ok && p != i {
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
		for _, k := range kids[i] {
			if visiting[k] {
				continue
			}
			if g := lift(k); g == groupNeedsYou || (g == groupRunning || g == groupOpen) && own != groupService {
				best = min(best, slices.Index(groupOrder, g))
			}
		}
		infos[i].Group = groupOrder[best]
		return infos[i].Group
	}
	for i := range infos {
		infos[i].Active = !infos[i].Service && infos[i].Kind == "" && (infos[i].Status == statusRunning || infos[i].Status == statusWaiting)
		clear(visiting)
		lift(i)
	}
}
