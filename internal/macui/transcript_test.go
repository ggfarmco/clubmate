package macui_test

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const imageScript = `{"prompt": [
  {"match": "Code", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "` + "`shot.png`" + ` is the screenshot"}}},
  {"match": "Link", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "[shot.png](shot.png) is the screenshot"}}}]}`

func TestTranscriptImageLinkOpensViewer(t *testing.T) {
	in := start(t, "macui-image", imageScript)
	f, err := os.Create(filepath.Join(in.repo, "shot.png"))
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, image.NewGray(image.Rect(0, 0, 40, 30))))
	require.NoError(t, f.Close())
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Code")
	sid := spawned.Session

	for i, tc := range []struct{ prompt, link string }{{"Code", "`shot.png`"}, {"Link", "[shot.png]"}} {
		t.Run(tc.prompt, func(t *testing.T) {
			in := in.at(t)
			if i > 0 {
				in.cm("agent", "tell", sid, tc.prompt)
			}
			var msg *element
			in.eventually("the agent's message shows", func() bool {
				msg = in.snapshot().main().find(func(e element) bool {
					return strings.HasPrefix(e.ID, sid+"/msg/agent/") && strings.HasPrefix(e.Label, tc.link)
				})
				return msg != nil
			})
			in.pointer("click", msg.ID, "--at", "12,8")
			in.eventually("the image viewer sheet opens", func() bool { return in.snapshot().kind("sheet") != nil })
			in.key("esc")
			in.eventually("esc closes it", func() bool { return in.snapshot().kind("sheet") == nil })
		})
	}
}

func TestTranscriptTableTextStaysInsideItsFrame(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("testdata", "table-message.json"))
	require.NoError(t, err)
	in := start(t, "macui-prose-table", string(script))
	in.focusSection("agents", "agents.sidebar")
	in.cm("agent", "spawn", "Plan it")
	in.eventually("the message's text fits its frame in a wide pane", func() bool {
		prose := in.snapshotAt("1600x900").main().find(func(e element) bool { return e.Role == "text" && e.Label == "План такой." })
		return prose != nil && !slices.Contains(prose.Flags, "text-overflow")
	})
}

const taskScript = `{"prompt": [{"update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "#1 is filed, PR #3 is not a task."}}}]}`

func TestTranscriptTaskLinkRevealsTheTask(t *testing.T) {
	in := start(t, "macui-tasklink", taskScript)
	in.cm("command", "run", "backlog.add", "title=Pin the exact task", "status=todo")
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "File it")
	sid := spawned.Session
	var transcript struct {
		Entries []struct {
			TaskLinks map[string]string `json:"taskLinks"`
		} `json:"entries"`
	}
	in.eventually("the message links task 1", func() bool {
		in.json(&transcript, "command", "run", "agents.transcript", "session="+sid)
		return slices.ContainsFunc(transcript.Entries, func(e struct {
			TaskLinks map[string]string `json:"taskLinks"`
		}) bool {
			return e.TaskLinks["1"] == "Pin the exact task"
		})
	})
	var msg *element
	in.eventually("the agent's message shows", func() bool {
		msg = in.snapshot().main().find(func(e element) bool {
			return strings.HasPrefix(e.ID, sid+"/msg/agent/") && strings.HasPrefix(e.Label, "#1")
		})
		return msg != nil
	})
	in.pointer("click", msg.ID, "--at", "6,8")
	in.eventually("the Backlog opens with task 1 selected", func() bool { return in.selected("tasks#1") })
}

const refusedScript = `{"modes": {"currentModeId": "auto", "availableModes": [{"id": "auto", "name": "Auto"}]}, "prompt": [
  {"match": "[Clubmate Tool call review]", "wait": "/nonexistent/never"},
  {"match": "Push", "update": {"sessionUpdate": "tool_call", "toolCallId": "p1", "title": "git push", "kind": "execute", "status": "pending", "rawInput": {"command": "git push origin main"}}},
  {"match": "Push", "permission": {"toolCall": {"toolCallId": "p1", "name": "Bash", "rawInput": {"command": "git push origin main"}}, "options": [{"optionId": "allow", "name": "Allow", "kind": "allow_once"}, {"optionId": "deny", "name": "Deny", "kind": "reject_once"}]}}]}`

func TestRefusedCallTitleShowsWholeAt640(t *testing.T) {
	in := start(t, "macui-refused", refusedScript)
	in.cm("command", "run", "agents.gate-add", "repo="+in.repo, "point=tool", "how=command", "text=git push")
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Push")
	type reviewer struct {
		ID      string `json:"id"`
		GateFor *struct {
			Lander string `json:"lander"`
			Key    string `json:"key"`
		} `json:"gateFor"`
	}
	var r reviewer
	in.eventually("the call's review starts", func() bool {
		var list []reviewer
		in.json(&list, "command", "run", "agents.sessions")
		i := slices.IndexFunc(list, func(s reviewer) bool {
			return s.GateFor != nil && s.GateFor.Lander == spawned.Session && s.GateFor.Key != ""
		})
		if i >= 0 {
			r = list[i]
		}
		return i >= 0
	})

	in.runAs(r.ID, "command", "run", "--agent", "agents.land-verdict", "key="+r.GateFor.Key, "outcome=changes", "findings=main.go:1 has no test")

	in.eventually("the refused block's title is drawn whole at 640", func() bool {
		e := in.snapshotAt("930x800").main().label("text", "Refused by the gate · round 1 · changes")
		return e != nil && !slices.Contains(e.Flags, "truncated")
	})
}
