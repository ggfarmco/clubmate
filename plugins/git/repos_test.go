package main_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

const graphsManifest = `{"id":"graphs","name":"Graphs","version":"0.1.0","protocol":1,"command":["echo"],"contributes":{"commands":[{"id":"graphs.trusted","title":"Trusted Graphs","hidden":true}]},"echoResults":{"graphs.trusted":{"graphs":["ship"]}}}`

type repoFacts struct {
	Name          string      `json:"name"`
	Path          string      `json:"path"`
	Remote        string      `json:"remote"`
	Branch        string      `json:"branch"`
	Clean         bool        `json:"clean"`
	Dirty         int         `json:"dirty"`
	Behind        int         `json:"behind"`
	Ahead         int         `json:"ahead"`
	DefaultBranch string      `json:"defaultBranch"`
	Fetched       bool        `json:"fetched"`
	LastCommit    time.Time   `json:"lastCommit"`
	Brings        repoBrings  `json:"brings"`
	Trusted       repoTrusted `json:"trusted"`
}

type repoBrings struct {
	Graphs       []string `json:"graphs"`
	Hooks        []string `json:"hooks"`
	Instructions []string `json:"instructions"`
	Skills       []string `json:"skills"`
}

type repoTrusted struct {
	Hooks  bool     `json:"hooks"`
	Graphs []string `json:"graphs"`
}

func bareOrigin(t *testing.T) string {
	t.Helper()
	seed := committedRepo(t)
	bare, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, bare, "clone", "-q", "--bare", seed, bare)
	return bare
}

func cloneOf(t *testing.T, bare string) string {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	git(t, repo, "clone", "-q", bare, repo)
	git(t, repo, "config", "user.name", "t")
	git(t, repo, "config", "user.email", "t@t")
	return repo
}

func pushCommit(t *testing.T, bare string) string {
	t.Helper()
	other := cloneOf(t, bare)
	git(t, other, "commit", "-q", "--allow-empty", "-m", "upstream")
	git(t, other, "push", "-q", "origin", "main")
	return git(t, other, "rev-parse", "HEAD")
}

func TestNewWorktreeSlugStartsFromFetchedDefault(t *testing.T) {
	bare := bareOrigin(t)
	repo := cloneOf(t, bare)
	git(t, repo, "switch", "-q", "-c", "unrelated")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "user's work")
	tip := pushCommit(t, bare)
	h, id := open(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "feat/pay", "slug": "pay", "base": "default", "switch": false})

	dir := filepath.Join(repo, ".worktrees", "pay")
	assert.Equal(t, opResult{Done: true, Path: dir}, res)
	assert.Equal(t, tip, git(t, dir, "rev-parse", "HEAD"))
	assert.Equal(t, "feat/pay", git(t, dir, "symbolic-ref", "--short", "HEAD"))
	assert.Contains(t, listWorktrees(t, h, id), worktreeEntry{Repo: repo, Path: dir, Branch: "feat/pay", Head: git(t, dir, "rev-parse", "--short", "HEAD")})
	assert.Empty(t, awaitOp(t, h, id, "git.worktree-remove", map[string]string{"path": dir}).Error)
	assert.NoDirExists(t, dir)
}

func TestNewWorktreeFailedFetchFallsBackToLocalDefault(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "local only")
	tip := git(t, repo, "rev-parse", "main")
	git(t, repo, "switch", "-q", "-c", "unrelated")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "user's work")
	git(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	h, id := open(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "feat/pay", "base": "default", "switch": false})

	assert.Empty(t, res.Error)
	assert.Contains(t, res.Note, "main")
	assert.Equal(t, tip, git(t, res.Path, "rev-parse", "HEAD"))
}

func TestNewWorktreeBaseLeavesExistingBranchAlone(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	git(t, repo, "branch", "feat/pay")
	git(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone"))
	h, id := open(t, repo)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "feat/pay", "base": "default", "switch": false})

	assert.Equal(t, opResult{Done: true, Path: filepath.Join(repo, ".worktrees", "feat", "pay")}, res)
}

func TestNewWorktreeRefusesBadSlugAndBase(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"slug climbs out", map[string]any{"branch": "x", "slug": "../x"}, "not a valid worktree name"},
		{"unknown base", map[string]any{"branch": "x", "base": "origin/main"}, "want one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := startOp(t, h, id, "git.worktree-new", tt.args)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func factsOf(t *testing.T, h *testutil.PluginHarness, id, path string) repoFacts {
	t.Helper()
	var facts repoFacts
	require.NoError(t, runCommand(t, h, id, "git.repo-facts", map[string]string{"path": path}, &facts))
	return facts
}

