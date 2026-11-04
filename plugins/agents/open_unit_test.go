package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openSession(t *testing.T, turn int, texts []string, items ...openItem) *session {
	t.Helper()
	s := newSession(record{ID: "s"}, nil, true, nil)
	s.turn = turn
	for i, text := range texts {
		s.upsert(entryAgent, "m"+string(rune('0'+i)), turn).Text = text
	}
	if len(items) > 0 {
		raw, err := json.Marshal(openItemsPayload{Items: items})
		require.NoError(t, err)
		s.apply(envelope{Kind: kindOpenItems, Turn: turn, Payload: raw})
	}
	return s
}

func onlyMarked(t *testing.T, s *session) string {
	t.Helper()
	marked := s.markedLocked()
	require.Len(t, marked, 1)
	for _, text := range marked {
		return text
	}
	return ""
}

func TestMarkerGoesAfterTheWholeSentence(t *testing.T) {
	t.Parallel()
	s := openSession(t, 1, []string{"So **agents.sessions** is your call (#185). Then the rest."}, openItem{Quote: "agents.sessions", State: itemOpen})
	assert.Equal(t, "So **agents.sessions** is your call (#185). ① Then the rest.", onlyMarked(t, s))
}

func TestMarkersKeepTheirItemNumbers(t *testing.T) {
	t.Parallel()
	s := openSession(t, 1, []string{"One.\nTwo.\nThree"},
		openItem{Quote: "Three", State: itemOpen},
		openItem{Quote: "Missing.", State: itemOpen},
		openItem{Quote: "One.", State: itemAnswered},
		openItem{Quote: "One.", State: itemHanded})
	assert.Equal(t, "One. ③\nTwo.\nThree ①", onlyMarked(t, s))
}

func TestMarkerFindsTheQuoteInAnEarlierMessageOfTheTurn(t *testing.T) {
	t.Parallel()
	s := openSession(t, 1, []string{"The report. Your call.", "Posted."}, openItem{Quote: "Your call.", State: itemOpen})
	assert.Equal(t, "The report. Your call. ①", onlyMarked(t, s))
}

func TestMayAskReadsOnlyTheLastParagraphOfTheTurn(t *testing.T) {
	t.Parallel()
	assert.True(t, openSession(t, 1, []string{"Готово.\n\nЕсли хочешь, добавлю экран."}).mayAskLocked())
	assert.False(t, openSession(t, 1, []string{"Your call.\n\nShipped."}).mayAskLocked())
	assert.False(t, openSession(t, 1, nil).mayAskLocked())
	assert.False(t, openSession(t, 1, []string{"Your call."}, openItem{Quote: "Your call.", State: itemOpen}).mayAskLocked())

	asks := openSession(t, 1, []string{"Your call?"})
	asks.rec.Question = "Your call?"
	assert.False(t, asks.mayAskLocked())
}

func TestParseItemsTellsTheAgentWhatToFix(t *testing.T) {
	t.Parallel()
	good := itemInput{Kind: itemDecision, Header: "Lists", Question: "How?", Quote: "q", Options: []string{"a", "b"}, Descriptions: []string{"x", "y"}}
	failing := func(edit func(*itemInput)) error {
		in := good
		edit(&in)
		_, err := parseItems([]itemInput{in})
		return err
	}

	_, err := parseItems([]itemInput{good, {Kind: itemOffer, Label: "do", Quote: "q"}})
	require.NoError(t, err)
	assert.ErrorContains(t, failing(func(in *itemInput) { in.Header = "thirteen char" }), "items[0]: header")
	assert.ErrorContains(t, failing(func(in *itemInput) { in.Options = []string{"a"}; in.Descriptions = []string{"x"} }), "2 to 4 options")
	assert.ErrorContains(t, failing(func(in *itemInput) { in.Descriptions = []string{"x"} }), "every option needs a description")
	assert.ErrorContains(t, failing(func(in *itemInput) { in.Quote = " " }), "quote is required")
	assert.ErrorContains(t, failing(func(in *itemInput) { in.Kind = "todo" }), "kind must be")
	_, err = parseItems(nil)
	assert.ErrorIs(t, err, errNoItems)
}

func TestItemsStayOnAChildOnceItsParentIsClosed(t *testing.T) {
	t.Parallel()
	a := &app{registry: newRegistry()}
	a.sessions["open"] = newSession(record{ID: "open", Status: statusDone}, nil, true, nil)
	a.sessions["closed"] = newSession(record{ID: "closed", Status: statusClosed}, nil, true, nil)

	assert.Equal(t, "open", a.openParent(record{lineage: lineage{ParentSession: "open"}}))
	assert.Empty(t, a.openParent(record{lineage: lineage{ParentSession: "closed"}}))
	assert.Empty(t, a.openParent(record{lineage: lineage{ParentSession: "gone"}}))
}
