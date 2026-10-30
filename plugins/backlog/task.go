package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	statusIdea       = "idea"
	statusTodo       = "todo"
	statusInProgress = "in_progress"
	statusDone       = "done"
	statusNotNow     = "not_now"

	priorityHigh   = "high"
	priorityNormal = "normal"
	priorityLow    = "low"

	maxTitleChars = 200
	maxDescBytes  = 32 << 10
)

var (
	errTitleEmpty   = errors.New("title must not be empty")
	errTitleLine    = errors.New("title must be one line")
	errTitleLong    = fmt.Errorf("title must be at most %d characters", maxTitleChars)
	errDescLong     = fmt.Errorf("description must be at most %d KiB", maxDescBytes>>10)
	errBadStatus    = errors.New("status must be one of idea, todo, in_progress, done, not_now")
	errBadPriority  = errors.New("priority must be high, normal or low")
	errTaskNotFound = errors.New("task not found")
)

type Task struct {
	ID            int       `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description,omitempty"`
	Status        string    `json:"status"`
	Priority      string    `json:"priority"`
	ClaimedBy     string    `json:"claimedBy,omitempty"`
	ClaimedAt     time.Time `json:"claimedAt,omitzero"`
	ClaimedFrom   string    `json:"claimedFrom,omitempty"`
	LastClaimedBy string    `json:"lastClaimedBy,omitempty"`
	Note          string    `json:"note,omitempty"`
	CreatedBy     string    `json:"createdBy"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func validStatus(s string) bool {
	return slices.Contains([]string{statusIdea, statusTodo, statusInProgress, statusDone, statusNotNow}, s)
}

func validPriority(p string) bool {
	return slices.Contains([]string{priorityHigh, priorityNormal, priorityLow}, p)
}

func cleanTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", errTitleEmpty
	case strings.ContainsAny(title, "\r\n"):
		return "", errTitleLine
	case utf8.RuneCountInString(title) > maxTitleChars:
		return "", errTitleLong
	}
	return title, nil
}

func checkDescription(d string) error {
	if len(d) > maxDescBytes {
		return errDescLong
	}
	return nil
}

func priorityRank(p string) int {
	switch p {
	case priorityHigh:
		return 0
	case priorityLow:
		return 2
	}
	return 1
}

func sortTasks(tasks []Task) {
	slices.SortFunc(tasks, func(a, b Task) int {
		aDone, bDone := a.Status == statusDone, b.Status == statusDone
		switch {
		case aDone != bDone:
			if aDone {
				return 1
			}
			return -1
		case aDone:
			return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(b.ID, a.ID))
		}
		return cmp.Or(cmp.Compare(priorityRank(a.Priority), priorityRank(b.Priority)), cmp.Compare(a.ID, b.ID))
	})
}
