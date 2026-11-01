package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeletedWorkspacesExpire(t *testing.T) {
	now := time.Now()
	a := newApp(0)
	a.forget("p", now)
	a.setSection("p", sectionID)
	assert.NotContains(t, a.sections, "p")

	a.pruneDeleted(now.Add(deletedTTL - time.Second))
	assert.Contains(t, a.deleted, "p")
	a.pruneDeleted(now.Add(deletedTTL))
	assert.Empty(t, a.deleted)
}

func TestVisibleWhilePhoneWatches(t *testing.T) {
	a := newApp(0)
	a.setSection("p", "agents")
	assert.False(t, a.visible("p"))

	assert.True(t, a.setWatched("p", []string{"agents", sectionID}))
	assert.True(t, a.visible("p"))
	assert.False(t, a.setWatched("p", []string{sectionID}))

	a.setWatched("p", []string{})
	assert.False(t, a.visible("p"))
}
