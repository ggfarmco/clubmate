package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/internal/claudectx"
)

func TestContextViewReplacesTranscriptAndFollowsTreeEvents(t *testing.T) {
	t.Parallel()
	v := transcriptView{Session: record{ID: "agt_1", Worktree: "/r", Agent: agentClaude, AgentSessionID: "sid", Status: statusDone}}
	v.Entries = []*entry{{Kind: entryAgent, ID: "m1", Turn: 1, Text: "hi"}}
	data := claudectx.Context{
		Instructions: []claudectx.File{{Path: "/r/CLAUDE.md", Kind: "Project", Content: "agents only when asked"}},
		Skills:       []claudectx.Entry{{Name: "ui", Text: "Build views", Uses: 2}, {Name: "deploy", Text: "Ship it"}},
		NeedsAuth:    []string{"slack"},
	}
	st := &sessionUI{}
	render := func() sdui.Node {
		sc := screen{View: v, Composer: "agt_1/composer/0"}
		if st.context {
			sc.Context = &contextScreen{Data: data, Selected: st.contextSelected, Open: st.contextOpen, Worktree: "/r"}
		}
		tree := mainView(sc)
		require.NoError(t, internalsdui.Validate(tree))
		return tree
	}
	button := func(tree sdui.Node) sdui.Node {
		for _, b := range nodesOf(tree, "button") {
			if b.Props["action"] == actContext {
				return b
			}
		}
		t.Fatal("no Context button")
		return sdui.Node{}
	}

	assert.Equal(t, "Context", button(render()).Props["label"])
	assert.Len(t, nodesOf(render(), "transcript"), 1)

	st.contextEvent(actContext, "")
	tree := render()
	assert.Equal(t, "Transcript", button(tree).Props["label"])
	assert.Empty(t, nodesOf(tree, "transcript"))
	ctxTree := nodesOf(tree, "tree")
	require.Len(t, ctxTree, 1)
	assert.Equal(t, "agt_1/context", ctxTree[0].ID)
	groups := ctxTree[0].Props["items"].([]sdui.TreeItem)
	assert.Equal(t, sdui.TreeItem{ID: "i:/r/CLAUDE.md", Label: "CLAUDE.md", Detail: "project", Truncate: "middle"}, groups[0].Children[0])
	assert.Equal(t, "1 of 2 used", groups[1].Detail)
	assert.Empty(t, groups[1].Children)
	assert.Equal(t, "agents only when asked", nodesOf(tree, "markdown")[0].Props["text"])

	st.contextEvent(actExpand, "g:skills")
	st.contextEvent(actSelect, "s:deploy")
	tree = render()
	groups = nodesOf(tree, "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Equal(t, []sdui.TreeItem{
		{ID: "s:ui", Label: "ui", Detail: "×2"},
		{ID: "s:deploy", Label: "deploy", Muted: true},
	}, groups[1].Children)
	assert.Equal(t, "Ship it", nodesOf(tree, "markdown")[0].Props["text"])

	st.contextEvent(actExpand, "g:mcp")
	groups = nodesOf(render(), "tree")[0].Props["items"].([]sdui.TreeItem)
	assert.Equal(t, sdui.TreeItem{ID: "mx:slack", Label: "slack", Detail: "needs sign-in", Icon: "exclamationmark.triangle", Tone: "warning"}, groups[2].Children[0])
}

func TestContextEventsRouteByNodeNotSidebar(t *testing.T) {
	t.Parallel()
	for node, want := range map[string]bool{"agt_1/context": true, "agt_1/context/toggle": true, "sessions": false, "agt_1/contextual": false} {
		_, ok := isContextEvent(pluginsdk.UIEvent{NodeID: node, Action: actExpand})
		assert.Equal(t, want, ok, node)
	}
}

func TestDisplayPathKeepsTheEndThatDiffers(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ".worktrees/feat/x/CLAUDE.md", displayPath("/r/.worktrees/feat/x/CLAUDE.md", "/r"))
	assert.Equal(t, "/…/memory/MEMORY.md", displayPath("/a/b/c/projects/memory/MEMORY.md", "/r"))
	assert.Equal(t, "/etc/CLAUDE.md", displayPath("/etc/CLAUDE.md", "/r"))
}
