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
	gateSessionsLine   = "Pick top-level or children; AFK only narrows them."
	gateDoneSentence   = "Holds a %s agent's done turn in %s%s; %s decides."
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
	heldBadgeFormat    = "held · %s"
	heldLineFormat     = "Held · %s · %s…"
	heldBlockTitle     = "Gate · %s · round %d"
	heldRoundLine      = "Round %d · %s"
	heldReviewPrefix   = "heldReview/"
	heldWaitsTrust     = "waits for trust"
	heldLineWaits      = "Held · %s · waits for trust"
	actHeldReview      = "openHeldReview"
	gateTrustDoneText  = "This gate pinned %s. The file is now %s. Until you trust it, the gate runs nothing: a turn it holds stays held and its review runs on the new version once you trust it."
)

var doneKindWords = map[string]string{doneTop: "top-level", doneChild: "child", doneAFK: "AFK"}

func doneKinds(sessions []string) []string {
	var out []string
	for _, k := range []string{doneTop, doneChild, doneAFK} {
		if slices.Contains(sessions, k) {
			out = append(out, doneKindWords[k])
		}
	}
	return out
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
	who := joinWords(doneKinds(sessions), " and ") + " sessions"
	var what string
	switch wroteMode(wrote) {
	case gateWroteNone:
		what = "every turn that changed something"
	case gateWrotePlans:
		what = "plans and specs: " + strings.Join(wrote, " or ")
	case gateWroteAny:
		what = "a turn that wrote any file"
	default:
		what = "files like " + strings.Join(wrote, " or ")
	}
	return who + " · " + what
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

func joinWords(words []string, last string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + last + words[len(words)-1]
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
	tabs := sdui.WithAction(sdui.Tabs(gateSheetID+"/holds", []sdui.Item{
		{ID: gatePointLand, Label: gatePointLanding, Icon: gateLandIcon},
		{ID: gatePointTurn, Label: gatePointDone, Icon: gateDoneIcon},
	}, cmp.Or(d.Point, gatePointLand)), actGateHolds)
	line := gateHoldsLine
	if d.Point == gatePointTurn {
		line = gateHoldsDoneLine
	}
	return []sdui.Node{sdui.HStack(sdui.MutedText("Holds"), tabs), sdui.MutedText(line)}
}

func gateDoneFields(d gateDraft) ([]sdui.Node, bool) {
	kinds := []sdui.Node{sdui.MutedText("Sessions")}
	for _, k := range []string{doneTop, doneChild, doneAFK} {
		label := map[string]string{doneTop: "Top-level", doneChild: "Children", doneAFK: "AFK"}[k]
		kinds = append(kinds, sdui.WithAction(sdui.Checkbox(gateSheetID+"/session/"+k, label, slices.Contains(d.Sessions, k)), actGateSession))
	}
	out := []sdui.Node{sdui.HStack(kinds...)}
	ok := validKinds(d.Sessions)
	if !ok {
		out = append(out, sdui.WithTone(sdui.MutedText(gateSessionsLine), toneWarning))
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
	return fmt.Sprintf(gateDoneSentence, joinWords(doneKinds(d.Sessions), " or "), repo, doneWroteClause(d.wrote()), graph)
}

type heldBlock struct {
	Graph     string
	Round     int
	Rounds    []finishedRound
	Reviewing bool
	Waits     bool
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
	facts := a.heldAskOf(ctx, a.gateByID(ctx, h.Gate))
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
	b.Waits = a.heldIdle(rec) && heldWaits(rec)
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
	case b.Waits:
		lines = append(lines, heldRoundRow(sid, b.Round, fmt.Sprintf(heldRoundLine, b.Round, heldWaitsTrust), ""))
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

func (a *app) heldReviewing(rec record) bool {
	return a.heldIdle(rec) && rec.Held.Wait == ""
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
