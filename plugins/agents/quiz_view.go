package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	actQuizPick = "quizPick"
	actQuizRate = "quizRate"
	actQuizUndo = "quizUndo"
	actQuizSkip = "quizSkip"
	actQuizOpen = "quizOpen"

	quizAsking   = "asking"
	quizSending  = "sending"
	quizChecking = "checking"
	quizDone     = "answered"
	quizSkippedS = "skipped"
	quizCancelS  = "cancelled"

	quizOpenKey   = "opt+g"
	quizRevealPad = 2
	optionKeys    = "abcd"
)

type quizWords struct {
	Asking, Answered, Checking, Skipped, Cancelled string
	Hints                                          map[string]string
	HowSure                                        string
	Levels                                         [3]string
	SendsLine, SendsRange, SendsOption             string
	PickLine, PickRange, PickOption                string
	OnlyRates                                      string
	Open                                           string
	Correct, Close, Checked                        string
	Wrong                                          map[string]string
	OneLine, ManyLines, Lines                      string
	SureWrong, SureWrongConcept                    string
	You, Answer                                    string
	Labels                                         sdui.QuizLabels
}

var (
	quizEnglish = quizWords{
		Asking: "%s is asking", Answered: "Answered", Checking: "Checking", Skipped: "Skipped", Cancelled: "Cancelled",
		Hints:   map[string]string{quizLine: "point to the code", quizRange: "a range", quizValue: "a value", quizText: "in your words", quizChoice: "pick one"},
		HowSure: "How sure?", Levels: [3]string{"guess", "think so", "sure"},
		SendsLine: "Line %d · a chip sends", SendsRange: "Lines %d–%d · a chip sends", SendsOption: "%s · a chip sends",
		PickLine: "Pick a line, or rate and answer in words", PickRange: "Pick a range, or rate and answer in words", PickOption: "Pick an option, or rate and answer in words",
		OnlyRates: "chips only rate here; ⌘↩ sends",
		Open:      "Open in nvim",
		Correct:   "Correct", Close: "Close", Checked: "Checking",
		Wrong:   map[string]string{quizLine: "Not this line", quizRange: "Not this range", quizValue: "Not quite", quizText: "Not quite", quizChoice: "Not this one"},
		OneLine: "line %d", ManyLines: "lines %d–%d", Lines: "lines %s",
		SureWrong: "You were sure. The guide asks it again later.", SureWrongConcept: "You were sure. %s goes back on the map as missed; the guide asks it again later.",
		You: "you", Answer: "answer",
		Labels: sdui.QuizLabels{Skip: "Skip", Undo: "Undo", Sending: "Sending", Fold: "Fold", AnswerHint: "Typing answers in words; the guide grades it", You: "You:", Answer: "Answer:", AllLines: "All {n} lines", SecondsLeft: "{n} s left", SayIt: "Say it in words…", OpenOnMac: "Open on Mac", Line: "Line", Range: "Range"},
	}
	quizRussian = quizWords{
		Asking: "%s спрашивает", Answered: "Отвечено", Checking: "Проверка", Skipped: "Пропущено", Cancelled: "Отменено",
		Hints:   map[string]string{quizLine: "укажите в коде", quizRange: "диапазон", quizValue: "значение", quizText: "своими словами", quizChoice: "выберите один"},
		HowSure: "Насколько уверены?", Levels: [3]string{"наугад", "думаю, да", "уверен"},
		SendsLine: "Строка %d · оценка отправляет", SendsRange: "Строки %d–%d · оценка отправляет", SendsOption: "%s · оценка отправляет",
		PickLine: "Выберите строку или оцените и ответьте словами", PickRange: "Выберите диапазон или оцените и ответьте словами", PickOption: "Выберите вариант или оцените и ответьте словами",
		OnlyRates: "здесь оценка только оценивает; ⌘↩ отправляет",
		Open:      "Открыть в nvim",
		Correct:   "Верно", Close: "Близко", Checked: "Проверка",
		Wrong:   map[string]string{quizLine: "Не эта строка", quizRange: "Не этот диапазон", quizValue: "Не совсем", quizText: "Не совсем", quizChoice: "Не этот"},
		OneLine: "строка %d", ManyLines: "строки %d–%d", Lines: "строки %s",
		SureWrong: "Вы были уверены. Гид спросит это снова позже.", SureWrongConcept: "Вы были уверены. %s вернётся на карту как пропущенное; гид спросит снова позже.",
		You: "вы", Answer: "ответ",
		Labels: sdui.QuizLabels{Skip: "Пропустить", Undo: "Отменить", Sending: "Отправка", Fold: "Свернуть", AnswerHint: "Ответ словами; гид его оценит", You: "Вы:", Answer: "Ответ:", AllLines: "Все строки: {n}", SecondsLeft: "осталось {n} с", SayIt: "Скажите словами…", OpenOnMac: "Открыть на Mac", Line: "Строка", Range: "Диапазон"},
	}
)

