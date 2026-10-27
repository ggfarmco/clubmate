package fswatch_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/fswatch"
)

func TestKeep(t *testing.T) {
	for path, want := range map[string]bool{
		"repo/a.go":                 true,
		"repo/.git/objects/ab/cd":   false,
		"repo/.git/index":           true,
		"repo/.git/refs/heads/main": true,
		"repo/.git/logs/HEAD":       false,
		"repo/.git/HEAD":            true,
		"repo/.git/refs/tags/v1":    false,

		"repo/.git/worktrees/wt/HEAD":      true,
		"repo/.git/worktrees/wt/logs/HEAD": false,
	} {
		assert.Equal(t, want, fswatch.Keep(path), path)
	}
}
