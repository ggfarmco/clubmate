package main

import (
	"encoding/json"
	"slices"
	"time"

	"clubmate/internal/proc"
	"clubmate/plugins/agents/acp"
)

var cycleKinds = []string{acp.UpdateAgentMessageChunk, acp.UpdateAgentThoughtChunk, acp.UpdateToolCall}

type backgroundUpdate struct {
	Cost json.RawMessage `json:"cost"`
	Meta struct {
		ClaudeCode struct {
			ToolResponse json.RawMessage `json:"toolResponse"`
		} `json:"claudeCode"`
	} `json:"_meta"`
}

func (s *session) busyLocked() bool {
	return s.running || s.cycle || len(s.background) > 0
}

func (s *session) workingLocked() bool {
	return s.running || s.cycle
}

func (s *session) trackLocked(kind string, payload json.RawMessage) (started, ended bool) {
	var u backgroundUpdate
	_ = json.Unmarshal(payload, &u)
	switch {
	case s.rec.closed():
	case kind == acp.UpdateToolCallUpdate:
		var res struct {
			BackgroundTaskID string `json:"backgroundTaskId"`
		}
		if json.Unmarshal(u.Meta.ClaudeCode.ToolResponse, &res) == nil && res.BackgroundTaskID != "" {
			if s.background == nil {
				s.background = map[string]bool{}
			}
			s.background[res.BackgroundTaskID] = true
		}
	case kind == acp.UpdateUsage && s.cycle && !s.running && len(u.Cost) > 0 && string(u.Cost) != "null":
		s.cycle = false
		return false, true
	case slices.Contains(cycleKinds, kind) && !s.workingLocked() && s.proc != nil:
		s.cycle = true
		s.background = nil
		s.started = time.Now()
		s.settleLocked()
		return true, false
	}
	return false, false
}

func (s *session) dropBackgroundLocked() {
	s.cycle = false
	s.background = nil
}

func (a *app) pruneBackground(s *session) {
	s.mu.Lock()
	p, agentID, pending := s.proc, s.rec.AgentSessionID, len(s.background) > 0
	s.mu.Unlock()
	if !pending {
		return
	}
	if p != nil {
		procs, err := proc.List()
		if err != nil {
			return
		}
		alive, known := sessionBackground(p.pgid, agentID, len(p.attached()) == 1, procs, proc.ReadArgs)
		if !known || alive {
			return
		}
	}
	s.mu.Lock()
	s.background = nil
	s.mu.Unlock()
}

func sessionBackground(leader int, agentID string, alone bool, procs []proc.Process, args func(int) []string) (alive, known bool) {
	for _, p := range procs {
		if p.PPID != leader {
			continue
		}
		argv := args(p.PID)
		if slices.Contains(argv, "--resume="+agentID) || slices.Contains(argv, "--session-id="+agentID) {
			return runsShell(p.PID, procs), true
		}
	}
	if alone {
		return runsShell(leader, procs), true
	}
	return false, false
}

func runsShell(root int, procs []proc.Process) bool {
	kids := make(map[int][]proc.Process, len(procs))
	for _, p := range procs {
		kids[p.PPID] = append(kids[p.PPID], p)
	}
	var walk func(pid int) bool
	walk = func(pid int) bool {
		return slices.ContainsFunc(kids[pid], func(c proc.Process) bool { return isShell(c.Comm) || walk(c.PID) })
	}
	return walk(root)
}

func (a *app) settleBackground(s *session) {
	a.pruneBackground(s)
	s.mu.Lock()
	if s.busyLocked() || s.rec.Status != statusRunning {
		s.mu.Unlock()
		return
	}
	s.setStatusLocked(statusDone, "")
	ended := s.turnLocked()
	s.mu.Unlock()
	s.turned(ended)
	s.changed(changeStatus, true)
	a.cleanupChild(s)
	a.closeVanishedChildren(s)
}
