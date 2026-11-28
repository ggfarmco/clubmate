package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const hookInput = `{"tool_name":"Bash","tool_use_id":"toolu_1","cwd":"/w","tool_input":{"command":"make test"}}`

func agentHere() bool { return false }

func TestSlotHookPrintsDenyOnlyWhenDenied(t *testing.T) {
	t.Setenv(agentSessionEnv, "agt_0123456789abcdef")
	for name, c := range map[string]struct {
		answers []slotAnswer
		out     string
	}{
		"none":    {answers: []slotAnswer{{State: "none"}}},
		"granted": {answers: []slotAnswer{{State: "wait", Request: "sr_1"}, {State: "granted", Request: "sr_1"}}},
		"denied":  {answers: []slotAnswer{{State: "wait", Request: "sr_1"}, {State: "denied", Request: "sr_1", Text: "no test slot"}}, out: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"no test slot"}}` + "\n"},
	} {
		var calls []map[string]any
		call := func(_ context.Context, args map[string]any, out *slotAnswer) error {
			calls = append(calls, args)
			*out = c.answers[len(calls)-1]
			return nil
		}
		var stdout bytes.Buffer
		require.NoError(t, slotHook(t.Context(), strings.NewReader(hookInput), &stdout, call, agentHere), name)
		assert.Equal(t, c.out, stdout.String(), name)
		require.Len(t, calls, len(c.answers), name)
		assert.Equal(t, map[string]any{"call": "toolu_1", "command": "make test", "cwd": "/w", "request": ""}, calls[0], name)
		if len(calls) > 1 {
			assert.Equal(t, "sr_1", calls[1]["request"], name)
		}
	}
}

func TestSlotHookCoreDownRunsTheCall(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	t.Setenv("CLUBMATE_STATE_DIR", dirs.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", dirs.Runtime)
	t.Setenv(agentSessionEnv, "agt_0123456789abcdef")
	before := slotRetry
	slotRetry = 300 * time.Millisecond
	t.Cleanup(func() { slotRetry = before })
	var stdout bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- slotHook(t.Context(), strings.NewReader(hookInput), &stdout, (&app{}).slotAcquire, agentHere)
	}()
	var err error
	testutil.Eventually(t, func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	}, 5*time.Second)
	require.NoError(t, err)
	assert.Empty(t, stdout.String())
}

func TestSlotHookStopsWhenItsAgentIsGone(t *testing.T) {
	t.Setenv(agentSessionEnv, "agt_0123456789abcdef")
	calls := 0
	call := func(_ context.Context, _ map[string]any, out *slotAnswer) error {
		calls++
		*out = slotAnswer{State: "wait", Request: "sr_1"}
		return nil
	}
	var stdout bytes.Buffer
	require.NoError(t, slotHook(t.Context(), strings.NewReader(hookInput), &stdout, call, func() bool { return calls > 0 }))
	assert.Equal(t, 1, calls)
	assert.Empty(t, stdout.String())
}
