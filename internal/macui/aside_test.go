package macui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const asideScript = `{"fork": true, "resume": true,
  "modes": {"currentModeId": "default", "availableModes": [{"id": "default", "name": "Default"}, {"id": "plan", "name": "Plan"}]},
  "prompt": [
    {"match": "Ask", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "Lists only keep a detail up to 6 em, so the key gives way to the title. Fixing it means changing that rule for all lists or adding a new prop."}}},
    {"match": "Ask", "elicitation": {"mode": "form", "toolCallId": "ask1", "message": "One decision",
      "requestedSchema": {"type": "object", "properties": {
        "question_0": {"type": "string", "title": "Lists", "description": "How should a long detail in a list row behave?",
          "oneOf": [{"const": "Change the rule for all lists", "title": "Change the rule for all lists"}, {"const": "Add an SDUI prop", "title": "Add an SDUI prop"}]},
        "question_0_custom": {"type": "string", "title": "Other"}}}}},
    {"match": "Summarize", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "detailMin only matters for the Send sheet today."}}},
    {"echo": true}]}`

func (in *instance) look(name, size string) {
	in.t.Helper()
	dir := os.Getenv("MACUI_LOOK")
	if dir == "" {
		return
	}
	args := []string{"ui", "snapshot", "--out", filepath.Join(dir, name)}
	if size != "" {
		args = append(args, "--size", size)
	}
	in.cm(args...)
}

func TestAsideBesideFullPageAndSendBack(t *testing.T) {
	in := start(t, "macui-aside", asideScript)
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Ask about lists")
	card := spawned.Session + "/elicitation/elicitation-2"
	row := card + "/f/0#Add an SDUI prop"
	in.eventually("the question card shows", func() bool { return in.snapshot().main().has(card + "/submit") })
	in.look("0-card", "1280x800")

	before := in.sessions()
	in.cm("ui", "menu", row, "Ask aside", "--context")
	aside := in.added(before).ID
	in.eventually("the aside opens beside the parent", func() bool {
		w := in.snapshot().main()
		return w.has(aside+"/asideFull") && w.has(card+"/submit")
	})
	in.focusIn(aside)
	in.cm("command", "run", "agents.prompt", "session="+aside, "text=Would Backlog want detailMin too?")
	in.eventually("the aside answers", func() bool {
		for _, s := range in.sessions() {
			if s.ID == aside {
				return s.Status == "done"
			}
		}
		return false
	})
	in.look("1-beside-1280", "1280x800")
	in.look("1-beside-900", "900x800")

	in.keyIn(aside, "cmd+shift+\\")
	in.eventually("the full page pins the question", func() bool {
		w := in.snapshot().main()
		return w.has(aside+"/pinned") && !w.has(card+"/submit")
	})
	in.look("2-full-1280", "1280x800")

	in.keyIn(aside, "cmd+shift+return")
	form := aside + "/sendBack/form"
	in.eventually("the Send back panel holds the summary", func() bool {
		n := in.sdui(form + "/text")
		return n != nil && n.Props["value"] != nil && n.Props["value"] != ""
	})
	in.look("3-sendback-1280", "1280x800")

	in.key("cmd+return")
	in.eventually("the parent's card has the option picked", func() bool { return in.selected(row) })
	assert.False(t, in.snapshot().main().has(aside+"/asideFull"))
	in.look("4-sent-1280", "1280x800")
}

func (in *instance) userMessage(session, part string) bool {
	in.t.Helper()
	var slots []struct {
		Tree sduiNode `json:"tree"`
	}
	in.json(&slots, "ui", "dump", "--workspace", in.workspace)
	var walk func(n *sduiNode) bool
	walk = func(n *sduiNode) bool {
		if n.Type == "message" && strings.HasPrefix(n.ID, session+"/msg/user/") {
			if text, _ := n.Props["text"].(string); strings.Contains(text, part) {
				return true
			}
		}
		for i := range n.Children {
			if walk(&n.Children[i]) {
				return true
			}
		}
		return false
	}
	for i := range slots {
		if walk(&slots[i].Tree) {
			return true
		}
	}
	return false
}

func (in *instance) keyIn(session string, keys ...string) {
	in.t.Helper()
	if focusedSession(in.snapshot().key()) == session {
		in.key(keys...)
		return
	}
	in.key(append([]string{"--focus", session + "/composer/0"}, keys...)...)
}

func (in *instance) waitDone(id string) {
	in.t.Helper()
	in.eventually(id+" is done", func() bool {
		for _, s := range in.sessions() {
			if s.ID == id {
				return s.Status == "done"
			}
		}
		return false
	})
}

func TestAsideFromTheLastAnswerAndAnOpenItem(t *testing.T) {
	in := start(t, "macui-aside-passage", asideScript)
	in.focusSection("agents", "agents.sidebar")
	parent := in.doneWithOpenItems("Tell me about lists")
	row := parent + "/open/oi-1#Global"
	in.eventually("the open items card shows", func() bool { return in.snapshot().main().has(row) })

	before := in.sessions()
	in.keyIn(parent, "cmd+\\")
	passage := in.added(before)
	assert.Contains(t, passage.Title, "Aside · “")
	in.eventually("the passage's aside opens beside", func() bool { return in.snapshot().main().has(passage.ID + "/forked") })
	in.look("6-passage-1280", "1280x800")
	in.look("6-passage-640", "640x800")

	in.keyIn(passage.ID, "cmd+[")
	in.focusIn(parent)
	in.snapshotAt("1280x800")
	card := in.snapshot().main().node(parent + "/open/card")
	in.pointer("click", parent+"/open/card", "--at", fmt.Sprintf("%d,4", int(card.Frame.Width)-20))
	before = in.sessions()
	in.key("a")
	in.eventually("a arms the card", func() bool {
		n := in.sdui(parent + "/open/keys")
		return n != nil && n.Props["hints"] == "1–4 ask aside about · ⎋ cancel"
	})
	in.key("2")
	option := in.added(before)
	assert.Equal(t, "Aside · Global", option.Title)
	in.eventually("a then 2 opens the aside about Global", func() bool { return in.snapshot().main().has(option.ID + "/asideFull") })
	assert.False(t, in.selected(row))
	in.look("7-open-item-a", "1280x800")

	in.keyIn(option.ID, "opt+cmd+delete")
	in.eventually("opt+cmd+delete closes only the focused aside", func() bool {
		ids := map[string]bool{}
		for _, s := range in.sessions() {
			ids[s.ID] = true
		}
		return !ids[option.ID] && ids[parent] && ids[passage.ID]
	})

	in.keyIn(parent, "cmd+]")
	in.eventually("cmd+] shows the passage's aside beside", func() bool { return in.snapshot().main().has(passage.ID + "/asideFull") })
	in.cm("command", "run", "agents.prompt", "session="+passage.ID, "text=What does it mean?")
	in.waitDone(passage.ID)
	in.keyIn(passage.ID, "cmd+shift+return")
	form := passage.ID + "/sendBack/form"
	in.eventually("the panel holds the summary", func() bool {
		n := in.sdui(form + "/text")
		return n != nil && n.Props["value"] != nil && n.Props["value"] != ""
	})
	in.look("8-sendback-draft", "1280x800")
	in.key("cmd+return")
	in.eventually("the draft lands in the parent's composer", func() bool {
		n := in.sdui(parent + "/composer/0")
		return n != nil && n.Props["draft"] != nil
	})
	in.look("9-draft", "1280x800")
	in.keyIn(parent, "cmd+return")
	in.eventually("the parent sends the quote and the summary", func() bool { return in.userMessage(parent, "> [From another agent session") })
}
