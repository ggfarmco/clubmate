package automation

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWordsSayWhatARunDid(t *testing.T) {
	titles := map[string]string{"agt_1": "Review loop · Reviewer", "agt_2": "Nightly tidy", "backlog.list": "List backlog tasks"}
	prompt := json.RawMessage(`{"session":"agt_1","text":"review"}`)
	tests := []struct {
		name    string
		command string
		args    json.RawMessage
		run     Run
		want    string
	}{
		{"new session", agentNew, nil, Run{Status: StatusOK, Result: `{"session":"agt_2","worktree":""}`}, "started Nightly tidy"},
		{"prompt returns null", PromptCommand, prompt, Run{Status: StatusOK, Result: "null"}, "prompted Review loop · Reviewer"},
		{"prompt to an expanded session starts a turn", PromptCommand, json.RawMessage(`{"session":"{{payload.session}}"}`), Run{Status: StatusOK, Target: "agt_2", Result: `{"turn":3}`}, "prompted Nightly tidy · its turn 3 started"},
		{"emit", agentEmit, nil, Run{Status: StatusOK, Result: `{"topic":"event.tidy.done","seq":4}`}, "emitted “tidy.done”"},
		{"text field", "github.ci", nil, Run{Status: StatusOK, Result: `{"summary":"main is green · 14 checks passed\nmore"}`}, "main is green · 14 checks passed"},
		{"null", "backlog.list", nil, Run{Status: StatusOK, Result: "null"}, "List backlog tasks · done"},
		{"other JSON", "backlog.list", nil, Run{Status: StatusOK, Result: `{"tasks":[]}`}, "ok"},
		{"cut JSON", "backlog.list", nil, Run{Status: StatusOK, Result: `{"tasks":[{"id"`}, "ok"},
		{"error", PromptCommand, prompt, Run{Status: StatusError, Reason: "jsonrpc2: code -32013 message: plugin agents command.run: invalid_params: session agt_1 is closed"}, "session Review loop · Reviewer is closed"},
		{"deleted session", PromptCommand, prompt, Run{Status: StatusError, Reason: "jsonrpc2: code -32013 message: plugin agents command.run: unknown agent session: agt_9"}, "its session was deleted"},
		{"missed", "backlog.list", nil, Run{Status: StatusSkipped, Reason: ReasonMissed}, "not run — Clubmate was closed or the Mac asleep; the policy skips it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Words(tt.command, tt.args, tt.run, titles))
		})
	}
}
