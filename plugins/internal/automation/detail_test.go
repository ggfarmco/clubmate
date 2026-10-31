package automation

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoesKeepsThePromptAndHidesTheRestBehindJSON(t *testing.T) {
	args := json.RawMessage(`{"session":"agt_1","text":"Check your children"}`)

	rows := Does("cj_1", "agents.prompt", args, false)
	require.Len(t, rows, 3)
	assert.Equal(t, "Check your children", rows[1].Children[1].Props["text"])
	assert.Equal(t, "cj_1/json", rows[2].Children[1].ID)
	assert.Equal(t, "Show as JSON", rows[2].Children[1].Props["label"])

	rows = Does("cj_1", "agents.prompt", args, true)
	require.Len(t, rows, 4)
	assert.Equal(t, "Hide JSON", rows[2].Children[1].Props["label"])
	assert.Equal(t, "{\n  \"session\": \"agt_1\"\n}", rows[3].Children[1].Props["text"])
}
