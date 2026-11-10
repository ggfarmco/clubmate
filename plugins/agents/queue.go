package main

import (
	"slices"
	"strconv"
	"strings"
)

type queuedPrompt struct {
	ID          string       `json:"id"`
	Text        string       `json:"text,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
	From        string       `json:"from,omitempty"`
	Voice       bool         `json:"voice,omitempty"`
	Review      *reviewSent  `json:"review,omitempty"`
	Answered    []string     `json:"answered,omitempty"`
	Land        bool         `json:"-"`
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
	s.queue = slices.DeleteFunc(s.queue, func(q queuedPrompt) bool { return q.ID == id })
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func storedQueue(queue []queuedPrompt) []queuedPrompt {
	return slices.DeleteFunc(slices.Clone(queue), func(q queuedPrompt) bool {
		return q.Land || slices.ContainsFunc(q.Attachments, func(a attachment) bool { return a.Data != "" })
	})
}

func (s *session) restoreQueue(queue []queuedPrompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = queue
	for _, q := range queue {
		if n, err := strconv.Atoi(strings.TrimPrefix(q.ID, "q")); err == nil {
			s.queueSeq = max(s.queueSeq, n)
		}
	}
}

func (s *session) queued() []queuedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.queue)
}

func (s *session) queuedLocked(args promptArgs) queuedPrompt {
	s.queueSeq++
	return queuedPrompt{ID: "q" + strconv.Itoa(s.queueSeq), Text: args.Text, Attachments: args.Attachments, From: args.From, Voice: args.Voice, Review: args.Review, Answered: args.Answered, Land: args.Land}
}

func (s *session) dequeueLocked() (promptArgs, bool) {
	if len(s.queue) == 0 {
		return promptArgs{}, false
	}
	from, voice := s.queue[0].From, s.queue[0].Voice
	n := slices.IndexFunc(s.queue, func(q queuedPrompt) bool { return q.From != from || q.Voice != voice || q.Review != nil })
	switch {
	case s.queue[0].Review != nil:
		n = 1
	case n < 0:
		n = len(s.queue)
	}
	parts := s.queue[:n]
	s.queue = slices.Clone(s.queue[n:])
	args := promptArgs{From: from, Voice: voice, Review: parts[0].Review}
	var texts []string
	for _, q := range parts {
		if strings.TrimSpace(q.Text) != "" {
			texts = append(texts, q.Text)
		}
		args.Attachments = append(args.Attachments, q.Attachments...)
		args.Answered = append(args.Answered, q.Answered...)
	}
	args.Text = strings.Join(texts, "\n\n")
	return args, true
}
