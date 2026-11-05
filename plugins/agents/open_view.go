package main

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	actOpenDraft   = "openDraft"
	actOpenSend    = "openSend"
	actOpenSendAll = "openSendAll"
	actOpenLater   = "openLater"
	actOpenBacklog = "openBacklog"
	actOpenNoAll   = "openNoAll"
	actOpenMove    = "openMove"
	actOpenShow    = "openShow"
	actOpenKey     = "openKey"
	actOpenConvert = "openConvert"
	actOpenSession = "openSession"

	openQueueNode = "openQueue"
	openKeysNode  = "keys"

	openTitle     = "Open after this turn"
	openNote      = "answer any time; nothing waits"
	openSendNote  = "Send starts a turn with your answers"
	openSendShort = "starts a turn"
	nextHeader    = "Next"
	nextQuestion  = "Next — tick what you want me to do"
	nextNote      = "any, or none"
	openIcon      = "checkmark.circle"
	openGap       = 8
	queueGap      = 0
	openFilterKey = "opt+o"
	openTone      = "warning"
	mayAskDetail  = "may have left a question"
	mayAskNote    = "Its last message may have left a question."
	mayAskIcon    = "questionmark.circle"
	convertLabel  = "Turn into items"
	queueHints    = "j/k item · 1–4 pick · x tick · o other · L later · B backlog · ⌫ no · ⌘↩ send all answered · ↩ open session"
	noneOpen      = "Nothing open"
	withAnswers   = " — your answers go with it"
)

var (
	openSessionKeys = []string{"j", "k", "up", "down", "1", "2", "3", "4", "x", "o", "l", "b", "delete", "shift+delete", "cmd+return"}
	openQueueKeys   = append(slices.Clone(openSessionKeys), "return")
)

type openCardData struct {
	Items    []openItem
	Resolved []openItem
	Numbers  map[string]int
	Overflow []int
	Later    bool
	Entry    string
}

type openQueueCard struct {
	Rec    record
	Branch string
	Data   openCardData
	Draft  openDraft
	Cursor string
}

type openQueueScreen struct {
	Cards []openQueueCard
	Now   time.Time
}

func (s *session) openCard() openCardData {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	data := openCardData{Numbers: map[string]int{}}
	numbers := openNumbers(s.opens)
	for _, en := range s.opens {
		held := numbers[en]
		if held == nil {
			continue
		}
		data.Entry, data.Later = en.ID, en.Later
		data.Overflow = append(data.Overflow, en.Overflow...)
		for i, it := range en.Items {
			data.Numbers[it.ID] = held[i]
			switch it.State {
			case itemOpen:
				data.Items = append(data.Items, it)
			case itemHanded:
			default:
				data.Resolved = append(data.Resolved, it)
			}
		}
	}
	return data
}

func openEntryNode(sc screen, en *entry) (sdui.Node, bool) {
	rec := sc.View.Session
	switch {
	case en.ID == sc.Open.Entry && rec.Status == statusWaiting:
		return sdui.Node{}, false
	case en.ID == sc.Open.Entry && sc.Open.Later && !sc.Expanded[en.ID]:
		return openFolded(rec, sc.Open), true
	case en.ID == sc.Open.Entry:
		return openCard(rec.ID, rec.OpenCount, sc.Open, sc.OpenDraft), true
	case hasOpen(en):
		return sdui.Node{}, false
	}
	line := resolvedLine(en.Items)
	return sdui.MutedText(line), line != ""
}

func openBadge(count int) sdui.HeaderBadge {
	return sdui.HeaderBadge{Text: openLabel(count), Tone: openTone, Outline: true}
}

func openLabel(count int) string {
	return strconv.Itoa(count) + " open"
}

func openCountNode(count int) sdui.Node {
	return sdui.WithOutline(sdui.Badge(openLabel(count), openTone))
}

func openGlyph() sdui.Node {
	return sdui.WithTone(sdui.Icon(openIcon), "success")
}

func openStrong(text string) sdui.Node {
	return sdui.Node{Type: "text", Props: map[string]any{"text": text, "style": "strong"}}
}

func openFolded(rec record, data openCardData) sdui.Node {
	show := sdui.WithVariant(sdui.WithAction(sdui.Button(rec.ID+"/"+openNode+"/show", "Show", ""), actOpenShow), "ghost")
	return sdui.HStack(openGlyph(), sdui.MutedText("Later"), openCountNode(rec.OpenCount), sdui.MutedText(openSplit(data.Items)), sdui.Spacer(), show)
}

func openSplit(items []openItem) string {
	offers := len(offerItems(items))
	switch decisions := len(items) - offers; {
	case decisions == 0:
		return counted(offers, "offer")
	case offers == 0:
		return counted(decisions, "decision")
	default:
		return counted(decisions, "decision") + " · " + counted(offers, "offer")
	}
}

