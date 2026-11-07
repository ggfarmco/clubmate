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
	if s.snapshot().Night != "" {
		return nil, errNightHandoff
	}
	finish, afk := a.sessionDefaults(ctx, s.snapshot().ParentSession)
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	a.landMu.Lock()
	defer a.landMu.Unlock()
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
	if old.Task != nil {
		finish = old.Finish
	}
	first := promptArgs{Text: handoffBrief(s.view(0), args.Note), From: from}
	a.closeSession(ctx, s, false)
	a.connectMu.Lock()
	defer a.connectMu.Unlock()
	next, err := a.launch(record{
		Workspace: old.Workspace,
		Worktree:  old.Worktree,
		Agent:     old.Agent,
		lineage: lineage{
			ParentSession: old.ParentSession,
			Advisor:       old.Advisor,
			Fork:          old.Fork,
			HandoffFrom:   old.ID,
		},
		Run:      old.Run,
		CronJob:  old.CronJob,
		Trigger:  old.Trigger,
		Notify:   old.Notify,
		Title:    old.Title,
		TitleSet: old.TitleSet,
		Finish:   finish,
		AFK:      afk || old.AFK,
		RepoPath: old.RepoPath,
		Task:     old.Task,

		AfkConnected: old.AfkConnected,

		ConnectRequests: s.snapshot().ConnectRequests,
	}, first, nil)
	s.mu.Lock()
	s.rec.HandoffTo = next.snapshot().ID
	s.rec.ConnectRequests = nil
	s.rec.Task = nil
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	if old.Task != nil {
		a.saveNow(ctx, next)
		a.saveNow(ctx, s)
		a.moveChildren(ctx, old.ID, next.snapshot().ID)
	}
	return next, err
}

func (a *app) moveChildren(ctx context.Context, from, to string) {
	for _, kid := range a.children(from) {
		kid.mu.Lock()
		kid.rec.ParentSession = to
		kid.mu.Unlock()
		kid.changed(changeUpdate, true)
		a.saveNow(ctx, kid)
	}
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
	return clipRunes(strings.TrimSpace(text), handoffPieceLimit)
}

func clipRunes(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit]) + "…"
}
