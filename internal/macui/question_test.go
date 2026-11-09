package macui_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const askScript = `{"prompt": [{"match": "Ask", "elicitation": {"mode": "form", "toolCallId": "ask1", "message": "Two decisions",
  "requestedSchema": {"type": "object", "properties": {
    "question_0": {"type": "string", "title": "Scope", "description": "Where should the rate limit apply?",
      "oneOf": [{"const": "Per workspace", "title": "Per workspace"}, {"const": "Global", "title": "Global"}]},
    "question_0_custom": {"type": "string", "title": "Other"},
    "question_1": {"type": "array", "title": "Exempt", "description": "Which notifications should skip the limit?",
      "items": {"anyOf": [{"const": "Questions", "title": "Questions"}, {"const": "Cron failures", "title": "Cron failures"}]}},
    "question_1_custom": {"type": "string", "title": "Other"}}}}}]}`

func TestQuestionCardPointerAndSendKey(t *testing.T) {
	in := start(t, "macui-question", askScript)
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Ask two things")
	card := spawned.Session + "/elicitation/elicitation-1"
	in.eventually("the question card shows", func() bool { return in.snapshot().main().has(card + "/submit") })

	for _, row := range []string{card + "/f/0#Per workspace", card + "/f/2#Questions"} {
		in.pointer("click", row)
		in.eventually("a click picks "+row, func() bool {
			e := in.snapshot().main().node(row)
			return e != nil && slices.Contains(strings.Fields(e.Role), "selected")
		})
	}

	assert.Equal(t, []string{"monitor"}, in.key("--focus", spawned.Session+"/composer/0", "cmd+return"))
	in.eventually("the first cmd+return from the empty composer answers the card", func() bool {
		return !in.snapshot().main().has(card + "/submit")
	})
}
