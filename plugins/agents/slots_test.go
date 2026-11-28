package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/proc"
	"clubmate/internal/testutil"
)

type slotAnswer struct {
	State   string `json:"state"`
	Request string `json:"request"`
	Kind    string `json:"kind"`
	Place   int    `json:"place"`
	Of      int    `json:"of"`
	Why     string `json:"why"`
	Text    string `json:"text"`
}

type slotsView struct {
	Enabled bool `json:"enabled"`
	Kinds   []struct {
		ID      string `json:"id"`
		InUse   int    `json:"inUse"`
		Line    string `json:"line"`
		Holders []struct {
			ID      string `json:"id"`
			Session string `json:"session"`
		} `json:"holders"`
		Waiters []struct {
			Request string `json:"request"`
			Session string `json:"session"`
			Place   int    `json:"place"`
		} `json:"waiters"`
	} `json:"kinds"`
}

var slotTags = []string{"tagone", "tagtwo", "tagthree"}

func slotScript(dir string) map[string]any {
	var steps []map[string]any
	for _, tag := range slotTags {
		call := tag + "-call"
		steps = append(steps,
			map[string]any{"match": tag, "update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": call, "title": "make test", "kind": "execute", "status": "pending", "rawInput": map[string]any{"command": "make test"}}},
			map[string]any{"match": tag, "wait": filepath.Join(dir, tag)},
			map[string]any{"match": tag, "update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": call, "status": "completed"}},
		)
	}
	steps = append(steps, map[string]any{"wait": filepath.Join(dir, "end")})
	return map[string]any{"prompt": steps}
}

func slotEnv(t *testing.T, extra ...string) ([]string, string) {
	t.Helper()
	host := filepath.Join(t.TempDir(), "host.json")
	write(t, host, `{"cpu": 10, "memFreeGB": 32}`)
	return append([]string{"CLUBMATE_AGENTS_SLOT_POLL=300ms", "CLUBMATE_AGENTS_SLOT_SILENT=2s", "CLUBMATE_AGENTS_SLOT_TICK=100ms", "CLUBMATE_AGENTS_SLOT_HOST=" + host}, extra...), host
}

func testKind(max int) map[string]any {
	return map[string]any{"id": "test", "label": "Tests", "max": max, "cpu": 0, "memFreeGB": 0, "settle": 0, "count": "processes", "patterns": []string{`\bmake\s+test\b`, `\bgo\s+test\b`}}
}

func simKind(max int) map[string]any {
	return map[string]any{"id": "sim", "label": "Simulators", "max": max, "cpu": 0, "memFreeGB": 0, "settle": 0, "count": "simulators", "patterns": []string{`\bsim\.sh\s+up\b`}}
}

func (f *fixture) setSlots(t *testing.T, workspace string, enabled bool, kinds ...map[string]any) {
	t.Helper()
	require.NoError(t, f.run(t, workspace, "agents.slot-config", map[string]any{"enabled": enabled, "maxWait": 480, "kinds": kinds}, nil))
}

func (f *fixture) acquire(t *testing.T, workspace, session, call, command, request string) slotAnswer {
	t.Helper()
	var out slotAnswer
	require.NoError(t, f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "agents.slot-acquire", "workspaceId": workspace, "caller": session, "args": map[string]any{"call": call, "command": command, "request": request}}, &out))
	return out
}

func (f *fixture) slots(t *testing.T, workspace string) slotsView {
	t.Helper()
	var out slotsView
	require.NoError(t, f.run(t, workspace, "agents.slots", map[string]any{}, &out))
	return out
}

func (f *fixture) runningSlotSession(t *testing.T, workspace, tag, title string) string {
	t.Helper()
	var out struct {
		Session string `json:"session"`
	}
	require.NoError(t, f.run(t, workspace, "agents.new", map[string]any{"title": title}, &out))
	f.prompt(t, workspace, out.Session, tag)
	f.waitStatus(t, workspace, out.Session, "running")
	return out.Session
}

func (f *fixture) acquireUntil(t *testing.T, workspace, session, call, command, request, state string) slotAnswer {
	t.Helper()
	var got slotAnswer
	reached := testutil.Poll(func() bool {
		got = f.acquire(t, workspace, session, call, command, request)
		return got.State == state
	}, wait)
	require.True(t, reached, "slot request %s never reached %s: %+v", request, state, got)
	return got
}

func TestSlotAcquireWaitsAndGrantsInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t)
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(2))
	one := f.runningSlotSession(t, workspace, "tagone", "One")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	three := f.runningSlotSession(t, workspace, "tagthree", "Three")

	assert.Equal(t, "none", f.acquire(t, workspace, one, "x", "ls -la", "").State)
	assert.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "make test ./...", "").State)
	assert.Equal(t, "granted", f.acquire(t, workspace, two, "tagtwo-call", "go test ./...", "").State)
	waiting := f.acquire(t, workspace, three, "tagthree-call", "make test", "")
	assert.Equal(t, slotAnswer{State: "wait", Request: waiting.Request, Kind: "test", Place: 1, Of: 1, Why: "2 of 2 in use"}, waiting)

	write(t, filepath.Join(dir, "tagone"), "")
	got := f.acquireUntil(t, workspace, three, "tagthree-call", "make test", waiting.Request, "granted")
	assert.Equal(t, waiting.Request, got.Request)
}

func TestSlotTwoKindsTakenInIDOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t)
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1), simKind(1))
	one := f.runningSlotSession(t, workspace, "tagone", "One")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	three := f.runningSlotSession(t, workspace, "tagthree", "Three")

	require.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "go test ./...", "").State)
	both := f.acquire(t, workspace, two, "tagtwo-call", "scripts/sim.sh up cm-x && go test ./...", "")
	assert.Equal(t, "test", both.Kind)
	simOnly := f.acquire(t, workspace, three, "tagthree-call", "scripts/sim.sh up cm-y", "")
	assert.Equal(t, slotAnswer{State: "wait", Request: simOnly.Request, Kind: "sim", Place: 1, Of: 1, Why: "1 of 1 in use"}, simOnly)

	write(t, filepath.Join(dir, "tagone"), "")
	f.acquireUntil(t, workspace, two, "tagtwo-call", "scripts/sim.sh up cm-x && go test ./...", both.Request, "granted")
	write(t, filepath.Join(dir, "tagtwo"), "")
	f.acquireUntil(t, workspace, three, "tagthree-call", "scripts/sim.sh up cm-y", simOnly.Request, "granted")
}

func TestSlotSilentWaiterLeavesAndItsSlotsAreFreed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t)
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1), simKind(1))
	one := f.runningSlotSession(t, workspace, "tagone", "One")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	require.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "go test ./...", "").State)
	require.Equal(t, "wait", f.acquire(t, workspace, two, "tagtwo-call", "scripts/sim.sh up cm-x && go test ./...", "").State)

	testutil.Eventually(t, func() bool {
		v := f.slots(t, workspace)
		return len(v.Kinds) == 2 && v.Kinds[0].InUse == 0 && len(v.Kinds[1].Waiters) == 0 && v.Kinds[1].InUse == 1
	}, wait)
}

func TestSlotDeniedAfterMaxWait(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t, "CLUBMATE_AGENTS_SLOT_MAXWAIT=1s")
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1))
	one := f.runningSlotSession(t, workspace, "tagone", "Holder")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	require.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "make test", "").State)

	first := f.acquire(t, workspace, two, "tagtwo-call", "make test", "")
	got := f.acquireUntil(t, workspace, two, "tagtwo-call", "make test", first.Request, "denied")
	assert.Equal(t, `[Clubmate slots] no test slot in 1s: 1 of 1 in use by "Holder". Run it again later, or end your turn and tell the user.`, got.Text)
}

func TestSlotBackgroundCallHoldsUntilTaskEnds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t)
	f := startEnv(t, env, map[string]any{
		"prompt": []map[string]any{
			{"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": "bg", "title": "make test", "kind": "execute", "status": "pending", "rawInput": map[string]any{"command": "make test", "run_in_background": true}}},
			{"wait": filepath.Join(dir, "go")},
			{"update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "bg", "status": "completed", "_meta": map[string]any{"claudeCode": map[string]any{"toolResponse": map[string]any{"backgroundTaskId": "b1"}}}}},
			{"shell": "sleep 30; :"},
		},
		"after": followUp(4000),
	})
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "running")
	require.Equal(t, "granted", f.acquire(t, workspace, session, "bg", "make test", "").State)

	write(t, filepath.Join(dir, "go"), "")
	endedTurn(t, f, workspace, session)
	testutil.Never(t, func() bool { return f.slots(t, workspace).Kinds[0].InUse == 0 }, time.Second)
	testutil.Eventually(t, func() bool { return f.slots(t, workspace).Kinds[0].InUse == 0 }, wait)
}

