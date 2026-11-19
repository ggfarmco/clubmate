package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	handoffID      = "handoff"
	handoffOff     = "off"
	handoffLanded  = "landed"
	handoffContext = "context"
	handoffEither  = "either"
	handoffLimit   = 250_000

	handoffHelp       = "Hands the session off to a fresh one by itself when a turn ends: after it landed into the default branch with no child running, or once its context is over 250k"
	handoffNotesHead  = "[Clubmate handoff notes]"
	handoffNotesText  = handoffNotesHead + " When this turn ends Clubmate hands this session off to a fresh one (%s): the new session gets the task, the last prompt and reply before this one, and your reply to this prompt; nothing else of this conversation. Don't start new work. Reply with what it needs that isn't in files it would find by itself: where the state lives (exact paths of the spec, plan, rules or notes, backlog task ids), the children still open and what each waits for, the user's rulings and decisions not written down, and what comes next. Keep it short."
	handoffAutoNote   = "Clubmate handed this session off by itself (%s). Notes the earlier session wrote for you:\n%s\n\nRead the state named above before acting. Start no new work until a prompt, a child or a verdict asks for it."
	handoffNoNotes    = "(it wrote none)"
	handoffTitleTo    = "Handed off to a new session"
	handoffTitleFrom  = "Continues a session handed off by itself"
	handoffWhyContext = "context %dk"
	handoffWhyLanded  = "landed into %s"
)

var handoffChoices = []advisorChoice{
	{value: handoffOff, label: "Off"},
	{value: handoffLanded, label: "After landing"},
	{value: handoffContext, label: "Over 250k"},
	{value: handoffEither, label: "Either"},
}

func (a *app) setHandoff(s *session, value string) error {
	if !slices.ContainsFunc(handoffChoices, func(c advisorChoice) bool { return c.value == value }) {
		return fmt.Errorf("%w: %s=%s", errUnknownValue, handoffID, value)
	}
	if value == handoffOff {
		value = ""
	}
	s.mu.Lock()
	s.rec.Handoff = value
	if value == "" {
		s.rec.HandoffLanded, s.rec.HandoffNotes = "", ""
	}
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return nil
}

func handoffOn(setting, trigger string) bool {
	return setting == trigger || setting == handoffEither
}

func (a *app) landDone(ctx context.Context, cmd pluginsdk.CommandRun, args landGateArgs) (any, error) {
	res, err := a.landDoneCommand(ctx, cmd, args)
	if err == nil {
		a.noteLanded(ctx, args)
	}
	return res, err
}

func (a *app) noteLanded(ctx context.Context, args landGateArgs) {
	s := a.session(args.Session)
	if s == nil || !handoffOn(s.snapshot().Handoff, handoffLanded) {
		return
	}
	if into, err := gitrepo.DefaultBranch(ctx, repoTop(args.Target)); err != nil || into != args.Into {
		return
	}
	s.mu.Lock()
	s.rec.HandoffLanded = args.Into
	s.mu.Unlock()
	a.saveNow(ctx, s)
}

func handoffWhy(rec record, used int) (string, bool) {
	switch {
	case handoffOn(rec.Handoff, handoffContext) && used > handoffLimit:
		return fmt.Sprintf(handoffWhyContext, used/1000), false
	case handoffOn(rec.Handoff, handoffLanded) && rec.HandoffLanded != "":
		return fmt.Sprintf(handoffWhyLanded, rec.HandoffLanded), true
	}
	return "", false
}

func (s *session) handoffState() (record, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	used := 0
	if s.usage != nil {
		used = s.usage.Used
	}
	return s.rec, used, len(s.queue)
}

func (a *app) handoffBlocked(rec record, queued int, landing bool) bool {
	switch {
	case rec.closed(), turnActive(rec), rec.HandoffTo != "", rec.Night != "", rec.isAside(), rec.GateFor != nil, rec.restricted(),
		rec.Held != nil, len(rec.Calls) > 0, rec.LimitWait != nil, queued > 0, len(rec.LandReviews) > 0, rec.OpenCount > 0,
		rec.ComposerDraft != nil && strings.TrimSpace(rec.ComposerDraft.Text) != "", len(a.asides(rec.ID)) > 0:
		return true
	}
	if _, err := os.Stat(rec.Worktree); err != nil {
		return true
	}
	return landing && slices.ContainsFunc(a.children(rec.ID), func(k *session) bool { return turnActive(k.snapshot()) })
}

func (a *app) handoffAfter(s *session, cancelled bool) {
	if !cancelled && s.snapshot().Handoff != "" {
		go a.autoHandoff(s)
	}
}

func (a *app) autoHandoff(s *session) {
	rec, used, queued := s.handoffState()
	if rec.Handoff == "" || rec.closed() {
		return
	}
	if rec.HandoffNotes != "" {
		v := s.view(0)
		turn, notes, ok := handoffNotes(v)
		if ok && a.handoffBlocked(rec, queued, false) {
			return
		}
		s.mu.Lock()
		s.rec.HandoffNotes = ""
		s.mu.Unlock()
		s.changed(changeUpdate, true)
		if ok {
			a.runAutoHandoff(s, rec.HandoffNotes, withoutTurn(v, turn), notes)
			return
		}
	}
	why, landing := handoffWhy(rec, used)
	if why == "" || a.handoffBlocked(rec, queued, landing) {
		return
	}
	s.mu.Lock()
	s.rec.HandoffNotes = why
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	if err := a.submit(s, &promptArgs{Text: fmt.Sprintf(handoffNotesText, why)}); err != nil {
		logError("handoff notes", err)
	}
}

func (a *app) runAutoHandoff(s *session, why string, v transcriptView, notes string) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	note := fmt.Sprintf(handoffAutoNote, why, cmp.Or(strings.TrimSpace(notes), handoffNoNotes))
	next, err := a.handoff(ctx, s, handoffArgs{Session: s.snapshot().ID, brief: handoffBrief(v, note, a.chainTask(s))}, "")
	logError("auto handoff", err)
	if next == nil {
		return
	}
	s.notice(handoffTitleTo, why)
	next.notice(handoffTitleFrom, why)
}

func handoffNotes(v transcriptView) (int, string, bool) {
	turn := -1
	for i := len(v.Entries) - 1; i >= 0 && turn < 0; i-- {
		if en := v.Entries[i]; en.Kind == entryUser && strings.HasPrefix(strings.TrimSpace(en.Text), handoffNotesHead) {
			turn = en.Turn
		}
	}
	if turn < 0 {
		return 0, "", false
	}
	var notes string
	for _, en := range v.Entries {
		if en.Kind == entryAgent && en.Turn == turn {
			notes = en.Text
		}
	}
	return turn, notes, true
}

func withoutTurn(v transcriptView, turn int) transcriptView {
	v.Entries = slices.DeleteFunc(slices.Clone(v.Entries), func(en *entry) bool { return en.Turn == turn })
	v.Turns = slices.DeleteFunc(slices.Clone(v.Turns), func(t turnInfo) bool { return t.Turn == turn })
	return v
}
