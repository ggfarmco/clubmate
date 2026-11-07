package pluginsdk

import (
	"encoding/json"
	"errors"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/contract"
)

type PluginError = contract.PluginErrorData

type reasonError struct {
	reason string
	err    error
}

func (e *reasonError) Error() string { return e.err.Error() }

func (e *reasonError) Unwrap() error { return e.err }

func WithReason(reason string, err error) error {
	return &reasonError{reason: reason, err: err}
}

func ReasonOf(err error) string {
	if reasoned, ok := errors.AsType[*reasonError](err); ok {
		return reasoned.reason
	}
	return ""
}

func PluginErrorOf(err error) (PluginError, bool) {
	rpcErr, ok := errors.AsType[*jsonrpc2.Error](err)
	if !ok || rpcErr.Code != contract.CodePluginError || rpcErr.Data == nil {
		return PluginError{}, false
	}
	var pe PluginError
	if json.Unmarshal(*rpcErr.Data, &pe) != nil || pe.Plugin == "" {
		return PluginError{}, false
	}
	return pe, true
}
