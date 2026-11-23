package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGateSummaryLandsBeforeTheTrailers(t *testing.T) {
	t.Parallel()
	g := startGate(t)
	require.Error(t, g.land(t))
	g.approveWithSummary(t, "Groups the inbox by workspace.")

	require.NoError(t, g.f.runAs(t, g.workspace, g.lander, "git.land", map[string]any{"path": g.worktree, "message": "feat: b\n\nBody.\n\nCo-Authored-By: A <a@a>"}, nil))

	assert.Equal(t, "A <a@a>", git(t, g.repo, "log", "-1", "--format=%(trailers:key=Co-Authored-By,valueonly)", "main"))
	assert.Equal(t, "Body.\n\nGroups the inbox by workspace.\n\nCo-Authored-By: A <a@a>", git(t, g.repo, "log", "-1", "--format=%b", "main"))
}
