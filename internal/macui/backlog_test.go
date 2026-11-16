package macui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBacklogStartPopoverAndDrop(t *testing.T) {
	in := start(t, "macui-backlog", "")
	in.cm("command", "run", "backlog.add", "title=Fix the flaky test", "status=todo")
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the task is listed", func() bool { return in.snapshot().main().has("tasks#1") })
	in.pointer("click", "tasks#1")
	in.eventually("the task opens", func() bool { return in.snapshot().main().has("start") })

	popover := func() bool { s := in.snapshot(); return s.kind("popover") != nil && s.kind("popover").Key }
	closed := func() bool { return in.snapshot().kind("popover") == nil }

	t.Run("a file dropped on Attachments is attached", func(t *testing.T) {
		in := in.at(t)
		file := filepath.Join(in.repo, "notes.txt")
		require.NoError(t, os.WriteFile(file, []byte("hello"), 0o600))
		in.pointer("drop", "attachText", file)
		var task struct {
			Attachments []struct {
				Path string `json:"path"`
			} `json:"attachments"`
		}
		in.eventually("the task lists the file", func() bool {
			in.json(&task, "command", "run", "backlog.get", "id=1")
			return len(task.Attachments) == 1 && task.Attachments[0].Path == file
		})
	})
	t.Run("cmd+return opens Start agent and esc closes it", func(t *testing.T) {
		in := in.at(t)
		in.key("cmd+return")
		in.eventually("the popover is the key window", popover)
		in.key("esc")
		in.eventually("esc closes the popover", closed)
		assert.Empty(t, in.sessions())
	})
	t.Run("return in a clicked-open popover starts the agent", func(t *testing.T) {
		in := in.at(t)
		in.pointer("click", "start")
		in.eventually("a click opens the popover as the key window", popover)
		in.key("return")
		in.eventually("the agent starts", func() bool { return len(in.sessions()) == 1 })
	})
}

func TestBacklogTagMenuPicksATag(t *testing.T) {
	in := start(t, "macui-tags", "")
	in.cm("command", "run", "backlog.add", "title=Tagged", "status=todo", "tags=a,b,c,d")
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("one tag chip and the rest behind +3", func() bool {
		w := in.snapshot().main()
		return w.has("tags#tag:a") && !w.has("tags#tag:b") && w.has("tagMenu")
	})
	in.pointer("menu", "tagMenu", "c 1")
	in.eventually("the picked tag is a selected chip", func() bool { return in.selected("tags#tag:c") })
}

func TestBacklogNewTaskFromPlusAndCmdN(t *testing.T) {
	in := start(t, "macui-bnew", "")
	in.cm("command", "run", "backlog.add", "title=First", "status=todo", "description=Body")
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the task is listed", func() bool { return in.snapshot().main().has("tasks#1") })
	assert.False(t, in.snapshot().main().has("title"), "the New task field waits for + or cmd+n")
	keyFocus := func() string {
		if w := in.snapshot().key(); w != nil {
			return w.Focus
		}
		return ""
	}

	in.pointer("click", "newTask")
	in.eventually("+ opens the New task field with the cursor in it", func() bool { return keyFocus() == "title" })
	in.key("t", "w", "o", "return")
	in.eventually("return adds the task and keeps the field", func() bool {
		return in.snapshot().main().has("tasks#2") && keyFocus() == "title"
	})
	in.pointer("click", "newClose")
	in.eventually("× closes the field", func() bool { return !in.snapshot().main().has("title") })

	in.pointer("click", "tasks#1")
	in.eventually("task 1 opens as a page", func() bool { return in.snapshot().main().has("doc") })
	assert.Equal(t, []string{"monitor", "monitor"}, in.key("s", "p"), "s and p open the Status and Priority menus")
	in.key("e")
	in.eventually("e edits the description", func() bool { return in.snapshot().main().has("description") })
	in.key("cmd+n")
	in.eventually("cmd+n opens the field again", func() bool { return keyFocus() == "title" })
}

func TestBacklogWheelOverDescriptionScrollsTheTask(t *testing.T) {
	in := start(t, "macui-bdesc", "")
	in.cm("command", "run", "backlog.add", "title=Long", "status=todo", "description="+strings.Repeat("A line of the description.\n\n", 20))
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the task is listed", func() bool { return in.snapshot().main().has("tasks#1") })
	in.pointer("click", "tasks#1")
	var doc *element
	in.eventually("the description shows", func() bool {
		doc = in.snapshot().main().node("doc")
		return doc != nil && doc.Frame.Height > 300
	})
	top := doc.Frame.Y
	in.eventually("a wheel over the description scrolls the task", func() bool {
		in.pointer("scroll", "doc", "--dy=10")
		d := in.snapshot().main().node("doc")
		return d != nil && d.Frame.Y < top
	})
}