func wordsFor(el elicitation) quizWords {
	if el.labels().Send == labelsRussian.Send {
		return quizRussian
	}
	return quizEnglish
}

func quizCardNode(sc screen, en *entry) sdui.Node {
	p := quizProps(en, sc.Pending[en.ID], agentLabel(sc.View.Session.Agent))
	return sdui.QuizCard(elicitationNode(sc.View.Session.ID, en.ID), p)
}

func dockedQuiz(sc screen) (quizWords, bool) {
	for _, en := range sc.View.Entries {
		if pending(sc, en) && en.Kind == entryElicitation && en.Elicitation != nil && en.Elicitation.Quiz != nil {
			return wordsFor(*en.Elicitation), true
		}
	}
	return quizWords{}, false
}

func withQuizContent(v *transcriptView, pending map[string]bool) {
	label := agentLabel(v.Session.Agent)
	for _, en := range v.Entries {
		if en.Kind == entryElicitation && en.Elicitation != nil && en.Elicitation.Quiz != nil {
			p := quizProps(en, pending[en.ID], label)
			en.Quiz = &p
		}
	}
}

func quizProps(en *entry, pending bool, agent string) sdui.QuizProps {
	spec, w := *en.Elicitation.Quiz, wordsFor(*en.Elicitation)
	var st quizState
	if en.QuizState != nil {
		st = *en.QuizState
	}
	p := sdui.QuizProps{Request: en.ID, Type: spec.Type, Question: spec.Question, Step: spec.Step, Concept: spec.Concept, Labels: w.Labels}
	p.State = quizStateOf(en, pending, st)
	switch p.State {
	case quizAsking, quizSending:
		p.Title, p.Hint, p.Dock = fmt.Sprintf(w.Asking, agent), w.Hints[spec.Type], true
		p.Excerpt = askingExcerpt(spec, st, w)
		p.Options = quizOptions(spec, st.Option)
		p.Confidence = quizConfidence(spec, st, w)
	default:
		p.Title = map[string]string{quizChecking: w.Checking, quizDone: w.Answered, quizSkippedS: w.Skipped, quizCancelS: w.Cancelled}[p.State]
		var ans quizAnswer
		if en.Response != nil && en.Response.Action == acp.ElicitationAccept {
			ans = answerOf(*en.Response)
		}
		grade := en.Grade
		if grade == nil {
			grade = &quizGrade{Verdict: verdictPending}
		}
		p.Excerpt = answeredExcerpt(spec, ans, *grade, w)
		p.Options = quizOptions(spec, ans.Option)
		if en.Response != nil && en.Response.Action == acp.ElicitationAccept {
			p.Reveal = quizReveal(spec, ans, *grade, w)
		}
	}
	return p
}

func quizStateOf(en *entry, pending bool, st quizState) string {
	switch {
	case pending && st.Holding:
		return quizSending
	case pending:
		return quizAsking
	case en.Response == nil || en.Response.Action == acp.ElicitationCancel:
		return quizCancelS
	case en.Response.Action == acp.ElicitationDecline:
		return quizSkippedS
	case en.Grade == nil || en.Grade.Verdict == verdictPending:
		return quizChecking
	}
	return quizDone
}

func excerptBase(spec quizSpec, w quizWords) *sdui.QuizExcerpt {
	if spec.File == "" {
		return nil
	}
	return &sdui.QuizExcerpt{File: spec.File, Commit: shortSHA(spec.Commit), Pick: "none", Open: &sdui.QuizOpen{Label: w.Open, Shortcut: quizOpenKey}, Span: foldLabel(spec.From, spec.last(), w)}
}