func counted(n int, word string) string {
	return strconv.Itoa(n) + " " + plural(n, word, word+"s")
}

func openCard(sid string, count int, data openCardData, d openDraft) sdui.Node {
	cursor := d.current(data.Items)
	head := sdui.WithGap(sdui.HStack(openGlyph(), openStrong(openTitle), openCountNode(count), sdui.WithShort(sdui.MutedText(openSplit(data.Items)+" · "+openNote), openSplit(data.Items))), openGap)
	rows := append([]sdui.Node{head}, openRows(sid, data, d, cursor)...)
	if line := resolvedLine(data.Resolved); line != "" {
		rows = append(rows, sdui.MutedText(line))
	}
	if line := overflowLine(data.Overflow); line != "" {
		rows = append(rows, sdui.MutedText(line))
	}
	rows = append(rows, openFooter(sid, d.args(sid, data.Items).picked() > 0))
	form := sdui.WithAction(sdui.WithAutosave(sdui.Form(openFormNode(sid), sdui.WithRows(sdui.Card("", "", rows...)))), actOpenDraft)
	return sdui.WithAction(sdui.Keys(openFormNode(sid)+"/"+openKeysNode, openSessionKeys, form), actOpenKey)
}

func openRows(sid string, data openCardData, d openDraft, cursor string) []sdui.Node {
	var rows []sdui.Node
	for _, it := range data.Items {
		if it.Kind != itemOffer {
			rows = append(rows, decisionChoice(sid, it, data.Numbers[it.ID], d, cursor))
		}
	}
	if offers := offerItems(data.Items); len(offers) > 0 {
		rows = append(rows, offersChoice(sid, offers, data.Numbers[offers[0].ID], d, cursor))
	}
	return rows
}

func decisionChoice(sid string, it openItem, number int, d openDraft, cursor string) sdui.Node {
	ans := d.answers[it.ID]
	style := "radio"
	if it.MultiSelect {
		style = "check"
	}
	options := make([]sdui.ChoiceOption, 0, len(it.Options))
	for i, o := range it.Options {
		options = append(options, sdui.ChoiceOption{Value: o.Label, Label: o.Label, Description: o.Description, Hint: strconv.Itoa(i + 1), On: ans.Option == o.Label || slices.Contains(ans.Options, o.Label)})
	}
	other := &sdui.ChoiceOther{ID: openOtherID(sid, it.ID), Label: otherLabel, Placeholder: otherPlaceholder, Hint: "o", On: d.others[it.ID], Value: ans.Other}
	if it.ID == cursor {
		other.Focus = d.focus
	}
	return sdui.Choice(openItemNode(sid, it.ID), sdui.ChoiceProps{
		Style: style, Title: it.Header, Text: it.Question, Quote: it.Quote, Number: number, Open: it.ID == cursor,
		OpenAction: actOpenMove, Options: options, Other: other,
	})
}

func offersChoice(sid string, offers []openItem, number int, d openDraft, cursor string) sdui.Node {
	options := make([]sdui.ChoiceOption, 0, len(offers))
	shown := ""
	for i, it := range offers {
		options = append(options, sdui.ChoiceOption{Value: it.ID, Label: it.Label, Description: it.Description, Hint: strconv.Itoa(i + 1), On: d.offers[it.ID]})
		if it.ID == cursor {
			shown = it.ID
		}
	}
	return sdui.Choice(openItemNode(sid, openNextNode), sdui.ChoiceProps{
		Style: "check", Title: nextHeader, Text: nextQuestion, Note: nextNote, Summary: counted(len(offers), "offer"), Number: number,
		Open: shown != "", Cursor: shown, OpenAction: actOpenMove, Options: options,
	})
}

