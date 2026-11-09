package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReposInStopsAtItsTimeLimit(t *testing.T) {
	parent := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(parent, "one", ".git"), 0o700))
	stuck := func(ctx context.Context, _ string) time.Time {
		<-ctx.Done()
		return time.Time{}
	}

	walked := findReposIn(t.Context(), parent, time.Now(), false, stuck)
	assert.True(t, walked.Partial)
	assert.Empty(t, walked.Repos)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	dated := findReposIn(ctx, parent, time.Now().Add(time.Minute), true, stuck)
	assert.True(t, dated.Partial)
	require.Len(t, dated.Repos, 1)
	assert.Equal(t, "one", dated.Repos[0].Name)
}
