package macui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const openItems = `{"session": %q, "items": [
  {"kind": "decision", "header": "Scope", "question": "Where?", "options": ["Per workspace", "Global"], "descriptions": ["a", "b"], "quote": "q1"},
  {"kind": "decision", "header": "Mode", "question": "How?", "options": ["Fast", "Slow"], "descriptions": ["c", "d"], "quote": "q2"},
  {"kind": "offer", "label": "Write docs", "quote": "q3"}]}`

func (in *instance) doneWithOpenItems(prompt string) string {
	in.t.Helper()
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", prompt)
	in.eventually(prompt+" is done", func() bool {
		for _, s := range in.sessions() {
			if s.ID == spawned.Session {
				return s.Status == "done"
			}
		}
		return false
	})
	in.cm("command", "run", "agents.open-items", "--args", fmt.Sprintf(openItems, spawned.Session))
	return spawned.Session
}

func (in *instance) cursorOn(id string) func() bool {
	return func() bool {
		n := in.sdui(id)
		return n != nil && n.Props["open"] == true
	}
}

func (in *instance) otherText(item string) string {
	in.t.Helper()
	n := in.sdui(item)
	if n == nil {
		return ""
	}
	other, _ := n.Props["other"].(map[string]any)
	text, _ := other["value"].(string)
	return text
}

func TestOpenItemsCardPointer(t *testing.T) {
	in := start(t, "macui-open", "")
	in.focusSection("agents", "agents.sidebar")
	a := in.doneWithOpenItems("First")
	b := in.doneWithOpenItems("Second")
	in.pointer("click", "sessions#"+a)
	in.eventually("the first session's card shows", func() bool { return in.snapshot().main().has(a + "/open/card") })

	t.Run("a click on the card's background gives it j and k", func(t *testing.T) {
		in := in.at(t)
		card := in.snapshot().main().node(a + "/open/card")
		in.pointer("click", a+"/open/card", "--at", fmt.Sprintf("%d,4", int(card.Frame.Width)-20))
		in.key("j")
		in.eventually("j moves to the second decision", in.cursorOn(a+"/open/oi-2"))
		in.key("k")
		in.eventually("k moves back", in.cursorOn(a+"/open/oi-1"))
	})
	t.Run("a click on an option picks it and the card keeps the keys", func(t *testing.T) {
		in := in.at(t)
		in.pointer("click", a+"/open/oi-2#Slow")
		in.eventually("Slow is picked", func() bool { return in.selected(a + "/open/oi-2#Slow") })
		in.eventually("the cursor follows the click", in.cursorOn(a+"/open/oi-2"))
		in.key("k")
		in.eventually("k still moves the card's cursor", in.cursorOn(a+"/open/oi-1"))
	})
	t.Run("typing after a click into Other stays in the field", func(t *testing.T) {
		in := in.at(t)
		in.pointer("click", a+"/open/oi-1#other")
		in.eventually("the Other field has the caret", func() bool { return in.snapshot().key().Focus == a+"/open/oi-1" })
		in.key("esc")
		in.eventually("esc gives the keys back to the card", func() bool { return in.snapshot().key().Focus == "" })
		in.pointer("click", a+"/open/oi-1#other", "--at", "100,34")
		in.key("h", "i", "space", "j", "k")
		in.eventually("j and k land in the field", func() bool { return in.otherText(a+"/open/oi-1") == "hi jk" })
		oneLine := in.snapshot().main().node(a + "/open/oi-1").Frame.Height
		in.key("opt+return", "x")
		in.eventually("a line break in Other becomes a space", func() bool { return in.otherText(a+"/open/oi-1") == "hi jk x" })
		assert.Equal(t, oneLine, in.snapshot().main().node(a+"/open/oi-1").Frame.Height, "the field shows no second line")
		assert.True(t, in.cursorOn(a+"/open/oi-1")())
		assert.True(t, strings.Contains(in.snapshot().key().Responder, "FieldEditor"))
	})
	t.Run("a click on a folded row in the queue opens it and selects its session", func(t *testing.T) {
		in := in.at(t)
		in.key("esc", "opt+o")
		in.eventually("the queue folds the second session's other items", func() bool { return in.snapshot().main().has(b + "/open/oi-2#question") })
		assert.True(t, in.selected("sessions#"+a))
		in.pointer("click", b+"/open/oi-2#question")
		in.eventually("the folded item opens", in.cursorOn(b+"/open/oi-2"))
		in.eventually("the sidebar selects its session", func() bool { return in.selected("sessions#" + b) })
	})
}