func openFooter(sid string, canSend bool) sdui.Node {
	id := openFormNode(sid)
	send := sdui.WithHint(sdui.WithVariant(sdui.WithForm(sdui.WithAction(sdui.Button(id+"/send", "Send answers", ""), actOpenSend)), "primary"), "cmd+return")
	if !canSend {
		send = sdui.WithDisabled(send)
	}
	later := sdui.WithHint(sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/later", "Later", ""), actOpenLater), "ghost"), "l")
	backlog := sdui.WithHint(sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/backlog", "To backlog", ""), actOpenBacklog), "ghost"), "b")
	no := sdui.WithHint(sdui.WithVariant(sdui.WithAction(sdui.Button(id+"/no", "No to all", ""), actOpenNoAll), "ghost"), "shift+delete")
	more := sdui.WithPopover(sdui.WithIconOnly(sdui.WithVariant(sdui.Button(id+"/more", "More", "ellipsis"), "ghost")), sdui.Form(id+"/more/form", no))
	return sdui.WithGap(sdui.HStack(send, later, backlog, more, sdui.Spacer(), sdui.WithShort(sdui.MutedText(openSendNote), openSendShort)), openGap)
}

func overflowLine(tasks []int) string {
	if len(tasks) == 0 {
		return ""
	}
	return fmt.Sprintf("+%d more went to the backlog: %s", len(tasks), taskRefs(tasks))
}

func resolvedLine(items []openItem) string {
	var answers, no []string
	var offers []string
	var tasks []int
	for _, it := range items {
		switch {
		case it.State == itemAnswered && it.Kind == itemOffer:
			offers = append(offers, it.Label)
		case it.State == itemAnswered:
			answers = append(answers, it.Header+answerArrow+it.Answer)
		case it.State == itemNo:
			no = append(no, it.title())
		case it.State == itemBacklog:
			tasks = append(tasks, it.Task)
		}
	}
	if len(offers) > 0 {
		answers = append(answers, doPrefix+strings.Join(offers, ", "))
	}
	parts := nonEmpty(strings.Join(answers, "; "))
	if len(no) > 0 {
		parts = append(parts, "no: "+strings.Join(no, ", "))
	}
	if len(tasks) > 0 {
		parts = append(parts, "to backlog: "+taskRefs(tasks))
	}
	return strings.Join(parts, " · ")
}

func openQueueView(q openQueueScreen) sdui.Node {
	items, answered, sessions, answeredIn := 0, 0, 0, 0
	for _, c := range q.Cards {
		if c.Rec.OpenCount > 0 {
			items += c.Rec.OpenCount
			sessions++
		}
		if n := c.Draft.args(c.Rec.ID, c.Data.Items).picked(); n > 0 {
			answered += n
			answeredIn++
		}
	}
	if len(q.Cards) == 0 {
		return sdui.EmptyState(noneOpen, sdui.EmptyProps{})
	}
	send := sdui.WithHint(sdui.WithVariant(sdui.WithAction(sdui.Button(openQueueNode+"/send", "Send answers", ""), actOpenSendAll), "primary"), "cmd+return")
	meta := ""
	if answered > 0 {
		send.Props["label"] = "Send " + counted(answered, "answer")
		meta = strconv.Itoa(answered) + " answered · to " + counted(answeredIn, "session")
	} else {
		send = sdui.WithDisabled(send)
	}
	title := "Open · " + counted(items, "item") + " in " + counted(sessions, "session")
	cards := make([]sdui.Node, 0, len(q.Cards)+1)
	for _, c := range q.Cards {
		cards = append(cards, queueCard(c, q.Now))
	}
	cards = append(cards, sdui.MutedText(queueHints))
	body := sdui.VStack(sdui.WithInline(sdui.Header(title, sdui.HeaderProps{Meta: meta}, send)), sdui.Scroll(sdui.WithGap(sdui.VStack(cards...), queueGap)))
	return sdui.WithPaneFocus(sdui.WithAction(sdui.Keys(openQueueNode, openQueueKeys, body), actOpenKey))
}

func queueCard(c openQueueCard, now time.Time) sdui.Node {
	sid := c.Rec.ID
	link := sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+openNode+"/session", "Open session", ""), actOpenSession), "ghost")
	where := strings.Join(nonEmpty(afkDetail(c.Rec), c.Branch, ago(c.Rec.UpdatedAt, now)), " · ")
	head := []sdui.Node{openGlyph(), openStrong(cmp.Or(c.Rec.Title, "New session"))}
	if c.Rec.OpenCount > 0 {
		head = append(head, openCountNode(c.Rec.OpenCount))
	}
	head = append(head, sdui.WithShort(sdui.MutedText(where), ago(c.Rec.UpdatedAt, now)), sdui.Spacer(), link)
	rows := append([]sdui.Node{sdui.WithGap(sdui.HStack(head...), openGap)}, openRows(sid, c.Data, c.Draft, c.Cursor)...)
	if line := overflowLine(c.Data.Overflow); line != "" {
		rows = append(rows, sdui.MutedText(line))
	}
	if c.Rec.OpenCount == 0 {
		convert := sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+openNode+"/convert", convertLabel, ""), actOpenConvert), "ghost")
		rows = append(rows, sdui.WithGap(sdui.HStack(sdui.Icon(mayAskIcon), sdui.MutedText(mayAskNote), sdui.Spacer(), convert), openGap))
	}
	return sdui.WithAccordion(sdui.WithAction(sdui.WithAutosave(sdui.Form(openFormNode(sid), sdui.WithRows(sdui.Card("", "", rows...)))), actOpenDraft))
}
