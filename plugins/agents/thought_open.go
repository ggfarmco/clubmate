package main

import "strings"

const askUserQuestionTool = "AskUserQuestion"

func isQuestionPoint(en *entry) bool {
	return en.Kind == entryElicitation || en.Kind == entryTool && en.toolName == askUserQuestionTool
}

func openThoughts(entries []*entry, running int) map[*entry]bool {
	open := map[*entry]bool{}
	var thought *entry
	replied := false
	flush := func() {
		if thought != nil && !replied {
			open[thought] = true
		}
		thought, replied = nil, false
	}
	for i, en := range entries {
		switch {
		case isQuestionPoint(en):
			flush()
		case strings.TrimSpace(en.Text) == "":
		case en.Kind == entryAgent:
			replied = true
		case en.Kind == entryThought:
			thought = en
		}
		if i == len(entries)-1 || entries[i+1].Turn != en.Turn {
			if i != len(entries)-1 || en.Turn != running {
				flush()
			}
		}
	}
	return open
}

func (s *session) runningTurnLocked() int {
	if s.workingLocked() {
		return s.turn
	}
	return 0
}
