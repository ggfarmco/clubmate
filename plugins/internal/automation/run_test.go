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
