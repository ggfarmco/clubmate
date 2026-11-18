package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/landgate"
)

const (
	gateDoneIcon       = "checkmark.bubble"
	gatePointLanding   = "Landing"
	gateHoldsDoneLine  = "An agent's turn that ends done waits for the graph."
	gateHoldsDone      = "an agent's turn that ends done"
	gateSessionsLine   = "Holds done turns from the sessions you pick. Only AFK leaves sessions you're running yourself alone."
	gateOnlyAFK        = "Only AFK sessions"
	gateDoneSentence   = "Holds %s done turn in %s%s; %s decides."
	gatePickSessions   = "Pick top-level, children or both."
	gateWrotePlans     = "plans"
	gateWroteAny       = "any"
	gateWroteNone      = "none"
	gateWroteLike      = "like"
	gateWroteLikeHint  = "**/plans/*.md, docs/**/*.md"
	gateWroteLikeLine  = "Globs, comma-separated: ** spans folders, * stays within one."
	gateNoTurns        = "No turns held yet. The next turn this gate holds waits for %s."
	gateNoTurnsShort   = "no turns yet"
	gateDeleteDoneText = "Delete the gate %s on %s? Turns it holds then end done without a review. Its verdict history goes too."
	gateLiveNoteDone   = "Findings show here when it decides; the session gets them as a message from the gate."
	gateUndecidedDone  = "%s can't decide a turn: no node reports approved and changes"
	heldWord           = "held"
	heldAcceptShort    = "accept"
	heldPlanQuestion   = "Accept the plan as done?"
	heldTurnQuestion   = "Accept the turn as done?"
	heldFilesQuestion  = "Accept the changes as done?"
	heldRoundWords     = "round %d %s"
	heldBadgeFormat    = "held · %s"
	heldLineFormat     = "Held · %s · %s…"
	heldBlockTitle     = "Gate · %s · round %d"
	heldRoundLine      = "Round %d · %s"
	heldReviewPrefix   = "heldReview/"
	heldWaitsTrust     = "waits for trust"
	heldChecking       = "checking again"
	heldCheckingBadge  = "held · checking again"
	heldCheckingLine   = "Held · checking the gates again…"
	heldLineWaits      = "Held · %s · waits for trust"
	actHeldReview      = "openHeldReview"
	gateTrustDoneText  = "This gate pinned %s. The file is now %s. Until you trust it, the gate runs nothing: a turn it holds stays held and its review runs on the new version once you trust it."
)

func doneWho(sessions []string) (whose, who string) {
	top, child, afk := slices.Contains(sessions, doneTop), slices.Contains(sessions, doneChild), slices.Contains(sessions, doneAFK)
	kind := ""
	if afk {
		kind = "AFK "
	}
	switch {
	case top && child && afk:
		return "any AFK session's", "AFK sessions"
	case top && child:
		return "any session's", "all sessions"
	case top:
		return "a top-level " + kind + "session's", "top-level " + kind + "sessions"
	case child:
		return "a child " + kind + "session's", "child " + kind + "sessions"
	}
	return "", ""
}

func validKinds(sessions []string) bool {
	return slices.Contains(sessions, doneTop) || slices.Contains(sessions, doneChild)
}

func wroteMode(wrote []string) string {
	switch {
	case len(wrote) == 0:
		return gateWroteNone
	case sameSet(wrote, donePlansAndSpecs):
		return gateWrotePlans
	case slices.Equal(wrote, []string{globAny}):
		return gateWroteAny
	}
	return gateWroteLike
}

func wroteOfMode(mode string) []string {
	switch mode {
	case gateWrotePlans:
		return slices.Clone(donePlansAndSpecs)
	case gateWroteAny:
		return []string{globAny}
	}
	return []string{}
}

