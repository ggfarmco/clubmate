package main

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionMetaPutsPlainBetweenHintAndSelf(t *testing.T) {
	m := sessionMeta(record{ID: "a"}, "", selfParts{Prompt: "be brief"}, false, true)
	assert.Equal(t, askUserText+"\n\n"+plainRule+"\n\nbe brief", m.SystemPrompt.Append)
}

func TestPlainIsOnUnlessStoredOff(t *testing.T) {
	var mu sync.Mutex
	var stored json.RawMessage
	h := scriptedHost(t, func(method, key string) any {
		mu.Lock()
		defer mu.Unlock()
		if method == "storage.get" && key == plainKey && stored != nil {
			return map[string]any{"found": true, "value": stored}
		}
		return nil
	})
	a := &app{host: h}

	assert.True(t, a.plainEnabled(t.Context()))
	mu.Lock()
	stored = json.RawMessage("false")
	mu.Unlock()
	assert.False(t, a.plainEnabled(t.Context()))
}
