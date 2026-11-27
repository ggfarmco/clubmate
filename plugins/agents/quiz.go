package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
)

const (
	elicitQuiz    = "quiz"
	kindQuizState = "quiz/state"
	kindQuizGrade = "quiz/grade"

	quizLine   = "line"
	quizRange  = "range"
	quizValue  = "value"
	quizText   = "text"
	quizChoice = "choice"

	verdictCorrect = "correct"
	verdictClose   = "close"
	verdictWrong   = "wrong"
	verdictPending = "pending"
	flagSureWrong  = "sure_wrong"

	confGuess = "guess"
	confThink = "think"
	confSure  = "sure"

	quizPick   = "pick"
	quizRate   = "rate"
	quizUndo   = "undo"
	quizSend   = "send"
	quizSkip   = "skip"
	quizOpenIt = "open"

	quizAsked     = "waiting"
	quizAnswered  = "answered"
	quizSkipped   = "skipped"
	quizCancelled = "cancelled"

	maxQuizLines   = 80
	minQuizOptions = 2
	maxQuizOptions = 4
	quizTextRunes  = 2000
	quizHoldMs     = 2000
	quizHoldEnv    = "CLUBMATE_AGENTS_QUIZ_HOLD"
	gradeByPlugin  = "plugin"
	gradeByAgent   = "agent"
)

var (
	quizHold    = quizHoldMs * time.Millisecond
	quizTypes   = []string{quizLine, quizRange, quizValue, quizText, quizChoice}
	confidences = []string{confGuess, confThink, confSure}
	verdicts    = []string{verdictCorrect, verdictClose, verdictWrong}

	errQuizCaller   = errors.New("agents.quiz asks from an agent session: call it from the session that asks")
	errQuizType     = errors.New("invalid_params: type must be line, range, value, text or choice")
	errQuizQuestion = errors.New("invalid_params: question is required")
	errQuizExcerpt  = errors.New("invalid_params: line and range need file, from and to")
	errQuizSpan     = fmt.Errorf("invalid_params: from..to must be 1..%d lines inside the file", maxQuizLines)
	errQuizKey      = errors.New("invalid_params: the key does not fit the type: lines (one or more inside from..to) for line, lines [first, last] for range, accept for value and choice")
	errQuizOptions  = fmt.Errorf("invalid_params: choice needs %d to %d options", minQuizOptions, maxQuizOptions)
	errQuizFile     = errors.New("invalid_params: file must be a path inside the session's worktree")
	errNotAQuiz     = errors.New("not a pending quiz of this session")
	errQuizAction   = errors.New("action must be pick, rate, undo, send, skip or open")
	errQuizPick     = errors.New("the pick is outside the excerpt or not an option")
	errRateFirst    = errors.New("rate first: 1 guess · 2 think so · 3 sure")
	errConfidence   = errors.New("confidence must be guess, think or sure")
	errQuizText     = errors.New("type an answer first")
	errQuizGraded   = errors.New("this quiz has no answer waiting for a grade")
	errQuizVerdict  = errors.New("invalid_params: verdict must be correct, close or wrong")
	errQuizAccept   = errors.New("answer a quiz with agents.quiz-answer")
	errQuizAttached = errors.New("a quiz answer takes no attachments: remove them, then send")
)

type quizSpec struct {
	Type     string         `json:"type"`
	Question string         `json:"question"`
	Concept  string         `json:"concept,omitempty"`
	Step     string         `json:"step,omitempty"`
	File     string         `json:"file,omitempty"`
	Commit   string         `json:"commit,omitempty"`
	From     int            `json:"from,omitempty"`
	Lines    []string       `json:"lines,omitempty"`
	Key      []int          `json:"key,omitempty"`
	Accept   []string       `json:"accept,omitempty"`
	Options  []elicitOption `json:"options,omitempty"`
	Why      string         `json:"why,omitempty"`
	Ref      string         `json:"ref,omitempty"`
}

type quizState struct {
	Request    string `json:"request"`
	From       int    `json:"from,omitempty"`
	To         int    `json:"to,omitempty"`
	Option     string `json:"option,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Holding    bool   `json:"holding,omitempty"`
}

type quizAnswer struct {
	From       int    `json:"from,omitempty"`
	To         int    `json:"to,omitempty"`
	Option     string `json:"option,omitempty"`
	Text       string `json:"text,omitempty"`
	Confidence string `json:"confidence"`
}

type quizGrade struct {
	Request string `json:"request"`
	Verdict string `json:"verdict"`
	Why     string `json:"why,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Flag    string `json:"flag,omitempty"`
	By      string `json:"by"`
}

