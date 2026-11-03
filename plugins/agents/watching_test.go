package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShownSlotsWhilePhoneWatches(t *testing.T) {
	t.Parallel()
	a := &app{uiState: newUIState(), codePanel: true}
	a.present["p"] = true
	a.setSection("p", "processes")
	assert.Equal(t, []string{sidebarSlot}, a.shownSlots("p"))

	a.setWatched("p", []string{sectionID, editorSection})
	assert.Equal(t, []string{sidebarSlot, mainSlot, codeSlot}, a.shownSlots("p"))
}
