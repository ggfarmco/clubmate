package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
)

func TestFolderRoot(t *testing.T) {
	tests := []struct {
		name   string
		folder contract.Folder
		want   string
	}{
		{name: "plain", folder: contract.Folder{Path: "/a"}, want: "/a"},
		{name: "worktree", folder: contract.Folder{Path: "/a", Worktree: "/a/wt"}, want: "/a/wt"},
		{name: "missing", folder: contract.Folder{Path: "/a", Worktree: "/a/gone", Missing: true}, want: "/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.folder.Root())
		})
	}
}

func TestWorkspaceJSONCarriesLegacyFolderFields(t *testing.T) {
	w := contract.Workspace{ID: "prj_1", Folders: []contract.Folder{
		{Path: "/a"},
		{Path: "/b", Worktree: "/b/wt"},
		{Path: "/c", Worktree: "/c/gone", Missing: true},
	}}
	raw, err := json.Marshal(w)
	require.NoError(t, err)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.JSONEq(t, `[{"path":"/a"},{"path":"/b","worktree":"/b/wt"},{"path":"/c","worktree":"/c/gone","missing":true}]`, string(got["folders"]))
	assert.JSONEq(t, `["/a","/b","/c"]`, string(got["repos"]))
	assert.JSONEq(t, `["/a","/b/wt","/c"]`, string(got["roots"]))
	assert.JSONEq(t, `["/c/gone"]`, string(got["missing"]))
	var back contract.Workspace
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, w, back)
}

func TestEmptyWorkspaceJSONHasNoNullFolders(t *testing.T) {
	raw, err := json.Marshal(contract.Workspace{ID: "prj_1"})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"folders":[]`)
	assert.Contains(t, string(raw), `"repos":[]`)
	assert.Contains(t, string(raw), `"roots":[]`)
	assert.NotContains(t, string(raw), `"missing"`)
}
