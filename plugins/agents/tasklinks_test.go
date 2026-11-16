package main_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/testutil"
	"clubmate/pkg/sdui"
)

const backlogManifest = `{"id":"backlog","name":"Backlog","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"backlog.list","title":"List","hidden":true},{"id":"backlog.reveal","title":"Reveal","hidden":true}]},"echoResults":{"backlog.list":{"tasks":[{"id":601,"title":"Number search"}],"removed":[],"cursor":"c1","delta":false},"backlog.reveal":{}}}`

var taskReply = map[string]any{"prompt": []map[string]any{{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Filed #601, see PR #3 and `#601`."}}}}}

type taskEntry struct {
	Kind      string            `json:"kind"`
	TaskLinks map[string]string `json:"taskLinks"`
}

func taskLinked(n sdui.Node) []sdui.Node {
	return slices.DeleteFunc(cardsOf(n, "message"), func(m sdui.Node) bool { return m.Props["taskLinks"] == nil })
}

func backlogLists(events []bus.Event) int {
	return len(slices.DeleteFunc(slices.Clone(events), func(e bus.Event) bool {
		var p struct {
			ID string `json:"id"`
		}
		return json.Unmarshal(e.Payload, &p) != nil || p.ID != "backlog.list"
	}))
}

func TestAgentMessageLinksOnlyBacklogTasks(t *testing.T) {
	t.Parallel()
	f := start(t, taskReply, testutil.EchoPluginDirAs(t, backlogManifest))
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "file #601")

	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(taskLinked(n)) == 1 })
	msg := taskLinked(main)[0]
	assert.Equal(t, "agent", msg.Props["role"])
	assert.Equal(t, map[string]any{"601": "Number search"}, msg.Props["taskLinks"])
	testutil.WaitEvents(t, f.h.Bus, "agents.changed", func(events []bus.Event) bool {
		return slices.ContainsFunc(events, func(e bus.Event) bool {
			var p struct {
				Session string `json:"session"`
				Kind    string `json:"kind"`
			}
			return json.Unmarshal(e.Payload, &p) == nil && p.Session == session && p.Kind == "links"
		})
	})
	testutil.WaitEvents(t, f.h.Bus, "echo.command", func(events []bus.Event) bool { return backlogLists(events) == 1 })
	testutil.Never(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "echo.command", 0, 1000)
		return err != nil || backlogLists(events) > 1
	}, time.Second)
	var transcript struct {
		Entries []taskEntry `json:"entries"`
	}
	require.NoError(t, f.run(t, workspace, "agents.transcript", map[string]any{"session": session}, &transcript))
	assert.Equal(t, []taskEntry{{Kind: "agent", TaskLinks: map[string]string{"601": "Number search"}}}, slices.DeleteFunc(transcript.Entries, func(e taskEntry) bool { return e.TaskLinks == nil }))

	f.h.Event(t, workspace, "agents.main", msg.ID, "openDoc", "/task/601")

	assert.JSONEq(t, `{"id":601}`, string(echoed(t, f, "backlog.reveal")))
}

func TestTaskLinksComeBackAfterRestartAndADeletedTaskIsQuiet(t *testing.T) {
	t.Parallel()
	gone := strings.Replace(backlogManifest, `"echoResults"`, `"echoErrors":{"backlog.reveal":"task not found: #601"},"echoResults"`, 1)
	f := start(t, taskReply, testutil.EchoPluginDirAs(t, gone))
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "file it")
	f.waitStatus(t, workspace, session, "done")
	f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool { return len(taskLinked(n)) == 1 })
	before := f.rev(t, workspace, "agents.main")

	f.restartAgents(t, workspace)

	main := f.h.WaitSlot(t, workspace, "agents.main", func(n sdui.Node) bool {
		return len(taskLinked(n)) == 1 && f.rev(t, workspace, "agents.main") > before
	})
	f.h.Event(t, workspace, "agents.main", taskLinked(main)[0].ID, "openDoc", "/task/601")
	echoed(t, f, "backlog.reveal")
}
