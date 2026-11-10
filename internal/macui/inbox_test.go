package macui_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const capGraph = `{"title": "Cap", "nodes": [
  {"id": "tests", "role": "Tests", "start": true, "prompt": "Run the tests"},
  {"id": "coder", "role": "Coder", "prompt": "Fix them"}],
 "edges": [{"from": "tests", "to": "coder", "on": "done", "maxFires": 1}, {"from": "coder", "to": "tests", "on": "done"}]}`

const permissionScript = `{"prompt": [{"match": "Wait", "permission": {"toolCall": {"toolCallId": "x1", "title": "go test ./...", "kind": "execute", "status": "pending", "rawInput": {"command": "go test ./..."}},
  "options": [{"optionId": "allow", "name": "Yes", "kind": "allow_once"}, {"optionId": "reject", "name": "No", "kind": "reject_once"}]}}]}`

func TestInboxPinsTheFrontWorkspacesRequestOnceYouLookAway(t *testing.T) {
	in := start(t, "macui-inbox-look-away", permissionScript)
	in.focusSection("agents", "agents.sidebar")
	spawn := func(prompt string) string {
		var spawned struct {
			Session string `json:"session"`
		}
		in.json(&spawned, "agent", "spawn", prompt)
		require.NotEmpty(t, spawned.Session)
		in.eventually(prompt+" waits for a permission", func() bool {
			return slices.ContainsFunc(in.sessions(), func(s session) bool { return s.ID == spawned.Session && s.Status == "waiting" })
		})
		return spawned.Session
	}
	first := spawn("Wait one")
	in.cm("ui", "event", "--workspace="+in.workspace, "--slot=agents.sidebar", "--node=sessions", "--action=select", `--value="`+first+`"`)
	in.eventually("the first session is on screen", func() bool { return in.selected("sessions#" + first) })
	second := spawn("Wait two")
	in.cm("ui", "event", "--workspace="+in.workspace, "--slot=agents.sidebar", "--node=sessions", "--action=select", `--value="`+second+`"`)
	in.eventually("the second session is on screen", func() bool { return in.selected("sessions#" + second) })

	in.cm("ui", "present", "inbox")
	in.eventually("the first session's request is pinned in the inbox", func() bool {
		p := in.snapshot().kind("popover")
		return p != nil && p.hasPrefix("inbox.pinned#"+first+"/")
	})
}

func TestInboxGraphPinOpensTheRun(t *testing.T) {
	in := start(t, "macui-inbox", "")
	dir := filepath.Join(in.repo, ".clubmate", "graphs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cap.json"), []byte(capGraph), 0o600))
	in.cm("ui", "present", "inbox")
	in.eventually("the inbox is open", func() bool { return in.snapshot().kind("popover") != nil })
	var run struct {
		ID string `json:"id"`
	}
	in.json(&run, "command", "run", "graphs.run", "graph=cap", "input=go")
	require.NotEmpty(t, run.ID)
	in.eventually("the edge back to the coder is held", func() bool {
		var shown struct {
			Held []any `json:"held"`
		}
		in.json(&shown, "command", "run", "graphs.show", "id="+run.ID)
		return len(shown.Held) > 0
	})

	pin := "inbox.pinned#" + run.ID + "/cap-0"
	in.eventually("the run's cap is pinned in the inbox", func() bool {
		p := in.snapshot().kind("popover")
		return p != nil && p.node(pin) != nil && strings.HasPrefix(p.node(pin).Label, "Question · Tests → Coder")
	})
	in.pointer("click", pin)
	in.eventually("Graphs opens on that run", func() bool {
		s := in.snapshot()
		return s.kind("popover") == nil && in.selected("runs#"+run.ID)
	})
}
