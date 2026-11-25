package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFirstPromptTextIsOnlyARecordedFirstPrompt(t *testing.T) {
	t.Parallel()
	block := json.RawMessage(`{"type":"text","text":"Add b.go"}`)
	recorded := &entry{Kind: entryUser, ID: promptEntryID(1), Turn: 1, Text: "Add b.go and a chunk", Content: []json.RawMessage{block}}
	cases := map[string]struct {
		entries []*entry
		want    string
	}{
		"recorded prompt":      {[]*entry{recorded}, "Add b.go"},
		"replayed chunk first": {[]*entry{{Kind: entryUser, ID: "user-1", Turn: 1, Text: "[Clubmate] from x"}, recorded}, ""},
		"voice first":          {[]*entry{{Kind: entryUser, ID: promptEntryID(0), Voice: &voicePrompt{Heard: "hi"}}, recorded}, ""},
		"verdict prompt first": {[]*entry{{Kind: entryUser, ID: promptEntryID(1), Turn: 1, Content: []json.RawMessage{json.RawMessage(`{"type":"text","text":"[Clubmate] Land review of x asks for changes"}`)}}}, ""},
	}
	for name, c := range cases {
		s := &session{loaded: true, entries: c.entries}
		assert.Equal(t, c.want, s.firstPromptText(), name)
	}
}