func shortSHA(sha string) string {
	return sha[:min(7, len(sha))]
}

func askingExcerpt(spec quizSpec, st quizState, w quizWords) *sdui.QuizExcerpt {
	x := excerptBase(spec, w)
	if x == nil {
		return nil
	}
	for i, text := range spec.Lines {
		x.Rows = append(x.Rows, sdui.QuizRow{N: spec.From + i, Text: text})
	}
	switch spec.Type {
	case quizLine:
		x.Pick = quizLine
	case quizRange:
		x.Pick = quizRange
	}
	x.Cursor = spec.From
	if st.From > 0 {
		x.Picked, x.Cursor = []int{st.From, st.To}, st.From
	}
	return x
}

func answeredExcerpt(spec quizSpec, ans quizAnswer, g quizGrade, w quizWords) *sdui.QuizExcerpt {
	x := excerptBase(spec, w)
	if x == nil {
		return nil
	}
	marks := quizMarks(spec, ans, g, w)
	if len(marks) == 0 {
		x.Rows = []sdui.QuizRow{{Fold: fmt.Sprintf(w.ManyLines, spec.From, spec.last())}}
		return x
	}
	lines := slices.Sorted(func(yield func(int) bool) {
		for n := range marks {
			if !yield(n) {
				return
			}
		}
	})
	first, last := max(spec.From, lines[0]-quizRevealPad), min(spec.last(), lines[len(lines)-1]+quizRevealPad)
	if first > spec.From {
		x.Rows = append(x.Rows, sdui.QuizRow{Fold: foldLabel(spec.From, first-1, w)})
	}
	for n := first; n <= last; n++ {
		row := sdui.QuizRow{N: n, Text: spec.Lines[n-spec.From]}
		if m, ok := marks[n]; ok {
			row.Tone = m.tone
			if prev, ok := marks[n-1]; !ok || prev != m {
				row.Label = m.label
			}
		}
		x.Rows = append(x.Rows, row)
	}
	if last < spec.last() {
		x.Rows = append(x.Rows, sdui.QuizRow{Fold: foldLabel(last+1, spec.last(), w)})
	}
	return x
}

func foldLabel(from, to int, w quizWords) string {
	if from == to {
		return fmt.Sprintf(w.OneLine, from)
	}
	return fmt.Sprintf(w.ManyLines, from, to)
}

type quizMark struct {
	tone, label string
}

func quizMarks(spec quizSpec, ans quizAnswer, g quizGrade, w quizWords) map[int]quizMark {
	if spec.Type != quizLine && spec.Type != quizRange || g.Verdict == verdictPending {
		return nil
	}
	key := map[int]bool{}
	if spec.Type == quizRange {
		for n := spec.Key[0]; n <= spec.Key[1]; n++ {
			key[n] = true
		}
	} else {
		for _, n := range spec.Key {
			key[n] = true
		}
	}
	yours := map[int]bool{}
	if ans.Text == "" && ans.From > 0 {
		for n := ans.From; n <= ans.To; n++ {
			yours[n] = true
		}
	}
	rated := w.You + " · " + levelLabel(ans.Confidence, w)
	marks := map[int]quizMark{}
	for n := range key {
		label := w.Answer
		switch {
		case yours[n] && g.Verdict == verdictCorrect:
			label = rated
		case yours[n]:
			label = w.You + " · " + w.Answer
		}
		marks[n] = quizMark{tone: "success", label: label}
	}
	for n := range yours {
		if key[n] {
			continue
		}
		label := w.You
		if ans.From == ans.To {
			label = rated
		}
		marks[n] = quizMark{tone: "danger", label: label}
	}
	return marks
}

func quizOptions(spec quizSpec, picked string) []sdui.QuizOption {
	if spec.Type != quizChoice {
		return nil
	}
	out := make([]sdui.QuizOption, 0, len(spec.Options))
	for i, o := range spec.Options {
		out = append(out, sdui.QuizOption{Value: o.Value, Label: o.Label, Key: optionKeys[i : i+1], On: o.Value == picked})
	}
	return out
}

