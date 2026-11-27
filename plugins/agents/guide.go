package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	entryStep       = "step"
	kindStep        = "step"
	kindStepPressed = "step/pressed"
	stepIDPrefix    = "step-"
	ledgerName      = "tour.json"
	ledgerLimit     = 1 << 20
	stepTitleRunes  = 60
	maxTourDepth    = 6
	tourPanelKey    = "cmd+shift+m"
	continueKey     = "cmd+return"
	asideStepText   = "step %q of a guided code reading: go deeper into it (a callee, an edge case or why it is built this way); this aside is not graded"
)

var (
	errStepCaller  = errors.New("agents.guide-step is only for an agent session: it marks a step in the caller's own session")
	errStepTitle   = errors.New("invalid_params: title is required")
	errLedgerPath  = errors.New("invalid_params: ledger must be an absolute path to a file named tour.json")
	errNoStep      = errors.New("the session has no step to act on")
	errStepAction  = errors.New("this step control is not offered now")
	errTourOffer   = errors.New("this concept has no such offer now")
	errStepUser    = errors.New("only the user presses a step control or a map offer")
	stepKeys       = map[string]string{sdui.StepTestMe: "opt+t", sdui.StepShowMe: "opt+s", sdui.StepGoDeeper: "opt+d", sdui.StepSmallerSteps: "opt+m", sdui.StepContinue: continueKey}
	stepActionList = []string{sdui.StepContinue, sdui.StepTestMe, sdui.StepShowMe, sdui.StepGoDeeper, sdui.StepSmallerSteps}
)

type guideWords struct {
	Step       string
	Actions    map[string]string
	Requests   map[string]string
	Title      string
	StepOf     string
	StepN      string
	Summary    string
	States     map[string]string
	AskAgain   string
	GoBack     string
	AskRequest string
	BackReq    string
	Answer     string
}

var (
	guideEnglish = guideWords{
		Step: "Step %d · %s",
		Actions: map[string]string{
			sdui.StepContinue: "Continue", sdui.StepTestMe: "I know this · test me", sdui.StepShowMe: "Show me", sdui.StepGoDeeper: "Go deeper", sdui.StepSmallerSteps: "Smaller steps",
		},
		Requests: map[string]string{
			sdui.StepContinue:     "Continue: go to the next step.",
			sdui.StepTestMe:       "I know this step: test me with one harder question.",
			sdui.StepShowMe:       "Show me: reveal the answer of this step.",
			sdui.StepSmallerSteps: "Smaller steps: split this step into smaller ones.",
		},
		Title: "Reading tour", StepOf: "step %d of %d", StepN: "step %d", Summary: "%d known · %d shaky · %d missed",
		States:   map[string]string{sdui.TourKnown: "known", sdui.TourShaky: "shaky", sdui.TourMissed: "missed", sdui.TourCurrent: "now", sdui.TourNotYet: "not yet"},
		AskAgain: "Ask again", GoBack: "Back to step %d",
		AskRequest: "Ask me again about %q (%s), in a new form.", BackReq: "Go back to step %d: %q (%s), and teach it again.",
		Answer: "answer",
	}
	guideRussian = guideWords{
		Step: "Шаг %d · %s",
		Actions: map[string]string{
			sdui.StepContinue: "Дальше", sdui.StepTestMe: "Знаю · проверь меня", sdui.StepShowMe: "Покажи", sdui.StepGoDeeper: "Глубже", sdui.StepSmallerSteps: "Мельче",
		},
		Requests: map[string]string{
			sdui.StepContinue:     "Дальше: следующий шаг.",
			sdui.StepTestMe:       "Знаю этот шаг: проверь меня одним вопросом посложнее.",
			sdui.StepShowMe:       "Покажи: открой ответ этого шага.",
			sdui.StepSmallerSteps: "Мельче: раздели этот шаг на шаги поменьше.",
		},
		Title: "Тур по коду", StepOf: "шаг %d из %d", StepN: "шаг %d", Summary: "знаю %d · шатко %d · промах %d",
		States:   map[string]string{sdui.TourKnown: "знаю", sdui.TourShaky: "шатко", sdui.TourMissed: "промах", sdui.TourCurrent: "сейчас", sdui.TourNotYet: "ещё нет"},
		AskAgain: "Спросить снова", GoBack: "Назад к шагу %d",
		AskRequest: "Спроси меня снова про «%s» (%s), в новой форме.", BackReq: "Вернись к шагу %d: «%s» (%s), и объясни снова.",
		Answer: "ответ",
	}
)

