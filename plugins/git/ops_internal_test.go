package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpIDsDoNotRepeatAcrossRestarts(t *testing.T) {
	var before, after opTable
	assert.NotEqual(t, before.add("p").id, after.add("p").id)
}
