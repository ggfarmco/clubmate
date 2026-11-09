package macui_test

import (
	"os"
	"path/filepath"
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

func TestBacklogTagChipsKeepTheSelectionWhenUnfolded(t *testing.T) {
	in := start(t, "macui-tags", "")
	in.cm("command", "run", "backlog.add", "title=Tagged", "status=todo", "tags=a,b,c,d,e,f,g,h")
	in.focusSection("backlog", "backlog.sidebar")
	in.eventually("the tags fold after six", func() bool { return in.snapshot().main().has("tags#more") })
	in.pointer("click", "tags#tag:b")
	in.eventually("the clicked tag is selected", func() bool { return in.selected("tags#tag:b") })
	in.pointer("click", "tags#more")
	in.eventually("the tags unfold", func() bool { return in.snapshot().main().has("tags#tag:h") })
	assert.True(t, in.selected("tags#tag:b"))
	assert.False(t, in.selected("tags#less"))
}
