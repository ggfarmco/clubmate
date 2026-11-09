package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestSwitchHoldsBusyGateWhileSettingWorktree(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	feat := filepath.Join(repo, ".worktrees", "feat")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"worktree", "add", "-q", "-b", "feat", feat},
	} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	var s state
	busyDuringSet := make(chan bool, 1)
	h := fakeCore(t, func(req *jsonrpc2.Request) (any, error) {
		p := pluginsdk.Workspace{ID: "p", Folders: []pluginsdk.Folder{{Path: repo}}}
		if req.Method == "workspace.worktree.set" {
			busyDuringSet <- !s.claim()
		}
		return p, nil
	})

	require.NoError(t, switchWorktree(t.Context(), h, "p", &s, "", feat))
	assert.True(t, <-busyDuringSet)
	assert.True(t, s.claim())
}

func TestRemoteForDisplayDropsCredentials(t *testing.T) {
	tests := []struct{ name, url, want string }{
		{"https token", "https://ghp_secret@github.com/a/b.git", "https://github.com/a/b.git"},
		{"https password", "https://me:pw@github.com/a/b.git", "https://github.com/a/b.git"},
		{"scp form", "git@github.com:a/b.git", "git@github.com:a/b.git"},
		{"ssh keeps the login", "ssh://git@github.com/a/b.git", "ssh://git@github.com/a/b.git"},
		{"local path", "/srv/git/b.git", "/srv/git/b.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, remoteForDisplay(tt.url))
		})
	}
}

func fakeCore(t *testing.T, handle func(*jsonrpc2.Request) (any, error)) *pluginsdk.Host {
	t.Helper()
	coreSide, pluginSide := net.Pipe()
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		return handle(req)
	})
	conn := jsonrpc2.NewConn(t.Context(), jsonrpc2.NewPlainObjectStream(coreSide), handler)
	t.Cleanup(func() { conn.Close() })
	ready := make(chan *pluginsdk.Host, 1)
	init := func(_ context.Context, h *pluginsdk.Host) error {
		ready <- h
		return nil
	}
	go func() { _ = pluginsdk.RunConn(t.Context(), pluginSide, pluginsdk.Plugin{Init: init}) }()
	require.NoError(t, conn.Call(t.Context(), "plugin.initialize", map[string]any{"protocol": pluginsdk.ProtocolVersion, "pluginId": "git"}, nil))
	select {
	case h := <-ready:
		return h
	case <-time.After(3 * time.Second):
		t.Fatal("Init not called")
		return nil
	}
}

func branchRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	gitT(t, repo, "config", "user.name", "t")
	gitT(t, repo, "config", "user.email", "t@t")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("one\n"), 0o600))
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-qm", "one")
	gitT(t, repo, "checkout", "-q", "-b", "feat")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "b.go"), []byte("two\n"), 0o600))
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-qm", "two")
	gitT(t, repo, "checkout", "-q", "main")
	return repo
}

func gitT(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func TestDeleteBranchKeepsWhatGitWouldRefuse(t *testing.T) {
	t.Parallel()
	repo := branchRepo(t)
	tip := gitT(t, repo, "rev-parse", "feat")

	require.Error(t, deleteBranchIn(t.Context(), repo, "feat", tip))
	require.ErrorIs(t, deleteBranchIn(t.Context(), repo, "feat", "0000"), errBranchMoved)
	require.ErrorIs(t, deleteBranchIn(t.Context(), repo, "main", gitT(t, repo, "rev-parse", "main")), errDefaultBranch)

	assert.Equal(t, tip, gitT(t, repo, "rev-parse", "feat"))
}

func TestDeleteBranchKeepsABranchOnTheRemote(t *testing.T) {
	t.Parallel()
	repo := branchRepo(t)
	tip := gitT(t, repo, "rev-parse", "feat")
	gitT(t, repo, "merge", "-q", "feat")
	gitT(t, repo, "update-ref", "refs/remotes/origin/feat", tip)

	require.ErrorIs(t, deleteBranchIn(t.Context(), repo, "feat", tip), errBranchOnRemote)

	assert.Equal(t, tip, gitT(t, repo, "rev-parse", "refs/remotes/origin/feat"))
	assert.Equal(t, tip, gitT(t, repo, "rev-parse", "refs/heads/feat"))
}

func TestDeleteBranchTakesWhatLanded(t *testing.T) {
	t.Parallel()
	tests := map[string]func(t *testing.T, repo string){
		"merged": func(t *testing.T, repo string) { gitT(t, repo, "merge", "-q", "--no-ff", "-m", "m", "feat") },
		"squashed": func(t *testing.T, repo string) {
			gitT(t, repo, "merge", "-q", "--squash", "feat")
			gitT(t, repo, "commit", "-qm", "s")
		},
	}
	for name, land := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := branchRepo(t)
			tip := gitT(t, repo, "rev-parse", "feat")
			land(t, repo)
			hooks, marker := t.TempDir(), filepath.Join(t.TempDir(), "ran")
			hook := "#!/bin/sh\ntouch " + marker + "\n"
			require.NoError(t, os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(hook), 0o755))
			gitT(t, repo, "config", "core.hooksPath", hooks)
			gitT(t, repo, "config", "branch.feat.remote", "origin")

			require.NoError(t, deleteBranchIn(t.Context(), repo, "feat", tip))

			assert.Empty(t, gitT(t, repo, "branch", "--list", "feat"))
			assert.NoFileExists(t, marker)
			assert.NotContains(t, gitT(t, repo, "config", "list", "--local", "--name-only"), "branch.feat.")
		})
	}
}

func TestDeleteBranchKeepsALandedBranchMovedOrInUse(t *testing.T) {
	t.Parallel()
	repo := branchRepo(t)
	tip := gitT(t, repo, "rev-parse", "feat")
	gitT(t, repo, "merge", "-q", "--squash", "feat")
	gitT(t, repo, "commit", "-qm", "s")

	require.ErrorIs(t, forceDeleteBranch(t.Context(), repo, "feat", gitT(t, repo, "rev-parse", "main")), errBranchMoved)
	gitT(t, repo, "worktree", "add", "-q", filepath.Join(t.TempDir(), "feat"), "feat")
	require.Error(t, deleteBranchIn(t.Context(), repo, "feat", tip))

	assert.Equal(t, tip, gitT(t, repo, "rev-parse", "feat"))
}

func TestDeleteBranchWithoutExactProofStays(t *testing.T) {
	t.Parallel()
	repo := branchRepo(t)
	tip := gitT(t, repo, "rev-parse", "feat")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "b.go"), []byte("tw o\n"), 0o600))
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-qm", "nearly the same")

	require.Error(t, deleteBranchIn(t.Context(), repo, "feat", tip))

	assert.Equal(t, tip, gitT(t, repo, "rev-parse", "feat"))
}

func TestDeleteBranchWithNoNetChangeStays(t *testing.T) {
	t.Parallel()
	repo := branchRepo(t)
	gitT(t, repo, "checkout", "-q", "feat")
	gitT(t, repo, "revert", "--no-edit", "HEAD")
	gitT(t, repo, "checkout", "-q", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "later.go"), []byte("later\n"), 0o600))
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-qm", "later")
	tip := gitT(t, repo, "rev-parse", "feat")

	require.Error(t, deleteBranchIn(t.Context(), repo, "feat", tip))

	assert.Equal(t, tip, gitT(t, repo, "rev-parse", "feat"))
}