func quizConfidence(spec quizSpec, st quizState, w quizWords) *sdui.QuizConfidence {
	c := &sdui.QuizConfidence{Label: w.HowSure, Value: st.Confidence, Enabled: true, NeedsRating: st.Confidence == "" || st.Holding, Sends: spec.sends(), HoldMs: int(quizHold / time.Millisecond)}
	for i, v := range confidences {
		c.Levels = append(c.Levels, sdui.QuizLevel{Value: v, Label: w.Levels[i], Key: strconv.Itoa(i + 1)})
	}
	if !spec.sends() {
		c.Hint = w.OnlyRates
		return c
	}
	switch {
	case spec.Type == quizChoice && st.Option != "":
		c.Hint = fmt.Sprintf(w.SendsOption, optionLabelOf(spec, st.Option))
	case spec.Type == quizChoice:
		c.Hint = w.PickOption
	case st.From > 0 && st.From == st.To:
		c.Hint = fmt.Sprintf(w.SendsLine, st.From)
	case st.From > 0:
		c.Hint = fmt.Sprintf(w.SendsRange, st.From, st.To)
	case spec.Type == quizRange:
		c.Hint = w.PickRange
	default:
		c.Hint = w.PickLine
	}
	return c
}

func optionLabelOf(spec quizSpec, value string) string {
	for _, o := range spec.Options {
		if o.Value == value {
			return o.Label
		}
	}
	return value
}

func levelLabel(value string, w quizWords) string {
	if i := slices.Index(confidences, value); i >= 0 {
		return w.Levels[i]
	}
	return value
}

func quizReveal(spec quizSpec, ans quizAnswer, g quizGrade, w quizWords) *sdui.QuizReveal {
	r := &sdui.QuizReveal{Verdict: g.Verdict, Yours: answerWords(spec, ans, w), Confidence: levelLabel(ans.Confidence, w), Why: g.Why, Ref: g.Ref, Flag: g.Flag}
	switch g.Verdict {
	case verdictCorrect:
		r.Label = w.Correct
	case verdictClose:
		r.Label = w.Close
	case verdictWrong:
		r.Label = w.Wrong[spec.Type]
	default:
		r.Label = w.Checked
	}
	if g.Verdict == verdictClose || g.Verdict == verdictWrong {
		r.Answer = keyWords(spec, w)
	}
	if g.Flag == flagSureWrong {
		r.Note = w.SureWrong
		if spec.Concept != "" {
			r.Note = fmt.Sprintf(w.SureWrongConcept, spec.Concept)
		}
	}
	return r
}

func answerWords(spec quizSpec, ans quizAnswer, w quizWords) string {
	switch {
	case ans.Text != "":
		return ans.Text
	case ans.Option != "":
		return optionLabelOf(spec, ans.Option)
	case ans.From > 0:
		return foldLabel(ans.From, ans.To, w)
	}
	return ""
}

func keyWords(spec quizSpec, w quizWords) string {
	switch spec.Type {
	case quizLine:
		if len(spec.Key) == 1 {
			return fmt.Sprintf(w.OneLine, spec.Key[0])
		}
		parts := make([]string, 0, len(spec.Key))
		for _, n := range spec.Key {
			parts = append(parts, strconv.Itoa(n))
		}
		return fmt.Sprintf(w.Lines, strings.Join(parts, ", "))
	case quizRange:
		return foldLabel(spec.Key[0], spec.Key[1], w)
	case quizChoice:
		labels := make([]string, 0, len(spec.Accept))
		for _, v := range spec.Accept {
			labels = append(labels, optionLabelOf(spec, v))
		}
		return strings.Join(labels, ", ")
	case quizValue:
		if len(spec.Accept) > 0 {
			return spec.Accept[0]
		}
	}
	return ""
}

func quizUIAct(ev pluginsdk.UIEvent, s *session) (quizAct, error) {
	var v struct {
		Request    string `json:"request"`
		From       int    `json:"from"`
		To         int    `json:"to"`
		Option     string `json:"option"`
		Confidence string `json:"confidence"`
	}
	if err := decode(ev.Value, &v); err != nil {
		return quizAct{}, err
	}
	action := map[string]string{actQuizPick: quizPick, actQuizRate: quizRate, actQuizUndo: quizUndo, actQuizSkip: quizSkip, actQuizOpen: quizOpenIt}[ev.Action]
	return quizAct{Session: s.snapshot().ID, Request: v.Request, Action: action, From: v.From, To: v.To, Option: v.Option, Confidence: v.Confidence}, nil
}
