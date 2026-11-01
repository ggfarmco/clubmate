package main

import (
	"os"

	"clubmate/internal/proc"
	"clubmate/pkg/pluginsdk"
)

const (
	coreID          = "core"
	coreTitle       = "Clubmate core"
	sessionHostComm = "clubmate-session"
)

var coreSampler = proc.NewSampler()

func coreGroup(workspaceID string) group {
	return group{ID: coreID, WorkspaceID: workspaceID, Title: coreTitle, Owner: coreID}
}

func scanCore(tracked []pluginsdk.ProcessNode) ([]pluginsdk.ProcessNode, error) {
	root := os.Getppid()
	if root <= 1 {
		return nil, nil
	}
	procs, err := proc.List()
	if err != nil {
		return nil, err
	}
	members := coreMembers(procs, root, tracked)
	if len(members) == 0 {
		return nil, nil
	}
	sampled := coreSampler.Sample(members)
	nodes := make([]pluginsdk.ProcessNode, 0, len(sampled))
	for _, p := range sampled {
		p.Args = proc.ReadArgs(p.PID)
		nodes = append(nodes, pluginsdk.ProcessNode{Process: p, SessionID: coreID})
	}
	return nodes, nil
}

func coreMembers(procs []pluginsdk.ProcessInfo, root int, tracked []pluginsdk.ProcessNode) []pluginsdk.ProcessInfo {
	skip := make(map[int]bool, len(tracked))
	for _, n := range tracked {
		skip[n.Process.PID] = true
	}
	children := make(map[int][]int, len(procs))
	byPID := make(map[int]pluginsdk.ProcessInfo, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
		if p.PID != p.PPID {
			children[p.PPID] = append(children[p.PPID], p.PID)
		}
	}
	if _, ok := byPID[root]; !ok {
		return nil
	}
	out := []pluginsdk.ProcessInfo{byPID[root]}
	for i := 0; i < len(out); i++ {
		for _, pid := range children[out[i].PID] {
			p := byPID[pid]
			if skip[pid] || p.Comm == sessionHostComm {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}
