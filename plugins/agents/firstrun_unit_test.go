package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnInstallWithSessionsBeforeTheHintsGetsNone(t *testing.T) {
	kv := memStore{}
	var old firstRun
	require.NoError(t, old.load(t.Context(), kv, true))
	var fresh firstRun
	require.NoError(t, fresh.load(t.Context(), memStore{}, false))

	var reloaded firstRun
	require.NoError(t, reloaded.load(t.Context(), kv, false))
	assert.Equal(t, spentHints(), reloaded.get())
	assert.Equal(t, firstRunHints{}, fresh.get())
}

func TestATurnZeroNoticeDrawsNoOfferRow(t *testing.T) {
	v := transcriptView{
		Session: record{ID: "agt_1", Agent: agentClaude},
		Entries: []*entry{{Kind: entryNotice, ID: "n0", Notice: json.RawMessage(`{"title":"Started on main"}`)}},
	}

	tree := transcript(screen{View: v})

	assert.Empty(t, nodesOf(tree, "offer"))
	assert.NotEmpty(t, texts(tree))
}

func TestAFailedOfferLeavesThePhoneHintForTheNextTurn(t *testing.T) {
	var f firstRun
	require.NoError(t, f.load(t.Context(), memStore{}, false))
	f.claim(func(h *firstRunHints) bool { h.Session = "agt_1"; return true })

	_, failed := f.offerPhone("agt_1", 1, func() (string, error) { return "", errors.New("remote not running") })
	state, offered := f.offerPhone("agt_1", 2, func() (string, error) { return "offer", nil })
	_, again := f.offerPhone("agt_1", 3, func() (string, error) { return "offer", nil })

	assert.False(t, failed)
	assert.True(t, offered)
	assert.Equal(t, "offer", state)
	assert.False(t, again)
	assert.Equal(t, "agt_1/2", f.get().Phone)
}
