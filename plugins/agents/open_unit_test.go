package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
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

func TestOffersShareOneNumberAfterTheDecisions(t *testing.T) {
	t.Parallel()
	s := openSession(t, 1, []string{"Offer. Ask."},
		openItem{Kind: itemOffer, Quote: "Offer.", State: itemOpen},
		openItem{Kind: itemDecision, Quote: "Ask.", State: itemOpen})
	assert.Equal(t, "Offer. ② Ask. ①", onlyMarked(t, s))
}

func TestOneCardHoldsTheOpenSetAndFoldsWhenLater(t *testing.T) {
	t.Parallel()
	first := &entry{ID: "open-1", Kind: entryOpenItems, Items: []openItem{{ID: "oi-1", Kind: itemDecision, Header: "Lists", Question: "Which?", State: itemOpen, Options: []openOption{{Label: "A", Description: "a"}}}}}
	last := &entry{ID: "open-2", Kind: entryOpenItems, Items: []openItem{{ID: "oi-2", Kind: itemOffer, Label: "Do it", State: itemOpen}}}
	s := newSession(record{ID: "s"}, nil, true, nil)
	s.opens = []*entry{first, last}
	data := s.openCard()
	sc := screen{View: transcriptView{Session: record{ID: "s", Status: statusDone, OpenCount: 2}}, Open: data}

	_, shown := openEntryNode(sc, first)
	assert.False(t, shown)
	card, shown := openEntryNode(sc, last)
	require.True(t, shown)
	require.NoError(t, internalsdui.Validate(card))
	assert.Len(t, nodesOf(card, "choice"), 2)
	assert.Equal(t, "2 open", nodesOf(card, "badge")[0].Props["text"])

	sc.Open.Later = true
	folded, shown := openEntryNode(sc, last)
	require.True(t, shown)
	assert.Empty(t, nodesOf(folded, "choice"))
	sc.Expanded = map[string]bool{"open-2": true}
	open, _ := openEntryNode(sc, last)
	assert.NotEmpty(t, nodesOf(open, "choice"))
}

func TestSidebarOpenFilterCountsItemsAndKeepsAMayAskRow(t *testing.T) {
	t.Parallel()
	rows := []sessionRow{
		{Rec: record{ID: "a", Status: statusDone, Worktree: "/r", OpenCount: 3}},
		{Rec: record{ID: "b", Status: statusDone, Worktree: "/r", OpenCount: 2}},
		{Rec: record{ID: "c", Status: statusDone, Worktree: "/r", MayAsk: true}},
		{Rec: record{ID: "d", Status: statusDone, Worktree: "/r"}},
	}
	view := sidebarView(rows, "", nil, time.Now(), "/r", false, filterOpen, sidebarSearch{}, 0)
	require.NoError(t, internalsdui.Validate(view))
	var tabs []sdui.Item
	raw, _ := json.Marshal(nodesOf(view, "tabs")[0].Props["items"])
	require.NoError(t, json.Unmarshal(raw, &tabs))
	assert.Equal(t, "5", tabs[2].Badge)
	assert.Equal(t, openFilterKey, tabs[2].Shortcut)
	var items []sdui.TreeItem
	raw, _ = json.Marshal(nodesOf(view, "tree")[0].Props["items"])
	require.NoError(t, json.Unmarshal(raw, &items))
	require.Len(t, items, 3)
	assert.True(t, items[0].Badges[0].Outline)
	assert.Equal(t, mayAskDetail, items[2].Detail)
	assert.True(t, items[2].DetailFaint)
}

func TestRadioPickAndOtherExcludeEachOther(t *testing.T) {
	t.Parallel()
	it := openItem{ID: "oi-1", Kind: itemDecision, Options: []openOption{{Label: "A"}, {Label: "B"}}}
	var d openDraft
	d.pick(it, 1)
	d.openOther(it)
	assert.Empty(t, d.answers["oi-1"].Option)
	assert.True(t, d.others["oi-1"])
	d.pick(it, 0)
	assert.False(t, d.others["oi-1"])
	assert.Equal(t, "A", d.answers["oi-1"].Option)
}
