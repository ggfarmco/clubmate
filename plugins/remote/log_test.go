package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

type logBuffer struct {
	mu    sync.Mutex
	lines []string
}

func captureLog(t *testing.T) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	prev := setLogSink(func(line string) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.lines = append(b.lines, line)
	})
	t.Cleanup(func() { setLogSink(prev) })
	return b
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}

func TestLogLinesCarryNoArgsOrTokens(t *testing.T) {
	f := newFixture(t)
	logs := captureLog(t)
	token := f.pair(t, "phone-0123456789")
	f.backend.failNextRun(pluginError("task changed", contract.ReasonStale))

	f.call(t, token, "command", map[string]any{"id": "agents.sessions", "workspace": "prj_1", "args": map[string]any{"text": "SECRET-TEXT"}})
	f.call(t, "SECRET-TOKEN", "workspaces", nil)
	entered, release := f.backend.holdNextRun()
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		<-entered
		f.clock.advance(3 * time.Second)
		close(release)
	}()
	f.call(t, token, "command", map[string]any{"id": "agents.sessions", "workspace": "prj_1"})
	<-advanced
	report := phoneReportParams("r1")
	report["text"] = "SECRET-TEXT"
	f.call(t, token, "report", report)
	f.call(t, token, "report", report)
	res, stream := f.openEvents(t, token, "")
	require.Equal(t, ": connected", frame(t, stream))
	streamID(t, stream)
	res.Body.Close()
	testutil.Eventually(t, func() bool { return strings.Contains(logs.String(), "stream close device=phone-01") }, 5*time.Second)

	out := logs.String()
	for _, want := range []string{
		"pair result=ok",
		"rpc device=phone-01 method=command id=agents.sessions ms=0 err=stale",
		"auth device=- path=/v1/rpc err=unknown",
		"rpc device=phone-01 method=command id=agents.sessions ms=3000",
		"report device=phone-01 bytes=13 id=rpt_1",
		"dedupe hit device=phone-01 request=r1",
		"stream open device=phone-01 resume=live path=tailnet",
		"stream close device=phone-01 lived=0s reason=client",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "SECRET-TEXT")
	assert.NotContains(t, out, "SECRET-TOKEN")
}
