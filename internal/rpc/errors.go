package rpc

import (
	"errors"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/core"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
)

const (
	codeMethodNotFound = jsonrpc2.CodeMethodNotFound
	codeInvalidParams  = jsonrpc2.CodeInvalidParams
	codeInternal       = jsonrpc2.CodeInternalError
	codeNotFound       = -32004
	codeNotTracked     = -32010
)

var (
	ErrNotFound       = errors.New("not found")
	ErrNotTracked     = errors.New("not tracked")
	ErrInvalidParams  = errors.New("invalid params")
	ErrMethodNotFound = errors.New("method not found")
)

type RemoteError struct {
	Code    int
	Message string
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
	case ErrInvalidParams:
		return e.Code == codeInvalidParams
	case ErrMethodNotFound:
		return e.Code == codeMethodNotFound
	}
	return false
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
	case errors.Is(err, project.ErrNotFound), errors.Is(err, session.ErrNotFound):
		return codeNotFound
	case errors.Is(err, core.ErrNotTracked):
		return codeNotTracked
	case errors.Is(err, project.ErrInvalidRepo),
		errors.Is(err, project.ErrDuplicateRepo),
		errors.Is(err, project.ErrEmptyName),
		errors.Is(err, core.ErrEmptyKillRequest),
		errors.Is(err, core.ErrOrphansNeedSession),
		errors.Is(err, proc.ErrUnsafePID):
		return codeInvalidParams
	}
	return codeInternal
}
