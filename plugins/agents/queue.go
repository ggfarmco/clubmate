package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	queueSpillBytes = 64 << 10
	queueSpillName  = "queue.json"
)

type queuedPrompt struct {
	ID          string       `json:"id"`
	Text        string       `json:"text,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
	From        string       `json:"from,omitempty"`
	Voice       bool         `json:"voice,omitempty"`
	Review      *reviewSent  `json:"review,omitempty"`
	Answered    []string     `json:"answered,omitempty"`
	Sending     bool         `json:"sending,omitempty"`
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
	out := make([]queuedPrompt, 0, len(queue))
	for _, q := range queue {
		if q.Land || slices.ContainsFunc(q.Attachments, func(a attachment) bool { return a.Data != "" && a.Path == "" }) {
			continue
		}
		q.Attachments = slices.Clone(q.Attachments)
		for i := range q.Attachments {
			if q.Attachments[i].Path != "" {
				q.Attachments[i].Data = ""
			}
		}
		out = append(out, q)
	}
	return out
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
	s.requeueSendingLocked()
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
	sending := s.queuedLocked(args)
	sending.ID, sending.Sending = rand.Text(), true
	sending.Land = !slices.ContainsFunc(parts, func(q queuedPrompt) bool { return !q.Land })
	s.sending = &sending
	return args, true
}

func (s *session) requeueSendingLocked() {
	q := s.sending
	s.sending = nil
	if q == nil || strings.TrimSpace(q.Text) == "" && len(q.Attachments) == 0 {
		return
	}
	q.Sending = false
	s.queue = slices.Insert(s.queue, 0, *q)
}

func (s *session) storedQueueLocked() []queuedPrompt {
	if s.sending == nil {
		return storedQueue(s.queue)
	}
	return storedQueue(append([]queuedPrompt{*s.sending}, s.queue...))
}

func restoredQueue(stateDir, session string, queue []queuedPrompt) ([]queuedPrompt, bool) {
	if len(queue) == 0 || !queue[0].Sending {
		return queue, false
	}
	if sentInLog(logPath(stateDir, session), queue[0].ID) {
		return queue[1:], false
	}
	queue[0].Sending = false
	return queue, true
}

func sentInLog(path, token string) bool {
	sent := false
	err := newEventLog(path).replay(func(e envelope) {
		if sent || e.Kind != kindPrompt || !bytes.Contains(e.Payload, []byte(token)) {
			return
		}
		var p promptPayload
		sent = json.Unmarshal(e.Payload, &p) == nil && p.Sent == token
	})
	return sent && err == nil
}

func queueSpillPath(stateDir, session string) string {
	return filepath.Join(stateDir, uploadsDir, session, queueSpillName)
}

func spillQueue(stateDir, session string, queue []queuedPrompt) (bool, error) {
	path := queueSpillPath(stateDir, session)
	raw, err := json.Marshal(queue)
	if err != nil || len(raw) <= queueSpillBytes {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}

func dropSpilledQueue(stateDir, session string) error {
	if err := os.Remove(queueSpillPath(stateDir, session)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readSpilledQueue(stateDir, session string) ([]queuedPrompt, error) {
	raw, err := os.ReadFile(queueSpillPath(stateDir, session))
	if err != nil {
		return nil, err
	}
	var queue []queuedPrompt
	return queue, json.Unmarshal(raw, &queue)
}
