package plural_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/internal/plural"
)

func TestPlural(t *testing.T) {
	for _, tc := range []struct {
		n         int
		word, cnt string
	}{
		{0, "runs", "missed 0 runs"},
		{1, "run", "missed 1 run"},
		{2, "runs", "missed 2 runs"},
	} {
		assert.Equal(t, tc.word, plural.Word(tc.n, "run", "runs"))
		assert.Equal(t, tc.cnt, plural.Count(tc.n, "missed 1 run", "missed %d runs"))
	}
}
