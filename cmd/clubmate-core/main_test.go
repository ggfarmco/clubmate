package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		run    bool
		stdout string
		stderr string
	}{
		{name: "none", run: true},
		{name: "help", args: []string{"--help"}, stdout: "usage: clubmate-core"},
		{name: "version", args: []string{"-version"}, stdout: "0.0.0-dev"},
		{name: "unknown flag", args: []string{"--bogus"}, code: 2, stderr: "flag provided but not defined: -bogus"},
		{name: "argument", args: []string{"start"}, code: 2, stderr: `unexpected argument "start"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, run := parseArgs(tt.args, &stdout, &stderr)
			assert.Equal(t, tt.code, code)
			assert.Equal(t, tt.run, run)
			assert.Contains(t, stdout.String(), tt.stdout)
			assert.Contains(t, stderr.String(), tt.stderr)
		})
	}
}
