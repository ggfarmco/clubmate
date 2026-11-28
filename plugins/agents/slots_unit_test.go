package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
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
