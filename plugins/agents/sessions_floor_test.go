package main

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionsPagePrunesOldRemovalsAndAnswersOlderCursorsInFull(t *testing.T) {
	var r sessionRevs
	infos := make([]sessionInfo, keptRemoved+2)
	for i := range infos {
		infos[i].ID = "agt_" + strconv.Itoa(i)
	}
	before := r.page("w", infos, "").Cursor
	r.page("w", infos[2:], "")
	r.page("w", infos[keptRemoved+1:], "")
	after := r.page("w", nil, "").Cursor

	assert.Len(t, r.workspaces["w"].seen, keptRemoved)
	assert.False(t, r.page("w", nil, before).Delta)
	page := r.page("w", nil, after)
	assert.True(t, page.Delta)
	assert.Empty(t, page.Removed)
}
