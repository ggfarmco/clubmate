package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveSelection(t *testing.T) {
	two := []terminal{{ID: "a"}, {ID: "b"}}
	assert.Equal(t, "b", resolveSelection(two, "b"))
	assert.Equal(t, "a", resolveSelection(two, "gone"))
	assert.Equal(t, "a", resolveSelection(two, ""))
	assert.Equal(t, "", resolveSelection(nil, "gone"))
}
