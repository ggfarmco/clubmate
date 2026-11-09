package macui_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGraphHoverAndTrustSheetKeys(t *testing.T) {
	in := start(t, "macui-graphs", "")
	in.focusSection("graphs", "graphs.sidebar")
	in.eventually("the built-in untrusted Review gate is listed", func() bool { return in.snapshot().main().has("runs#graph:review-gate") })
	hovered := func(id string) func() bool {
		return func() bool {
			e := in.snapshot().main().node(id)
			return e != nil && strings.Contains(e.Role, "hovered")
		}
	}
	in.pointer("hover", "runs#graph:pr-walkthrough")
	in.eventually("the hovered row tints", hovered("runs#graph:pr-walkthrough"))
	in.pointer("hover", "runs#help")
	in.eventually("the tint follows the pointer", hovered("runs#help"))
	assert.False(t, hovered("runs#graph:pr-walkthrough")())

	in.pointer("click", "runs#graph:review-gate")
	in.eventually("the graph is selected", func() bool { return in.snapshot().main().has("graph:review-gate/trust") })

	sheet := func() bool { s := in.snapshot(); return s.kind("sheet") != nil && s.kind("sheet").Key }
	gone := func() bool { return in.snapshot().kind("sheet") == nil }
	untrusted := func() { assert.True(t, in.snapshot().main().has("graph:review-gate/trust")) }

	in.key("cmd+return")
	in.eventually("the Trust sheet is the key window", sheet)
	in.key("esc")
	in.eventually("esc closes the sheet", gone)
	untrusted()

	in.key("cmd+return")
	in.eventually("the Trust sheet opens again", sheet)
	in.key("return")
	in.eventually("return presses the sheet's default Cancel", gone)
	untrusted()

	in.key("cmd+return")
	in.eventually("the Trust sheet opens a third time", sheet)
	in.key("cmd+return")
	in.eventually("cmd+return trusts the graph and Run shows", func() bool {
		w := in.snapshot().main()
		return !w.has("graph:review-gate/trust") && w.has("graph:review-gate/submit")
	})
}
