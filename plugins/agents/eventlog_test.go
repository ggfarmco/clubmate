package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogAppendAfterTornLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(`{"kind":"session/new"}`+"\n"+`{"kind":"tor`), 0o600))
	l := newEventLog(path)
	l.write(envelope{Kind: kindTurnEnd})
	l.close()

	var kinds []string
	require.NoError(t, l.replay(func(e envelope) { kinds = append(kinds, e.Kind) }))

	assert.Equal(t, []string{kindNew, kindTurnEnd}, kinds)
}
