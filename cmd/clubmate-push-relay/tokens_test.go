package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokensRecheckIsThrottledButPicksUpChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens")
	studio, err := addToken(path, "studio")
	require.NoError(t, err)
	toks, err := loadTokens(path)
	require.NoError(t, err)
	toks.recheck = time.Hour
	require.NoError(t, toks.refresh())

	air, err := addToken(path, "air")
	require.NoError(t, err)
	require.NoError(t, toks.refresh())
	_, _, early := toks.lookup(air, time.Now(), nil)

	toks.mu.Lock()
	toks.checked = time.Time{}
	toks.mu.Unlock()
	require.NoError(t, toks.refresh())
	name, _, late := toks.lookup(air, time.Now(), nil)
	_, _, known := toks.lookup(studio, time.Now(), nil)

	assert.False(t, early)
	assert.True(t, late)
	assert.Equal(t, "air", name)
	assert.True(t, known)
}
