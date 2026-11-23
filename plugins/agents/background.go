package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"clubmate/internal/proc"
	"clubmate/plugins/agents/acp"
)

var cycleKinds = []string{acp.UpdateAgentMessageChunk, acp.UpdateAgentThoughtChunk, acp.UpdateToolCall}

type backgroundUpdate struct {
	Cost       json.RawMessage `json:"cost"`
	ToolCallID string          `json:"toolCallId"`
	RawInput   struct {
		Command      string `json:"command"`
		InBackground bool   `json:"run_in_background"`
	} `json:"rawInput"`
	RawOutput json.RawMessage `json:"rawOutput"`
	Meta      struct {
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
	switch {
	case s.rec.closed():
	case kind == acp.UpdateToolCallUpdate:
		var u backgroundUpdate
		_ = json.Unmarshal(payload, &u)
		var res struct {
			BackgroundTaskID string `json:"backgroundTaskId"`
		}
		if json.Unmarshal(u.Meta.ClaudeCode.ToolResponse, &res) == nil && res.BackgroundTaskID != "" {
			if s.background == nil {
				s.background = map[string]bool{}
			}
			s.background[res.BackgroundTaskID] = true
		}
		s.noteBackgroundCallLocked(u, res.BackgroundTaskID)
	case slices.Contains(cycleKinds, kind) && s.running && s.answered:
		s.late = true
	case kind == acp.UpdateUsage && s.late && s.running && hasCost(payload):
		s.late = false
	case kind == acp.UpdateUsage && s.cycle && !s.running && hasCost(payload):
		s.cycle = false
		if s.dropping > 0 {
			s.followOwed = true
		} else {
			s.followedUpLocked()
		}
		s.refuseCallsLocked()
		return false, true
	case slices.Contains(cycleKinds, kind) && !s.workingLocked() && s.proc != nil:
		s.cycle = true
		s.started = time.Now()
		s.settleLocked()
		return true, false
	}
	return false, false
}

func hasCost(payload json.RawMessage) bool {
	var u backgroundUpdate
	_ = json.Unmarshal(payload, &u)
	return len(u.Cost) > 0 && string(u.Cost) != "null"
}

func (s *session) dropBackgroundLocked() {
	s.cycle = false
	s.background = nil
}

func (a *app) pruneBackground(s *session) bool {
	s.mu.Lock()
	p, agentID, pending := s.proc, s.rec.AgentSessionID, len(s.background) > 0
	s.mu.Unlock()
	if !pending {
		return false
	}
	if p != nil {
		procs, err := proc.List()
		if err != nil {
			return false
		}
		alive, known := sessionBackground(p.pgid, agentID, len(p.attached()) == 1, procs, proc.ReadArgs)
		if !known || alive {
			return false
		}
	}
	s.mu.Lock()
	s.background = nil
	s.mu.Unlock()
	return true
}

func (a *app) dropEnded(s *session) {
	s.mu.Lock()
	p, agentID := s.proc, s.rec.AgentSessionID
	known := slices.Collect(maps.Keys(s.background))
	var commands []bgCommand
	for _, c := range slices.Backward(s.bgCalls) {
		if c.task != "" && s.background[c.task] {
			commands = append(commands, bgCommand{task: c.task, text: plainCommand(c.command)})
		}
	}
	s.mu.Unlock()
	if len(known) == 0 {
		return
	}
	var live map[string]bool
	if p != nil {
		if procs, err := proc.List(); err == nil {
			live = liveCommands(p.pgid, agentID, len(p.attached()) == 1, procs, proc.ReadArgs, commands)
		}
	}
	s.mu.Lock()
	s.dropDeadLocked(known, live)
	s.mu.Unlock()
}

func (s *session) dropDeadLocked(known []string, live map[string]bool) {
	for _, task := range known {
		if !live[task] {
			delete(s.background, task)
		}
	}
	if len(s.background) == 0 {
		s.background = nil
	}
}

type bgCommand struct {
	task string
	text string
}

func liveCommands(leader int, agentID string, alone bool, procs []proc.Process, args func(int) []string, commands []bgCommand) map[string]bool {
	root, known := sessionRoot(leader, agentID, alone, procs, args)
	if !known {
		return nil
	}
	kids := childrenOf(procs)
	live := map[string]bool{}
	var walk func(pid int)
	walk = func(pid int) {
		for _, c := range kids[pid] {
			if isShell(c.Comm) {
				argv := plainCommand(strings.Join(args(c.PID), " "))
				if i := slices.IndexFunc(commands, func(b bgCommand) bool { return !live[b.task] && runsCommand(argv, b.text) }); i >= 0 {
					live[commands[i].task] = true
					continue
				}
			}
			walk(c.PID)
		}
	}
	walk(root)
	return live
}

func childrenOf(procs []proc.Process) map[int][]proc.Process {
	kids := make(map[int][]proc.Process, len(procs))
	for _, p := range procs {
		kids[p.PPID] = append(kids[p.PPID], p)
	}
	return kids
}

func runsCommand(argv, command string) bool {
	if command == "" {
		return false
	}
	for at := 0; ; {
		i := strings.Index(argv[at:], command)
		if i < 0 {
			return false
		}
		start, end := at+i, at+i+len(command)
		if (start == 0 || !commandRune(argv[start-1])) && (end == len(argv) || !commandRune(argv[end])) {
			return true
		}
		at = start + 1
	}
}

func commandRune(b byte) bool {
	return b == '-' || b == '_' || b == '.' || b == '/' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

var unquote = strings.NewReplacer("'", "", `"`, "", `\`, "")

func plainCommand(text string) string {
	return strings.Join(strings.Fields(unquote.Replace(text)), " ")
}

func sessionBackground(leader int, agentID string, alone bool, procs []proc.Process, args func(int) []string) (alive, known bool) {
	root, known := sessionRoot(leader, agentID, alone, procs, args)
	return known && len(shellsUnder(root, procs)) > 0, known
}

func sessionRoot(leader int, agentID string, alone bool, procs []proc.Process, args func(int) []string) (int, bool) {
	for _, p := range procs {
		if p.PPID != leader {
			continue
		}
		argv := args(p.PID)
		if slices.Contains(argv, "--resume="+agentID) || slices.Contains(argv, "--session-id="+agentID) {
			return p.PID, true
		}
	}
	return leader, alone
}

func shellsUnder(root int, procs []proc.Process) []int {
	kids := childrenOf(procs)
	var out []int
	var walk func(pid int)
	walk = func(pid int) {
		for _, c := range kids[pid] {
			if isShell(c.Comm) {
				out = append(out, c.PID)
			}
			walk(c.PID)
		}
	}
	walk(root)
	return out
}

func (a *app) settleBackground(s *session) {
	a.pruneBackground(s)
	a.logRefusals(s)
	a.finishTurn(s)
	a.retryAfterCycle(s)
}
