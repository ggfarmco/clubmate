package main

import (
	"os"
	"sync"

	"clubmate/internal/proc"
	"clubmate/pkg/pluginsdk"
)

const (
	coreID          = "core"
	coreTitle       = "Clubmate core"
	sessionHostComm = "clubmate-session"
)

var (
	coreSampler = proc.NewSampler()
	coreArgs    = newArgvCache(proc.ReadArgs)
)

type argvKey struct {
	pid   int
	start int64
}

type argvCache struct {
	mu   sync.Mutex
	read func(int) []string
	args map[argvKey][]string
}

func newArgvCache(read func(int) []string) *argvCache {
	return &argvCache{read: read, args: map[argvKey][]string{}}
}

func (c *argvCache) fill(procs []pluginsdk.ProcessInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make(map[argvKey][]string, len(procs))
	for i := range procs {
		key := argvKey{pid: procs[i].PID, start: procs[i].StartTime.UnixNano()}
		args, ok := c.args[key]
		if !ok {
			args = c.read(procs[i].PID)
		}
		if args != nil {
			kept[key] = args
		}
		procs[i].Args = args
	}
	c.args = kept
}

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
	coreArgs.fill(sampled)
	nodes := make([]pluginsdk.ProcessNode, 0, len(sampled))
	for _, p := range sampled {
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
