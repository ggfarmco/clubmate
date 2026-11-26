package main

import "strings"

type handoffMark struct {
	To string `json:"to,omitempty"`
}

func (s *session) handoffsLocked() map[*entry]*handoffMark {
	if s.rec.Run == nil || len(s.rec.Run.Next) == 0 {
		return nil
	}
	relayed := map[int]bool{}
	for _, t := range s.turns {
		relayed[t.Turn] = t.relayed && !t.failed
	}
	running := s.runningTurnLocked()
	last := map[int]*entry{}
	for _, en := range s.entries {
		if en.Kind == entryAgent && en.Subagent == "" && relayed[en.Turn] && en.Turn != running && strings.TrimSpace(en.Text) != "" {
			last[en.Turn] = en
		}
	}
	h := &handoffMark{To: strings.Join(s.rec.Run.Next, ", ")}
	out := make(map[*entry]*handoffMark, len(last))
	for _, en := range last {
		out[en] = h
	}
	return out
}

func sameHandoff(a, b *handoffMark) bool {
	return a == b || a != nil && b != nil && *a == *b
}
