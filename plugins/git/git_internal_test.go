package main

import (
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"

	"clubmate/pkg/contract"
)

func TestRPCMessage(t *testing.T) {
	withData := &jsonrpc2.Error{Code: contract.CodePluginError, Message: "plugin editor-nvim command.run: unsaved buffers: a.go"}
	withData.SetError(contract.PluginErrorData{Plugin: "editor-nvim", Method: "command.run", Message: "unsaved buffers: a.go"})
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"plugin error data", withData, "unsaved buffers: a.go"},
		{"plain rpc error", &jsonrpc2.Error{Code: contract.CodePluginError, Message: "unsaved buffers: a.go; nvim gone"}, "unsaved buffers: a.go; nvim gone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rpcMessage(tt.err))
		})
	}
}
