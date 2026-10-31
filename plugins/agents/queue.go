package main

import (
	"slices"
	"strconv"
	"strings"
)

type queuedPrompt struct {
	ID          string
	Text        string
	Attachments []attachment
	From        string
}

func (s *session) enqueueLocked(args promptArgs) {
	s.queue = append(s.queue, s.queuedLocked(args))
}

func (s *session) requeueLocked(args promptArgs) {
	if strings.TrimSpace(args.Text) == "" && len(args.Attachments) == 0 {
		return
	}
	s.queue = slices.Insert(s.queue, 0, s.queuedLocked(args))
}

func (s *session) unqueue(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = slices.DeleteFunc(s.queue, func(q queuedPrompt) bool { return q.ID == id })
}

func (s *session) queued() []queuedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.queue)
}

func (s *session) queuedLocked(args promptArgs) queuedPrompt {
	s.queueSeq++
	return queuedPrompt{ID: "q" + strconv.Itoa(s.queueSeq), Text: args.Text, Attachments: args.Attachments, From: args.From}
}

func (s *session) dequeueLocked() (promptArgs, bool) {
	if len(s.queue) == 0 {
		return promptArgs{}, false
	}
	from := s.queue[0].From
	n := slices.IndexFunc(s.queue, func(q queuedPrompt) bool { return q.From != from })
	if n < 0 {
		n = len(s.queue)
	}
	parts := s.queue[:n]
	s.queue = slices.Clone(s.queue[n:])
	args := promptArgs{From: from}
	var texts []string
	for _, q := range parts {
		if strings.TrimSpace(q.Text) != "" {
			texts = append(texts, q.Text)
		}
		args.Attachments = append(args.Attachments, q.Attachments...)
	}
	args.Text = strings.Join(texts, "\n\n")
	return args, true
}
