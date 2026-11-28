package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
	"clubmate/pkg/sdui"
)

type fakeLoad struct {
	cpu  float64
	mem  float64
	sims []simDevice
}

func (f fakeLoad) CPU() float64 { return f.cpu }

func (f fakeLoad) MemFreeGB() float64 { return f.mem }

func (f fakeLoad) BootedSims() []simDevice { return f.sims }

func TestGrantRuleCountCPUMemorySettle(t *testing.T) {
	test := slotKind{ID: kindTest, Max: 3, CPU: 80, Settle: seconds(20 * time.Second), Count: countProcesses}
	sim := slotKind{ID: kindSim, Max: 2, CPU: 80, MemFreeGB: 4, Settle: seconds(20 * time.Second), Count: countSimulators}
	calm := fakeLoad{cpu: 40, mem: 16}
	for _, c := range []struct {
		kind  slotKind
		inUse int
		load  fakeLoad
		since time.Duration
		ok    bool
		why   string
	}{
		{test, 3, calm, time.Hour, false, "3 of 3 in use"},
		{test, 1, fakeLoad{cpu: 91, mem: 16}, time.Hour, false, "CPU 91 % > 80 %"},
		{sim, 1, fakeLoad{cpu: 40, mem: 2.1}, time.Hour, false, "2.1 GB free < 4 GB"},
		{test, 1, calm, 12 * time.Second, false, "settle pause, 12s of 20s"},
		{test, 0, fakeLoad{cpu: 99, mem: 0}, 0, true, ""},
		{test, 2, calm, time.Hour, true, ""},
	} {
		ok, why := grantable(c.kind, c.inUse, c.load, c.since)
		assert.Equal(t, c.ok, ok, why)
		assert.Equal(t, c.why, why)
	}
}

func TestSlotConfigRefusesBadValues(t *testing.T) {
	good := defaultSlotConfig(18)
	_, err := good.validate()
	require.NoError(t, err)
	assert.Equal(t, 3, good.Kinds[1].Max)
	for name, edit := range map[string]func(*slotConfig){
		"pattern": func(c *slotConfig) { c.Kinds[0].Patterns = []string{"("} },
		"max":     func(c *slotConfig) { c.Kinds[0].Max = 0 },
		"cpu":     func(c *slotConfig) { c.Kinds[0].CPU = 120 },
		"memory":  func(c *slotConfig) { c.Kinds[0].MemFreeGB = -1 },
		"id":      func(c *slotConfig) { c.Kinds[1].ID = c.Kinds[0].ID },
		"wait":    func(c *slotConfig) { c.MaxWait = seconds(time.Second) },
		"count":   func(c *slotConfig) { c.Kinds[0].Count = "threads" },
	} {
		cfg := defaultSlotConfig(18).clone()
		edit(&cfg)
		_, err := cfg.validate()
		assert.Error(t, err, name)
	}
	cfg := defaultSlotConfig(18).clone()
	cfg.Kinds[0].Patterns = []string{"("}
	_, err = cfg.validate()
	assert.ErrorContains(t, err, "missing closing )")
}

func TestStepHolderLivesWithItsProcess(t *testing.T) {
	a := &app{}
	b := &slotBoard{owned: map[string]bool{"owned": true}}
	start, err := proc.StartTime(os.Getpid())
	require.NoError(t, err)
	assert.False(t, a.holderGone(b, slotHolder{ID: "live", Step: true, PID: os.Getpid(), PIDStart: start}))
	assert.True(t, a.holderGone(b, slotHolder{ID: "reused", Step: true, PID: os.Getpid(), PIDStart: start.Add(-time.Hour)}))
	assert.False(t, a.holderGone(b, slotHolder{ID: "owned", Step: true}))
	assert.True(t, a.holderGone(b, slotHolder{ID: "left", Step: true}))
}

func TestSlotPatternsMatchDefaults(t *testing.T) {
	cfg := defaultSlotConfig(18)
	res, err := cfg.validate()
	require.NoError(t, err)
	b := &slotBoard{cfg: cfg, res: res}
	for command, kinds := range map[string][]string{
		"make test":                                        {kindTest},
		"go test ./plugins/agents/ -run Slot":              {kindTest},
		"xcodebuild -scheme Companion test":                {kindTest},
		"scripts/sim.sh up cm-a":                           {kindSim},
		"scripts/sim.sh up cm-a && make companion-ui-test": {kindSim},
		"make app":                             nil,
		"xcodebuild -destination 'id=X' build": nil,
	} {
		assert.Equal(t, kinds, b.match(command), command)
	}
	assert.Equal(t, "cm-a", simTarget("scripts/sim.sh up cm-a && echo"))
	assert.Equal(t, "ABC", simTarget("xcrun simctl boot 'ABC'"))
}