func TestCloneFromLocalBareRepo(t *testing.T) {
	bare := bareOrigin(t)
	into := filepath.Join(t.TempDir(), "code", "app")
	h, id := open(t, committedRepo(t))

	res := awaitOp(t, h, id, "git.clone", map[string]string{"url": bare, "into": into})

	assert.Equal(t, opResult{Done: true, Path: into}, res)
	assert.Equal(t, git(t, bare, "rev-parse", "main"), git(t, into, "rev-parse", "HEAD"))
}

func TestCloneFailureIsTheOperationsError(t *testing.T) {
	h, id := open(t, committedRepo(t))

	res := awaitOp(t, h, id, "git.clone", map[string]string{"url": filepath.Join(t.TempDir(), "gone"), "into": filepath.Join(t.TempDir(), "app")})

	assert.Contains(t, res.Error, "does not exist")
}

func TestCloneRefusesBadTarget(t *testing.T) {
	bare := bareOrigin(t)
	full := t.TempDir()
	write(t, filepath.Join(full, "kept"), "one\n")
	h, id := open(t, committedRepo(t))
	tests := []struct {
		name string
		args map[string]string
		want string
	}{
		{"non-empty folder", map[string]string{"url": bare, "into": full}, "not empty"},
		{"relative path", map[string]string{"url": bare, "into": "app"}, "must be absolute"},
		{"no url", map[string]string{"into": filepath.Join(full, "app")}, "args.url: required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := startOp(t, h, id, "git.clone", tt.args)
			assert.ErrorContains(t, err, tt.want)
		})
	}
	assert.FileExists(t, filepath.Join(full, "kept"))
}

func TestRepoFactsCountsUncommittedChanges(t *testing.T) {
	repo := committedRepo(t)
	write(t, filepath.Join(repo, "a.go"), "two\n")
	write(t, filepath.Join(repo, "new.go"), "one\n")
	h, id := open(t, committedRepo(t))

	facts := factsOf(t, h, id, repo)

	assert.False(t, facts.Clean)
	assert.Equal(t, 2, facts.Dirty)
}

func TestRepoFactsReadLocalRefsOutsideTheWorkspace(t *testing.T) {
	bare := bareOrigin(t)
	repo := cloneOf(t, bare)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "local")
	pushCommit(t, bare)
	pushCommit(t, bare)
	git(t, repo, "fetch", "-q", "origin")
	h, id := open(t, committedRepo(t))

	facts := factsOf(t, h, id, repo)

	assert.WithinDuration(t, time.Now(), facts.LastCommit, time.Minute)
	facts.LastCommit = time.Time{}
	assert.Equal(t, repoFacts{
		Name:          filepath.Base(repo),
		Path:          repo,
		Remote:        bare,
		Branch:        "main",
		Clean:         true,
		Behind:        2,
		Ahead:         1,
		DefaultBranch: "main",
		Brings:        repoBrings{Graphs: []string{}, Hooks: []string{}, Instructions: []string{}, Skills: []string{}},
		Trusted:       repoTrusted{Hooks: true, Graphs: []string{}},
	}, facts)
}

func TestRepoFactsComparesBranchWithoutUpstreamToDefault(t *testing.T) {
	bare := bareOrigin(t)
	repo := cloneOf(t, bare)
	git(t, repo, "switch", "-q", "-c", "unpushed")
	pushCommit(t, bare)
	git(t, repo, "fetch", "-q", "origin")
	h, id := open(t, committedRepo(t))

	assert.Equal(t, 1, factsOf(t, h, id, repo).Behind)
}

func TestRepoFactsWithoutRemote(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)

	facts := factsOf(t, h, id, repo)

	assert.Empty(t, facts.Remote)
	assert.False(t, facts.Fetched)
	assert.True(t, facts.Clean)
	assert.Equal(t, "main", facts.DefaultBranch)
}

func TestRepoFactsNamesTheRepoFromAWorktree(t *testing.T) {
	repo := committedRepo(t)
	dir := filepath.Join(repo, ".worktrees", "pay")
	git(t, repo, "worktree", "add", "-q", "-b", "feat/pay", dir)
	h, id := open(t, repo)

	facts := factsOf(t, h, id, dir)

	assert.Equal(t, filepath.Base(repo), facts.Name)
	assert.Equal(t, dir, facts.Path)
	assert.Equal(t, "feat/pay", facts.Branch)
}