func parseGlobs(text string) []string {
	out := []string{}
	for part := range strings.SplitSeq(text, ",") {
		if p := strings.TrimSpace(part); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func doneMatchWords(sessions, wrote []string) string {
	_, who := doneWho(sessions)
	var what string
	switch wroteMode(wrote) {
	case gateWroteNone:
		what = "anything"
	case gateWrotePlans:
		what = "plans and specs: " + strings.Join(wrote, " or ")
	case gateWroteAny:
		what = "any file"
	default:
		what = "files like " + strings.Join(wrote, " or ")
	}
	return who + " · wrote " + what
}

func doneWroteClause(wrote []string) string {
	switch wroteMode(wrote) {
	case gateWroteNone:
		return ""
	case gateWrotePlans:
		return " when it wrote a plan or a spec"
	case gateWroteAny:
		return " when it wrote a file"
	}
	return " when it wrote files like " + strings.Join(wrote, ", ")
}

func pointIcon(point string) string {
	if point == gatePointTurn {
		return gateDoneIcon
	}
	return gateLandIcon
}

func pointOrder(point string) int {
	if point == gatePointTurn {
		return 1
	}
	return 0
}

func dockGateLabel(name string) string {
	if label, ok := strings.CutPrefix(name, gateNameDone); ok {
		return label
	}
	return strings.TrimPrefix(name, gateNameLanding)
}

func gateHoldsField(d gateDraft) []sdui.Node {
	if d.Edit != "" {
		word := gatePointLanding
		if d.Point == gatePointTurn {
			word = gatePointDone
		}
		return []sdui.Node{sdui.HStack(sdui.MutedText("Holds"), sdui.Text(word))}
	}
	tabs := sdui.WithAction(sdui.WithSegmented(sdui.Tabs(gateSheetID+"/holds", []sdui.Item{
		{ID: gatePointLand, Label: gatePointLanding, Icon: gateLandIcon},
		{ID: gatePointTurn, Label: gatePointDone, Icon: gateDoneIcon},
	}, cmp.Or(d.Point, gatePointLand))), actGateHolds)
	line := gateHoldsLine
	if d.Point == gatePointTurn {
		line = gateHoldsDoneLine
	}
	return []sdui.Node{sdui.HStack(sdui.MutedText("Holds"), tabs), sdui.MutedText(line)}
}

func gateDoneFields(d gateDraft) ([]sdui.Node, bool) {
	check := func(k, label string) sdui.Node {
		return sdui.WithAction(sdui.Checkbox(gateSheetID+"/session/"+k, label, slices.Contains(d.Sessions, k)), actGateSession)
	}
	ok := validKinds(d.Sessions)
	note := sdui.MutedText(gateSessionsLine)
	if !ok {
		note = sdui.WithTone(note, toneWarning)
	}
	out := []sdui.Node{
		sdui.HStack(sdui.MutedText("Sessions"), check(doneTop, "Top-level"), check(doneChild, "Children")),
		sdui.HStack(sdui.MutedText(""), check(doneAFK, gateOnlyAFK)),
		note,
	}
	mode := d.WroteMode
	options := []sdui.Option{
		{Value: gateWrotePlans, Label: "Plans and specs"},
		{Value: gateWroteAny, Label: "Any file"},
		{Value: gateWroteNone, Label: "Nothing required"},
		{Value: gateWroteLike, Label: "Files like…"},
	}
	field := sdui.WithAction(sdui.Select(gateSheetID+"/wrote", options, mode), actGateWrote)
	if g, taken := d.takenDone(); taken && ok {
		field = sdui.WithError(field, fmt.Sprintf(gateMatchTaken, doneGateName(g.Wrote)))
		ok = false
	}
	row := []sdui.Node{sdui.MutedText("Wrote"), field}
	var line string
	switch mode {
	case gateWrotePlans:
		line = "= " + strings.Join(donePlansAndSpecs, ", ")
	case gateWroteLike:
		like := sdui.WithAction(sdui.WithLive(sdui.TextField(gateSheetID+"/wroteLike", d.WroteLike, gateWroteLikeHint)), actGateWroteLike)
		globs := parseGlobs(d.WroteLike)
		if slices.ContainsFunc(globs, func(g string) bool { return validGlob(g) != nil }) {
			like = sdui.WithError(like, errGateWrote.Error())
		}
		if len(globs) == 0 || slices.ContainsFunc(globs, func(g string) bool { return validGlob(g) != nil }) {
			ok = false
		}
		row, line = append(row, like), gateWroteLikeLine
	}
	out = append(out, sdui.HStack(row...))
	if line != "" {
		out = append(out, sdui.MutedText(line))
	}
	return out, ok
}

func (d gateDraft) wrote() []string {
	if d.WroteMode == gateWroteLike {
		return parseGlobs(d.WroteLike)
	}
	return wroteOfMode(d.WroteMode)
}

func (d gateDraft) takenDone() (gateConfig, bool) {
	primary := d.primary()
	kinds, globs, err := doneMatch(d.Sessions, d.wrote())
	if err != nil {
		return gateConfig{}, false
	}
	want := gateConfig{Sessions: kinds, Wrote: globs}
	i := slices.IndexFunc(d.Gates, func(g gateConfig) bool {
		return g.ID != d.Edit && g.Repo == primary && g.Point == gatePointTurn && sameDone(g, want)
	})
	if i < 0 {
		return gateConfig{}, false
	}
	return d.Gates[i], true
}

func doneSentenceOf(d gateDraft, graph, repo string) string {
	if !validKinds(d.Sessions) {
		return gatePickSessions
	}
	whose, _ := doneWho(d.Sessions)
	return fmt.Sprintf(gateDoneSentence, whose, repo, doneWroteClause(d.wrote()), graph)
}

type heldBlock struct {
	Graph     string
	Round     int
	Rounds    []finishedRound
	Reviewing bool
	Wait      string
	Live      string
}

type finishedRound struct {
	Round    int
	Verdict  string
	Findings int
	Reviewer string
}

func (a *app) heldBlockOf(ctx context.Context, rec record) *heldBlock {
	h := rec.Held
	if h == nil || rec.closed() {
		return nil
	}
	facts := a.heldFactsOf(h.Gate)
	b := &heldBlock{Graph: cmp.Or(h.Graph, facts.graph, heldGraphFallback), Round: max(h.Round, 1)}
	history, err := a.readGateHistory(ctx, h.Gate)
	logError("read held turn rounds", err)
	for _, v := range slices.Backward(history) {
		if v.Lander != rec.ID || v.By != landgate.ByReviewer || v.At.Before(h.Since) {
			continue
		}
		r := finishedRound{Round: max(v.Round, 1), Verdict: v.Outcome, Findings: len(v.Findings)}
		if s := a.session(v.Reviewer); s != nil && s.snapshot().Workspace == rec.Workspace {
			r.Reviewer = v.Reviewer
		}
		b.Rounds = append(b.Rounds, r)
	}
	if a.heldIdle(rec) {
		b.Wait = heldWaitWords(h)
	}
	if b.Reviewing = a.heldReviewing(rec); b.Reviewing {
		if r := a.heldReviewer(rec); r != nil {
			b.Live = r.ID
		}
	}
	return b
}

func heldNode(sid string, b heldBlock) sdui.Node {
	lines := []sdui.Node{sdui.Text(fmt.Sprintf(heldBlockTitle, b.Graph, b.Round))}
	for _, r := range b.Rounds {
		words := r.Verdict
		if r.Verdict == gateChanges && r.Findings > 0 {
			words += " · " + counted(r.Findings, "finding")
		}
		lines = append(lines, heldRoundRow(sid, r.Round, fmt.Sprintf(heldRoundLine, r.Round, words), r.Reviewer))
	}
	switch {
	case b.Reviewing:
		lines = append(lines, heldRoundRow(sid, b.Round, fmt.Sprintf(heldRoundLine, b.Round, gateStateReview), b.Live))
	case b.Wait != "":
		lines = append(lines, heldRoundRow(sid, b.Round, fmt.Sprintf(heldRoundLine, b.Round, b.Wait), ""))
	}
	return sdui.WithID(sdui.Card(toneAccent, landingIcon, lines...), sid+"/held")
}

func heldRoundRow(sid string, round int, text, reviewer string) sdui.Node {
	row := []sdui.Node{sdui.MutedText(text), sdui.Spacer()}
	if reviewer != "" {
		row = append(row, sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+heldReviewPrefix+strconv.Itoa(round)+"/"+reviewer, landingOpenReview, ""), actHeldReview), sdui.VariantGhost))
	}
	return sdui.HStack(row...)
}

