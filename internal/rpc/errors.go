package rpc

import (
	"errors"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/kv"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/sdui"
	"clubmate/internal/session"
)

const (
	codeMethodNotFound    = jsonrpc2.CodeMethodNotFound
	codeInvalidParams     = jsonrpc2.CodeInvalidParams
	codeInternal          = jsonrpc2.CodeInternalError
	codeNotFound          = -32004
	codeNotTracked        = -32010
	codeSessionRunning    = -32011
	codePluginUnavailable = -32012
	codePluginError       = -32013
)

var (
	ErrNotFound          = errors.New("not found")
	ErrNotTracked        = errors.New("not tracked")
	ErrSessionRunning    = errors.New("session is running")
	ErrInvalidParams     = errors.New("invalid params")
	ErrMethodNotFound    = errors.New("method not found")
	ErrPluginUnavailable = errors.New("plugin unavailable")
	ErrPluginError       = errors.New("plugin error")
)

type RemoteError struct {
	Code    int
	Message string
}

type PluginError struct {
	PluginID string
	Method   string
	Message  string
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
	}
	return false
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
	return &jsonrpc2.Error{Code: errorCode(err), Message: err.Error()}
}

func errorCode(err error) int64 {
	switch {
	case errors.Is(err, project.ErrNotFound),
		errors.Is(err, session.ErrNotFound),
		errors.Is(err, errUnknownSubscription),
		errors.Is(err, errUnknownSection),
		errors.Is(err, errUnknownCommand):
		return codeNotFound
	case errors.Is(err, ErrPluginUnavailable):
		return codePluginUnavailable
	case errors.Is(err, ErrPluginError):
		return codePluginError
	case errors.Is(err, core.ErrNotTracked):
		return codeNotTracked
	case errors.Is(err, session.ErrRunning):
		return codeSessionRunning
	case errors.Is(err, project.ErrInvalidRepo),
		errors.Is(err, project.ErrDuplicateRepo),
		errors.Is(err, project.ErrEmptyName),
		errors.Is(err, core.ErrEmptyKillRequest),
		errors.Is(err, core.ErrOrphansNeedSession),
		errors.Is(err, proc.ErrUnsafePID),
		errors.Is(err, bus.ErrInvalidTopic),
		errors.Is(err, bus.ErrInvalidPattern),
		errors.Is(err, bus.ErrInvalidPayload),
		errors.Is(err, bus.ErrPayloadTooLarge),
		errors.Is(err, kv.ErrInvalidKey),
		errors.Is(err, kv.ErrInvalidValue),
		errors.Is(err, kv.ErrValueTooLarge),
		errors.Is(err, errEmptyConsumer),
		errors.Is(err, sdui.ErrInvalidTree):
		return codeInvalidParams
	}
	return codeInternal
}
