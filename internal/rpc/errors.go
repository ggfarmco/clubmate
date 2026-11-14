package rpc

import (
	"encoding/json"
	"errors"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/kv"
	"clubmate/internal/proc"
	"clubmate/internal/sdui"
	"clubmate/internal/session"
	"clubmate/internal/workspace"
	"clubmate/pkg/contract"
)

const (
	codeMethodNotFound    = jsonrpc2.CodeMethodNotFound
	codeInvalidParams     = jsonrpc2.CodeInvalidParams
	codeInternal          = jsonrpc2.CodeInternalError
	codeNotFound          = -32004
	codeNotTracked        = -32010
	codeSessionRunning    = -32011
	codePluginUnavailable = contract.CodePluginUnavailable
	codePluginError       = contract.CodePluginError
	codeDenied            = -32014
	codeRateLimited       = -32015
	codeStaleBase         = -32016
)

var (
	ErrNotFound          = errors.New("not found")
	ErrNotTracked        = errors.New("not tracked")
	ErrSessionRunning    = errors.New("session is running")
	ErrInvalidParams     = errors.New("invalid params")
	ErrMethodNotFound    = errors.New("method not found")
	ErrPluginUnavailable = errors.New("plugin unavailable")
	ErrPluginDisabled    = errors.New("plugin disabled")
	ErrOutcomeUnknown    = errors.New("outcome unknown")
	ErrPluginError       = errors.New("plugin error")
	ErrUnknownPlugin     = errors.New("unknown plugin")
	ErrDenied            = errors.New("denied")
	ErrRateLimited       = errors.New("rate limited")
)

type RemoteError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

type PluginError struct {
	PluginID string
	Method   string
	Message  string
	Reason   string
}

func NewRemoteError(e *jsonrpc2.Error) *RemoteError {
	out := &RemoteError{Code: int(e.Code), Message: e.Message}
	if e.Data != nil {
		out.Data = *e.Data
	}
	return out
}

func (e *RemoteError) Error() string {
	return e.Message
}

func (e *RemoteError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Code == codeNotFound
	case ErrNotTracked:
		return e.Code == codeNotTracked
	case ErrSessionRunning:
		return e.Code == codeSessionRunning
	case ErrInvalidParams:
		return e.Code == codeInvalidParams
	case ErrMethodNotFound:
		return e.Code == codeMethodNotFound
	case ErrPluginUnavailable:
		return e.Code == codePluginUnavailable
	case ErrPluginError:
		return e.Code == codePluginError
	case ErrDenied:
		return e.Code == codeDenied
	case ErrRateLimited:
		return e.Code == codeRateLimited
	}
	return false
}

type outcomeUnknown struct {
	err error
}

func OutcomeUnknown(err error) error {
	return outcomeUnknown{err: err}
}

func (e outcomeUnknown) Error() string {
	return e.err.Error()
}

func (e outcomeUnknown) Unwrap() error {
	return e.err
}

func (e outcomeUnknown) Is(target error) bool {
	return target == ErrOutcomeUnknown
}

func (e *PluginError) Error() string {
	return "plugin " + e.PluginID + " " + e.Method + ": " + e.Message
}

func (e *PluginError) Is(target error) bool {
	return target == ErrPluginError
}

func toRPCError(err error) *jsonrpc2.Error {
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	out := &jsonrpc2.Error{Code: errorCode(err), Message: err.Error()}
	if pe, ok := errors.AsType[*PluginError](err); ok {
		out.SetError(contract.PluginErrorData{Plugin: pe.PluginID, Method: pe.Method, Message: pe.Message, Reason: pe.Reason})
	} else if errors.Is(err, workspace.ErrNotFound) {
		out.SetError(contract.ErrorReason{Reason: contract.ReasonStale})
	} else if errors.Is(err, ErrOutcomeUnknown) {
		out.SetError(contract.ErrorReason{Reason: contract.ReasonUnknown})
	}
	return out
}

func errorCode(err error) int64 {
	switch {
	case errors.Is(err, workspace.ErrNotFound),
		errors.Is(err, session.ErrNotFound),
		errors.Is(err, session.ErrNotRunning),
		errors.Is(err, session.ErrNoSnapshot),
		errors.Is(err, errUnknownSubscription),
		errors.Is(err, errUnknownSection),
		errors.Is(err, errUnknownCommand),
		errors.Is(err, ErrUnknownPlugin):
		return codeNotFound
	case errors.Is(err, ErrPluginUnavailable), errors.Is(err, ErrPluginDisabled):
		return codePluginUnavailable
	case errors.Is(err, ErrPluginError):
		return codePluginError
	case errors.Is(err, ErrDenied):
		return codeDenied
	case errors.Is(err, ErrRateLimited):
		return codeRateLimited
	case errors.Is(err, sdui.ErrStaleBase):
		return codeStaleBase
	case errors.Is(err, core.ErrNotTracked):
		return codeNotTracked
	case errors.Is(err, session.ErrRunning):
		return codeSessionRunning
	case errors.Is(err, workspace.ErrInvalidFolder),
		errors.Is(err, workspace.ErrDuplicateFolder),
		errors.Is(err, workspace.ErrEmptyName),
		errors.Is(err, workspace.ErrNotFolder),
		errors.Is(err, workspace.ErrLastFolder),
		errors.Is(err, workspace.ErrNotDirectory),
		errors.Is(err, core.ErrNoFolders),
		errors.Is(err, core.ErrEmptyKillRequest),
		errors.Is(err, core.ErrOrphansNeedSession),
		errors.Is(err, proc.ErrUnsafePID),
		errors.Is(err, core.ErrNotOwned),
		errors.Is(err, core.ErrInvalidLabel),
		errors.Is(err, bus.ErrInvalidTopic),
		errors.Is(err, bus.ErrInvalidPattern),
		errors.Is(err, bus.ErrInvalidPayload),
		errors.Is(err, bus.ErrPayloadTooLarge),
		errors.Is(err, kv.ErrInvalidKey),
		errors.Is(err, kv.ErrInvalidValue),
		errors.Is(err, kv.ErrValueTooLarge),
		errors.Is(err, errEmptyConsumer),
		errors.Is(err, sdui.ErrInvalidTree),
		errors.Is(err, sdui.ErrUnknownWorkspace):
		return codeInvalidParams
	}
	return codeInternal
}