func (s *session) working() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workingLocked()
}

func (a *app) heldIdle(rec record) bool {
	h := rec.Held
	if h == nil || rec.closed() || h.Request != "" || h.Last == gateChanges {
		return false
	}
	s := a.session(rec.ID)
	return s == nil || !s.working()
}

func (a *app) turnHeld(rec record) bool {
	return rec.heldWaits() || a.heldIdle(rec)
}

func heldQuestion(h heldTurn) string {
	if h.Unread {
		return heldUnreadAsk
	}
	switch h.Name {
	case gateNamePlanSpecs:
		return heldPlanQuestion
	case gateNameAnyTurn:
		return heldTurnQuestion
	}
	return heldFilesQuestion
}

type heldRoundView struct {
	Text string `json:"text"`
	Run  string `json:"run,omitempty"`
}

func (a *app) heldRound(rec record) *heldRoundView {
	if !a.turnHeld(rec) || rec.Held.Unread {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	b := a.heldBlockOf(ctx, rec)
	if b == nil {
		return nil
	}
	return &heldRoundView{Text: heldRoundText(*b), Run: rec.Held.Run}
}

func heldRoundText(b heldBlock) string {
	parts := []string{b.Graph}
	for _, r := range b.Rounds {
		parts = append(parts, fmt.Sprintf(heldRoundWords, r.Round, r.Verdict))
	}
	switch {
	case b.Reviewing:
		parts = append(parts, fmt.Sprintf(heldRoundWords, b.Round, gateStateReview))
	case b.Wait != "":
		parts = append(parts, fmt.Sprintf(heldRoundWords, b.Round, b.Wait))
	}
	return strings.Join(parts, " · ")
}

func (a *app) heldReviewing(rec record) bool {
	return a.heldIdle(rec) && rec.Held.Wait == "" && !rec.Held.Unread
}

func heldWaitWords(h *heldTurn) string {
	switch {
	case h == nil:
		return ""
	case h.Unread:
		return heldChecking
	case h.Wait != "":
		return heldWaitsTrust
	}
	return ""
}

func heldWaits(rec record) bool {
	return rec.Held != nil && rec.Held.Wait != ""
}

func trustText(point string) string {
	if point == gatePointTurn {
		return gateTrustDoneText
	}
	return gateTrustText
}

func heldCount(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n) + " " + heldWord
}
