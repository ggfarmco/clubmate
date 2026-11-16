package macui_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphHoverAndTrustSheetKeys(t *testing.T) {
	in := start(t, "macui-graphs", "")
	in.focusSection("agents", "agents.sidebar")
	in.cm("command", "run", "agents.graph-start")
	in.eventually("the built-in untrusted Review gate is listed in the dock", func() bool { return in.snapshot().main().has("sessions#graph:review-gate") })
	hovered := func(id string) func() bool {
		return func() bool {
			e := in.snapshot().main().node(id)
			return e != nil && strings.Contains(e.Role, "hovered")
		}
	}
	in.pointer("hover", "sessions#graph:pr-walkthrough")
	in.eventually("the hovered row tints", hovered("sessions#graph:pr-walkthrough"))
	in.pointer("hover", "sessions#graph-help")
	in.eventually("the tint follows the pointer", hovered("sessions#graph-help"))
	assert.False(t, hovered("sessions#graph:pr-walkthrough")())

	in.pointer("click", "sessions#graph:review-gate")
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

const stepGraph = `{"title":"Step graph","nodes":[{"id":"coder","role":"Coder","start":true,"prompt":"go"},{"id":"tick","role":"Tick","run":"echo ticked","end":["done"]}],"edges":[{"from":"coder","to":"tick","on":"done"}]}`

func TestGraphRunPageInAgents(t *testing.T) {
	in := start(t, "macui-run-page", "")
	dir := filepath.Join(in.repo, ".clubmate", "graphs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "step.json"), []byte(stepGraph), 0o600))
	in.cm("command", "run", "agents.graph-trust", "graph=step")
	var session struct {
		Session string `json:"session"`
	}
	in.json(&session, "command", "run", "agents.new", "title=Solo")
	var run struct {
		ID string `json:"id"`
	}
	in.json(&run, "command", "run", "agents.graph-run", "graph=step", "input=go")
	waiting := func() bool {
		var shown struct {
			Nodes []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"nodes"`
		}
		in.json(&shown, "command", "run", "agents.graph-show", "id="+run.ID)
		return slices.ContainsFunc(shown.Nodes, func(n struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}) bool {
			return n.ID == "tick" && n.Status == "waiting"
		})
	}
	in.eventually("the step waits for approval", waiting)
	in.focusSection("agents", "agents.sidebar")
	in.pointer("click", "sessions#"+session.Session)
	in.eventually("the session is selected", func() bool { return in.selected("sessions#" + session.Session) })
	row := "sessions#run:" + run.ID
	cursor := func() bool {
		e := in.snapshot().main().node(row)
		return e != nil && strings.Contains(e.Role, "focused")
	}
	for _, step := range []string{"k", "k", "k", "k", "j", "j", "j", "j", "j", "j", "j", "j"} {
		if cursor() {
			break
		}
		in.key(step)
	}
	require.True(t, cursor(), "the keyboard cursor reaches the run row")
	in.key("return")
	in.eventually("return opens the run page", func() bool { return in.snapshot().main().has("back") })
	in.key("opt+1")
	in.eventually("opt+1 answers the step card", func() bool { return !waiting() })
	in.key("cmd+[")
	in.eventually("cmd+[ goes back to the session", func() bool { return in.selected("sessions#" + session.Session) })
}
