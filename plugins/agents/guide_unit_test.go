package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

func TestQuizConceptState(t *testing.T) {
	answered := func(confidence, verdict, flag string) entry {
		resp := acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: map[string]any{"answer": "30", "confidence": confidence}}
		return entry{Response: &resp, Grade: &quizGrade{Verdict: verdict, Flag: flag}}
	}
	for _, c := range []struct {
		name string
		en   entry
		want string
	}{
		{"correct and sure", answered("sure", verdictCorrect, ""), sdui.TourKnown},
		{"correct and think so", answered("think", verdictCorrect, ""), sdui.TourShaky},
		{"close", answered("sure", verdictClose, ""), sdui.TourShaky},
		{"wrong", answered("guess", verdictWrong, ""), sdui.TourMissed},
		{"sure and wrong", answered("sure", verdictWrong, flagSureWrong), sdui.TourMissed},
		{"waiting for a grade", answered("sure", verdictPending, ""), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, quizConceptState(c.en))
		})
	}
}
