package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"clubmate/internal/proc"
	"clubmate/pkg/pluginsdk"
)

const (
	coreID          = "core"
	coreTitle       = "Clubmate core"
	sessionHostComm = "clubmate-session"
)

func coreGroup(workspaceID string) group {
	return group{ID: coreID, WorkspaceID: workspaceID, Title: coreTitle, Owner: coreID}
}

func scanCore(ctx context.Context, tracked []pluginsdk.ProcessNode) ([]pluginsdk.ProcessNode, error) {
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
	usage, err := readUsage(ctx, members)
	if err != nil {
		return nil, err
	}
	nodes := make([]pluginsdk.ProcessNode, 0, len(members))
	for _, p := range members {
		u, ok := usage[p.PID]
		if !ok {
			continue
		}
		p.RSSKiB, p.CPU, p.Args = u.rssKiB, u.cpu, proc.ReadArgs(p.PID)
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

type usage struct {
	rssKiB int64
	cpu    float64
}

func readUsage(ctx context.Context, procs []pluginsdk.ProcessInfo) (map[int]usage, error) {
	pids := make([]string, len(procs))
	for i, p := range procs {
		pids[i] = strconv.Itoa(p.PID)
	}
	out, err := exec.CommandContext(ctx, "/bin/ps", "-o", "pid=,rss=,pcpu=", "-p", strings.Join(pids, ",")).Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("run ps: %w", err)
	}
	return parseUsage(string(out)), nil
}

func parseUsage(out string) map[int]usage {
	usages := make(map[int]usage)
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		rss, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		usages[pid] = usage{rssKiB: rss, cpu: cpu}
	}
	return usages
}
