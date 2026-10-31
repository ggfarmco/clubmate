package automation

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoesShowsThePromptAndTheRestOfTheArgs(t *testing.T) {
	rows := Does("agents.prompt", json.RawMessage(`{"session":"agt_1","text":"Check your children"}`))
	require.Len(t, rows, 3)
	assert.Equal(t, "Check your children", rows[1].Children[1].Props["text"])
	assert.Equal(t, `{"session":"agt_1"}`, rows[2].Children[1].Props["text"])
}