func TestStoredStepHolderKeepsItsSlotAcrossRestart(t *testing.T) {
	t.Parallel()
	env, _ := slotEnv(t)
	f := startEnv(t, env, slotScript(t.TempDir()))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1))
	step := exec.Command("sleep", "600")
	require.NoError(t, step.Start())
	done := false
	t.Cleanup(func() {
		if !done {
			_ = step.Process.Kill()
			_ = step.Wait()
		}
	})
	start, err := proc.StartTime(step.Process.Pid)
	require.NoError(t, err)
	state, err := json.Marshal(map[string]any{"holders": []map[string]any{{"id": "ss_1:test", "request": "ss_1", "kind": "test", "step": true, "label": "make test", "pid": step.Process.Pid, "pidStart": start, "since": time.Now()}}, "waiters": []any{}})
	require.NoError(t, err)

	require.NoError(t, f.h.Sup.SetEnabled(t.Context(), "agents", false))
	require.NoError(t, f.h.KV.Set(t.Context(), "agents", "slots.state", state))
	require.NoError(t, f.h.Sup.SetEnabled(t.Context(), "agents", true))
	testutil.Eventually(t, func() bool { return f.run(t, workspace, "agents.slots", map[string]any{}, nil) == nil }, restartWait)

	testutil.Never(t, func() bool { return f.slots(t, workspace).Kinds[0].InUse != 1 }, time.Second)
	require.NoError(t, step.Process.Kill())
	_ = step.Wait()
	done = true
	testutil.Eventually(t, func() bool { return f.slots(t, workspace).Kinds[0].InUse == 0 }, wait)
}

func TestSlotsOffGrantsWaiters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t, "CLUBMATE_AGENTS_SLOT_POLL=1m")
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1))
	one := f.runningSlotSession(t, workspace, "tagone", "One")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	require.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "make test", "").State)

	done := make(chan slotAnswer, 1)
	go func() {
		var out slotAnswer
		_ = f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": "agents.slot-acquire", "workspaceId": workspace, "caller": two, "args": map[string]any{"call": "tagtwo-call", "command": "make test"}}, &out)
		done <- out
	}()
	testutil.Eventually(t, func() bool { return len(f.slots(t, workspace).Kinds[0].Waiters) == 1 }, wait)
	f.setSlots(t, workspace, false, testKind(1))
	var got slotAnswer
	testutil.Eventually(t, func() bool {
		select {
		case got = <-done:
			return true
		default:
			return false
		}
	}, wait)
	assert.Equal(t, "granted", got.State)
}

func TestSlotSimCountsBootedAgentSims(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, host := slotEnv(t)
	sims, err := json.Marshal(map[string]any{"cpu": 10, "memFreeGB": 32, "sims": []map[string]string{{"name": "cm-a", "udid": "A"}, {"name": "cm-b", "udid": "B"}, {"name": "iPhone 17", "udid": "C"}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(host, sims, 0o600))
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, simKind(2))
	one := f.runningSlotSession(t, workspace, "tagone", "One")

	assert.Equal(t, slotAnswer{State: "wait", Kind: "sim", Place: 1, Of: 1, Why: "2 of 2 in use"}, withoutRequest(f.acquire(t, workspace, one, "tagone-call", "scripts/sim.sh up cm-c", "")))
	assert.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call2", "scripts/sim.sh up cm-a", "").State)
}

func withoutRequest(a slotAnswer) slotAnswer {
	a.Request = ""
	return a
}

func (f *fixture) runCaller(t *testing.T, workspace, caller, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "workspaceId": workspace, "caller": caller, "args": args}, out)
}

