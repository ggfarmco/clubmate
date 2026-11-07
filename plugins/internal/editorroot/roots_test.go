package editorroot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

func TestWantMovesTheRootToASelectedFolderItAlreadyHas(t *testing.T) {
	t.Parallel()
	a, b := t.TempDir(), t.TempDir()
	p := pluginsdk.Workspace{ID: "w", Folders: []contract.Folder{{Path: a}, {Path: b}}}
	roots := NewRoots()
	_, _, _, err := roots.Select(p, []string{a, b}, "")
	require.NoError(t, err)

	want, err := Want(p, b)
	require.NoError(t, err)
	root, moved, ok, err := roots.Select(p, []string{a, b}, want)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, moved)
	assert.Equal(t, b, root)
	assert.Equal(t, CodeRoot{Folder: b, Root: b}, Describe(p, root))
}

func TestWantRefusesAPathThatIsNoFolder(t *testing.T) {
	t.Parallel()
	_, err := Want(pluginsdk.Workspace{Folders: []contract.Folder{{Path: "/a"}}}, "/b")
	assert.ErrorIs(t, err, ErrNotFolder)
}
