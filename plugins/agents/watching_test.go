package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShownSlotsWhilePhoneWatches(t *testing.T) {
	t.Parallel()
	a := &app{uiState: newUIState(), codePanel: true}
	a.present["p"] = true
	a.setSection("p", "processes")
	assert.Equal(t, []string{sidebarSlot, sheetSlot}, a.shownSlots("p"))

	a.setWatched("p", []string{sectionID, editorSection})
	assert.Equal(t, []string{sidebarSlot, sheetSlot, mainSlot, codeSlot}, a.shownSlots("p"))
}

func TestForgetWorkspaceDropsItsRemindersAndRunNeeds(t *testing.T) {
	t.Parallel()
	a := &app{uiState: newUIState()}
	a.reminders.set("p", []reminder{{Session: "agt_1", At: time.Unix(100, 0)}})
	a.runNeeds.set("p", []runNeed{{Run: "run_1"}})

	a.forgetWorkspace("p")

	assert.NotContains(t, a.reminders.byWS, "p")
	assert.NotContains(t, a.runNeeds.byWS, "p")
}
