package automation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunJSON(t *testing.T) {
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		run  Run
		want string
	}{
		{"scheduled", Run{At: at, Status: StatusOK, Result: "{}"}, `{"at":"2026-10-31T09:00:00Z","status":"ok","result":"{}"}`},
		{"event", Run{At: at, Topic: "event.ping", Seq: 7, Status: StatusSkipped, Reason: ReasonBusy}, `{"at":"2026-10-31T09:00:00Z","topic":"event.ping","seq":7,"status":"skipped","reason":"busy"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.run)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(raw))
		})
	}
}

func TestPauseSpansCloseOnResumeAndOutliveNoKeptRun(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	spans := Pause(nil, at, "")
	spans = Pause(spans, at.Add(time.Hour), "boom")
	spans = Resume(spans, at.Add(2*time.Hour))
	spans = Pause(spans, at.Add(4*time.Hour), "boom")
	assert.Equal(t, []Span{{From: at, To: at.Add(2 * time.Hour)}, {From: at.Add(4 * time.Hour), Reason: "boom"}}, spans)

	assert.Equal(t, spans[1:], KeepSpans(spans, []Run{{At: at.Add(3 * time.Hour)}}))
	assert.Equal(t, spans, KeepSpans(spans, []Run{{At: at.Add(time.Hour)}}))
}
