package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntoRankOrdersMatches(t *testing.T) {
	t.Parallel()
	ranks := map[string]int{}
	for _, match := range []string{"release/1.0", "release/*", "base", "default", "any", "feat/*"} {
		ranks[match] = intoRank(match, "release/1.0", "release/1.0", "")
	}

	assert.Equal(t, map[string]int{"release/1.0": 5, "release/*": 4, "base": 0, "default": 2, "any": 1, "feat/*": 0}, ranks)
	assert.Equal(t, 2, intoRank("default", "main", "", ""))
}
