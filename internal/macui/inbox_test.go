package macui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestInboxClipsALongListToThePopover(t *testing.T) {
	in := start(t, "macui-inbox-long", "")
	for i := range 12 {
		deadline := time.Now().Add(30 * time.Second)
		for {
			out, err := in.run("notify", "--title", fmt.Sprintf("Note %d", i), fmt.Sprintf("Message %d", i))
			if err == nil {
				break
			}
			require.Contains(t, string(out), "too many notifications")
			require.True(t, time.Now().Before(deadline), "note %d is still rate limited", i)
			time.Sleep(time.Second)
		}
	}
	in.cm("ui", "present", "inbox")
	in.eventually("the inbox shows a full list", func() bool {
		p := in.snapshot().kind("popover")
		return p != nil && p.label("row", "Note 4") != nil
	})

	shown := func() (*window, float64) {
		p := in.snapshot().kind("popover")
		list := p.node("inbox.list")
		require.NotNil(t, list)
		bottom := list.Frame.Y + list.Frame.Height
		require.LessOrEqual(t, bottom-list.Frame.X, 480.0, "the popover keeps its 480pt cap")
		for _, e := range p.elements {
			if e.Role != "row" || e.Frame.Y >= bottom || e.Frame.Y+e.Frame.Height <= list.Frame.Y {
				continue
			}
			drawn := e.Frame
			if e.Visible != nil {
				drawn = *e.Visible
			}
			require.GreaterOrEqual(t, drawn.Y, list.Frame.Y, "%s is drawn over the header", e.Label)
			require.LessOrEqual(t, drawn.Y+drawn.Height, bottom, "%s is drawn past the list's bottom edge", e.Label)
		}
		return p, bottom
	}
	shown()
	in.eventually("the oldest note scrolls into the list", func() bool {
		p, bottom := shown()
		if last := p.label("row", "Note 0"); last != nil && last.Frame.Y+last.Frame.Height <= bottom {
			return true
		}
		row := p.find(func(e element) bool { return e.Role == "row" && e.Frame.Y >= 0 && e.Frame.Y+e.Frame.Height < bottom })
		require.NotNil(t, row)
		in.pointer("scroll", row.ID, "--dy=20")
		return false
	})
}
