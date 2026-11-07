package main

import (
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"

	"clubmate/pkg/contract"
)

func TestAPlainErrorIsFailedAndAReasonBecomesTheCode(t *testing.T) {
	t.Parallel()
	refused := &jsonrpc2.Error{Code: contract.CodePluginError, Message: "plugin agents command.run: this task changed since you saw the plan"}
	refused.SetError(contract.PluginErrorData{Plugin: "agents", Method: "command.run", Message: "this task changed since you saw the plan", Reason: contract.ReasonStale})
	gone := &jsonrpc2.Error{Code: -32004, Message: "workspace not found"}
	gone.SetError(contract.ErrorReason{Reason: contract.ReasonStale})

	assert.Equal(t, apiError{Code: contract.ReasonStale, Message: "this task changed since you saw the plan"}, toAPIError(refused))
	assert.Equal(t, apiError{Code: contract.ReasonStale, Message: "workspace not found"}, toAPIError(gone))
	assert.Equal(t, apiError{Code: codeFailed, Message: "connection refused"}, toAPIError(&jsonrpc2.Error{Code: -32603, Message: "connection refused"}))
}
