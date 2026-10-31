package main

import (
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
)

func TestEditorErrorTrimsOnlyPluginErrors(t *testing.T) {
	const message = "plugin editor-nvim command.run: nvim is not running"
	tests := []struct {
		name string
		code int64
		want string
	}{
		{"plugin error", codePluginError, "nvim is not running"},
		{"core error", jsonrpc2.CodeInternalError, message},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, editorError(&jsonrpc2.Error{Code: tt.code, Message: message}))
		})
	}
}
