package macui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const capGraph = `{"title": "Cap", "nodes": [
  {"id": "tests", "role": "Tests", "start": true, "prompt": "Run the tests"},
  {"id": "coder", "role": "Coder", "prompt": "Fix them"}],
 "edges": [{"from": "tests", "to": "coder", "on": "done", "maxFires": 1}, {"from": "coder", "to": "tests", "on": "done"}]}`

func TestInboxGraphPinOpensTheRun(t *testing.T) {
	in := start(t, "macui-inbox", "")
	dir := filepath.Join(in.repo, ".clubmate", "graphs")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cap.json"), []byte(capGraph), 0o600))
	var run struct {
		ID string `json:"id"`
	}
	in.json(&run, "command", "run", "graphs.run", "graph=cap", "input=go")
	require.NotEmpty(t, run.ID)
	in.eventually("the edge back to the coder is held", func() bool {
		var shown struct {
			Held []any `json:"held"`
		}
		in.json(&shown, "command", "run", "graphs.show", "id="+run.ID)
		return len(shown.Held) > 0
	})

	in.cm("ui", "present", "inbox")
	pin := "inbox.pinned#" + run.ID + "/cap-0"
	in.eventually("the run's cap is pinned in the inbox", func() bool {
		p := in.snapshot().kind("popover")
		return p != nil && p.node(pin) != nil && strings.HasPrefix(p.node(pin).Label, "Question · Tests → Coder")
	})
	in.pointer("click", pin)
	in.eventually("Graphs opens on that run", func() bool {
		s := in.snapshot()
		return s.kind("popover") == nil && in.selected("runs#"+run.ID)
	})
}
