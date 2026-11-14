package contract

import "encoding/json"

const (
	CodePluginUnavailable = -32012
	CodePluginError       = -32013
)

const (
	ReasonStale    = "stale"
	ReasonAnswered = "answered"
	ReasonUnknown  = "unknown"
)

type PluginErrorData struct {
	Plugin  string `json:"plugin"`
	Method  string `json:"method"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

type ErrorReason struct {
	Reason string `json:"reason"`
}

func ReasonIn(data *json.RawMessage) string {
	if data == nil {
		return ""
	}
	var r ErrorReason
	if json.Unmarshal(*data, &r) != nil {
		return ""
	}
	return r.Reason
}
