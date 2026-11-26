package macui_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
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

func TestWikiPageFindOpensOnCmdFAndEscClosesIt(t *testing.T) {
	in := start(t, "macui-wiki-find", "")
	args, err := json.Marshal(map[string]string{"page": "a.md", "content": "# A\n\nThe fox and the fox.\n", "summary": "seed"})
	require.NoError(t, err)
	in.cm("command", "run", "wiki.write", "--args", string(args))
	in.focusSection("wiki", "wiki.sidebar")
	in.eventually("the page shows", func() bool { return in.snapshot().main().has("page") })

	findFocused := func() bool { w := in.snapshot().key(); return w != nil && w.Focus == "page" && w.Responder == "NSTextView" }
	require.Equal(t, []string{"monitor"}, in.key("cmd+f"))
	in.eventually("⌘F puts the caret in the find field over the page", findFocused)
	for _, key := range []string{"shift+return", "esc"} {
		require.NotContains(t, in.key("--focus", "search", key), "monitor", "%s in the sidebar's search field stays there while the find bar is open", key)
	}
	require.Equal(t, []string{"monitor"}, in.key("cmd+f"))
	in.eventually("⌘F from the sidebar takes the caret back to the find field", findFocused)
	require.Equal(t, []string{"monitor"}, in.key("esc"))
	in.eventually("Esc closes the find field", func() bool { return !findFocused() })
}

func TestWikiChatPanelOpensOnPicker(t *testing.T) {
	in := start(t, "macui-wiki-chat", askScript)
	for i := range 4 {
		args, err := json.Marshal(map[string]string{"page": "a.md", "content": "# A\n\nBody " + strconv.Itoa(i) + ".\n", "summary": "v" + strconv.Itoa(i)})
		require.NoError(t, err)
		in.cm("command", "run", "wiki.write", "--args", string(args))
	}
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Hello")
	in.focusSection("wiki", "wiki.sidebar")
	var page *element
	in.eventually("the page shows at 1280", func() bool {
		page = in.snapshotAt("1280x800").main().find(func(e element) bool { return e.ID == "page" })
		return page != nil
	})
	marginShown := func() bool {
		for y := 90; y <= 140; y += 5 {
			in.pointer("click", "page", "--at", fmt.Sprintf("%d,%d", int(page.Frame.Width)-110, y))
			if in.snapshot().main().has("changes") {
				return true
			}
		}
		return false
	}
	in.eventually("the margin is beside the page", marginShown)
	in.key("cmd+y")
	in.eventually("cmd+y goes back to the page", func() bool { return in.snapshot().main().has("page") })

	in.key("cmd+b")
	row := "chat/sessions#" + spawned.Session
	in.eventually("cmd+b opens the picker with the suggestion selected", func() bool { return hasRole(in.snapshot().main().node(row), "selected") })
	in.key("return")
	in.focusIn(spawned.Session)
	in.eventually("the margin is folded while the panel shows", func() bool { return !marginShown() })

	in.key("cmd+b")
	in.eventually("cmd+b again closes the panel", func() bool { return !in.snapshot().main().has("chat/sessions") })
	in.eventually("the margin is back", func() bool {
		page = in.snapshotAt("1280x800").main().find(func(e element) bool { return e.ID == "page" })
		return page != nil && marginShown()
	})
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

func TestChatPickerFilterTakesN(t *testing.T) {
	in := start(t, "macui-chat-filter", "")
	args, err := json.Marshal(map[string]string{"page": "a.md", "content": "# A\n\nBody.\n", "summary": "seed"})
	require.NoError(t, err)
	in.cm("command", "run", "wiki.write", "--args", string(args))
	prompts := []string{"Check nq row", "Fix q bug"}
	for i := range 9 {
		prompts = append(prompts, "Fix bug "+strconv.Itoa(i))
	}
	var first struct {
		Session string `json:"session"`
	}
	in.json(&first, "agent", "spawn", prompts[0])
	for i, p := range prompts[1:] {
		if i < 7 {
			in.cm("agent", "spawn", p)
		} else {
			in.runAs(first.Session, "agent", "spawn", p)
		}
	}
	in.eventually("every session has its title", func() bool {
		var titles []string
		for _, s := range in.sessions() {
			titles = append(titles, s.Title)
		}
		return len(titles) == len(prompts) && !slices.ContainsFunc(prompts, func(p string) bool { return !slices.Contains(titles, p) })
	})
	in.focusSection("wiki", "wiki.sidebar")
	in.key("cmd+b")
	rows := func() []string {
		var labels []string
		for _, e := range in.snapshot().main().elements {
			if strings.HasPrefix(e.ID, "chat/sessions#agt_") {
				labels = append(labels, e.Label)
			}
		}
		return labels
	}
	in.eventually("the picker lists every session", func() bool { return len(rows()) == len(prompts) })
	w := in.snapshot().main()
	list, add := w.node("chat/sessions"), w.node("chat/new")
	require.NotNil(t, list)
	require.NotNil(t, add)
	require.Greater(t, add.Frame.X, list.Frame.X, "the chat column insets New session from the pane edge")

	in.key("/")
	in.eventually("/ focuses the filter", func() bool { return strings.Contains(in.snapshot().key().Responder, "FieldEditor") })
	in.key("n", "q")

	only := func() bool {
		r := rows()
		return len(r) == 1 && strings.Contains(r[0], "Check nq row")
	}
	in.eventually("n goes into the filter, so nq shows only its row", only)
	testutil.Never(t, func() bool { return len(in.sessions()) != len(prompts) || !only() }, testutil.Timeout(time.Second))
}