func TestSlotSettingsRefuseBadInputAndAgentWrites(t *testing.T) {
	t.Parallel()
	env, _ := slotEnv(t)
	f := startEnv(t, env, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(2))
	session := f.newSession(t, workspace)
	bad := testKind(2)
	bad["patterns"] = []string{"("}
	zero := testKind(0)

	assert.ErrorContains(t, f.run(t, workspace, "agents.slot-config", map[string]any{"kinds": []map[string]any{bad}}, nil), "missing closing )")
	assert.ErrorContains(t, f.run(t, workspace, "agents.slot-config", map[string]any{"kinds": []map[string]any{zero}}, nil), "max is 1 or more")
	assert.ErrorContains(t, f.runCaller(t, workspace, session, "agents.slot-config", map[string]any{"enabled": false}, nil), "only the user")
	assert.ErrorContains(t, f.runCaller(t, workspace, session, "agents.slot-release", map[string]any{"id": "x"}, nil), "only the user")
	var cfg struct {
		Enabled bool `json:"enabled"`
		Kinds   []struct {
			Max int `json:"max"`
		} `json:"kinds"`
	}
	require.NoError(t, f.run(t, workspace, "agents.slot-config", map[string]any{}, &cfg))
	assert.True(t, cfg.Enabled)
	require.Len(t, cfg.Kinds, 1)
	assert.Equal(t, 2, cfg.Kinds[0].Max)
}

func TestSessionShowsSlotWait(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t, "CLUBMATE_AGENTS_SLOT_SILENT=1m")
	f := startEnv(t, env, slotScript(dir))
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1))
	one := f.runningSlotSession(t, workspace, "tagone", "One")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	require.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "make test", "").State)
	require.Equal(t, "wait", f.acquire(t, workspace, two, "tagtwo-call", "make test", "").State)

	type row struct {
		ID       string          `json:"id"`
		NeedsYou bool            `json:"needsYou"`
		SlotWait json.RawMessage `json:"slotWait"`
		SlotHold json.RawMessage `json:"slotHold"`
		Landing  struct {
			Words string `json:"words"`
			Line  string `json:"line"`
		} `json:"landing"`
	}
	var got row
	testutil.Eventually(t, func() bool {
		var list []row
		require.NoError(t, f.run(t, workspace, "agents.sessions", map[string]any{}, &list))
		for _, r := range list {
			if r.ID == two {
				got = r
			}
		}
		return got.Landing.Words != ""
	}, wait)
	assert.JSONEq(t, `{"kind":"test","place":1,"of":1,"why":"1 of 1 in use","words":"waits for test slot"}`, string(got.SlotWait))
	assert.Equal(t, "waits for test slot", got.Landing.Words)
	assert.Equal(t, "waits for test slot · 1 of 1 in use", got.Landing.Line)
	assert.False(t, got.NeedsYou)
	assert.Empty(t, got.SlotHold)
}

func TestSlotWaiterOfAnEarlierAgentIsNotGrantedAfterRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env, _ := slotEnv(t, "CLUBMATE_AGENTS_SLOT_SILENT=1m")
	script := slotScript(dir)
	script["resume"] = true
	f := startEnv(t, env, script)
	workspace := f.workspace(t, newRepo(t))
	f.setSlots(t, workspace, true, testKind(1))
	one := f.runningSlotSession(t, workspace, "tagone", "One")
	two := f.runningSlotSession(t, workspace, "tagtwo", "Two")
	agentID := f.agentID(t, workspace, two)
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, agentID, "tagtwo")) > 0 }, wait)
	require.Equal(t, "granted", f.acquire(t, workspace, one, "tagone-call", "make test", "").State)
	old := f.acquire(t, workspace, two, "tagtwo-call", "make test", "")
	require.Equal(t, "wait", old.State)

	f.restartPlugin(t, workspace)
	testutil.Eventually(t, func() bool { return len(f.promptsTo(t, agentID, cutOffText)) > 0 }, wait)
	testutil.Never(t, func() bool { return f.slots(t, workspace).Kinds[0].InUse > 0 }, time.Second)
	assert.Equal(t, "denied", f.acquire(t, workspace, two, "tagtwo-call", "make test", old.Request).State)
	assert.Empty(t, f.slots(t, workspace).Kinds[0].Waiters)
	assert.Equal(t, "granted", f.acquire(t, workspace, two, "tagtwo-again", "make test", "").State)
	assert.Equal(t, "wait", f.acquire(t, workspace, one, "tagone-again", "make test", "sr_neverseen").State)
}
