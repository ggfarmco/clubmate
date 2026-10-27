package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePorcelain(t *testing.T) {
	out := []byte(" M a.go\x00?? new dir/\x00R  new name.go\x00old\x00A  x\x00 D gone\x00UU c\x00")
	assert.Equal(t, map[string]string{
		"/r/a.go":        "M",
		"/r/new dir":     "U",
		"/r/new name.go": "R",
		"/r/x":           "A",
		"/r/gone":        "D",
		"/r/c":           "U",
	}, parsePorcelain("/r", out))
}
