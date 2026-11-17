package macui_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikiAgentNotesFoldHistoryKeyAndRestoreConfirm(t *testing.T) {
	in := start(t, "macui-wiki", "")
	write := func(page, content string) {
		args, err := json.Marshal(map[string]string{"page": page, "content": content, "summary": "seed"})
		require.NoError(t, err)
		in.cm("command", "run", "wiki.write", "--args", string(args))
	}
	write("a.md", "# A\n\nBody of A.\n")
	write("a.md", "# A\n\nNew body of A.\n")
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
	changes := func() []element {
		var rows []element
		for _, e := range in.snapshot().main().elements {
			if strings.HasPrefix(e.ID, "changes#") && !strings.HasPrefix(e.ID, "changes#day:") {
				rows = append(rows, e)
			}
		}
		slices.SortFunc(rows, func(a, b element) int { return cmp.Compare(a.Frame.Y, b.Frame.Y) })
		return rows
	}
	in.eventually("both changes are listed", func() bool { return len(changes()) == 2 })
	oldest := changes()[1].ID
	in.pointer("click", oldest)
	in.eventually("restore is offered for the older change", func() bool {
		n := in.sdui("restore")
		return n != nil && n.Props["disabled"] != true
	})
	in.pointer("click", "restore")
	in.eventually("restore asks first", func() bool { return in.alert() != nil })
	in.key("esc")
	in.eventually("esc restores nothing", func() bool { return in.alert() == nil && len(changes()) == 2 })
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

func TestWikiFolderRowOpensItsIndexPage(t *testing.T) {
	in := start(t, "macui-wiki-index", "")
	for page, content := range map[string]string{"a.md": "# A\n\nBody.\n", "guides/sdui/index.md": "# SDUI protocol\n\nIndex.\n", "guides/sdui/nodes.md": "# Nodes\n\nNodes.\n", "guides/sdui/examples/e.md": "# Example\n\nE.\n", "solo/index.md": "# Solo page\n\nSolo.\n"} {
		args, err := json.Marshal(map[string]string{"page": page, "content": content, "summary": "seed"})
		require.NoError(t, err)
		in.cm("command", "run", "wiki.write", "--args", string(args))
	}
	in.focusSection("wiki", "wiki.sidebar")

	row := func(suffix string) *element {
		return in.snapshot().main().find(func(e element) bool { return strings.HasPrefix(e.ID, "pages#") && strings.HasSuffix(e.ID, suffix) })
	}
	in.eventually("a nested folder with a page is one row, a folder holding only its page a page row", func() bool {
		r := row("/guides/sdui")
		return r != nil && r.Label == "SDUI protocol" && row("/guides/sdui/index.md") == nil && row("/solo/index.md") != nil
	})
	title := func() any {
		var slots []struct {
			Slot string   `json:"slot"`
			Tree sduiNode `json:"tree"`
		}
		in.json(&slots, "ui", "dump", "--workspace", in.workspace)
		for _, s := range slots {
			if h := s.Tree.find("header"); s.Slot == "wiki.main" && h != nil {
				return h.Props["title"]
			}
		}
		return nil
	}
	selected := func(suffix string) bool { return hasRole(row(suffix), "selected") }
	unfolded := func() bool { return row("/guides/sdui/nodes.md") != nil }
	chevron := func() { in.pointer("click", row("/guides/sdui").ID, "--at", "14,10") }

	page := strings.TrimPrefix(row("/guides/sdui").ID, "pages#dir:") + "/index.md"
	in.cm("ui", "event", "--workspace="+in.workspace, "--slot=wiki.sidebar", "--node=pages", "--action=select", `--value="`+page+`"`)
	in.eventually("the index page opens from outside the tree", func() bool { return title() == "SDUI protocol" && selected("/guides/sdui") })
	in.cm("command", "run", "ui.focus", "section=wiki", "slot=wiki.main")
	in.focusSection("wiki", "wiki.sidebar")
	in.key("j")
	in.eventually("focusing the sidebar puts the cursor on the folder, so j steps to its first child", func() bool { return hasRole(row("/guides/sdui/examples"), "focused") })
	in.pointer("click", row("/guides/sdui/examples").ID)
	in.eventually("a click on a plain subfolder folds it", func() bool { return row("/guides/sdui/examples/e.md") == nil && row("/guides/sdui/examples") != nil })

	in.pointer("click", row("/a.md").ID)
	in.eventually("page A shows", func() bool { return title() == "A" })
	chevron()
	in.eventually("the chevron folds and leaves the selection", func() bool { return !unfolded() && title() == "A" && selected("/a.md") })
	in.pointer("click", row("/guides/sdui").ID)
	in.eventually("a click on the folded row opens its page and unfolds it", func() bool {
		return title() == "SDUI protocol" && selected("/guides/sdui") && unfolded()
	})
	in.pointer("click", row("/guides/sdui").ID)
	in.eventually("a second click never folds", func() bool { return unfolded() && title() == "SDUI protocol" })

	in.pointer("click", row("/a.md").ID)
	chevron()
	in.eventually("folded again on page A", func() bool { return !unfolded() && title() == "A" })
	in.key("j", "j", "return")
	in.eventually("j skips the section label and return opens and unfolds the folder", func() bool {
		return title() == "SDUI protocol" && selected("/guides/sdui") && unfolded()
	})

	in.pointer("click", row("/solo/index.md").ID)
	in.eventually("a folder holding only its page opens like a page", func() bool { return title() == "Solo page" && selected("/solo/index.md") })
}
