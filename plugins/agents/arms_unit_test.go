package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/agents/api"
)

func TestPickArmIsStableAndEven(t *testing.T) {
	t.Parallel()
	arms := []string{"opus", "sonnet"}
	counts := map[string]int{}
	for i := range 100 {
		sid := fmt.Sprintf("%s_%032x", api.IDPrefix, i)
		arm := pickArm(sid, arms)
		assert.Equal(t, arm, pickArm(sid, arms))
		counts[arm]++
	}
	assert.InDelta(t, 50, counts["opus"], 20)
	assert.InDelta(t, 50, counts["sonnet"], 20)
}