func TestRepoFactsBrings(t *testing.T) {
	repo := committedRepo(t)
	for path, content := range map[string]string{
		".clubmate/graphs/ship.json":       "{}\n",
		".clubmate/graphs/Not A Name.json": "{}\n",
		".clubmate/setup.sh":               "true\n",
		".clubmate/worktree-include":       ".env\n",
		"CLAUDE.md":                        "rules\n",
		".claude/skills/land/SKILL.md":     "skill\n",
		".claude/skills/empty/notes.md":    "not a skill\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(repo, path)), 0o755))
		write(t, filepath.Join(repo, path), content)
	}
	h, id := open(t, committedRepo(t), testutil.EchoPluginDirAs(t, graphsManifest))

	facts := factsOf(t, h, id, repo)

	assert.Equal(t, repoBrings{Graphs: []string{"ship"}, Hooks: []string{"setup.sh"}, Instructions: []string{"CLAUDE.md"}, Skills: []string{"land"}}, facts.Brings)
	assert.Equal(t, repoTrusted{Graphs: []string{"ship"}}, facts.Trusted)
}

func TestRepoFactsHookTrustFollowsTheFolder(t *testing.T) {
	repo := hookRepo(t, map[string]string{"setup.sh": "true\n"})
	h, id := open(t, repo)
	assert.False(t, factsOf(t, h, id, repo).Trusted.Hooks)
	newApprover(t, h).answer.Store(true)
	assert.Empty(t, awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "x", "switch": false}).Error)

	assert.True(t, factsOf(t, h, id, repo).Trusted.Hooks)

	write(t, filepath.Join(repo, ".clubmate", "helper.sh"), "true\n")
	assert.False(t, factsOf(t, h, id, repo).Trusted.Hooks)
}

func TestRepoFactsRefusesWhatIsNotARepo(t *testing.T) {
	h, id := open(t, committedRepo(t))
	for _, path := range []string{t.TempDir(), "relative", filepath.Join(t.TempDir(), "gone")} {
		err := runCommand(t, h, id, "git.repo-facts", map[string]string{"path": path}, nil)
		assert.ErrorContains(t, err, "invalid_params")
	}
}

func TestRepoFactsRunNothingFromTheRepo(t *testing.T) {
	bare := bareOrigin(t)
	repo := cloneOf(t, bare)
	pushCommit(t, bare)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "evil.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\ncat\n"), 0o700))
	git(t, repo, "config", "remote.origin.uploadpack", script)
	git(t, repo, "config", "filter.a=b.clean", script)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt filter=a=b\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "x.txt"), []byte("x\n"), 0o600))
	git(t, repo, "config", "--unset", "filter.a=b.clean")
	git(t, repo, "add", ".gitattributes", "x.txt")
	git(t, repo, "commit", "-qm", "attributes")
	git(t, repo, "config", "filter.a=b.clean", script)
	require.NoError(t, os.WriteFile(filepath.Join(repo, "x.txt"), []byte("y\n"), 0o600))
	h, id := open(t, committedRepo(t))

	facts := factsOf(t, h, id, repo)

	assert.False(t, facts.Fetched)
	assert.Zero(t, facts.Behind)
	assert.NoFileExists(t, marker)
}

func TestRepoFactsNeverRunFsmonitor(t *testing.T) {
	repo := cloneOf(t, bareOrigin(t))
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "fsmonitor.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700))
	git(t, repo, "config", "core.fsmonitor", script)
	h, id := open(t, committedRepo(t))

	factsOf(t, h, id, repo)

	assert.NoFileExists(t, marker)
}

func TestCloneLeavesTheWorkspaceFree(t *testing.T) {
	repo := committedRepo(t)
	h, id := open(t, repo)
	into := filepath.Join(t.TempDir(), "slow")
	_, err := startOp(t, h, id, "git.clone", map[string]string{"url": "ext::sh -c \"sleep 3\"", "into": into})
	require.NoError(t, err)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"branch": "feat/pay", "switch": false})

	assert.Empty(t, res.Error)
}

