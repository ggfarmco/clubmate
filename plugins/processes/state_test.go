package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeletedProjectsExpire(t *testing.T) {
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
