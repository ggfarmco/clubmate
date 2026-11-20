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
	blockWaitsMany   = "%s waits inside this call. If any gate asks for changes, the refusal waits for the others; then the findings of all follow it in one message."
	blockReviewing   = "reviewing · %s"
	blockDecided     = "%s · waits for %s"
	blockMore        = "%s +%d"
	blockAsking      = "asking"
	blockRefusedWord = "refused"
	blockInputLines  = 3
	gateTitlePrefix  = "[Clubmate] "
	doneGateHead     = "[Clubmate] From the gate · "
	doneNotBlocking  = "Not blocking:"
	bulletPrefix     = "- "
	blockSep         = " · "
	blockRoundWord   = "round "
	blockChangesEnd  = " · changes"
)

type callBlock struct {
	State   string          `json:"state"`
	Word    string          `json:"word,omitempty"`
	Title   string          `json:"title,omitempty"`
	Caption string          `json:"caption,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Input   string          `json:"input,omitempty"`
	Lines   []callBlockLine `json:"lines,omitempty"`
	Note    string          `json:"note,omitempty"`
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
		return b
	}
	b.Tool, b.Input = callShown(en)
	if m.State == callStateRefd {
		b.Title, b.Caption = fmt.Sprintf(blockRefusedOne, max(m.Round, 1)), refusedCaption(m.Holds)
		if msg == nil {
			return b
		}
		if caption := messageCaption(*msg); caption != "" {
			b.Caption = caption
		}
		if len(msg.Groups) > 1 {
			b.Title = fmt.Sprintf(blockRefusedMany, len(msg.Groups))
		}
		return b
	}
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
	parts := []string{holds[0].Repo}
	for _, h := range holds {
		parts = append(parts, h.Name)
	}
	return strings.Join(parts, " · ")
}

func messageCaption(m gateMessage) string {
	var repo string
	var matches, captions []string
	for _, g := range m.Groups {
		head, ok := strings.CutPrefix(g.Head, doneGateHead)
		if !ok {
			return ""
		}
		head = strings.TrimSuffix(head, blockChangesEnd)
		r, rest, ok := strings.Cut(head, blockSep)
		i := strings.LastIndex(rest, blockSep+blockRoundWord)
		if !ok || i < 0 {
			return ""
		}
		repo = cmp.Or(repo, r)
		matches, captions = append(matches, rest[:i]), append(captions, head)
	}
	if len(captions) == 1 {
		return captions[0]
	}
	return strings.Join(append([]string{repo}, matches...), blockSep)
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
	for _, l := range b.Lines {
		row := []sdui.Node{sdui.Text(l.Text)}
		if l.State != "" {
			row = append(row, sdui.WithTone(sdui.Text(l.State), toneAccent))
		}
		row = append(row, sdui.Spacer())
		if len(b.Lines) > 1 && l.Review != "" {
			row = append(row, blockReview(sid, l))
		}
		rows = append(rows, sdui.HStack(row...))
	}
	if b.Note != "" {
		rows = append(rows, sdui.MutedText(b.Note))
	}
	return sdui.WithID(sdui.Card(toneAccent, landingIcon, rows...), id)
}

func blockReview(sid string, l callBlockLine) sdui.Node {
	return sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+heldReviewPrefix+strconv.Itoa(l.Round)+"/"+l.Review, landingOpenReview, ""), actHeldReview), sdui.VariantGhost)
}

func gateMessageProp(m gateMessage) sdui.GateMessage {
	out := sdui.GateMessage{Tail: m.Tail}
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
	lines := strings.Split(body, "\n")
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

func (a *app) withCallContent(v *transcriptView, s *session) {
	lives := a.callLives(v.Session)
	agent, now := agentLabel(v.Session.Agent), time.Now()
	for _, en := range v.Entries {
		en.Call = callBlockOf(en, lives[en.ID], refusalMessage(en, s.messageFor), agent, now)
		for _, h := range en.HeldCalls {
			h.Call = callBlockOf(h, lives[h.ID], nil, agent, now)
		}
		if en.Gate != nil {
			en.Gate = titled(*en.Gate)
		}
	}
}

func titled(m gateMessage) *gateMessage {
	m.Groups = slices.Clone(m.Groups)
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

func gateDecorated(en *entry) bool {
	return markOf(en) != nil || len(en.HeldCalls) > 0
}

func doneMessage(args promptArgs, gate bool) *gateMessage {
	if !gate || args.Message != nil {
		return nil
	}
	return doneGateMessage(args.Text)
}
