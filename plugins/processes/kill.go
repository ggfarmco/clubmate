package main

import (
	"context"

	"clubmate/pkg/pluginsdk"
)

type killedEvent struct {
	ProjectID string `json:"projectId"`
	PIDs      []int  `json:"pids"`
}

func killSelected(ctx context.Context, h *pluginsdk.Host, projectID string, st projectState) error {
	if st.pid == 0 {
		return nil
	}
	nodes, err := h.ProcessTree(ctx, projectID, st.session)
	if err != nil {
		return err
	}
	pids := subtree(nodes, st.pid)
	if len(pids) == 0 {
		return nil
	}
	_, err = h.KillProcesses(ctx, pluginsdk.KillRequest{PIDs: pids, ProjectID: projectID, SessionID: st.session})
	return err
}

func killOrphans(ctx context.Context, h *pluginsdk.Host, projectID, sessionID string) ([]int, error) {
	return h.KillProcesses(ctx, pluginsdk.KillRequest{ProjectID: projectID, SessionID: sessionID, OrphansOnly: true})
}

func (a *app) autoKill(ctx context.Context, h *pluginsdk.Host) {
	a.mu.Lock()
	var enabled []string
	for id, st := range a.projects {
		if st.auto {
			enabled = append(enabled, id)
		}
	}
	a.mu.Unlock()
	for _, id := range enabled {
		sweep(ctx, h, id)
	}
}

func sweep(ctx context.Context, h *pluginsdk.Host, projectID string) {
	killed, err := killOrphans(ctx, h, projectID, "")
	if err != nil {
		logError("auto-kill "+projectID, err)
		return
	}
	if len(killed) == 0 {
		return
	}
	_, err = h.Publish(ctx, killedTopic, killedEvent{ProjectID: projectID, PIDs: killed})
	logError("publish "+killedTopic, err)
}

func subtree(nodes []pluginsdk.ProcessNode, root int) []int {
	children := make(map[int][]int, len(nodes))
	found := false
	for _, n := range nodes {
		children[n.Process.PPID] = append(children[n.Process.PPID], n.Process.PID)
		found = found || n.Process.PID == root
	}
	if !found {
		return nil
	}
	pids := []int{root}
	seen := map[int]bool{root: true}
	for i := 0; i < len(pids); i++ {
		for _, child := range children[pids[i]] {
			if !seen[child] {
				seen[child] = true
				pids = append(pids, child)
			}
		}
	}
	return pids
}
