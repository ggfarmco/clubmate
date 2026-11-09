package macui_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDragOnTheDividerResizesTheSidebar(t *testing.T) {
	in := start(t, "macui-drag", "")
	in.focusSection("agents", "agents.sidebar")
	divider := func() float64 {
		n := in.snapshot().main().node("agents.sidebar/resize")
		require.NotNil(t, n)
		return n.Frame.X + n.Frame.Width/2
	}
	before := divider()
	in.pointer("drag", "agents.sidebar/resize", "worktree.pill")
	in.eventually("the sidebar shrinks from 248 to its 200 pt minimum", func() bool { return math.Abs(before-48-divider()) < 1 })
}
