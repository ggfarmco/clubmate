package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/internal/gitrepo"
)

func TestStatusMap(t *testing.T) {
	entries := []gitrepo.Entry{
		{Path: "/r/a.go", X: ' ', Y: 'M'},
		{Path: "/r/new", X: '?', Y: '?'},
		{Path: "/r/e.go", Orig: "/r/d.go", X: 'R', Y: ' '},
		{Path: "/r/x", X: 'A', Y: ' '},
		{Path: "/r/gone", X: ' ', Y: 'D'},
		{Path: "/r/c", X: 'U', Y: 'U'},
		{Path: "/r/bin", X: '!', Y: '!'},
	}
	assert.Equal(t, map[string]string{
		"/r/a.go": "M",
		"/r/new":  "U",
		"/r/e.go": "R",
		"/r/x":    "A",
		"/r/gone": "D",
		"/r/c":    "U",
		"/r/bin":  "!",
	}, statusMap(entries))
}
