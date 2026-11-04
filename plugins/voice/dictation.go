package main

import (
	"bufio"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/plugins/internal/claudecli"
)

const (
	modeAssistant = "assistant"
	modeDictation = "dictation"

	cleanTimeout = 3 * time.Second
	maxCleanGrow = 2
)

var (
	errCleanRejected = errors.New("cleaned text rejected")

	modes = []string{modeAssistant, modeDictation}

	//go:embed dictation.md
	dictationPrompt string
)

type cleaner interface {
	Clean(ctx context.Context, text string) (string, error)
	Close()
}

type claudeCleaner struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   io.Reader
	err   error
	once  sync.Once
}

type streamUserMessage struct {
	Type    string        `json:"type"`
	Message streamMessage `json:"message"`
}

type streamMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

type cleanResult struct {
	text string
	err  error
}

func (v *voice) dictate(u *utterance, heard string) {
	ctx, cancel := context.WithTimeout(v.ctx, cleanTimeout)
	defer cancel()
	began := v.now()
	text, err := u.clean.Clean(ctx, heard)
	if err == nil && !plausible(heard, text) {
		err = errCleanRejected
	}
	if err != nil {
		text = heard
	}
	fmt.Fprintf(os.Stderr, "voice: dictation %s clean=%dms fallback=%v\n", u.state.Utterance, v.now().Sub(began).Milliseconds(), err)
	v.update(u, func(s *State) {
		s.Text = text
		s.Phase = phaseDone
	})
}

func plausible(heard, text string) bool {
	n := len([]rune(text))
	return strings.IndexFunc(text, isWordRune) >= 0 && n <= maxCleanGrow*len([]rune(heard))
}

func startCleaner(d dictionary) cleaner {
	claude, err := claudecli.Find()
	if err != nil {
		return &claudeCleaner{err: err}
	}
	cmd := claudecli.Command(context.Background(), claude, claudecli.Haiku, cleanPrompt(d), "--input-format", "stream-json", "--output-format", "stream-json", "--verbose")
	return newClaudeCleaner(cmd)
}

func newClaudeCleaner(cmd *exec.Cmd) *claudeCleaner {
	c := &claudeCleaner{cmd: cmd}
	if c.stdin, c.err = cmd.StdinPipe(); c.err != nil {
		return c
	}
	if c.out, c.err = cmd.StdoutPipe(); c.err != nil {
		return c
	}
	c.err = cmd.Start()
	return c
}

func (c *claudeCleaner) Clean(ctx context.Context, text string) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	line, err := json.Marshal(streamUserMessage{Type: "user", Message: streamMessage{Role: "user", Content: "<dictation>" + text + "</dictation>"}})
	if err != nil {
		return "", err
	}
	if _, err := c.stdin.Write(append(line, '\n')); err != nil {
		return "", err
	}
	_ = c.stdin.Close()
	done := make(chan cleanResult, 1)
	go func() { done <- c.read() }()
	select {
	case r := <-done:
		return r.text, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *claudeCleaner) read() cleanResult {
	scanner := bufio.NewScanner(c.out)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		var ev streamEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil || ev.Type != "result" {
			continue
		}
		if ev.IsError {
			return cleanResult{err: fmt.Errorf("claude: %s %s", ev.Subtype, ev.Result)}
		}
		return cleanResult{text: strings.TrimSpace(ev.Result)}
	}
	return cleanResult{err: cmp.Or(scanner.Err(), io.ErrUnexpectedEOF)}
}

func (c *claudeCleaner) Close() {
	c.once.Do(func() {
		if c.cmd == nil || c.cmd.Process == nil {
			return
		}
		_ = c.cmd.Process.Kill()
		go func() { _ = c.cmd.Wait() }()
	})
}

func cleanPrompt(d dictionary) string {
	var terms []string
	for _, e := range d.Entries {
		if !slices.Contains(terms, e.To) {
			terms = append(terms, e.To)
		}
	}
	if len(terms) == 0 {
		return dictationPrompt
	}
	return dictationPrompt + "\nProject terms: " + strings.Join(terms, ", ") + "\n"
}
