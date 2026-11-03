package automation

import (
	"cmp"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const (
	LabelOK      = "ok"
	LabelFailed  = "failed"
	LabelMissed  = "missed"
	LabelSkipped = "skipped"

	notRun = "not run — "
)

var (
	rpcPrefix   = regexp.MustCompile(`^(jsonrpc2: code -?\d+ message: |plugin [a-z0-9-]+ command\.run: )`)
	codePrefix  = regexp.MustCompile(`^[a-z]+(_[a-z]+)+: `)
	textFields  = []string{"text", "message", "summary"}
	agentErrors = map[string]string{
		"unknown agent session": "its session was deleted",
		"the session is closed": "its session is closed",
	}
	reasons = map[string]string{
		ReasonBusy:     "the session was busy",
		ReasonNoApp:    "no app was open to approve it",
		ReasonMissed:   "Clubmate was closed or the Mac asleep; the policy skips it",
		ReasonInFlight: "the previous run was still running",
		ReasonGone:     "its owner session is closed",
		ReasonUnknown:  "Clubmate could not tell whether its owner session is open",
	}
)

func StatusLabel(r Run) (label, tone string) {
	switch {
	case r.Status == StatusOK:
		return LabelOK, "ok"
	case r.Status == StatusError:
		return LabelFailed, "danger"
	case r.Reason == ReasonMissed:
		return LabelMissed, "warn"
	}
	return LabelSkipped, ""
}

func Words(command string, args json.RawMessage, r Run, titles map[string]string) string {
	switch r.Status {
	case StatusError:
		return Names(ErrorWords(r.Reason), titles)
	case StatusSkipped:
		return notRun + Names(reasonWords(r.Reason), titles)
	}
	return Names(resultWords(command, args, r, titles), titles)
}

func ErrorWords(err string) string {
	for {
		trimmed := codePrefix.ReplaceAllString(rpcPrefix.ReplaceAllString(err, ""), "")
		if trimmed == err {
			break
		}
		err = trimmed
	}
	err = strings.TrimSpace(err)
	for prefix, words := range agentErrors {
		if strings.HasPrefix(err, prefix) {
			return words
		}
	}
	return err
}

func reasonWords(reason string) string {
	if words, ok := reasons[reason]; ok {
		return words
	}
	return reason
}

func resultWords(command string, args json.RawMessage, r Run, titles map[string]string) string {
	raw := r.Result
	var fields map[string]any
	_ = json.Unmarshal([]byte(raw), &fields)
	str := func(m map[string]any, key string) string {
		s, _ := m[key].(string)
		return strings.TrimSpace(s)
	}
	switch command {
	case agentNew:
		if s := str(fields, "session"); s != "" {
			return "started " + s
		}
	case PromptCommand:
		var target map[string]any
		_ = json.Unmarshal(args, &target)
		if s := cmp.Or(r.Target, str(target, "session")); s != "" {
			if turn, _ := fields["turn"].(float64); turn > 0 {
				return "prompted " + s + " · its turn " + strconv.Itoa(int(turn)) + " started"
			}
			return "prompted " + s
		}
	case agentEmit:
		if topic := str(fields, "topic"); topic != "" {
			return "emitted “" + strings.TrimPrefix(topic, "event.") + "”"
		}
	}
	for _, key := range textFields {
		if s := str(fields, key); s != "" {
			line, _, _ := strings.Cut(s, "\n")
			return line
		}
	}
	switch strings.TrimSpace(raw) {
	case "", "null", "{}", "[]":
		var target map[string]any
		_ = json.Unmarshal(args, &target)
		return doesLine(command, target, titles) + " · done"
	}
	return LabelOK
}
