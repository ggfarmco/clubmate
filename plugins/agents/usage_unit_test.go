package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShareLeavesOutAWindowThatRoundsToZero(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000, 0)
	s := shareState{Windows: map[string]*shareWindow{
		windowFiveHour: {ResetsAt: 2_000, Base: 0.10, Last: 0.11, Total: 1_000, Sessions: map[string]float64{"tiny": 1, "big": 999}},
		windowSevenDay: {ResetsAt: 2_000, Base: 0.10, Last: 0.20, Total: 2, Sessions: map[string]float64{"tiny": 1, "big": 1}},
	}}

	assert.Equal(t, map[string]float64{windowSevenDay: 5}, s.of("tiny", now))
}
