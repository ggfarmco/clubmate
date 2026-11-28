package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	blockHeldOne     = "Held by a gate"
	blockHeldMany    = "Held by %d gates"
	blockRefusedOne  = "Refused by the gate · round %d · changes"
	blockRefusedMany = "Refused by %d gates · changes"
	blockCaption     = "%s · %s · round %d of %d"
	blockBoth        = "%s · runs when both approve"
	blockAll         = "%s · runs when all approve"
	blockChain       = "%s → approved / changes"
	blockGateLine    = "%s · %s · round %d of %d"
	blockWaitsOne    = "%s waits inside this call. Approved runs it. Changes refuse it, and the gate then sends its findings in a message."
	blockWaitsTwo    = "%s waits inside this call. If either gate asks for changes, the refusal waits for the other; then the findings of both follow it in one message."
	blockWaitsMany   = "%s waits inside this call. If any gate asks for changes, the refusal waits for the others; then all their findings follow it in one message."
	blockNoTurn      = "%s · refused at once: gates hold only calls made inside a turn"
	blockRefusedLine = "%s · round %d of %d"
	blockReviewing   = "reviewing · %s"
	blockDecided     = "%s · waits for %s"
	blockMore        = "%s +%d"
	blockAsking      = "asking"
	blockRefusedWord = "refused"
	blockInputLines  = 3
	gateTitlePrefix  = "[Clubmate] "
	doneGateHead     = "[Clubmate] From the gate · "
	doneGateJoin     = "\n\n" + doneGateHead
	doneNotBlocking  = "Not blocking:"
	bulletPrefix     = "- "
	blockSep         = " · "
	blockRoundWord   = "round "
	blockChangesEnd  = " · changes"
	titleToneCaption = "caption"
)