type quizRun struct {
	done chan struct{}
	hold *time.Timer
	gen  int
}

type quizArgs struct {
	Request  string         `json:"request"`
	Timeout  int            `json:"timeout"`
	Type     string         `json:"type"`
	Question string         `json:"question"`
	Concept  string         `json:"concept"`
	Step     string         `json:"step"`
	File     string         `json:"file"`
	Commit   string         `json:"commit"`
	From     int            `json:"from"`
	To       int            `json:"to"`
	Lines    []int          `json:"lines"`
	Accept   []string       `json:"accept"`
	Options  []elicitOption `json:"options"`
	Why      string         `json:"why"`
	Ref      string         `json:"ref"`
}

type quizResult struct {
	Request    string `json:"request"`
	Status     string `json:"status"`
	Answer     string `json:"answer,omitempty"`
	Lines      []int  `json:"lines,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Verdict    string `json:"verdict,omitempty"`
	Correct    *bool  `json:"correct,omitempty"`
}

type quizGradeArgs struct {
	Request string `json:"request"`
	Verdict string `json:"verdict"`
	Why     string `json:"why"`
	Ref     string `json:"ref"`
}

type quizAct struct {
	Session    string `json:"session"`
	Request    string `json:"request"`
	Action     string `json:"action"`
	From       int    `json:"from"`
	To         int    `json:"to"`
	Option     string `json:"option"`
	Confidence string `json:"confidence"`
	Text       string `json:"text"`
}

func (a *app) quizCommand(ctx context.Context, cmd pluginsdk.CommandRun, args quizArgs) (any, error) {
	s := a.session(cmd.Caller)
	if s == nil || s.snapshot().Workspace != cmd.WorkspaceID {
		return nil, errQuizCaller
	}
	if args.Request == "" {
		spec, err := quizSpecOf(ctx, s.snapshot().Worktree, args)
		if err != nil {
			return nil, err
		}
		args.Request = s.askQuiz(spec)
	}
	return s.awaitQuiz(ctx, args.Request, waitTimeout(args.Timeout))
}

func (a *app) quizGradeCommand(_ context.Context, cmd pluginsdk.CommandRun, args quizGradeArgs) (any, error) {
	s := a.session(cmd.Caller)
	if s == nil || s.snapshot().Workspace != cmd.WorkspaceID {
		return nil, errQuizCaller
	}
	if !slices.Contains(verdicts, args.Verdict) {
		return nil, errQuizVerdict
	}
	return nil, s.gradeQuiz(args)
}

func (a *app) quizAnswerCommand(ctx context.Context, cmd pluginsdk.CommandRun, args quizAct) (any, error) {
	if !a.userWord(cmd) {
		return nil, errUserOnly
	}
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return nil, a.actOnQuiz(ctx, cmd.WorkspaceID, s, args)
}

func (a *app) actOnQuiz(ctx context.Context, workspaceID string, s *session, act quizAct) error {
	if act.Action != quizOpenIt {
		return s.quizAct(act)
	}
	spec, st, ok := s.quizOf(act.Request)
	if !ok || spec.File == "" {
		return errNotAQuiz
	}
	line := spec.From
	switch {
	case st.From > 0:
		line = st.From
	case len(spec.Key) > 0 && s.quizAnsweredOf(act.Request):
		line = spec.Key[0]
	}
	return a.host.RunCommand(ctx, openCmd, workspaceID, pathValue{Path: filepath.Join(s.snapshot().Worktree, spec.File), Line: line, Focus: true}, nil)
}

func quizSpecOf(ctx context.Context, root string, args quizArgs) (quizSpec, error) {
	spec := quizSpec{Type: args.Type, Question: strings.TrimSpace(args.Question), Concept: clipRunes(strings.TrimSpace(args.Concept), 40), Step: clipRunes(strings.TrimSpace(args.Step), 40), Why: strings.TrimSpace(args.Why), Ref: strings.TrimSpace(args.Ref)}
	if !slices.Contains(quizTypes, spec.Type) {
		return quizSpec{}, errQuizType
	}
	if spec.Question == "" {
		return quizSpec{}, errQuizQuestion
	}
	needsExcerpt := spec.Type == quizLine || spec.Type == quizRange
	if args.File != "" || needsExcerpt {
		if args.File == "" || args.From < 1 || args.To < 1 {
			return quizSpec{}, errQuizExcerpt
		}
		if err := spec.readExcerpt(ctx, root, args); err != nil {
			return quizSpec{}, err
		}
	}
	if err := spec.setKey(args); err != nil {
		return quizSpec{}, err
	}
	if spec.Ref == "" && spec.File != "" && len(spec.Key) > 0 {
		spec.Ref = spec.File + ":" + strconv.Itoa(spec.Key[0])
	}
	return spec, nil
}

func quizFileIn(roots []string, file string) (string, bool) {
	paths := []string{filepath.Clean(file)}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(file)); err == nil {
		paths = append(paths, filepath.Join(dir, filepath.Base(file)))
	}
	for _, r := range roots {
		for _, p := range paths {
			if rel, err := filepath.Rel(r, p); err == nil && filepath.IsLocal(rel) {
				return filepath.ToSlash(rel), true
			}
		}
	}
	return "", false
}

func (q *quizSpec) readExcerpt(ctx context.Context, root string, args quizArgs) error {
	roots := []string{root}
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		root = real
		roots = append(roots, real)
	}
	file := filepath.ToSlash(filepath.Clean(args.File))
	if filepath.IsAbs(args.File) {
		rel, ok := quizFileIn(roots, args.File)
		if !ok {
			return errQuizFile
		}
		file = rel
	}
	if file == "." || strings.HasPrefix(file, "../") || file == ".." {
		return errQuizFile
	}
	from, to := args.From, args.To
	if to < from || to-from+1 > maxQuizLines {
		return errQuizSpan
	}
	commit := strings.TrimSpace(args.Commit)
	if commit == "" {
		commit = "HEAD"
	}
	top := repoTop(root)
	prefix, err := filepath.Rel(top, root)
	if err != nil {
		return errQuizFile
	}
	sha, err := gitOutput(ctx, top, nil, "rev-parse", "--verify", "--end-of-options", commit+"^{commit}")
	if err != nil {
		return fmt.Errorf("invalid_params: commit %q: %w", commit, err)
	}
	out, err := gitrepo.RunEnv(ctx, top, nil, "show", sha+":"+path.Join(filepath.ToSlash(prefix), file))
	if err != nil {
		return fmt.Errorf("invalid_params: %s at %s: %w", file, sha[:min(7, len(sha))], err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if to > len(lines) {
		return errQuizSpan
	}
	q.File, q.Commit, q.From = file, sha, from
	for _, l := range lines[from-1 : to] {
		q.Lines = append(q.Lines, strings.ReplaceAll(strings.TrimRight(l, "\r"), "\t", "    "))
	}
	return nil
}

func (q *quizSpec) setKey(args quizArgs) error {
	inside := func(n int) bool { return n >= q.From && n < q.From+len(q.Lines) }
	switch q.Type {
	case quizLine:
		if len(args.Lines) == 0 || !allOf(args.Lines, inside) {
			return errQuizKey
		}
		q.Key = slices.Sorted(slices.Values(args.Lines))
	case quizRange:
		if len(args.Lines) != 2 || !allOf(args.Lines, inside) || args.Lines[0] > args.Lines[1] {
			return errQuizKey
		}
		q.Key = slices.Clone(args.Lines)
	case quizValue:
		if len(args.Accept) == 0 {
			return errQuizKey
		}
		q.Accept = slices.Clone(args.Accept)
	case quizChoice:
		if len(args.Options) < minQuizOptions || len(args.Options) > maxQuizOptions {
			return errQuizOptions
		}
		for _, o := range args.Options {
			if o.Value == "" {
				return errQuizOptions
			}
			q.Options = append(q.Options, elicitOption{Value: o.Value, Label: cmpLabel(o)})
		}
		if len(args.Accept) == 0 || !allOf(args.Accept, func(v string) bool {
			return slices.ContainsFunc(q.Options, func(o elicitOption) bool { return o.Value == v })
		}) {
			return errQuizKey
		}
		q.Accept = slices.Clone(args.Accept)
	}
	return nil
}

func cmpLabel(o elicitOption) string {
	if o.Label != "" {
		return o.Label
	}
	return o.Value
}

func allOf[T any](list []T, ok func(T) bool) bool {
	return !slices.ContainsFunc(list, func(v T) bool { return !ok(v) })
}

func (q quizSpec) sends() bool {
	return q.Type == quizLine || q.Type == quizRange || q.Type == quizChoice
}

func (q quizSpec) last() int {
	return q.From + len(q.Lines) - 1
}

func (s *session) askQuiz(spec quizSpec) string {
	params, _ := json.Marshal(map[string]any{"mode": elicitQuiz, "message": spec.Question, "quiz": spec})
	perm := &pendingPermission{at: time.Now().UTC(), reply: make(chan acp.PermissionOutcome, 1), elicit: make(chan acp.ElicitationResponse, 1)}
	run := &quizRun{done: make(chan struct{})}
	s.mu.Lock()
	s.loadLocked()
	id := entryElicitation + "-" + strconv.Itoa(s.nextID+1)
	perm.request = id
	s.perms[id] = perm
	if s.quizzes == nil {
		s.quizzes = map[string]*quizRun{}
	}
	s.quizzes[id] = run
	s.asked = true
	s.recordLocked(s.actor(), kindElicitation, elicitationPayload{ID: id, Params: params})
	s.settleLocked()
	waiting := s.turnLocked()
	s.mu.Unlock()
	s.turned(waiting)
	s.changed(changeStatus, true)
	if s.requested != nil {
		s.requested(entryElicitation, id)
	}
	go s.finishQuiz(id, perm, run)
	return id
}

func (s *session) finishQuiz(id string, perm *pendingPermission, run *quizRun) {
	out := acp.ElicitationResponse{Action: acp.ElicitationCancel}
	select {
	case out = <-perm.elicit:
	case <-perm.reply:
	}
	s.mu.Lock()
	delete(s.perms, id)
	delete(s.quizzes, id)
	if run.hold != nil {
		run.hold.Stop()
	}
	s.recordLocked(actorUser, kindElicitationAnswer, elicitationAnswerPayload{Request: id, Response: out})
	if i, ok := s.index[key(entryElicitation, id)]; ok && out.Action == acp.ElicitationAccept {
		if en := s.entries[i]; en.Elicitation != nil && en.Elicitation.Quiz != nil {
			s.recordLocked(actorClubmate, kindQuizGrade, autoGrade(id, *en.Elicitation.Quiz, answerOf(out)))
		}
	}
	finish := s.settleLocked()
	s.mu.Unlock()
	close(run.done)
	s.changed(changeStatus, true)
	if s.resolved != nil {
		s.resolved(entryElicitation, id)
	}
	if finish && s.idled != nil {
		go s.idled()
	}
}

func (s *session) awaitQuiz(ctx context.Context, id string, d time.Duration) (quizResult, error) {
	s.mu.RLock()
	run := s.quizzes[id]
	s.mu.RUnlock()
	if run != nil {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-run.done:
		case <-t.C:
		case <-ctx.Done():
			return quizResult{}, ctx.Err()
		}
	}
	return s.quizResult(id)
}

func (s *session) quizResult(id string) (quizResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	en := s.quizEntryLocked(id)
	if en == nil {
		return quizResult{}, errNotAQuiz
	}
	out := quizResult{Request: id}
	switch {
	case en.Response == nil && (s.perms[id] != nil || s.quizzes[id] != nil):
		out.Status = quizAsked
	case en.Response == nil || en.Response.Action == acp.ElicitationCancel:
		out.Status = quizCancelled
	case en.Response.Action == acp.ElicitationDecline:
		out.Status = quizSkipped
	default:
		ans := answerOf(*en.Response)
		out.Status, out.Confidence = quizAnswered, ans.Confidence
		out.Answer, out.Lines = ans.text(), ans.lines()
		out.Verdict = verdictPending
		if en.Grade != nil {
			out.Verdict = en.Grade.Verdict
		}
		if out.Verdict != verdictPending {
			correct := out.Verdict == verdictCorrect
			out.Correct = &correct
		}
	}
	return out, nil
}

func (s *session) quizEntryLocked(id string) *entry {
	i, ok := s.index[key(entryElicitation, id)]
	if !ok || s.entries[i].Elicitation == nil || s.entries[i].Elicitation.Quiz == nil {
		return nil
	}
	return s.entries[i]
}

func (s *session) quizOf(id string) (quizSpec, quizState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	en := s.quizEntryLocked(id)
	if en == nil {
		return quizSpec{}, quizState{}, false
	}
	var st quizState
	if en.QuizState != nil {
		st = *en.QuizState
	}
	return *en.Elicitation.Quiz, st, true
}

func (s *session) quizAnsweredOf(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	en := s.quizEntryLocked(id)
	return en != nil && en.Response != nil
}

func (s *session) pendingQuiz() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, en := range s.entries {
		if en.Kind == entryElicitation && en.Elicitation != nil && en.Elicitation.Quiz != nil && s.perms[en.ID] != nil {
			return en.ID
		}
	}
	return ""
}

func (s *session) quizAct(act quizAct) error {
	s.mu.Lock()
	en := s.quizEntryLocked(act.Request)
	perm, run := s.perms[act.Request], s.quizzes[act.Request]
	if en == nil || perm == nil || run == nil {
		s.mu.Unlock()
		return errNotAQuiz
	}
	spec := *en.Elicitation.Quiz
	var st quizState
	if en.QuizState != nil {
		st = *en.QuizState
	}
	st.Request = act.Request
	var err error
	switch act.Action {
	case quizPick:
		err = s.pickQuizLocked(spec, &st, run, act)
	case quizRate:
		err = s.rateQuizLocked(spec, &st, run, act.Confidence)
	case quizUndo:
		if st.Holding {
			s.stopHoldLocked(run)
			st.Holding, st.Confidence = false, ""
			s.recordLocked(actorUser, kindQuizState, st)
		}
	case quizSend:
		err = s.sendQuizLocked(perm, run, st, act)
	case quizSkip:
		s.stopHoldLocked(run)
		delete(s.perms, act.Request)
		perm.elicit <- acp.ElicitationResponse{Action: acp.ElicitationDecline}
	default:
		err = errQuizAction
	}
	s.mu.Unlock()
	if err == nil {
		s.changed(changeUpdate, true)
	}
	return err
}

func (s *session) pickQuizLocked(spec quizSpec, st *quizState, run *quizRun, act quizAct) error {
	switch spec.Type {
	case quizChoice:
		if !slices.ContainsFunc(spec.Options, func(o elicitOption) bool { return o.Value == act.Option }) {
			return errQuizPick
		}
		st.From, st.To, st.Option = 0, 0, act.Option
	case quizLine, quizRange:
		to := act.To
		if to == 0 {
			to = act.From
		}
		from, to := min(act.From, to), max(act.From, to)
		if from < spec.From || to > spec.last() {
			return errQuizPick
		}
		st.From, st.To, st.Option = from, to, ""
	default:
		return errQuizPick
	}
	s.stopHoldLocked(run)
	st.Holding, st.Confidence = false, ""
	s.recordLocked(actorUser, kindQuizState, *st)
	return nil
}

func (s *session) rateQuizLocked(spec quizSpec, st *quizState, run *quizRun, confidence string) error {
	if !slices.Contains(confidences, confidence) {
		return errConfidence
	}
	st.Confidence = confidence
	if spec.sends() && (st.From > 0 || st.Option != "") {
		s.stopHoldLocked(run)
		st.Holding = true
		gen, id := run.gen, st.Request
		run.hold = time.AfterFunc(quizHold, func() { s.commitQuiz(id, gen) })
	}
	s.recordLocked(actorUser, kindQuizState, *st)
	return nil
}

func (s *session) stopHoldLocked(run *quizRun) {
	run.gen++
	if run.hold != nil {
		run.hold.Stop()
		run.hold = nil
	}
}

func (s *session) commitQuiz(id string, gen int) {
	s.mu.Lock()
	en := s.quizEntryLocked(id)
	perm, run := s.perms[id], s.quizzes[id]
	if en == nil || perm == nil || run == nil || run.gen != gen || en.QuizState == nil || !en.QuizState.Holding {
		s.mu.Unlock()
		return
	}
	st := *en.QuizState
	delete(s.perms, id)
	perm.elicit <- acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: quizAnswer{From: st.From, To: st.To, Option: st.Option, Confidence: st.Confidence}.content()}
	s.mu.Unlock()
}

func (s *session) sendQuizLocked(perm *pendingPermission, run *quizRun, st quizState, act quizAct) error {
	text := clipRunes(strings.TrimSpace(act.Text), quizTextRunes)
	if text == "" {
		return errQuizText
	}
	confidence := act.Confidence
	if confidence == "" && !st.Holding {
		confidence = st.Confidence
	}
	if confidence == "" {
		return errRateFirst
	}
	if !slices.Contains(confidences, confidence) {
		return errConfidence
	}
	s.stopHoldLocked(run)
	delete(s.perms, act.Request)
	perm.elicit <- acp.ElicitationResponse{Action: acp.ElicitationAccept, Content: quizAnswer{Text: text, Confidence: confidence}.content()}
	return nil
}

func (s *session) gradeQuiz(args quizGradeArgs) error {
	s.mu.Lock()
	en := s.quizEntryLocked(args.Request)
	if en == nil || en.Response == nil || en.Response.Action != acp.ElicitationAccept || en.Grade != nil && en.Grade.Verdict != verdictPending {
		s.mu.Unlock()
		return errQuizGraded
	}
	ans := answerOf(*en.Response)
	g := quizGrade{Request: args.Request, Verdict: args.Verdict, Why: clipRunes(strings.TrimSpace(args.Why), 400), Ref: strings.TrimSpace(args.Ref), By: gradeByAgent}
	if g.Verdict == verdictWrong && ans.Confidence == confSure {
		g.Flag = flagSureWrong
	}
	s.recordLocked(actorClubmate, kindQuizGrade, g)
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return nil
}

func (s *session) cancelQuizzesLocked() {
	for id := range s.quizzes {
		if perm := s.perms[id]; perm != nil {
			delete(s.perms, id)
			perm.reply <- acp.PermissionOutcome{Outcome: acp.OutcomeCancelled}
		}
	}
}

func (s *session) applyQuizState(e envelope) {
	var st quizState
	if json.Unmarshal(e.Payload, &st) != nil {
		return
	}
	if i, ok := s.index[key(entryElicitation, st.Request)]; ok {
		s.entries[i].QuizState = &st
		s.touch(s.entries[i])
	}
}

func (s *session) applyQuizGrade(e envelope) {
	var g quizGrade
	if json.Unmarshal(e.Payload, &g) != nil {
		return
	}
	if i, ok := s.index[key(entryElicitation, g.Request)]; ok {
		s.entries[i].Grade = &g
		s.touch(s.entries[i])
	}
}

func (q quizAnswer) content() map[string]any {
	raw, _ := json.Marshal(q)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func answerOf(resp acp.ElicitationResponse) quizAnswer {
	raw, _ := json.Marshal(resp.Content)
	var q quizAnswer
	_ = json.Unmarshal(raw, &q)
	return q
}

func (q quizAnswer) text() string {
	switch {
	case q.Text != "":
		return q.Text
	case q.Option != "":
		return q.Option
	case q.From > 0 && q.From == q.To:
		return strconv.Itoa(q.From)
	case q.From > 0:
		return strconv.Itoa(q.From) + "-" + strconv.Itoa(q.To)
	}
	return ""
}

func (q quizAnswer) lines() []int {
	if q.From == 0 || q.Text != "" {
		return nil
	}
	return []int{q.From, q.To}
}

func autoGrade(id string, spec quizSpec, ans quizAnswer) quizGrade {
	g := quizGrade{Request: id, Verdict: verdictPending, By: gradeByPlugin}
	switch {
	case ans.Text != "" && spec.Type == quizValue:
		g.Verdict = verdictWrong
		if slices.ContainsFunc(spec.Accept, func(v string) bool { return sameValue(v, ans.Text) }) {
			g.Verdict = verdictCorrect
		}
	case ans.Text != "":
		return g
	case spec.Type == quizChoice:
		g.Verdict = verdictWrong
		if slices.Contains(spec.Accept, ans.Option) {
			g.Verdict = verdictCorrect
		}
	case spec.Type == quizLine:
		g.Verdict = verdictWrong
		hits := slices.ContainsFunc(spec.Key, func(k int) bool { return k >= ans.From && k <= ans.To })
		switch {
		case hits && ans.From == ans.To:
			g.Verdict = verdictCorrect
		case hits:
			g.Verdict = verdictClose
		}
	case spec.Type == quizRange && len(spec.Key) == 2:
		g.Verdict = verdictWrong
		switch {
		case ans.From == spec.Key[0] && ans.To == spec.Key[1]:
			g.Verdict = verdictCorrect
		case ans.From <= spec.Key[1] && ans.To >= spec.Key[0]:
			g.Verdict = verdictClose
		}
	}
	g.Why, g.Ref = spec.Why, spec.Ref
	if g.Verdict == verdictWrong && ans.Confidence == confSure {
		g.Flag = flagSureWrong
	}
	return g
}

func sameValue(want, got string) bool {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	w, g := norm(want), norm(got)
	if w == g {
		return true
	}
	wn, err1 := strconv.ParseFloat(w, 64)
	gn, err2 := strconv.ParseFloat(g, 64)
	return err1 == nil && err2 == nil && wn == gn
}