func TestSlotFormOpensInPlaceWithTheNowBlockAndFooter(t *testing.T) {
	a := &app{}
	a.resources.draft = &slotDraft{orig: kindTest, kind: slotKind{ID: kindTest, Label: "Tests", Max: 1, Count: countProcesses}}
	now := time.Now()
	v := slotsView{CPU: 34, Kinds: []slotKindView{{slotKind: slotKind{ID: kindTest, Max: 1}, InUse: 1,
		Holders: []slotHolderView{{ID: "h", Title: "Run the Go tests of the agents plugin with the race detector", Command: "go test -race ./plugins/agents/...", Since: now.Add(-6 * time.Minute)}},
		Waiters: []slotWaiterView{{Request: "w", Title: "Companion UI tests", Command: "xcodebuild -scheme Companion test", Place: 1, Since: now.Add(-3 * time.Minute)}}}}}

	sheet, ok := a.slotSheet(v)
	require.True(t, ok)
	assert.Empty(t, sheet.Props["title"])
	texts := textsOf(sheet)
	for _, want := range []string{"Now", "1 of 1 in use · CPU 34 %", "Holding", "6 min", "Waiting", "3 min", slotPattNote} {
		assert.Contains(t, texts, want)
	}
	release := findNode(sheet, actSlotRelease+"/h")
	require.NotNil(t, release)
	footer := sheet.Children[len(sheet.Children)-1]
	assert.Equal(t, []any{"Remove…", "Save"}, []any{footer.Children[0].Props["label"], footer.Children[len(footer.Children)-1].Props["label"]})
	commands := slices.DeleteFunc(nodesOf(sheet, "text"), func(n sdui.Node) bool { return n.Props["style"] != "mono" })
	require.Len(t, commands, 2)
	assert.Equal(t, sdui.TruncateMiddle, commands[0].Props["truncate"])
}

func TestWaiterWhyChangeAloneIsNotAStateChange(t *testing.T) {
	test := slotKind{ID: kindTest, Max: 3, CPU: 80, Count: countProcesses}
	load := &fakeLoad{cpu: 90, mem: 32}
	b := &slotBoard{
		cfg:   slotConfig{Enabled: true, Kinds: []slotKind{test}},
		host:  load,
		owned: map[string]bool{},
		st: slotState{
			Holders: []slotHolder{{ID: "h:test", Kind: kindTest}},
			Waiters: []slotWaiter{{ID: "w", Kind: kindTest}},
		},
	}
	assert.False(t, b.grantLocked(time.Now()))
	assert.Equal(t, "CPU 90 % > 80 %", b.st.Waiters[0].Why)
	assert.True(t, b.whyMoved)

	b.whyMoved = false
	load.cpu = 85
	assert.False(t, b.grantLocked(time.Now()))
	assert.Equal(t, "CPU 85 % > 80 %", b.st.Waiters[0].Why)
	assert.True(t, b.whyMoved)
}

func TestUnreadableSlotStateIsKeptUntilItIsRead(t *testing.T) {
	a := &app{}
	held := slotHolder{ID: "ss_1:test", Request: "ss_1", Kind: kindTest, Step: true}
	b := &a.resources
	b.cfg = slotConfig{Enabled: true, Kinds: []slotKind{{ID: kindTest, Max: 1, Count: countProcesses}}}
	b.host = fakeLoad{mem: 32}
	b.owned = map[string]bool{}
	b.st = slotState{Holders: []slotHolder{held}, Waiters: []slotWaiter{{ID: "w", Kind: kindTest}}}

	a.adoptSlotState(t.Context(), slotState{}, false, errors.New("connection closed"))
	assert.True(t, b.unread)
	assert.Equal(t, []slotHolder{held}, b.st.Holders)
	assert.False(t, b.grantLocked(time.Now()))
	a.saveSlots(t.Context())
}

