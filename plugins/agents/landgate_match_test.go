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

func TestIntoGlobDepth(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, intoRank("release/*", "release/1.0/hotfix", "main", ""))
	assert.Equal(t, 4, intoRank("release/**", "release/1.0/hotfix", "main", ""))
}

func TestValidInto(t *testing.T) {
	t.Parallel()
	for _, into := range []string{"any", "default", "base", "main", "release/*", "release/**", "feat/a[0-9]"} {
		assert.NoError(t, validInto(into), into)
	}
	for _, into := range []string{"", " main", "re lease", "refs/heads/main", "Any", "DEFAULT", "bAse", "a[", "a\\", "x/[a/b]", "x//y", "x/[A-Z]", "x/[[:alpha:]]", "x/[[=a=]]", "x/[[.a.]]"} {
		assert.ErrorIs(t, validInto(into), errGateInto, into)
	}
}
