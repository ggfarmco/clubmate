package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	handoffPieceLimit = 2000
	handoffHead       = "[Clubmate handoff] You continue agent session %q (%s) in %s; it is closed now. Read its full transcript with agents.transcript session=%s when this summary is not enough."
	handoffDefault    = "Pick up where it left off."
)

var (
	errHandoffRunning = errors.New("stop the session's running turn before handing it off")
	errHandedOff      = errors.New("the session was already handed off")
)

type handoffArgs struct {
	Session string `json:"session"`
	Note    string `json:"note"`
}

type handoffResult struct {
	Session string `json:"session"`
}

func (a *app) handoff(ctx context.Context, s *session, args handoffArgs, from string) (*session, error) {
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	old := s.snapshot()
	switch {
	case turnActive(old):
		return nil, errHandoffRunning
	case old.HandoffTo != "":
		return nil, errHandedOff
	}
	if err := a.canSpawnLocked(old.ParentSession); err != nil {
		return nil, err
	}
	first := promptArgs{Text: handoffBrief(s.view(0), args.Note), From: from}
	a.closeSession(ctx, s, false)
	next, err := a.launch(record{
		Workspace:     old.Workspace,
		Worktree:      old.Worktree,
		Agent:         old.Agent,
		ParentSession: old.ParentSession,
		Advisor:       old.Advisor,
		Notify:        old.Notify,
		Fork:          old.Fork,
		Title:         old.Title,
		TitleSet:      old.TitleSet,
		HandoffFrom:   old.ID,
	}, first, nil)
	s.mu.Lock()
	s.rec.HandoffTo = next.snapshot().ID
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return next, err
}

func handoffBrief(v transcriptView, note string) string {
	var task, last, reply *entry
	for _, en := range v.Entries {
		switch en.Kind {
		case entryUser:
			if task == nil {
				task = en
			}
			last = en
		case entryAgent:
			reply = en
		}
	}
	if task == nil {
		return strings.TrimSpace(note)
	}
	rec := v.Session
	parts := []string{fmt.Sprintf(handoffHead, cmp.Or(rec.Title, "New session"), rec.ID, rec.Worktree, rec.ID), "Task (turn " + strconv.Itoa(task.Turn) + "):\n" + clip(task.Text)}
	if last != task {
		parts = append(parts, "Last prompt (turn "+strconv.Itoa(last.Turn)+"):\n"+clip(last.Text))
	}
	if reply != nil {
		parts = append(parts, "Last reply (turn "+strconv.Itoa(reply.Turn)+"):\n"+clip(reply.Text))
	}
	if n := len(v.Turns); n > 0 {
		t := v.Turns[n-1]
		state := cmp.Or(t.StopReason, "no stop reason")
		if t.CheckpointSha != "" {
			state += ", checkpoint " + t.CheckpointSha + " taken before it"
		}
		parts = append(parts, "Last turn "+strconv.Itoa(t.Turn)+": "+state+".")
	}
	return strings.Join(append(parts, cmp.Or(strings.TrimSpace(note), handoffDefault)), "\n\n")
}

func clip(text string) string {
	text = strings.TrimSpace(text)
	r := []rune(text)
	if len(r) <= handoffPieceLimit {
		return text
	}
	return string(r[:handoffPieceLimit]) + "…"
}
