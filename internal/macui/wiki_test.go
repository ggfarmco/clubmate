package macui_test

import (
	"encoding/json"
	"fmt"
	"strconv"
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

func TestWikiMarginAllChangesOpensHistory(t *testing.T) {
	in := start(t, "macui-wiki-margin", "")
	for i := range 4 {
		args, err := json.Marshal(map[string]string{"page": "a.md", "content": "# A\n\nBody " + strconv.Itoa(i) + ".\n", "summary": "v" + strconv.Itoa(i)})
		require.NoError(t, err)
		in.cm("command", "run", "wiki.write", "--args", string(args))
	}
	in.focusSection("wiki", "wiki.sidebar")
	var page *element
	in.eventually("the page shows at 1280", func() bool {
		page = in.snapshotAt("1280x800").main().find(func(e element) bool { return e.ID == "page" })
		return page != nil
	})

	opened := func() bool { return in.snapshot().main().has("changes") }
	for y := 90; y <= 140 && !opened(); y += 5 {
		in.pointer("click", "page", "--at", fmt.Sprintf("%d,%d", int(page.Frame.Width)-110, y))
	}
	in.eventually("a click on All 4 changes in the margin opens History", opened)
}
