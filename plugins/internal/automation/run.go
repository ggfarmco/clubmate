package automation

import (
	"slices"
	"time"
	"unicode/utf8"
)

const (
	StatusOK      = "ok"
	StatusError   = "error"
	StatusSkipped = "skipped"
)

const (
	ReasonBusy     = "busy"
	ReasonNoApp    = "no app"
	ReasonInFlight = "previous run still running"
	ReasonGone     = "owner gone"
	ReasonUnknown  = "owner unknown"
)

const (
	MaxHistory  = 20
	MaxFailures = 3
	MaxResult   = 200
)

type Run struct {
	At     time.Time `json:"at"`
	Topic  string    `json:"topic,omitempty"`
	Seq    int64     `json:"seq,omitempty"`
	Status string    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	Result string    `json:"result,omitempty"`
}

func Record(history *[]Run, failures *int, lastError *string, r Run) (paused bool) {
	*history = append(*history, r)
	if extra := len(*history) - MaxHistory; extra > 0 {
		*history = (*history)[extra:]
	}
	switch r.Status {
	case StatusOK:
		*failures = 0
		*lastError = ""
	case StatusError:
		*failures++
		*lastError = r.Reason
		return *failures >= MaxFailures
	}
	return false
}

func LastRun(history []Run) (Run, bool) {
	if len(history) == 0 {
		return Run{}, false
	}
	return slices.MaxFunc(history, func(x, y Run) int { return x.At.Compare(y.At) }), true
}

func NewestFirst(history []Run) []Run {
	runs := slices.Clone(history)
	slices.SortStableFunc(runs, func(x, y Run) int { return y.At.Compare(x.At) })
	return runs
}

func StatusTone(status string) string {
	switch status {
	case StatusOK:
		return "ok"
	case StatusError:
		return "danger"
	}
	return ""
}

func Truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
