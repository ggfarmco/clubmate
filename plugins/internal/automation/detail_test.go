package automation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoesSaysItInWordsAndClampsThePrompt(t *testing.T) {
	args := json.RawMessage(`{"session":"agt_1","text":"[Agent graph x, run gr_1: you are node b]\n\nsee gr_1\n2\n3\n4"}`)
	titles := map[string]string{"agt_1": "Review loop · Coder", "gr_1": "Review loop"}

	rows := Does("tr_1", PromptCommand, args, titles, Open{})
	require.Len(t, rows, 3)
	assert.Equal(t, "Send a prompt to Review loop · Coder", rows[0].Children[1].Props["text"])
	assert.Equal(t, "card", rows[1].Type)
	prompt := rows[1].Children[0]
	assert.Equal(t, "see Review loop\n2\n3\n4", prompt.Props["text"])
	assert.Equal(t, 3, prompt.Props["lines"])
	assert.Equal(t, "Show as JSON", rows[2].Children[0].Props["label"])

	rows = Does("tr_1", PromptCommand, args, titles, Open{JSON: true})
	require.Len(t, rows, 4)
	assert.Equal(t, "Hide JSON", rows[2].Children[0].Props["label"])
	assert.Contains(t, rows[3].Props["text"], `"command": "agents.prompt"`)
}

func TestUntil(t *testing.T) {
	now := time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC)
	assert.Equal(t, "in 42 min", Until(now.Add(42*time.Minute), now))
	assert.Equal(t, "in 13 h", Until(now.Add(13*time.Hour), now))
	assert.Equal(t, "in 3 days", Until(now.Add(72*time.Hour), now))
}