func TestSlotCardShowsTheWaitAndTheDenial(t *testing.T) {
	tool := func(id, status, text string) *entry {
		content, err := json.Marshal([]map[string]any{{"type": "content", "content": map[string]any{"type": "text", "text": text}}})
		require.NoError(t, err)
		return &entry{Kind: entryTool, ID: id, Tool: map[string]json.RawMessage{"kind": json.RawMessage(`"execute"`), "status": json.RawMessage(`"` + status + `"`), "content": content}}
	}
	now := time.Now()
	w := &slotWaitView{Kind: kindTest, Place: 2, Words: "waits for test slot", Calls: []string{"c1"}, Since: now.Add(-3 * time.Minute), MaxWait: time.Hour,
		Held: &slotHeldView{Session: "agt_one", Workspace: "prj_a", Title: "One", Command: "make test", Since: now.Add(-6 * time.Minute)}}
	waits := tool("c1", "pending", "")
	n := slotCard(toolCard("t1", "", waits, false, false, false, ""), waits, w, "prj_a", now)
	assert.Equal(t, []any{"pending", "waits for test slot", "hourglass", "Waits for test slot · 1 ahead", "1 ahead · waited 3 min of 60"}, []any{n.Props["status"], n.Props["statusWord"], n.Props["statusIcon"], n.Props["statusLine"], n.Props["slotWait"]})
	assert.Equal(t, map[string]any{"title": "One", "session": "agt_one", "rest": "make test · 6 min"}, n.Props["slotHeld"])
	assert.NotContains(t, slotCard(toolCard("t1", "", waits, false, false, false, ""), waits, w, "prj_b", now).Props["slotHeld"], "session")

	denied := tool("c2", "failed", "[Clubmate slots] no test slot in 60 min: 1 of 1 in use by \"One\". Run it again later, or end your turn and tell the user.")
	n = slotCard(toolCard("t2", "", denied, false, false, false, ""), denied, w, "prj_a", now)
	assert.Equal(t, []any{"not run · no test slot in 60 min", "Not run · no test slot in 60 min"}, []any{n.Props["statusWord"], n.Props["statusLine"]})
	assert.Nil(t, n.Props["statusIcon"])

	other := tool("c3", "failed", "exit 1")
	assert.Nil(t, slotCard(toolCard("t3", "", other, false, false, false, ""), other, w, "prj_a", now).Props["statusWord"])
}

func TestSlotWaitRowKeepsItsWordAndNamesTheLineInTheTooltip(t *testing.T) {
	l := &landingView{Words: "test slot", Glyph: slotWaitGlyph, MacLine: "waits for test slot · next in line · 1 of 1 in use · 3 min"}
	item := sessionItem(sessionRow{Rec: record{ID: "agt_a", Title: "Companion UI tests", Status: statusRunning}, Landing: l}, 0, time.Now())
	assert.Equal(t, []string{"test slot", "hourglass", "", "Companion UI tests · waits for test slot · next in line · 1 of 1 in use · 3 min"}, []string{item.Detail, item.Icon, item.Tone, item.Help})
}

func TestCachedToolCardPicksUpASlotWait(t *testing.T) {
	s := newSession(record{}, nil, true, nil)
	s.running = true
	s.applyTool(true, 1, rawJSON(t, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "c1", "title": "make test", "kind": "execute", "status": "pending"}))
	v := s.view(0)
	require.Len(t, v.Entries, 1)
	assert.Nil(t, entryNode(screen{View: v}, v.Entries[0], "").node.Props["statusWord"])
	w := &slotWaitView{Kind: kindTest, Place: 1, Words: "waits for test slot", Calls: []string{"c1"}}
	assert.Equal(t, "waits for test slot", entryNode(screen{View: v, Slot: w}, v.Entries[0], "").node.Props["statusWord"])
}

func TestSlotRowWordFitsNineCharacters(t *testing.T) {
	assert.Equal(t, []string{"test slot", "sim slot", "android", "integration"}, []string{slotRowWordOf("test"), slotRowWordOf("sim"), slotRowWordOf("android"), slotRowWordOf("integration")})
}

func TestSlotLoopRefreshesWhenAShownMinuteMoves(t *testing.T) {
	t0 := time.Now()
	b := &slotBoard{}
	assert.False(t, b.minuteMovedLocked(t0))
	b.st.Waiters = []slotWaiter{{ID: "w", Kind: kindTest, Since: t0}}
	b.st.Holders = []slotHolder{{ID: "h", Kind: kindTest, Since: t0.Add(-30 * time.Second)}}
	assert.True(t, b.minuteMovedLocked(t0.Add(10*time.Second)))
	assert.False(t, b.minuteMovedLocked(t0.Add(20*time.Second)))
	assert.True(t, b.minuteMovedLocked(t0.Add(40*time.Second)))
	assert.True(t, b.minuteMovedLocked(t0.Add(61*time.Second)))
	assert.False(t, b.minuteMovedLocked(t0.Add(63*time.Second)))
}
