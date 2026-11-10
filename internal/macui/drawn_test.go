package macui_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDrawnTriggerLanesKeys(t *testing.T) {
	in := start(t, "macui-drawn-triggers", "")
	var added struct {
		ID string `json:"id"`
	}
	in.json(&added, "command", "run", "triggers.add", "name=Ping", "on=event.ping", "command=agents.sessions")
	for range 3 {
		in.cm("command", "run", "triggers.emit", "name=ping")
		time.Sleep(1100 * time.Millisecond)
	}
	in.focusSection("triggers", "triggers.sidebar")
	in.pointer("click", "triggers#"+added.ID)
	lanes := added.ID + "/lanes#"
	in.eventually("the lanes draw three runs", func() bool { return in.snapshot().main().has(lanes + "5") })

	assert.False(t, in.snapshot().main().has("history"), "the runs table starts folded")
	in.pointer("click", added.ID+"/runJson")
	in.pointer("click", lanes+"1")
	in.eventually("the clicked dot has the cursor", func() bool { return hasRole(in.snapshot().main().node(lanes+"1"), "selected") })
	in.eventually("opening a run unfolds the runs table", func() bool { return in.snapshot().main().has("history") })
	shows := func(move string) func() bool {
		return func() bool {
			w := in.snapshot().main()
			dot, doc := w.node(lanes+move), w.node("runJson/text")
			return dot != nil && doc != nil && strings.Contains(doc.Label, "T"+strings.Fields(dot.Label)[2])
		}
	}
	in.eventually("the clicked run is open", shows("1"))

	assert.Equal(t, []string{"monitor"}, in.key("right"))
	in.eventually("right moves the cursor to the next run", func() bool { return hasRole(in.snapshot().main().node(lanes+"2"), "selected") })
	assert.True(t, shows("1")(), "the cursor moves without opening")
	assert.Equal(t, []string{"monitor"}, in.key("return"))
	in.eventually("return opens the run under the cursor", shows("2"))
	in.key("left")
	in.eventually("left moves back", func() bool { return hasRole(in.snapshot().main().node(lanes+"1"), "selected") })
}

