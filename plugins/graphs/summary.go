package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/claudecli"
)

const (
	summaryTimeout = 2 * time.Minute
	summaryInput   = 32 << 10
	replyKeep      = 600

	stepSummaryPrompt = `You read the output of a command that failed as a step of an automated pipeline. Say what broke in at most three short lines: the failing package, test, file or check by name, and the reason in a few words (the assertion, the error message, the exit cause). No preamble, no advice, no markdown headings; plain text, no more than 300 characters.`

	agentSummaryPrompt = `You read the transcript of one turn of a coding agent that ran as a step of an automated pipeline. Say what the agent did and how it ended in at most three short lines: what it changed (files, commits) or found, and whether it left anything unfinished. Write about the agent's work, never about the transcript itself; when the turn shows no work, say so in a few words. No preamble, no markdown headings; plain text, no more than 300 characters.`
)

var failLine = regexp.MustCompile(`(?m)^.*(--- FAIL|^FAIL|panic:|Error:|error:).*$`)

func askHaiku(ctx context.Context, system, input string) (string, error) {
	return claudecli.Ask(ctx, claudecli.Haiku, system, input)
}

func (a *app) queueSummary(runID string, move int, system string, input func(ctx context.Context) (string, error)) {
	key := runID + "/" + strconv.Itoa(move)
	a.mu.Lock()
	if a.summarizing[key] {
		a.mu.Unlock()
		return
	}
	a.summarizing[key] = true
	a.mu.Unlock()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
		defer cancel()
		text, err := input(ctx)
		if err == nil && strings.TrimSpace(text) != "" {
			text, err = a.summarize(ctx, system, text)
		}
		if err != nil && !errors.Is(err, claudecli.ErrMissing) {
			logError(fmt.Errorf("summary %s: %w", key, err))
		}
		a.mu.Lock()
		delete(a.summarizing, key)
		r := a.runs[runID]
		if r == nil || move >= len(r.Moves) {
			a.mu.Unlock()
			return
		}
		if err == nil && text != "" {
			r.Moves[move].Summary = text
			logError(a.store.Save(ctx, r))
		}
		a.touch(r.Workspace)
		a.mu.Unlock()
		a.flush(context.Background())
	})
}

func stepInput(command string, exit *int, output, tail string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		text := tail
		if raw, err := os.ReadFile(output); err == nil {
			text = string(raw)
		}
		if strings.TrimSpace(text) == "" {
			return "", nil
		}
		head := "$ " + command
		if exit != nil {
			head += "\nexit " + strconv.Itoa(*exit)
		}
		if len(text) <= summaryInput {
			return head + "\n\n" + text, nil
		}
		fails := strings.Join(failLine.FindAllString(text[:len(text)-summaryInput], 40), "\n")
		return head + "\n\nFailure lines from the start of the output:\n" + fails + "\n\nLast part of the output:\n" + text[len(text)-summaryInput:], nil
	}
}

func (a *app) turnInput(workspace, session string, turn int) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		args, err := json.Marshal(map[string]any{"session": session, "fromTurn": turn, "compact": true})
		if err != nil {
			return "", err
		}
		raw, err := a.runCommand(ctx, automation.CommandRequest{ID: "agents.transcript", WorkspaceID: workspace, Args: args})
		if err != nil {
			return "", fmt.Errorf("transcript %s: %w", session, err)
		}
		var tr struct {
			Entries json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(raw, &tr); err != nil {
			return "", fmt.Errorf("transcript %s: %w", session, err)
		}
		text := string(tr.Entries)
		return text[max(0, len(text)-summaryInput):], nil
	}
}

func clip(text string, n int) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "…"
	}
	return text
}
