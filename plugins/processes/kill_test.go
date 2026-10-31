package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/pkg/pluginsdk"
)

func TestSubtreeSurvivesCycles(t *testing.T) {
	nodes := []pluginsdk.ProcessNode{
		{Process: pluginsdk.ProcessInfo{PID: 1, PPID: 1}},
		{Process: pluginsdk.ProcessInfo{PID: 2, PPID: 1}},
		{Process: pluginsdk.ProcessInfo{PID: 3, PPID: 4}},
		{Process: pluginsdk.ProcessInfo{PID: 4, PPID: 3}},
	}
	assert.Equal(t, []int{1, 2}, subtree(nodes, 1))
	assert.Equal(t, []int{3, 4}, subtree(nodes, 3))
}
