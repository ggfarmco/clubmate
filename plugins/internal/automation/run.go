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
	ReasonMissed   = "missed"
)

const (
	MaxHistory  = 100
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
	Chain  *Chain    `json:"chain,omitempty"`
}

type Chain struct {
	User        bool   `json:"user,omitempty"`
	Session     string `json:"session,omitempty"`
	Title       string `json:"title,omitempty"`
	Turn        int    `json:"turn,omitempty"`
	Status      string `json:"status,omitempty"`
	CronJob     string `json:"cronJob,omitempty"`
	CronName    string `json:"cronName,omitempty"`
	Trigger     string `json:"trigger,omitempty"`
	TriggerName string `json:"triggerName,omitempty"`
}

type Span struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to,omitzero"`
	Reason string    `json:"reason,omitempty"`
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

func Pause(spans []Span, at time.Time, reason string) []Span {
	if _, open := OpenSpan(spans); open {
		return spans
	}
	return append(slices.Clone(spans), Span{From: at, Reason: reason})
}

func Resume(spans []Span, at time.Time) []Span {
	if _, open := OpenSpan(spans); !open {
		return spans
	}
	spans = slices.Clone(spans)
	spans[len(spans)-1].To = at
	return spans
}

func OpenSpan(spans []Span) (Span, bool) {
	if n := len(spans); n > 0 && spans[n-1].To.IsZero() {
		return spans[n-1], true
	}
	return Span{}, false
}

func KeepSpans(spans []Span, history []Run) []Span {
	if len(history) == 0 {
		if s, open := OpenSpan(spans); open {
			return []Span{s}
		}
		return nil
	}
	oldest := slices.MinFunc(history, func(x, y Run) int { return x.At.Compare(y.At) }).At
	return slices.DeleteFunc(slices.Clone(spans), func(s Span) bool { return !s.To.IsZero() && s.To.Before(oldest) })
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

func Truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