func guideWordsFor(texts ...string) guideWords {
	if mostlyCyrillic(texts...) {
		return guideRussian
	}
	return guideEnglish
}

type stepMark struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Check   bool   `json:"check,omitempty"`
	Concept string `json:"concept,omitempty"`
	Ledger  string `json:"ledger,omitempty"`
	N       int    `json:"n"`
	Of      int    `json:"of,omitempty"`
	Pressed string `json:"-"`
}

type stepPressed struct {
	Step   string `json:"step"`
	Action string `json:"action"`
}

type guideStepArgs struct {
	Title   string `json:"title"`
	Check   bool   `json:"check"`
	Concept string `json:"concept"`
	Ledger  string `json:"ledger"`
}

type stepResult struct {
	Step string `json:"step"`
	N    int    `json:"n"`
	Of   int    `json:"of,omitempty"`
}

type stepActArgs struct {
	Session string `json:"session"`
	Action  string `json:"action"`
}

type stepActResult struct {
	Aside string `json:"aside,omitempty"`
}

type tourOfferArgs struct {
	Session string `json:"session"`
	Concept string `json:"concept"`
	Offer   string `json:"offer"`
}

type ledger struct {
	Current  string          `json:"current"`
	Concepts []ledgerConcept `json:"concepts"`
}

type ledgerConcept struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Status  string         `json:"status"`
	Parent  string         `json:"parent"`
	Links   []string       `json:"links"`
	Answers []ledgerAnswer `json:"answers"`
}

type ledgerAnswer struct {
	Missed string `json:"missed"`
}

type guideState struct {
	steps []*entry
}

type ledgerCache struct {
	mu    sync.Mutex
	items map[string]cachedLedger
}

type cachedLedger struct {
	stamp string
	value ledger
	ok    bool
}

var ledgers = ledgerCache{items: map[string]cachedLedger{}}

