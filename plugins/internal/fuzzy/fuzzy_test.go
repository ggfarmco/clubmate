package fuzzy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/fuzzy"
)

func TestRankPrefersBasename(t *testing.T) {
	paths := []string{"docs/domain.md", "main/app.go", "src/app/main.go"}

	got := fuzzy.Rank(paths, "main", 10)
	require.Len(t, got, 3)
	assert.ElementsMatch(t, []string{"src/app/main.go", "main/app.go"}, []string{got[0].Path, got[1].Path})
	assert.Equal(t, "docs/domain.md", got[2].Path)

	got = fuzzy.Rank(paths, "SAM", 1)
	require.Len(t, got, 1)
	assert.Equal(t, "src/app/main.go", got[0].Path)
	assert.Equal(t, []int{0, 4, 8}, got[0].Positions)
}