func TestBacklogSearch(t *testing.T) {
	in := start(t, "macui-bsearch", "")
	in.cm("command", "run", "backlog.add", "title=Fix the mic", "status=todo")
	in.cm("command", "run", "backlog.add", "title=Other", "status=todo")
	in.cm("command", "run", "backlog.add", "title=Old mic", "status=todo")
	in.cm("command", "run", "backlog.done", "id=3")
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the tasks are listed", func() bool { return in.snapshot().main().has("tasks#2") })
	keyFocus := func() string {
		if w := in.snapshot().key(); w != nil {
			return w.Focus
		}
		return ""
	}

	in.pointer("click", "search")
	in.key("m", "i", "c")
	in.eventually("only the matches show, done included", func() bool {
		w := in.snapshot().main()
		return w.has("tasks#1") && w.has("tasks#3") && !w.has("tasks#2")
	})
	in.pointer("click", "tasks#group:todo")
	in.key("cmd+n")
	in.eventually("cmd+n focuses New task, not the search", func() bool { return keyFocus() == "title" })

	in.pointer("click", "search")
	in.key("cmd+a", "delete")
	in.eventually("a cleared search lists every task", func() bool { return in.snapshot().main().has("tasks#2") })
	assert.True(t, in.snapshot().main().has("tasks#1"), "a header click during the search left Todo open")
	assert.False(t, in.snapshot().main().has("tasks#3"), "Done folds again")

	in.pointer("click", "tasks#2")
	in.focusSection("backlog", "backlog.sidebar")
	in.key("g", "c")
	in.eventually("gc in the tree focuses the search", func() bool { return keyFocus() == "search" })
}

func TestBacklogNumberSearchOpensTheTask(t *testing.T) {
	in := start(t, "macui-bnumber", "")
	for i := 1; i <= 12; i++ {
		title := "Item " + string(rune('a'+i))
		if i == 3 {
			title = "Follow up on 12"
		}
		in.cm("command", "run", "backlog.add", "title="+title, "status=todo")
	}
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the tasks are listed", func() bool { return in.snapshot().main().has("tasks#12") })
	in.pointer("click", "tasks#5")
	in.eventually("task 5 opens", func() bool { return in.snapshot().main().has("description") })
	in.pointer("click", "description")
	in.key("n", "o", "t", "e")

	in.pointer("click", "search")
	in.key("1", "2")
	in.eventually("#12 is selected and pinned above its text matches, focus stays in the search", func() bool {
		s := in.snapshot()
		w := s.main()
		pin, group := w.node("tasks#12"), w.node("tasks#group:todo")
		return in.selected("tasks#12") && pin != nil && group != nil && pin.Frame.Y < group.Frame.Y && w.has("tasks#3") && s.key() != nil && s.key().Focus == "search" && in.sdui("task-12") != nil
	})
	var task struct {
		Description string `json:"description"`
	}
	in.eventually("the edit in task 5 was saved", func() bool {
		in.json(&task, "command", "run", "backlog.get", "id=5")
		return task.Description == "note"
	})
	in.cm("command", "run", "backlog.reveal", "id=7")
	in.eventually("reveal clears the search hiding #7 and selects it", func() bool {
		return in.selected("tasks#7") && in.snapshot().main().has("tasks#1")
	})
	in.key("g", "c")
	in.eventually("the task list had the keys: gc moves them to the empty search", func() bool {
		s := in.snapshot()
		return s.key() != nil && s.key().Focus == "search" && in.sdui("search").Props["value"] == nil
	})
	in.key("9", "9")
	in.eventually("a number with no task says so", func() bool { return in.snapshot().main().has("tasks#missing") })
}

func TestBacklogFocusedNoteTakesAnAgentNote(t *testing.T) {
	in := start(t, "macui-bnote", "")
	in.cm("command", "run", "backlog.add", "title=Task", "status=todo")
	in.cm("command", "run", "backlog.update", "id=1", "note=mine")
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the task is listed", func() bool { return in.snapshot().main().has("tasks#1") })
	in.pointer("click", "tasks#1")
	in.eventually("the note shows", func() bool { return in.snapshot().main().has("note") })
	in.pointer("click", "note")
	in.eventually("the caret is in Note", func() bool { s := in.snapshot(); return s.key() != nil && s.key().Focus == "note" })

	in.cm("command", "run", "backlog.update", "--agent", "id=1", "note=from the agent")
	in.eventually("the agent's note reaches the form", func() bool {
		n := in.sdui("note")
		return n != nil && n.Props["value"] == "from the agent"
	})
	in.pointer("menu", "priority", "↓ Low")
	var task struct {
		Priority string `json:"priority"`
		Note     string `json:"note"`
	}
	in.eventually("the priority is saved", func() bool {
		in.json(&task, "command", "run", "backlog.get", "id=1")
		return task.Priority == "low"
	})
	assert.Equal(t, "from the agent", task.Note, "a focused Note the user didn't type in takes the agent's note, so the save keeps it")
}
