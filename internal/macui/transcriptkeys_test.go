package macui_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const transcriptKeysScript = `{"fork": true, "resume": true, "prompt": [
  {"match": "Edit", "update": {"sessionUpdate": "tool_call", "toolCallId": "r1", "title": "Read ` + "`go.mod`" + `", "kind": "read", "status": "completed", "locations": [{"path": "go.mod"}]}},
  {"match": "Edit", "update": {"sessionUpdate": "tool_call", "toolCallId": "e1", "title": "Edit ` + "`notes.txt`" + `", "kind": "edit", "status": "completed", "locations": [{"path": "notes.txt"}], "content": [{"type": "diff", "path": "notes.txt", "oldText": "a\n", "newText": "b\n"}]}},
  {"match": "Edit", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "Edited the notes."}}},
  {"match": "Wait", "permission": {"toolCall": {"toolCallId": "x1", "title": "go test ./...", "kind": "execute", "status": "pending", "rawInput": {"command": "go test ./..."}},
    "options": [{"optionId": "allow", "name": "Yes", "kind": "allow_once"}, {"optionId": "reject", "name": "No", "kind": "reject_once"}]}}]}`

const readBoardJXA = `ObjC.import('AppKit');
function run() {
	return $.NSPasteboard.pasteboardWithName('com.clubmate.test').stringForType('public.utf8-plain-text').js;
}`

func (in *instance) barOn(id string) func() bool {
	return func() bool {
		e := in.snapshot().main().node("transcript.bar")
		return e != nil && e.Label == id
	}
}

func TestTranscriptBlockKeys(t *testing.T) {
	in := start(t, "macui-transcript-keys", transcriptKeysScript)
	in.focusSection("agents", "agents.sidebar")
	sid := in.idleSession("Edit")
	group, edit := sid+"/tool/r1/actions", sid+"/tool/e1"
	in.pointer("click", sid+"/composer/0")
	in.focusIn(sid)
	in.stepOut("opt+cmd+up")
	in.mode("READ")

	t.Run("j k step blocks and l h fold the actions", func(t *testing.T) {
		in := in.at(t)
		in.key("g", "g", "j")
		in.eventually("the bar is on the actions", in.barOn(group))
		in.key("l")
		in.eventually("the actions unfold", func() bool { return in.snapshot().main().has(edit) })
		in.key("h")
		in.eventually("the actions fold", func() bool { return !in.snapshot().main().has(edit) })
	})
	t.Run("cmd+c copies the focused block as Markdown", func(t *testing.T) {
		in := in.at(t)
		in.key("g", "g", "j", "j")
		in.eventually("the bar is on the answer", in.barOn(sid+"/msg/agent/agent-1"))
		assert.Contains(t, in.key("cmd+c"), "monitor")
		in.eventually("the answer is on the board", func() bool {
			out, err := exec.Command("osascript", "-l", "JavaScript", "-e", readBoardJXA).Output()
			return err == nil && strings.TrimSpace(string(out)) == "Edited the notes."
		})
	})
	t.Run("gf opens the focused edit in nvim", func(t *testing.T) {
		in := in.at(t)
		in.key("g", "g", "j", "l", "j", "j")
		in.eventually("the bar is on the edit", in.barOn(edit))
		in.key("g")
		in.eventually("the which-key line offers f", func() bool { return strings.Contains(in.statusBar("statusbar.hints"), "f file") })
		require.Contains(t, in.key("f"), "monitor")
		in.eventually("nvim shows", func() bool { return in.snapshot().main().hasPrefix("editor-nvim") })
	})
}

func TestAsideKeyOnANonAnswerBlockAsksAboutTheLastAnswer(t *testing.T) {
	in := start(t, "macui-transcript-aside", transcriptKeysScript)
	in.focusSection("agents", "agents.sidebar")
	sid := in.idleSession("Edit")
	in.pointer("click", sid+"/composer/0")
	in.focusIn(sid)
	in.stepOut("opt+cmd+up")
	in.mode("READ")
	in.key("g", "g", "j")
	in.eventually("the bar is on the actions", in.barOn(sid+"/tool/r1/actions"))
	before := in.sessions()
	assert.Contains(t, in.key("cmd+\\"), "monitor")
	aside := in.added(before).ID
	in.eventually("the aside opens beside", func() bool { return in.snapshot().main().has(aside + "/asideClose") })
}

func TestBareDigitAnswersTheCardFromTheTranscript(t *testing.T) {
	in := start(t, "macui-transcript-card", transcriptKeysScript)
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Wait")
	pending := func() string {
		return in.nodeID(func(n *sduiNode) bool { return n.Type == "permissionCard" && n.Props["outcome"] == nil })
	}
	in.eventually("the card docks", func() bool { return pending() != "" && in.snapshot().main().has(pending()) })
	in.pointer("click", spawned.Session+"/composer/0")
	in.focusIn(spawned.Session)
	in.key("x")
	in.stepOut("esc")
	in.mode("READ")
	assert.Contains(t, in.key("y"), "monitor")
	in.eventually("y allowed the call", func() bool { return pending() == "" })
}