func readLedger(path string) (ledger, bool) {
	if path == "" {
		return ledger{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > ledgerLimit {
		ledgers.mu.Lock()
		delete(ledgers.items, path)
		ledgers.mu.Unlock()
		return ledger{}, false
	}
	stamp := info.ModTime().String() + "/" + strconv.FormatInt(info.Size(), 10)
	ledgers.mu.Lock()
	c, hit := ledgers.items[path]
	ledgers.mu.Unlock()
	if hit && c.stamp == stamp {
		return c.value, c.ok
	}
	raw, err := os.ReadFile(path)
	var l ledger
	ok := err == nil && json.Unmarshal(raw, &l) == nil && len(l.Concepts) > 0
	ledgers.mu.Lock()
	ledgers.items[path] = cachedLedger{stamp: stamp, value: l, ok: ok}
	ledgers.mu.Unlock()
	return l, ok
}

func (l ledger) index(concept string) int {
	return slices.IndexFunc(l.Concepts, func(c ledgerConcept) bool {
		return c.ID == concept || strings.EqualFold(c.Title, concept)
	})
}

func (a *app) guideStepCommand(_ context.Context, cmd pluginsdk.CommandRun, args guideStepArgs) (any, error) {
	s := a.session(cmd.Caller)
	if s == nil || s.snapshot().Workspace != cmd.WorkspaceID {
		return nil, errStepCaller
	}
	mark := stepMark{Title: clipRunes(strings.Join(strings.Fields(args.Title), " "), stepTitleRunes), Check: args.Check, Concept: strings.TrimSpace(args.Concept)}
	if mark.Title == "" {
		return nil, errStepTitle
	}
	if args.Ledger != "" {
		if !filepath.IsAbs(args.Ledger) || filepath.Base(args.Ledger) != ledgerName {
			return nil, errLedgerPath
		}
		mark.Ledger = filepath.Clean(args.Ledger)
	}
	if l, ok := readLedger(cmp.Or(mark.Ledger, s.keptLedger())); ok {
		if i := l.index(cmp.Or(mark.Concept, l.Current)); i >= 0 {
			mark.N, mark.Of = i+1, len(l.Concepts)
		}
	}
	s.mu.Lock()
	s.loadLocked()
	mark.ID = stepIDPrefix + strconv.Itoa(s.nextID+1)
	if mark.N == 0 {
		mark.N = len(s.guide.steps) + 1
	}
	s.recordLocked(s.actor(), kindStep, mark)
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return stepResult{Step: mark.ID, N: mark.N, Of: mark.Of}, nil
}

func (s *session) applyStep(e envelope) {
	var m stepMark
	if json.Unmarshal(e.Payload, &m) != nil || m.ID == "" {
		return
	}
	s.nextID++
	en := s.upsert(entryStep, m.ID, e.Turn)
	en.Mark = &m
	w := guideWordsFor(m.Title)
	en.Step = &sdui.GuideStepProps{Step: m.ID, Label: fmt.Sprintf(w.Step, m.N, m.Title)}
	s.guide.steps = append(s.guide.steps, en)
}

func (s *session) applyStepPressed(e envelope) {
	var p stepPressed
	if json.Unmarshal(e.Payload, &p) != nil {
		return
	}
	if i, ok := s.index[key(entryStep, p.Step)]; ok && s.entries[i].Mark != nil {
		m := *s.entries[i].Mark
		m.Pressed = p.Action
		s.entries[i].Mark = &m
		s.touch(s.entries[i])
	}
}

type guideSnapshot struct {
	step    *stepMark
	quizzes []entry
}

func (s *session) guideSnapshot() (guideSnapshot, bool) {
	defer s.lockLoaded()()
	if len(s.guide.steps) == 0 {
		return guideSnapshot{}, false
	}
	last := s.guide.steps[len(s.guide.steps)-1]
	if last.Mark == nil {
		return guideSnapshot{}, false
	}
	m := *last.Mark
	out := guideSnapshot{step: &m}
	out.step.Ledger = s.keptLedgerLocked()
	for _, en := range s.entries {
		if en.Kind == entryElicitation && en.Elicitation != nil && en.Elicitation.Quiz != nil && en.Grade != nil && en.Response != nil {
			out.quizzes = append(out.quizzes, *en)
		}
	}
	return out, true
}

func (s *session) keptLedger() string {
	defer s.lockLoaded()()
	return s.keptLedgerLocked()
}

func (s *session) keptLedgerLocked() string {
	for i := len(s.guide.steps) - 1; i >= 0; i-- {
		if p := s.guide.steps[i].Mark; p != nil && p.Ledger != "" {
			return p.Ledger
		}
	}
	return ""
}

type guideView struct {
	steps *sdui.StepControlsProps
	tour  *sdui.TourMapProps
}

func (a *app) guideViewOf(s *session, hidden bool) guideView {
	g, ok := s.guideSnapshot()
	if !ok {
		return guideView{}
	}
	rec := s.snapshot()
	var out guideView
	if !rec.closed() && len(s.pendingIDs()) == 0 {
		out.steps = a.stepControls(rec, *g.step)
	}
	out.tour = tourOf(rec.ID, g, !hidden)
	return out
}

func (a *app) stepAside(rec record, step string) *session {
	for _, c := range a.asides(rec.ID) {
		if r := c.snapshot(); r.isAside() && r.Aside.Anchor.Step == step {
			return c
		}
	}
	return nil
}

func (a *app) stepControls(rec record, m stepMark) *sdui.StepControlsProps {
	w := guideWordsFor(m.Title)
	running := turnActive(rec)
	p := &sdui.StepControlsProps{Session: rec.ID, Step: m.ID, Pressed: m.Pressed}
	if c := a.stepAside(rec, m.ID); c != nil {
		r := c.snapshot()
		p.Aside = &sdui.StepAside{Step: m.ID, Title: r.Title, Open: !r.closed(), Session: r.ID}
		if p.Aside.Open {
			p.Pressed = sdui.StepGoDeeper
		}
	}
	for _, id := range stepActionList {
		if id == sdui.StepContinue && m.Check {
			continue
		}
		enabled := !running
		if id == sdui.StepGoDeeper {
			enabled = enabled && (p.Aside != nil && p.Aside.Open || a.canAside(rec))
		}
		p.Actions = append(p.Actions, sdui.StepAction{ID: id, Label: w.Actions[id], Enabled: enabled, Primary: id == sdui.StepContinue, Shortcut: stepKeys[id]})
	}
	return p
}

func offersStep(p *sdui.StepControlsProps, action string) bool {
	return p != nil && slices.ContainsFunc(p.Actions, func(x sdui.StepAction) bool { return x.ID == action && x.Enabled })
}

func (a *app) actOnStep(ctx context.Context, workspaceID string, s *session, action string, beside bool) (stepActResult, error) {
	g, ok := s.guideSnapshot()
	if !ok {
		return stepActResult{}, errNoStep
	}
	rec := s.snapshot()
	if rec.closed() || len(s.pendingIDs()) > 0 {
		return stepActResult{}, errStepAction
	}
	p := a.stepControls(rec, *g.step)
	if !offersStep(p, action) {
		return stepActResult{}, errStepAction
	}
	out, err := a.runStepAction(ctx, workspaceID, s, *g.step, action, beside)
	if err != nil {
		return stepActResult{}, err
	}
	s.mu.Lock()
	s.recordLocked(actorUser, kindStepPressed, stepPressed{Step: g.step.ID, Action: action})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return out, nil
}

func (a *app) runStepAction(ctx context.Context, workspaceID string, s *session, step stepMark, action string, beside bool) (stepActResult, error) {
	if action != sdui.StepGoDeeper {
		return stepActResult{}, a.prompt(workspaceID, promptArgs{Session: s.snapshot().ID, Text: guideWordsFor(step.Title).Requests[action], User: true})
	}
	an := asideAnchor{Step: step.ID, Quote: step.Title}
	if beside {
		return stepActResult{}, a.openAside(ctx, workspaceID, s, an)
	}
	c, _, err := a.newAside(ctx, s, an, "")
	if err != nil {
		return stepActResult{}, err
	}
	a.kick(workspaceID)
	return stepActResult{Aside: c.snapshot().ID}, nil
}

func (a *app) stepActCommand(ctx context.Context, cmd pluginsdk.CommandRun, args stepActArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errStepUser
	}
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return a.actOnStep(ctx, cmd.WorkspaceID, s, args.Action, false)
}

func (a *app) tourOfferCommand(_ context.Context, cmd pluginsdk.CommandRun, args tourOfferArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errStepUser
	}
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return nil, a.actOnTour(cmd.WorkspaceID, s, args.Concept, args.Offer)
}

