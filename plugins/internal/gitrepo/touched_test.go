package gitrepo_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
)

func TestTouchedFiles(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	for _, name := range []string{"edited.go", "gone.go", "same.go", ".gitignore"} {
		write(t, filepath.Join(repo, name), "x\n")
	}
	write(t, filepath.Join(repo, ".gitignore"), "*.log\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "init")
	git(t, repo, "switch", "-qc", "feat")
	write(t, filepath.Join(repo, "committed.go"), "c\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "feat")
	write(t, filepath.Join(repo, "edited.go"), "y\n")
	git(t, repo, "rm", "-q", "gone.go")
	write(t, filepath.Join(repo, "new.go"), "n\n")
	write(t, filepath.Join(repo, "debug.log"), "l\n")

	got, err := gitrepo.TouchedFiles(t.Context(), repo, "main")

	require.NoError(t, err)
	assert.Equal(t, []string{"committed.go", "edited.go", "gone.go", "new.go"}, got.Files)
	assert.Equal(t, []string{"new.go"}, got.Untracked)
	assert.NotEmpty(t, got.MergeBase)
}
