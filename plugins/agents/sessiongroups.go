package main

import "slices"

const (
	groupNeedsYou = "needsYou"
	groupRunning  = "running"
	groupDone     = "done"
	groupClosed   = "closed"
	groupService  = "service"
)

var groupOrder = []string{groupNeedsYou, groupRunning, groupDone, groupClosed, groupService}

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
	var lift func(i int, visiting map[int]bool) string
	lift = func(i int, visiting map[int]bool) string {
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
			if g := lift(k, visiting); g == groupNeedsYou || g == groupRunning && own != groupService {
				best = min(best, slices.Index(groupOrder, g))
			}
		}
		infos[i].Group = groupOrder[best]
		return infos[i].Group
	}
	for i := range infos {
		infos[i].Active = !infos[i].Service && (infos[i].Status == statusRunning || infos[i].Status == statusWaiting)
		lift(i, map[int]bool{})
	}
}
