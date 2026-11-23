package macui_test

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func (in *instance) pickerRows() (top, keyCap string) {
	in.t.Helper()
	var rows []element
	for _, e := range in.snapshot().main().elements {
		if strings.HasPrefix(e.ID, "chat/sessions#") {
			rows = append(rows, e)
		}
	}
	if len(rows) == 0 {
		return "", ""
	}
	slices.SortFunc(rows, func(a, b element) int { return cmp.Compare(a.Frame.Y, b.Frame.Y) })
	for _, e := range rows {
		if slices.Contains(strings.Fields(e.Role), "keycap") {
			keyCap = e.ID
		}
	}
	return rows[0].ID, keyCap
}

func (in *instance) suggestionFocused() bool {
	in.t.Helper()
	top, keyCap := in.pickerRows()
	return top != "" && keyCap == top && in.selected(top)
}

func TestChatPickerTakesFocusOnOpen(t *testing.T) {
	in := start(t, "macui-chatpanel", "")
	in.cm("agent", "spawn", "First")
	in.cm("agent", "spawn", "Second")
	in.eventually("both sessions are open", func() bool { return len(in.sessions()) == 2 })
	args, err := json.Marshal(map[string]string{"page": "a.md", "content": "# A\n\nBody.\n", "summary": "seed"})
	require.NoError(t, err)
	in.cm("command", "run", "wiki.write", "--args", string(args))

	moves := func(in *instance) {
		in.key("j")
		in.eventually("j moves the cursor off the suggestion", func() bool {
			top, keyCap := in.pickerRows()
			return keyCap != "" && keyCap != top
		})
	}

	t.Run("cmd+b over Neovim opens the picker focused on the suggestion", func(t *testing.T) {
		in := in.at(t)
		in.focusSection("editor-nvim", "editor-nvim.main")
		in.key("cmd+b")
		in.eventually("the picker shows", func() bool { return in.snapshot().main().has("chat/sessions") })
		in.key("cmd+b")
		in.eventually("the picker hides and Neovim has focus", func() bool {
			s := in.snapshot()
			return !s.main().has("chat/sessions") && s.key().Responder == "TerminalSurfaceView"
		})
		in.key("cmd+b")
		in.eventually("the suggestion is the focused row with its return cap", func() bool {
			return in.suggestionFocused()
		})
		moves(in)
	})
	t.Run("Got it on the Wiki hint gives focus back to the picker", func(t *testing.T) {
		in := in.at(t)
		in.focusSection("wiki", "wiki.main")
		in.key("cmd+b")
		in.eventually("the hint shows over the focused picker", func() bool {
			return in.snapshot().main().has("barNote/dismiss") && in.suggestionFocused()
		})
		in.pointer("click", "barNote/dismiss")
		in.eventually("the hint goes and the picker keeps focus", func() bool {
			return !in.snapshot().main().has("barNote/dismiss") && in.suggestionFocused()
		})
		moves(in)
	})
}
