package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

type gateTreeResult struct {
	Path   string `json:"path"`
	Commit string `json:"commit"`
}

func gateTree(t *testing.T, h *testutil.PluginHarness, id string, args map[string]any) (gateTreeResult, error) {
	t.Helper()
	var out gateTreeResult
	err := h.PluginCallResult(t, "agents", "command.run", map[string]any{"id": "git.gate-tree", "workspaceId": id, "args": args}, &out)
	return out, err
}

func TestGateTreeReusedAcrossRounds(t *testing.T) {
	repo, feat := featWorktree(t)
	h, id := open(t, repo)
	old := git(t, repo, "rev-parse", "main")
	first := git(t, feat, "rev-parse", "HEAD^{tree}")

	one, err := gateTree(t, h, id, map[string]any{"repo": repo, "gate": "gt_1", "key": "lt_1", "old": old, "tree": first})
	require.NoError(t, err)
	write(t, filepath.Join(one.Path, "s.go"), "dirty\n")
	write(t, filepath.Join(one.Path, "junk.txt"), "left\n")
	write(t, filepath.Join(feat, "c.go"), "three\n")
	git(t, feat, "add", ".")
	git(t, feat, "commit", "-qm", "three")
	second := git(t, feat, "rev-parse", "HEAD^{tree}")
	two, err := gateTree(t, h, id, map[string]any{"repo": repo, "gate": "gt_1", "key": "lt_1", "old": old, "tree": second})
	require.NoError(t, err)

	assert.Equal(t, one.Path, two.Path)
	assert.Equal(t, filepath.Join(repo, ".worktrees", ".gate-gt_1-lt_1"), two.Path)
	assert.Equal(t, two.Commit, git(t, two.Path, "rev-parse", "HEAD"))
	assert.Equal(t, []string{second, old}, []string{git(t, two.Path, "rev-parse", "HEAD^{tree}"), git(t, two.Path, "rev-parse", "HEAD^")})
	assert.Empty(t, git(t, two.Path, "status", "--porcelain"))
	assert.Len(t, listWorktrees(t, h, id), 2)
}

func TestGateTreeKeepsItsCommitForTheSameTree(t *testing.T) {
	repo, feat := featWorktree(t)
	h, id := open(t, repo)
	args := map[string]any{"repo": repo, "gate": "gt_1", "key": "lt_1", "old": git(t, repo, "rev-parse", "main"), "tree": git(t, feat, "rev-parse", "HEAD^{tree}")}
	one, err := gateTree(t, h, id, args)
	require.NoError(t, err)
	git(t, one.Path, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--amend", "-qm", "same tree")
	kept := git(t, one.Path, "rev-parse", "HEAD")
	write(t, filepath.Join(one.Path, "junk.txt"), "left\n")

	two, err := gateTree(t, h, id, args)

	require.NoError(t, err)
	assert.Equal(t, kept, two.Commit)
	assert.Empty(t, git(t, two.Path, "status", "--porcelain"))
}

func TestGateTreeRecoversAHandDeletedDir(t *testing.T) {
	repo, feat := featWorktree(t)
	h, id := open(t, repo)
	args := map[string]any{"repo": repo, "gate": "gt_1", "key": "lt_1", "old": git(t, repo, "rev-parse", "main"), "tree": git(t, feat, "rev-parse", "HEAD^{tree}")}
	one, err := gateTree(t, h, id, args)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(one.Path))

	two, err := gateTree(t, h, id, args)

	require.NoError(t, err)
	assert.Equal(t, args["tree"], git(t, two.Path, "rev-parse", "HEAD^{tree}"))
}

func TestGateTreeRemove(t *testing.T) {
	repo, feat := featWorktree(t)
	h, id := open(t, repo)
	made := map[string]string{}
	for _, k := range [][2]string{{"gt_1", "lt_1"}, {"gt_1", "lt_2"}, {"gt_2", "lt_1"}} {
		out, err := gateTree(t, h, id, map[string]any{"repo": repo, "gate": k[0], "key": k[1], "old": git(t, repo, "rev-parse", "main"), "tree": git(t, feat, "rev-parse", "HEAD^{tree}")})
		require.NoError(t, err)
		made[k[0]+"/"+k[1]] = out.Path
	}

	_, err := gateTree(t, h, id, map[string]any{"repo": repo, "gate": "gt_1", "key": "lt_1", "remove": true})
	require.NoError(t, err)
	assert.NoDirExists(t, made["gt_1/lt_1"])
	assert.DirExists(t, made["gt_1/lt_2"])

	_, err = gateTree(t, h, id, map[string]any{"repo": repo, "gate": "gt_1", "remove": true})

	require.NoError(t, err)
	assert.NoDirExists(t, made["gt_1/lt_2"])
	assert.DirExists(t, made["gt_2/lt_1"])
	assert.NotContains(t, git(t, repo, "worktree", "list"), ".gate-gt_1")
}

func TestGateTreeOnlyForTheAgentsPlugin(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)

	err := runCommand(t, h, id, "git.gate-tree", map[string]any{"repo": repo, "gate": "gt_1", "remove": true}, nil)

	assert.ErrorContains(t, err, "agents plugin")
}