func TestDrawnCronRunstrip(t *testing.T) {
	app := start(t, "macui-drawn-cron", "")
	jobs := make([]string, 2)
	for i, name := range []string{"Alpha", "Beta"} {
		var job struct {
			ID string `json:"id"`
		}
		app.json(&job, "command", "run", "cron.add", "name="+name, "command=agents.sessions", "every=1h")
		jobs[i] = job.ID
	}
	for range 3 {
		for _, id := range jobs {
			app.cm("command", "run", "cron.run", "id="+id)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	for _, id := range jobs {
		app.cm("command", "run", "cron.pause", "id="+id)
	}
	app.focusSection("cron", "cron.sidebar")
	alpha, beta := jobs[0]+"/strip", jobs[1]+"/strip"
	app.pointer("click", "jobs#"+jobs[0])
	app.eventually("Alpha's strip draws its runs", func() bool { return app.snapshot().main().has(alpha + "#2") })

	t.Run("click a run, then left and right", func(t *testing.T) {
		in := app.at(t)
		selected := func(run string) func() bool {
			return func() bool { return hasRole(in.snapshot().main().node(alpha+"#"+run), "selected") }
		}
		assert.False(t, in.snapshot().main().has("history"), "the runs table starts folded")
		in.pointer("click", alpha+"#1")
		in.eventually("the clicked run is selected", selected("1"))
		in.eventually("selecting a run unfolds the runs table", func() bool { return in.snapshot().main().has("history") })
		assert.True(t, hasRole(in.snapshot().main().node(alpha+"#1"), "focused"), "the strip takes the keys")
		assert.Equal(t, []string{"monitor"}, in.key("left"))
		in.eventually("left selects the older run", selected("0"))
		assert.Equal(t, []string{"monitor"}, in.key("right"))
		in.eventually("right selects the newer run", selected("1"))
	})
	t.Run("each job keeps its own fold", func(t *testing.T) {
		in := app.at(t)
		in.pointer("click", alpha, "--at", "30,10")
		in.eventually("Alpha's runs fold", func() bool { w := in.snapshot().main(); return w.has(alpha) && !w.has(alpha+"#0") })
		in.pointer("click", "jobs#"+jobs[1])
		in.eventually("Beta's runs stay open", func() bool { return in.snapshot().main().has(beta + "#0") })
		in.pointer("click", "jobs#"+jobs[0])
		in.eventually("Alpha comes back folded", func() bool { w := in.snapshot().main(); return w.has(alpha) && !w.has(alpha+"#0") })
	})
}

func TestDrawnGraphRun(t *testing.T) {
	report := make([]string, 60)
	for i := range report {
		report[i] = fmt.Sprintf("Line %d of the coder's report.", i+1)
	}
	script, err := json.Marshal(map[string]any{"prompt": []any{map[string]any{
		"match":  "Implement",
		"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": strings.Join(report, "\n\n")}},
	}}})
	require.NoError(t, err)
	app := start(t, "macui-drawn-graph", string(script))
	graph, err := os.ReadFile(filepath.Join(root, "plugins", "graphs", "testdata", ".clubmate", "graphs", "review-loop.json"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(app.repo, ".clubmate", "graphs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(app.repo, ".clubmate", "graphs", "review-loop.json"), graph, 0o600))
	var run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Moves  []struct {
			Ended string `json:"ended"`
		} `json:"moves"`
		Nodes []struct {
			ID      string `json:"id"`
			Session string `json:"session"`
		} `json:"nodes"`
	}
	app.json(&run, "command", "run", "graphs.run", "graph=review-loop", "input=add a readme")
	app.eventually("the coder and the reviewer end their turns", func() bool {
		app.json(&run, "command", "run", "graphs.show", "id="+run.ID)
		return len(run.Moves) == 2 && run.Moves[1].Ended != ""
	})
	require.Equal(t, "running", run.Status)

	t.Run("flow edge: hover, collapsed group, click", func(t *testing.T) {
		in := app.at(t)
		var list struct {
			Triggers []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"triggers"`
		}
		in.json(&list, "command", "run", "triggers.list")
		named := func(name string) string {
			i := slices.IndexFunc(list.Triggers, func(t struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}) bool {
				return t.Name == "Review loop: "+name
			})
			require.GreaterOrEqual(t, i, 0, name)
			return list.Triggers[i].ID
		}
		done, changes := named("Coder done → Reviewer"), named("Reviewer says changes → Coder")
		in.focusSection("triggers", "triggers.sidebar")
		in.pointer("click", "triggers#help")
		in.eventually("the graph run group starts folded", func() bool {
			w := in.snapshot().main()
			return w.has("triggers#graph:"+run.ID) && !w.has("triggers#"+done)
		})
		in.pointer("click", "triggers#graph:"+run.ID, "--at", "14,10")
		in.eventually("the graph run group unfolds", func() bool { return in.snapshot().main().has("triggers#" + done) })
		in.pointer("click", "triggers#"+done)
		var edge string
		in.eventually("the flow draws the edge to the changes trigger", func() bool {
			e := in.snapshot().main().find(func(e element) bool {
				return strings.HasPrefix(e.ID, done+"/flow#edge/") && e.Label == "Reviewer says changes → Coder"
			})
			if e != nil {
				edge = e.ID
			}
			return e != nil
		})

		in.pointer("hover", edge)
		in.eventually("a tooltip box shows the edge's text", func() bool {
			w := in.snapshot().main()
			tip := w.node(done + "/flow/tooltip")
			return tip != nil && tip.Label == "Reviewer says changes → Coder" && hasRole(w.node(edge), "hovered")
		})

		in.pointer("click", "triggers#graph:"+run.ID, "--at", "14,10")
		in.eventually("the graph run group folds", func() bool { return !in.snapshot().main().has("triggers#" + changes) })
		in.pointer("click", edge)
		in.eventually("the edge's trigger opens, its group unfolded and the trigger selected", func() bool {
			return hasRole(in.snapshot().main().node("triggers#"+changes), "selected")
		})
	})

	diagram := run.ID + "/graph"
	coder := run.Nodes[slices.IndexFunc(run.Nodes, func(n struct {
		ID      string `json:"id"`
		Session string `json:"session"`
	}) bool {
		return n.ID == "coder"
	})].Session
	openRun := func(in *instance) {
		in.focusSection("graphs", "graphs.sidebar")
		in.pointer("click", "runs#"+run.ID)
		in.eventually("the run's diagram shows", func() bool { return in.snapshot().main().has(diagram + "#card/coder") })
	}

	t.Run("cmd+scroll zooms the diagram, not over chrome", func(t *testing.T) {
		in := app.at(t)
		openRun(in)
		zoom := func() string { return in.snapshot().main().node(diagram + "/zoom").Label }
		before := zoom()
		in.pointer("scroll", diagram+"#card/coder", "--dy=1")
		in.pointer("scroll", diagram+"/zoom", "--dy=-1", "--keys", "cmd")
		assert.Equal(t, before, zoom(), "neither a plain wheel nor ⌘+wheel over the zoom bar zooms")
		in.pointer("scroll", diagram+"#card/coder", "--dy=-1", "--keys", "cmd")
		in.eventually("cmd+wheel up over the diagram zooms in", func() bool { return zoom() != before })
		in.pointer("scroll", diagram+"#card/coder", "--dy=1", "--keys", "ctrl")
		in.eventually("ctrl+wheel down zooms back", func() bool { return zoom() == before })
	})

	t.Run("card, then a History move, scrolls the transcript to that turn", func(t *testing.T) {
		in := app.at(t)
		openRun(in)
		in.pointer("click", diagram+"#card/coder")
		in.eventually("the coder's turn shows beside the diagram", func() bool { return in.snapshot().main().has(run.ID + "/openTurn") })
		in.key("h")
		in.eventually("History opens", func() bool { return in.snapshot().main().has(diagram + "#0") })
		in.pointer("click", diagram+"#0")
		var msg, transcript *element
		in.eventually("Agents shows the coder's transcript", func() bool {
			w := in.snapshot().main()
			msg = w.find(func(e element) bool { return strings.HasPrefix(e.ID, coder+"/msg/user/") })
			transcript = w.find(func(e element) bool { return e.Role == "transcript" })
			return msg != nil && transcript != nil && msg.Frame.Y >= transcript.Frame.Y && msg.Frame.Y < transcript.Frame.Y+60
		})
		below := in.snapshot().main().find(func(e element) bool {
			return strings.HasPrefix(e.ID, coder+"/msg/") && e.Frame.Y+e.Frame.Height > transcript.Frame.Y+transcript.Frame.Height
		})
		require.NotNil(t, below, "the turn is longer than the pane, so the transcript scrolled away from its bottom")
		top := msg.Frame.Y
		in.pointer("scroll", transcript.ID, "--dy=10")
		in.eventually("a plain wheel scrolls the transcript", func() bool {
			m := in.snapshot().main().node(msg.ID)
			return m == nil || m.Frame.Y < top
		})
	})
}

func hasRole(e *element, role string) bool {
	return e != nil && slices.Contains(strings.Fields(e.Role), role)
}
