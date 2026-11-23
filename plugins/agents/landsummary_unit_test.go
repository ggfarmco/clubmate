package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/plugins/internal/landgate"
)

func TestOnlyAReviewersApprovalCarriesASummary(t *testing.T) {
	t.Parallel()
	for by, want := range map[string]string{landgate.ByReviewer: "Sums it up.", landgate.ByUser: "", landgate.ByDocs: ""} {
		assert.Equal(t, want, reviewedSummary(landgate.Verdict{Outcome: gateApproved, By: by, Summary: "Sums it up."}, true), by)
	}
}