func (a *app) actOnTour(workspaceID string, s *session, concept, offer string) error {
	g, ok := s.guideSnapshot()
	if !ok {
		return errTourOffer
	}
	t := tourOf(s.snapshot().ID, g, true)
	if t == nil {
		return errTourOffer
	}
	i := slices.IndexFunc(t.Concepts, func(c sdui.TourConcept) bool { return c.ID == concept })
	if i < 0 || !slices.ContainsFunc(t.Concepts[i].Offers, func(o sdui.TourOffer) bool { return o.ID == offer }) {
		return errTourOffer
	}
	c := t.Concepts[i]
	w := guideWordsFor(t.Title)
	text := fmt.Sprintf(w.AskRequest, c.Label, c.ID)
	if offer == sdui.TourGoBack {
		text = fmt.Sprintf(w.BackReq, c.Step, c.Label, c.ID)
	}
	return a.prompt(workspaceID, promptArgs{Session: s.snapshot().ID, Text: text, User: true})
}

func quizConceptState(en entry) string {
	switch {
	case en.Response.Action != acp.ElicitationAccept, en.Grade.Verdict == verdictPending:
		return ""
	case en.Grade.Flag == flagSureWrong, en.Grade.Verdict == verdictWrong:
		return sdui.TourMissed
	case en.Grade.Verdict == verdictCorrect && answerOf(*en.Response).Confidence == confSure:
		return sdui.TourKnown
	}
	return sdui.TourShaky
}

func quizNote(en entry, w guideWords) string {
	p := quizProps(&en, false, "")
	if p.Reveal == nil {
		return ""
	}
	r := p.Reveal
	note := strings.Join(nonEmpty(r.Yours, r.Confidence), " · ")
	if r.Answer != "" {
		note += "; " + w.Answer + " " + r.Answer
	}
	return note
}

func ledgerState(status string) string {
	switch status {
	case sdui.TourKnown, sdui.TourShaky, sdui.TourMissed:
		return status
	}
	return sdui.TourNotYet
}