type callBlock struct {
	State   string          `json:"state"`
	Word    string          `json:"word,omitempty"`
	Tone    string          `json:"tone,omitempty"`
	Title   string          `json:"title,omitempty"`
	Caption string          `json:"caption,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Input   string          `json:"input,omitempty"`
	Lines   []callBlockLine `json:"lines,omitempty"`
	Note    string          `json:"note,omitempty"`
	Icon    string          `json:"icon,omitempty"`
	Line    string          `json:"line,omitempty"`
	Held    *callBlockHeld  `json:"held,omitempty"`
	Wait    string          `json:"wait,omitempty"`
}

type callBlockHeld struct {
	Title   string `json:"title"`
	Rest    string `json:"rest,omitempty"`
	Session string `json:"session,omitempty"`
}

type callBlockLine struct {
	Text   string `json:"text"`
	State  string `json:"state,omitempty"`
	Round  int    `json:"round"`
	Review string `json:"review,omitempty"`
}

type callLive struct {
	Graph    string
	Last     string
	Wait     string
	Request  string
	Reviewer string
	Since    time.Time
}

func (a *app) callLives(rec record) map[string]map[string]callLive {
	out := map[string]map[string]callLive{}
	for _, c := range rec.Calls {
		if c.Call == "" || c.Hold == nil {
			continue
		}
		h := c.Hold
		l := callLive{Graph: cmp.Or(h.Graph, a.heldFactsOf(h.Gate).graph, heldGraphFallback), Last: h.Last, Wait: h.Wait, Request: h.Request, Since: h.Since}
		if g, ok := a.callGateLanding(rec, h.Gate); ok {
			l.Reviewer = g.Reviewer
		}
		if out[c.Call] == nil {
			out[c.Call] = map[string]callLive{}
		}
		out[c.Call][h.Gate] = l
	}
	for call, holds := range a.approvedHolds(rec.ID) {
		for _, h := range holds {
			if _, ok := out[call][h.Gate]; ok {
				continue
			}
			if out[call] == nil {
				out[call] = map[string]callLive{}
			}
			graph := h.Graph
			if graph == "" {
				graph = cmp.Or(a.heldFactsOf(h.Gate).graph, heldGraphFallback)
			}
			out[call][h.Gate] = callLive{Graph: graph, Last: gateApproved, Since: h.Since}
		}
	}
	return out
}

func (a *app) approvedHolds(id string) map[string][]heldTurn {
	s := a.session(id)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]heldTurn{}
	for _, c := range s.calls {
		for _, d := range c.decided {
			if c.taken && d.Hold != nil && d.Hold.Last == gateApproved {
				out[c.id] = append(out[c.id], *d.Hold)
			}
		}
	}
	return out
}

func markOf(en *entry) *gateMark {
	if en.Kind != entryTool {
		return nil
	}
	raw, ok := en.Tool["gate"]
	if !ok || string(raw) == "null" {
		return nil
	}
	var m gateMark
	if json.Unmarshal(raw, &m) != nil || m.State != callStateHeld && m.State != callStateRefd {
		return nil
	}
	return &m
}

func blockShown(en *entry) bool {
	m := markOf(en)
	return m != nil && (m.State == callStateHeld || m.Reason == "")
}

func keepHolds(old, fields map[string]json.RawMessage) {
	raw, ok := fields["gate"]
	if !ok {
		return
	}
	var next, prev gateMark
	if json.Unmarshal(raw, &next) != nil || next.State != callStateRefd || len(next.Holds) > 0 || json.Unmarshal(old["gate"], &prev) != nil || len(prev.Holds) == 0 {
		return
	}
	next.Holds = prev.Holds
	if merged, err := json.Marshal(next); err == nil {
		fields["gate"] = merged
	}
}

func callBlockOf(en *entry, live map[string]callLive, msg *gateMessage, agent string, now time.Time) *callBlock {
	m := markOf(en)
	if m == nil {
		return nil
	}
	b := &callBlock{State: m.State}
	if m.State == callStateRefd && m.Reason != "" {
		b.Word = blockRefusedWord
		if len(m.Holds) > 0 {
			b.Caption = fmt.Sprintf(blockNoTurn, matchesCaption(m.Holds))
		}
		return b
	}
	b.Tool, b.Input = callShown(en)
	if m.State == callStateRefd {
		refusedBlock(b, m, live, msg)
		return b
	}
	b.Tone = toneAccent
	if len(m.Holds) == 0 {
		b.Title = blockHeldOne
		return b
	}
	if len(m.Holds) == 1 {
		h := m.Holds[0]
		l := live[h.Gate]
		b.Title, b.Caption, b.Note = blockHeldOne, holdCaption(h), fmt.Sprintf(blockWaitsOne, agent)
		b.Lines = []callBlockLine{{Text: fmt.Sprintf(blockChain, cmp.Or(l.Graph, heldGraphFallback)), State: gateState(l, nil, now), Round: max(h.Round, 1), Review: l.Reviewer}}
		return b
	}
	b.Title, b.Caption = fmt.Sprintf(blockHeldMany, len(m.Holds)), fmt.Sprintf(blockAll, m.Holds[0].Repo)
	b.Note = fmt.Sprintf(blockWaitsMany, agent)
	if len(m.Holds) == 2 {
		b.Caption, b.Note = fmt.Sprintf(blockBoth, m.Holds[0].Repo), fmt.Sprintf(blockWaitsTwo, agent)
	}
	for i, h := range m.Holds {
		l := live[h.Gate]
		var others []string
		for j, o := range m.Holds {
			if j != i && live[o.Gate].Last == "" {
				others = append(others, o.Name)
			}
		}
		round := max(h.Round, 1)
		b.Lines = append(b.Lines, callBlockLine{Text: fmt.Sprintf(blockGateLine, h.Name, cmp.Or(l.Graph, heldGraphFallback), round, max(h.Of, round)), State: gateState(l, others, now), Round: round, Review: l.Reviewer})
	}
	return b
}

func refusedBlock(b *callBlock, m *gateMark, live map[string]callLive, msg *gateMessage) {
	b.Tone = toneNeutral
	b.Title, b.Caption = fmt.Sprintf(blockRefusedOne, max(m.Round, 1)), refusedCaption(m.Holds)
	var changed []string
	if msg != nil {
		if caption := messageCaption(*msg); caption != "" && len(m.Holds) == 0 {
			b.Caption = caption
		}
		if len(msg.Groups) > 1 {
			b.Title = fmt.Sprintf(blockRefusedMany, len(msg.Groups))
		}
		changed = changedGates(*msg)
	}
	if len(m.Holds) < 2 {
		return
	}
	for _, h := range m.Holds {
		round := max(h.Round, 1)
		text := fmt.Sprintf(blockRefusedLine, h.Name, round, max(h.Of, round))
		if g := live[h.Gate].Graph; g != "" {
			text = fmt.Sprintf(blockGateLine, h.Name, g, round, max(h.Of, round))
		}
		state := ""
		switch {
		case slices.Contains(changed, h.Gate) || slices.Contains(changed, clipRunes(h.Name, callLineMax)):
			state = gateChanges
		case msg != nil:
			state = gateApproved
		}
		b.Lines = append(b.Lines, callBlockLine{Text: text, State: state, Round: round})
	}
}

func holdCaption(h callMark) string {
	round := max(h.Round, 1)
	return fmt.Sprintf(blockCaption, h.Repo, cmp.Or(h.Caption, h.Name), round, max(h.Of, round))
}

func refusedCaption(holds []callMark) string {
	switch len(holds) {
	case 0:
		return ""
	case 1:
		return holdCaption(holds[0])
	}
	return matchesCaption(holds)
}

func matchesCaption(holds []callMark) string {
	parts := []string{holds[0].Repo}
	for _, h := range holds {
		parts = append(parts, h.Name)
	}
	return strings.Join(parts, blockSep)
}

type messageHead struct {
	repo, match, head string
}

func messageHeads(m gateMessage) []messageHead {
	var out []messageHead
	for _, g := range m.Groups {
		head, ok := strings.CutPrefix(g.Head, doneGateHead)
		if !ok {
			return nil
		}
		head = strings.TrimSuffix(head, blockChangesEnd)
		r, rest, ok := strings.Cut(head, blockSep)
		i := strings.LastIndex(rest, blockSep+blockRoundWord)
		if !ok || i < 0 {
			return nil
		}
		out = append(out, messageHead{repo: r, match: rest[:i], head: head})
	}
	return out
}

func messageMatches(m gateMessage) []string {
	var out []string
	for _, h := range messageHeads(m) {
		out = append(out, h.match)
	}
	return out
}

func changedGates(m gateMessage) []string {
	var out []string
	for _, g := range m.Groups {
		if g.Gate != "" {
			out = append(out, g.Gate)
		}
	}
	if len(out) == len(m.Groups) {
		return out
	}
	return messageMatches(m)
}

func messageCaption(m gateMessage) string {
	heads := messageHeads(m)
	switch len(heads) {
	case 0:
		return ""
	case 1:
		return heads[0].head
	}
	return strings.Join(append([]string{heads[0].repo}, messageMatches(m)...), blockSep)
}

func refusalMessage(en *entry, find func(string) *gateMessage) *gateMessage {
	if m := markOf(en); m == nil || m.State != callStateRefd || m.Reason != "" {
		return nil
	}
	return find(en.ID)
}

func messageIn(entries []*entry) func(string) *gateMessage {
	return func(call string) *gateMessage {
		i := slices.IndexFunc(entries, func(en *entry) bool { return en.Kind == entryUser && en.Gate != nil && en.Gate.Call == call })
		if i < 0 {
			return nil
		}
		return entries[i].Gate
	}
}

func (s *session) messageFor(call string) *gateMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if i, ok := s.index[key(entryUser, gateEntryID(call))]; ok && s.entries[i].Gate != nil {
		m := *s.entries[i].Gate
		return &m
	}
	return nil
}

func gateState(l callLive, others []string, now time.Time) string {
	switch {
	case l.Wait != "":
		return heldWaitsTrust
	case l.Request != "":
		return blockAsking
	case l.Last != "" && len(others) == 1:
		return fmt.Sprintf(blockDecided, l.Last, others[0])
	case l.Last != "" && len(others) > 1:
		return fmt.Sprintf(blockDecided, l.Last, fmt.Sprintf(blockMore, others[0], len(others)-1))
	case l.Last != "":
		return l.Last
	case l.Since.IsZero():
		return gateStateReview
	}
	return fmt.Sprintf(blockReviewing, elapsed(now.Sub(l.Since)))
}

func callShown(en *entry) (string, string) {
	f, kind, command := parseTool(en.Tool)
	tool := en.toolName
	if tool == "" && kind == toolExecute {
		tool = bashTool
	}
	if command != "" {
		return cmp.Or(tool, f.Title), clipRunes(strings.TrimSpace(command), callInputMax)
	}
	input := ""
	if len(f.RawInput) > 0 {
		if raw, err := json.Marshal(f.RawInput); err == nil {
			input = clipRunes(string(raw), callLineMax)
		}
	}
	return cmp.Or(tool, f.Title, kind), input
}

func callBlockNode(id, sid string, b callBlock) sdui.Node {
	title := sdui.Text(b.Title)
	title.Props["style"] = "strong"
	head := []sdui.Node{title}
	if b.Caption != "" {
		head = append(head, sdui.MutedText(b.Caption))
	}
	head = append(head, sdui.Spacer())
	if len(b.Lines) == 1 && b.Lines[0].Review != "" {
		head = append(head, blockReview(sid, b.Lines[0]))
	}
	rows := []sdui.Node{sdui.HStack(head...)}
	if b.Input != "" || b.Tool != "" {
		rows = append(rows, sdui.HStack(sdui.MutedText(b.Tool), sdui.WithLines(sdui.MonoText(b.Input), blockInputLines)))
	}
	tone := cmp.Or(b.Tone, toneAccent)
	for _, l := range b.Lines {
		var tail []sdui.Node
		if l.State != "" {
			state := sdui.MutedText(l.State)
			if tone == toneAccent {
				state = sdui.WithTone(sdui.Text(l.State), toneAccent)
			}
			tail = append(tail, state)
		}
		tail = append(tail, sdui.Spacer())
		if len(b.Lines) > 1 && l.Review != "" {
			tail = append(tail, blockReview(sid, l))
		}
		rows = append(rows, sdui.WithTwoRows(sdui.HStack(sdui.Text(l.Text), sdui.HStack(tail...))))
	}
	if b.Note != "" {
		rows = append(rows, sdui.MutedText(b.Note))
	}
	return sdui.WithID(sdui.Card(tone, landingIcon, rows...), id)
}

func blockReview(sid string, l callBlockLine) sdui.Node {
	return sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+heldReviewPrefix+strconv.Itoa(l.Round)+"/"+l.Review, landingOpenReview, ""), actHeldReview), sdui.VariantGhost)
}

func gateMessageProp(m gateMessage) sdui.GateMessage {
	out := sdui.GateMessage{Tail: m.Tail, TitleTone: titleToneCaption}
	for _, g := range m.Groups {
		out.Groups = append(out.Groups, sdui.GateGroup{Title: strings.TrimPrefix(g.Head, gateTitlePrefix), Findings: g.Findings, Deferred: g.Deferred})
	}
	return out
}

func doneGateMessage(text string) *gateMessage {
	if !strings.HasPrefix(text, doneGateHead) {
		return nil
	}
	body, tailed := strings.CutSuffix(text, heldChangesTail)
	lines := strings.Split(strings.TrimSuffix(strings.TrimSuffix(body, heldFixAsk), heldFixDeferred), "\n")
	g := gateFindings{Head: lines[0]}
	list := &g.Findings
	for _, line := range lines[1:] {
		switch {
		case line == doneNotBlocking:
			list = &g.Deferred
		case strings.HasPrefix(line, bulletPrefix):
			*list = append(*list, strings.TrimPrefix(line, bulletPrefix))
		case len(*list) > 0:
			(*list)[len(*list)-1] += "\n" + line
		}
	}
	m := &gateMessage{Groups: []gateFindings{g}}
	if tailed {
		m.Tail = strings.TrimPrefix(heldChangesTail, "\n")
	}
	return m
}

func gateUnderCalls(shown []*entry) []*entry {
	if !slices.ContainsFunc(shown, func(en *entry) bool {
		return en.Kind == entryUser && en.Gate != nil && en.Gate.Call != ""
	}) {
		return shown
	}
	at := map[string]int{}
	for i, en := range shown {
		if en.Kind == entryTool {
			at[en.ID] = i
		}
	}
	under := map[int][]*entry{}
	moved := map[*entry]bool{}
	for _, en := range shown {
		if en.Kind != entryUser || en.Gate == nil || en.Gate.Call == "" {
			continue
		}
		if i, ok := at[en.Gate.Call]; ok {
			m := *en
			m.Turn = shown[i].Turn
			under[i], moved[en] = append(under[i], &m), true
		}
	}
	if len(moved) == 0 {
		return shown
	}
	out := make([]*entry, 0, len(shown))
	for i, en := range shown {
		if !moved[en] {
			out = append(out, en)
		}
		out = append(out, under[i]...)
	}
	return out
}

func heldInside(sub *subagent) []*entry {
	var out []*entry
	for _, en := range sub.entries {
		if en.sub != nil {
			out = append(out, heldInside(en.sub)...)
			continue
		}
		if m := markOf(en); m != nil && m.State == callStateHeld {
			c := *en
			c.node, c.sub, c.Tool = nil, nil, maps.Clone(en.Tool)
			out = append(out, &c)
		}
	}
	return out
}

func withCallContent(v *transcriptView, s *session, lives map[string]map[string]callLive, slot *slotWaitView, workspace string) {
	agent, now := agentLabel(v.Session.Agent), time.Now()
	for _, en := range v.Entries {
		en.Call = callBlockOf(en, lives[en.ID], refusalMessage(en, s.messageFor), agent, now)
		for _, h := range en.HeldCalls {
			h.Call = callBlockOf(h, lives[h.ID], nil, agent, now)
		}
		if word := heldWordOf(en); en.Call == nil && word != "" {
			en.Call = &callBlock{State: callStateHeld, Word: word}
		}
		if en.Call == nil {
			en.Call = slotBlock(en, slot, workspace, now)
		}
		if en.Gate != nil {
			en.Gate = titled(*en.Gate)
		}
	}
}

func titled(m gateMessage) *gateMessage {
	m.Groups, m.TitleTone = slices.Clone(m.Groups), titleToneCaption
	for i := range m.Groups {
		m.Groups[i].Title = strings.TrimPrefix(m.Groups[i].Head, gateTitlePrefix)
	}
	return &m
}

func callStamp(lives map[string]map[string]callLive, now time.Time) []string {
	var out []string
	for _, call := range slices.Sorted(maps.Keys(lives)) {
		for _, gate := range slices.Sorted(maps.Keys(lives[call])) {
			l := lives[call][gate]
			out = append(out, call, gate, l.Graph, l.Last, l.Wait, l.Request, l.Reviewer, gateState(l, nil, now))
		}
	}
	return out
}

func heldWordOf(en *entry) string {
	var names []string
	held := false
	for _, h := range en.HeldCalls {
		if m := markOf(h); m != nil && m.State == callStateHeld {
			held = true
			for _, hold := range m.Holds {
				names = append(names, hold.Name)
			}
		}
	}
	switch {
	case len(names) == 0 && held:
		return heldWord
	case len(names) == 0:
		return ""
	case len(names) == 1:
		return fmt.Sprintf(heldBadgeFormat, names[0])
	}
	return fmt.Sprintf(callBadgeMore, names[0], len(names)-1)
}

func gateDecorated(en *entry) bool {
	return markOf(en) != nil || len(en.HeldCalls) > 0
}

func doneMessage(args promptArgs, gate bool) *gateMessage {
	if !gate || args.Message != nil {
		return nil
	}
	var out *gateMessage
	for i, part := range strings.Split(args.Text, doneGateJoin) {
		if i > 0 {
			part = doneGateHead + part
		}
		m := doneGateMessage(part)
		if m == nil {
			return nil
		}
		if out == nil {
			out = m
			continue
		}
		out.Groups, out.Tail = append(out.Groups, m.Groups...), cmp.Or(m.Tail, out.Tail)
	}
	return out
}
