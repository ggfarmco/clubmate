package macui_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikiAgentNotesFoldAndHistoryKey(t *testing.T) {
	in := start(t, "macui-wiki", "")
	write := func(page, content string) {
		args, err := json.Marshal(map[string]string{"page": page, "content": content, "summary": "seed"})
		require.NoError(t, err)
		in.cm("command", "run", "wiki.write", "--args", string(args))
	}
	write("a.md", "# A\n\nBody of A.\n")
	write(".agents/trap.md", "# Trap\n\nA note.\n")
	in.focusSection("wiki", "wiki.sidebar")

	row := func(suffix string) *element {
		return in.snapshot().main().find(func(e element) bool { return strings.HasPrefix(e.ID, "pages#") && strings.HasSuffix(e.ID, suffix) })
	}
	in.eventually("the notes row is folded under the pages", func() bool {
		return row("/a.md") != nil && row("/.agents") != nil && row("/.agents/trap.md") == nil
	})
	in.pointer("click", row("/.agents").ID)
	in.eventually("a click unfolds the agent notes", func() bool { return row("/.agents/trap.md") != nil })

	in.pointer("click", row("/a.md").ID)
	in.eventually("the page shows with Edit", func() bool { return in.snapshot().main().label("button", "Edit") != nil })
	in.key("cmd+y")
	in.eventually("cmd+y shows the page's history with the tree focused", func() bool { return in.snapshot().main().has("changes") })
	in.key("cmd+y")
	in.eventually("cmd+y again goes back to the page", func() bool { return in.snapshot().main().has("page") })
}
