package main_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const (
	fixtureDir      = "../../client/CompanionKit/Tests/CompanionKitTests/Fixtures"
	fixtureWorktree = "/Users/me/src/shop"
	fixtureProject  = "prj_00000000000000f1"
	fixtureTime     = "2026-10-30T09:41:00Z"
	fixtureSha      = "0123456789abcdef0123456789abcdef01234567"
	driftFormat     = "%s drifted from the agents JSON; run: go test ./plugins/agents -run Companion -update"
	refactorPrompt  = "Refactor the cart total into its own function"
	explainPrompt   = "Explain the checkout flow"
	agentMarkdown   = "## Plan\n\nI'll move the total into `cartTotal` and keep **behaviour** the same:\n\n1. Extract the loop\n2. Add a test\n\n```go\nfunc cartTotal(items []Item) int {\n\treturn sum(items)\n}\n```\n"
	explainMarkdown = "Checkout runs in three steps: *validate*, *reserve*, *charge*.\n\nShould I add retries to the charge step?"
)

var (
	update     = flag.Bool("update", false, "rewrite the companion fixtures")
	timeRe     = regexp.MustCompile(`"\d{4}-\d{2}-\d{2}T[0-9:.]+(Z|[+-]\d{2}:\d{2})"`)
	shaRe      = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	fakeIDRe   = regexp.MustCompile(`fake-\d+-(\d+)`)
	sessionIDs = regexp.MustCompile(`agt_[0-9a-f]{16}`)
)

func TestCompanionFixtures(t *testing.T) {
	repo := newRepo(t)
	write(t, filepath.Join(repo, "cart.go"), "func total(items []Item) int {\n\treturn sum(items)\n}\n")
	f := start(t, companionScript(repo))
	project := f.project(t, repo)

	waiting := f.newSession(t, project)
	f.prompt(t, project, waiting, refactorPrompt)
	f.waitStatus(t, project, waiting, "waiting")

	asking := f.newSession(t, project)
	f.prompt(t, project, asking, explainPrompt)
	f.waitStatus(t, project, asking, "done")

	closed := f.newSession(t, project)
	testutil.Eventually(t, func() bool { return f.session(t, project, closed).Mode != "" }, wait)
	require.NoError(t, f.run(t, project, "agents.close", map[string]any{"session": closed}, nil))
	f.waitStatus(t, project, closed, "closed")

	var sessions, transcript json.RawMessage
	require.NoError(t, f.run(t, project, "agents.sessions", map[string]any{}, &sessions))
	require.NoError(t, f.run(t, project, "agents.transcript", map[string]any{"session": waiting}, &transcript))
	require.NoError(t, f.run(t, project, "agents.cancel", map[string]any{"session": waiting}, nil))

	scrub := strings.NewReplacer(repo, fixtureWorktree, project, fixtureProject)
	checkFixture(t, "agents-sessions.json", normalize(t, scrub, sessions))
	checkFixture(t, "agents-transcript.json", normalize(t, scrub, transcript))
}

func companionScript(repo string) map[string]any {
	path := filepath.Join(repo, "cart.go")
	step := func(match string, update map[string]any) map[string]any {
		return map[string]any{"match": match, "update": update}
	}
	chunk := func(kind, text string) map[string]any {
		return map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": text}}
	}
	permission := permissionScript()
	permission["toolCall"] = map[string]any{"toolCallId": "run1", "title": "go test ./...", "kind": "execute", "rawInput": map[string]any{"command": "go test ./..."}}
	permission["options"] = []map[string]string{
		{"optionId": "allow_always", "name": "Always Allow", "kind": "allow_always"},
		{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
		{"optionId": "deny", "name": "Reject", "kind": "reject_once"},
	}
	return map[string]any{
		"modes": map[string]any{
			"currentModeId": "default",
			"availableModes": []map[string]string{
				{"id": "default", "name": "Default"},
				{"id": "plan", "name": "Plan Mode"},
			},
		},
		"prompt": []map[string]any{
			step(refactorPrompt, chunk("agent_thought_chunk", "The total is computed inline in cart.go.")),
			step(refactorPrompt, chunk("agent_message_chunk", agentMarkdown)),
			step(refactorPrompt, map[string]any{"sessionUpdate": "plan", "entries": []map[string]string{
				{"content": "Extract cartTotal", "status": "completed", "priority": "high"},
				{"content": "Add a test", "status": "in_progress", "priority": "medium"},
				{"content": "Run the tests", "status": "pending", "priority": "low"},
			}}),
			step(refactorPrompt, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "read1", "title": "Read cart.go", "kind": "read", "status": "completed", "locations": []map[string]any{{"path": path}}}),
			step(refactorPrompt, map[string]any{
				"sessionUpdate": "tool_call", "toolCallId": "edit1", "title": "Edit `" + path + "`", "kind": "edit", "status": "pending",
				"content":   []map[string]any{{"type": "diff", "path": path, "oldText": "func total(items []Item) int {\n", "newText": "func cartTotal(items []Item) int {\n"}},
				"locations": []map[string]any{{"path": path, "line": 1}},
			}),
			step(refactorPrompt, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "edit1", "status": "completed"}),
			{"match": refactorPrompt, "permission": permission},
			step(explainPrompt, chunk("agent_message_chunk", explainMarkdown)),
		},
	}
}

func normalize(t *testing.T, scrub *strings.Replacer, raw json.RawMessage) []byte {
	t.Helper()
	text := scrub.Replace(string(raw))
	ids := map[string]string{}
	text = sessionIDs.ReplaceAllStringFunc(text, func(id string) string {
		if _, ok := ids[id]; !ok {
			ids[id] = fmt.Sprintf("agt_%016x", len(ids)+1)
		}
		return ids[id]
	})
	text = timeRe.ReplaceAllString(text, `"`+fixtureTime+`"`)
	text = shaRe.ReplaceAllString(text, fixtureSha)
	text = fakeIDRe.ReplaceAllString(text, "claude-session-$1")
	var out bytes.Buffer
	require.NoError(t, json.Indent(&out, []byte(text), "", "  "))
	out.WriteByte('\n')
	return out.Bytes()
}

func checkFixture(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(fixtureDir, name)
	if *update {
		require.NoError(t, os.MkdirAll(fixtureDir, 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o600))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), driftFormat, name)
}