func TestUnseenAFKFolderRunsNothingOfItsOwnOnCheckoutUntilSeen(t *testing.T) {
	includes := map[string]string{
		"in the repo's config":   "",
		"included on the branch": "includeIf.onbranch:feat/**.path",
		"included in a worktree": "includeIf.gitdir:**/worktrees/**.path",
	}
	for name, include := range includes {
		t.Run(name, func(t *testing.T) {
			marks := t.TempDir()
			hook, configHook, smudge := filepath.Join(marks, "hook"), filepath.Join(marks, "config-hook"), filepath.Join(marks, "smudge")
			repo := committedRepo(t)
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("* filter=own\n"), 0o644))
			git(t, repo, "add", ".")
			git(t, repo, "commit", "-qm", "attributes")
			require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(repo, ".git", "hooks", "post-checkout"), []byte("#!/bin/sh\ntouch "+hook+"\n"), 0o700))
			config := filepath.Join(repo, ".git", "config")
			if include != "" {
				config = filepath.Join(repo, ".git", "own.cfg")
				git(t, repo, "config", include, "own.cfg")
			}
			git(t, repo, "config", "-f", config, "hook.a=b.event", "post-index-change")
			git(t, repo, "config", "-f", config, "hook.a=b.command", "touch "+configHook+" #")
			git(t, repo, "config", "-f", config, "filter.own.smudge", "touch "+smudge+"; cat")
			git(t, repo, "config", "-f", config, "filter.own.required", "true")
			h, id := open(t, committedRepo(t))
			_, err := h.Svc.AddRepo(t.Context(), id, repo, contract.FolderMeta{AddedBy: "Fix login", AddedAfk: true})
			require.NoError(t, err)

			res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"repo": repo, "branch": "feat/pay", "switch": false})

			assert.Empty(t, res.Error)
			assert.FileExists(t, filepath.Join(res.Path, "a.go"))
			assert.NoFileExists(t, hook)
			assert.NoFileExists(t, configHook)
			assert.NoFileExists(t, smudge)

			_, err = h.Svc.MarkFolderSeen(t.Context(), id, repo)
			require.NoError(t, err)

			res = awaitOp(t, h, id, "git.worktree-new", map[string]any{"repo": repo, "branch": "feat/more", "switch": false})

			assert.Empty(t, res.Error)
			assert.FileExists(t, hook)
			assert.FileExists(t, smudge)
		})
	}
}

func TestUnseenAFKFolderLeavesNoHalfMadeWorktree(t *testing.T) {
	repo := committedRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".git", "broken.cfg"), []byte("["), 0o644))
	git(t, repo, "config", "includeIf.onbranch:feat/**.path", "broken.cfg")
	h, id := open(t, committedRepo(t))
	_, err := h.Svc.AddRepo(t.Context(), id, repo, contract.FolderMeta{AddedBy: "Fix login", AddedAfk: true})
	require.NoError(t, err)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"repo": repo, "branch": "feat/pay", "switch": false})

	assert.NotEmpty(t, res.Error)
	assert.NotContains(t, git(t, repo, "worktree", "list"), "feat/pay")
	assert.Empty(t, git(t, repo, "branch", "--list", "feat/pay"))
}

func TestUnseenAFKFolderStartsFromLocalDefaultUntilSeen(t *testing.T) {
	bare := bareOrigin(t)
	repo := cloneOf(t, bare)
	tip := git(t, repo, "rev-parse", "main")
	upstream := pushCommit(t, bare)
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "pack.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\nexec git upload-pack \"$@\"\n"), 0o700))
	git(t, repo, "config", "remote.origin.uploadpack", script)
	h, id := open(t, committedRepo(t))
	_, err := h.Svc.AddRepo(t.Context(), id, repo, contract.FolderMeta{AddedBy: "Fix login", AddedAfk: true})
	require.NoError(t, err)

	res := awaitOp(t, h, id, "git.worktree-new", map[string]any{"repo": repo, "branch": "feat/pay", "base": "default", "switch": false})

	assert.Empty(t, res.Error)
	assert.Contains(t, res.Note, "main")
	assert.Equal(t, tip, git(t, res.Path, "rev-parse", "HEAD"))
	assert.NoFileExists(t, marker)

	_, err = h.Svc.MarkFolderSeen(t.Context(), id, repo)
	require.NoError(t, err)

	res = awaitOp(t, h, id, "git.worktree-new", map[string]any{"repo": repo, "branch": "feat/more", "base": "default", "switch": false})

	assert.Empty(t, res.Note)
	assert.Equal(t, upstream, git(t, res.Path, "rev-parse", "HEAD"))
	assert.FileExists(t, marker)
}