func tourOf(sid string, g guideSnapshot, shown bool) *sdui.TourMapProps {
	l, ok := readLedger(g.step.Ledger)
	if !ok {
		return nil
	}
	titles := make([]string, 0, len(l.Concepts))
	for _, c := range l.Concepts {
		titles = append(titles, c.Title)
	}
	w := guideWordsFor(strings.Join(titles, " "))
	current := l.index(l.Current)
	if current < 0 && g.step.Concept != "" {
		current = l.index(g.step.Concept)
	}
	p := &sdui.TourMapProps{Session: sid, Title: w.Title, Of: len(l.Concepts), Shown: shown, Shortcut: tourPanelKey}
	if current >= 0 {
		p.Step, p.Current = current+1, l.Concepts[current].ID
	}
	p.StepLabel = fmt.Sprintf(w.StepOf, max(p.Step, 1), p.Of)
	pos := map[string]int{}
	for i, c := range l.Concepts {
		pos[c.ID] = i
	}
	for i, c := range l.Concepts {
		state, note := ledgerState(c.Status), ""
		if n := len(c.Answers); n > 0 {
			note = c.Answers[n-1].Missed
		}
		for _, q := range g.quizzes {
			if spec := q.Elicitation.Quiz; spec.Concept != "" && (spec.Concept == c.ID || strings.EqualFold(spec.Concept, c.Title)) {
				if st := quizConceptState(q); st != "" {
					state, note = st, cmp.Or(quizNote(q, w), note)
				}
			}
		}
		if i == current {
			state = sdui.TourCurrent
		}
		tc := sdui.TourConcept{ID: c.ID, Label: c.Title, State: state, StateLabel: w.States[state], Step: i + 1, StepLabel: fmt.Sprintf(w.StepN, i+1), Depth: depthOf(l, pos, i), Note: note}
		switch state {
		case sdui.TourKnown:
			p.Counts.Known++
		case sdui.TourShaky:
			p.Counts.Shaky++
		case sdui.TourMissed:
			p.Counts.Missed++
		case sdui.TourNotYet:
			p.Counts.NotYet++
		}
		if state == sdui.TourKnown || state == sdui.TourShaky || state == sdui.TourMissed {
			tc.Offers = []sdui.TourOffer{{ID: sdui.TourAskAgain, Label: w.AskAgain}, {ID: sdui.TourGoBack, Label: fmt.Sprintf(w.GoBack, i+1)}}
		}
		if j, ok := pos[c.Parent]; ok && j < i {
			p.Edges = append(p.Edges, sdui.TourEdge{From: c.Parent, To: c.ID})
		}
		for _, link := range c.Links {
			if j, ok := pos[link]; ok && j != i {
				tc.Links = append(tc.Links, sdui.TourLink{ID: link, Label: l.Concepts[j].Title})
				p.Edges = append(p.Edges, sdui.TourEdge{From: c.ID, To: link, Branch: true})
			}
		}
		p.Concepts = append(p.Concepts, tc)
	}
	p.Summary = fmt.Sprintf(w.Summary, p.Counts.Known, p.Counts.Shaky, p.Counts.Missed)
	for _, st := range []string{sdui.TourKnown, sdui.TourShaky, sdui.TourMissed, sdui.TourCurrent, sdui.TourNotYet} {
		p.Legend = append(p.Legend, sdui.TourLegend{State: st, Label: w.States[st]})
	}
	return p
}

func depthOf(l ledger, pos map[string]int, i int) int {
	d := 0
	for d < maxTourDepth {
		j, ok := pos[l.Concepts[i].Parent]
		if !ok || j >= i {
			break
		}
		i, d = j, d+1
	}
	return d
}

func stepEntryNode(sid string, en *entry) (sdui.Node, bool) {
	if en.Step == nil {
		return sdui.Node{}, false
	}
	return sdui.GuideStep(sid+"/step/"+en.ID, *en.Step), true
}

type stepActValue struct {
	Action string `json:"action"`
}

type tourOfferValue struct {
	Concept string `json:"concept"`
	Offer   string `json:"offer"`
}

const (
	actStepAct   = "stepAct"
	actTourOffer = "tourOffer"
	actTourPanel = "tourPanel"
	stepsNode    = "steps"
	tourNode     = "tour"
	stripNode    = "strip"
)

func (a *app) guideAct(ctx context.Context, ev pluginsdk.UIEvent, s *session, st *sessionUI) error {
	switch ev.Action {
	case actStepAct:
		var v stepActValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		_, err := a.actOnStep(ctx, ev.WorkspaceID, s, v.Action, true)
		return err
	case actTourOffer:
		var v tourOfferValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.actOnTour(ev.WorkspaceID, s, v.Concept, v.Offer)
	}
	a.uiMu.Lock()
	st.tourHidden = !st.tourHidden
	a.uiMu.Unlock()
	a.kick(ev.WorkspaceID)
	return nil
}

func continueLabel(sc screen) string {
	p := sc.Guide.steps
	if !offersStep(p, sdui.StepContinue) {
		return ""
	}
	return p.Actions[0].Label
}

func (a *app) continuesOnEmpty(s *session, st *sessionUI, text string) bool {
	if strings.TrimSpace(text) != "" {
		return false
	}
	a.uiMu.Lock()
	attached := len(st.attachments) > 0
	a.uiMu.Unlock()
	if attached {
		return false
	}
	return offersStep(a.guideViewOf(s, true).steps, sdui.StepContinue)
}
