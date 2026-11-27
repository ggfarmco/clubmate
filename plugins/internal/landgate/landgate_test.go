package landgate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

type gitEnv struct {
	t     *testing.T
	hooks string
	marks string
}

func newGitEnv(t *testing.T) gitEnv {
	root := t.TempDir()
	g := gitEnv{t: t, hooks: filepath.Join(root, "hooks"), marks: filepath.Join(root, "verdicts")}
	require.NoError(t, landgate.Install(g.hooks))
	return g
}

func baseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "CLUBMATE_") {
			out = append(out, kv)
		}
	}
	return append(out, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
}

func (g gitEnv) agent(common string) []string {
	return g.agentGlob(common, nil)
}

func (g gitEnv) agentGlob(common string, glob []string) []string {
	return g.session(common, []string{"refs/heads/main"}, glob)
}

func (g gitEnv) session(common string, protect, glob []string) []string {
	env := baseEnv()
	for k, v := range landgate.SessionEnv(env, g.hooks, common, protect, glob, g.marks, landgate.QueueDir(g.hooks, common)) {
		env = append(env, k+"="+v)
	}
	return env
}

func (g gitEnv) hold(common, branch string) {
	mark := filepath.Join(landgate.QueueDir(g.hooks, common), "refs", "heads", branch)
	require.NoError(g.t, os.MkdirAll(filepath.Dir(mark), 0o755))
	require.NoError(g.t, os.WriteFile(mark, []byte("agt_head\n"), 0o644))
}

func (g gitEnv) repo() string {
	dir := gitrepo.Canonical(g.t.TempDir())
	g.run(dir, baseEnv(), "init", "-q", "-b", "main")
	g.run(dir, baseEnv(), "commit", "-q", "--allow-empty", "-m", "first")
	return dir
}

func (g gitEnv) git(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (g gitEnv) run(dir string, env []string, args ...string) string {
	out, err := g.git(dir, env, args...)
	require.NoError(g.t, err, out)
	return out
}

func TestHookRefusesProtectedUpdateWithoutVerdict(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	env := g.agent(filepath.Join(repo, ".git"))
	before := g.run(repo, env, "rev-parse", "main")

	out, err := g.git(repo, env, "commit", "--no-verify", "--allow-empty", "-m", "sneak")
	require.Error(t, err)
	assert.Contains(t, out, "git_land")
	assert.Equal(t, before, g.run(repo, env, "rev-parse", "main"))

	_, err = g.git(repo, env, "update-ref", "refs/heads/main", g.run(repo, env, "commit-tree", "-p", before, "-m", "x", before+"^{tree}"))
	require.Error(t, err)
	g.run(repo, env, "checkout", "-q", "-b", "feat")
	_, err = g.git(repo, env, "branch", "-D", "main")
	require.Error(t, err)
	g.run(repo, env, "checkout", "-q", "main")

	g.run(repo, env, "checkout", "-q", "feat")
	g.run(repo, env, "commit", "-q", "--allow-empty", "-m", "on a branch")

	tree := g.run(repo, env, "rev-parse", "main^{tree}")
	require.NoError(t, landgate.Write(g.marks, landgate.Verdict{Old: before, Tree: tree, Outcome: landgate.Approved, By: landgate.ByReviewer, At: time.Now()}))
	g.run(repo, env, "checkout", "-q", "main")
	g.run(repo, env, "commit", "-q", "--allow-empty", "-m", "reviewed")
}

func TestUserCommitPasses(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "user")
	assert.Empty(t, landgate.SessionEnv(baseEnv(), g.hooks, "", nil, nil, g.marks, ""))
}

func TestHookRefusesCommitIntoQueuedBranch(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	common := filepath.Join(repo, ".git")
	g.hold(common, "main")
	env := g.session(common, nil, nil)
	before := g.run(repo, env, "rev-parse", "main")

	out, err := g.git(repo, env, "commit", "--allow-empty", "-m", "sneak")

	require.Error(t, err)
	assert.Contains(t, out, "the branch main has a landing queue; stage your change and call git_land with mode index to take a place")
	assert.Equal(t, before, g.run(repo, env, "rev-parse", "main"))
	g.run(repo, env, "switch", "-qc", "work")
	g.run(repo, env, "commit", "-q", "--allow-empty", "-m", "own branch")
}

func TestHookPassesWhenQueueEmpty(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	env := g.session(filepath.Join(repo, ".git"), nil, nil)

	g.run(repo, env, "commit", "-q", "--allow-empty", "-m", "agent")
}

func TestUserCommitPassesQueuedBranch(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	g.hold(filepath.Join(repo, ".git"), "main")

	g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "user")
}

func TestApprovedNeedsParentOld(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	env := g.agent(filepath.Join(repo, ".git"))
	old := g.run(repo, env, "rev-parse", "main")
	tree := g.run(repo, env, "rev-parse", "main^{tree}")
	require.NoError(t, landgate.Write(g.marks, landgate.Verdict{Old: old, Tree: tree, Outcome: landgate.Approved, By: landgate.ByReviewer, At: time.Now()}))
	side := g.run(repo, env, "commit-tree", "-p", old, "-m", "side", tree)
	stranger := g.run(repo, env, "commit-tree", "-p", side, "-m", "stranger", tree)

	_, err := g.git(repo, env, "update-ref", "refs/heads/main", stranger)

	require.Error(t, err)
	assert.Equal(t, old, g.run(repo, env, "rev-parse", "main"))
	g.run(repo, env, "update-ref", "refs/heads/main", g.run(repo, env, "commit-tree", "-p", old, "-m", "reviewed", tree))
}

func TestAbortAfterRefusedRebasePasses(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	env := g.agent(filepath.Join(repo, ".git"))
	g.run(repo, baseEnv(), "checkout", "-q", "-b", "feat")
	g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "feat")
	g.run(repo, baseEnv(), "checkout", "-q", "main")
	g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "main")
	before := g.run(repo, env, "rev-parse", "main")

	_, err := g.git(repo, env, "rebase", "--keep-empty", "feat")
	require.Error(t, err)

	g.run(repo, env, "rebase", "--abort")
	assert.Equal(t, before, g.run(repo, env, "rev-parse", "main"))
}

func TestFastForwardToRemoteTrackingPasses(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	env := g.agent(filepath.Join(repo, ".git"))
	upstream := g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "upstream", "main^{tree}")
	g.run(repo, baseEnv(), "update-ref", "refs/remotes/origin/main", upstream)

	g.run(repo, env, "merge", "-q", "--ff-only", "origin/main")

	assert.Equal(t, upstream, g.run(repo, env, "rev-parse", "main"))
}

func TestOwnHooksRunThroughProxy(t *testing.T) {
	for _, hooksPath := range []string{"", ".husky"} {
		t.Run(hooksPath, func(t *testing.T) {
			g := newGitEnv(t)
			repo := g.repo()
			dir := filepath.Join(repo, ".git", "hooks")
			if hooksPath != "" {
				g.run(repo, baseEnv(), "config", "core.hooksPath", hooksPath)
				dir = filepath.Join(repo, hooksPath)
			}
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "pre-commit"), []byte("#!/bin/sh\necho refused by pre-commit >&2\nexit 1\n"), 0o755))
			env := g.agent(filepath.Join(repo, ".git"))
			g.run(repo, env, "checkout", "-q", "-b", "feat")

			out, err := g.git(repo, env, "commit", "--allow-empty", "-m", "x")
			require.Error(t, err)
			assert.Contains(t, out, "refused by pre-commit")
		})
	}
}

func TestOtherReposAreUntouched(t *testing.T) {
	g := newGitEnv(t)
	protected, other := g.repo(), g.repo()
	g.run(other, g.agent(filepath.Join(protected, ".git")), "commit", "-q", "--allow-empty", "-m", "temp repo")
}

func TestUnreviewedMovesThroughTheRemoteAreRefused(t *testing.T) {
	for name, move := range map[string]func(g gitEnv, repo string, env []string) error{
		"pushed branch merged into main": func(g gitEnv, repo string, env []string) error {
			g.run(repo, baseEnv(), "update-ref", "refs/heads/ahead", g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "ahead", "main^{tree}"))
			g.run(repo, env, "push", "-q", "origin", "ahead")
			_, err := g.git(repo, env, "merge", "-q", "--ff-only", "ahead")
			return err
		},
		"rewind to an older pushed commit": func(g gitEnv, repo string, env []string) error {
			_, err := g.git(repo, env, "reset", "-q", "--hard", "main~1")
			return err
		},
		"non-fast-forward move": func(g gitEnv, repo string, env []string) error {
			g.run(repo, baseEnv(), "push", "-q", "origin", "feat")
			_, err := g.git(repo, env, "reset", "-q", "--hard", "origin/feat")
			return err
		},
		"push a fast-forward onto main": func(g gitEnv, repo string, env []string) error {
			g.run(repo, baseEnv(), "update-ref", "refs/heads/ahead", g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "ahead", "main^{tree}"))
			_, err := g.git(repo, env, "push", "-q", "origin", "ahead:main")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGitEnv(t)
			repo := g.repo()
			origin := gitrepo.Canonical(t.TempDir())
			g.run(origin, baseEnv(), "init", "-q", "--bare")
			g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "second")
			g.run(repo, baseEnv(), "remote", "add", "origin", origin)
			g.run(repo, baseEnv(), "push", "-q", "-u", "origin", "main")
			g.run(repo, baseEnv(), "checkout", "-q", "-b", "feat", "main~1")
			g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "feat")
			g.run(repo, baseEnv(), "checkout", "-q", "main")
			env := g.agent(filepath.Join(repo, ".git"))
			before, remote := g.run(repo, env, "rev-parse", "main"), g.run(origin, baseEnv(), "rev-parse", "main")

			require.Error(t, move(g, repo, env))

			assert.Equal(t, before, g.run(repo, env, "rev-parse", "main"))
			assert.Equal(t, remote, g.run(origin, baseEnv(), "rev-parse", "main"))
		})
	}
}

func TestPushingTheProtectedBranchItselfPasses(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	origin := gitrepo.Canonical(t.TempDir())
	g.run(origin, baseEnv(), "init", "-q", "--bare")
	g.run(repo, baseEnv(), "remote", "add", "origin", origin)
	g.run(repo, baseEnv(), "push", "-q", "-u", "origin", "main")
	g.run(repo, baseEnv(), "commit", "-q", "--allow-empty", "-m", "landed")

	g.run(repo, g.agent(filepath.Join(repo, ".git")), "push", "-q", "origin", "main")

	assert.Equal(t, g.run(repo, baseEnv(), "rev-parse", "main"), g.run(origin, baseEnv(), "rev-parse", "main"))
}

func TestProtectedRefMatchesIgnoringCase(t *testing.T) {
	for name, move := range map[string][]string{
		"update-ref":   {"update-ref", "refs/heads/MAIN", "feat"},
		"branch force": {"branch", "-f", "MAIN", "feat"},
		"push to self": {"push", "-q", ".", "feat:MAIN"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newGitEnv(t)
			repo := g.repo()
			g.run(repo, baseEnv(), "update-ref", "refs/heads/feat", g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "feat", "main^{tree}"))
			env := g.agent(filepath.Join(repo, ".git"))
			before := g.run(repo, env, "rev-parse", "main")

			out, err := g.git(repo, env, move...)

			require.Error(t, err)
			assert.Contains(t, out, "git_land")
			assert.Equal(t, before, g.run(repo, env, "rev-parse", "main"))
		})
	}
}

func TestFailingAwkRefusesProtectedUpdate(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	g.run(repo, baseEnv(), "update-ref", "refs/heads/feat", g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "feat", "main^{tree}"))
	g.run(repo, baseEnv(), "checkout", "-q", "feat")
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "awk"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	env := g.agent(filepath.Join(repo, ".git"))
	for i, kv := range env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			env[i] = "PATH=" + bin + string(os.PathListSeparator) + path
		}
	}
	before := g.run(repo, env, "rev-parse", "main")

	for _, move := range [][]string{{"update-ref", "refs/heads/main", "feat"}, {"update-ref", "-d", "refs/heads/main"}} {
		out, err := g.git(repo, env, move...)
		require.Error(t, err, move)
		assert.Contains(t, out, "git_land")
	}
	assert.Equal(t, before, g.run(repo, env, "rev-parse", "main"))
}

func TestMatchSegments(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"release/*", "release/1.0", true},
		{"release/*", "release/1.0/hotfix", false},
		{"release/**", "release/1.0/hotfix", true},
		{"**/hotfix", "release/1.0/hotfix", true},
		{"release/**/hotfix", "release/hotfix", true},
		{"refs/heads/**", "refs/heads/feat/x", true},
		{"r?lease/[^0-9].*", "release/a.b", true},
		{"r?lease/[^0-9].*", "release/1.0", false},
		{"release/1.0", "release/1.0", true},
	} {
		assert.Equal(t, c.want, landgate.Match(c.pattern, c.name), c.pattern+" "+c.name)
	}
}

func TestHookMatchesPatternsLikeMatch(t *testing.T) {
	refs := []string{"main", "release/1.0", "release/2.0/hotfix", "release/hotfix", "release/a.b", "release/V1", "r1lease/a.b", "hotfix", "feat/x"}
	patterns := []string{"refs/heads/release/*", "refs/heads/release/**", "refs/heads/r?lease/[^0-9].*", "refs/heads/**/hotfix", "refs/**/hotfix", "refs/heads/release/**/hotfix", "refs/heads/**", "refs/heads/release/[^a-z]*", "refs/heads/release/[0-_]*"}
	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			g := newGitEnv(t)
			repo := g.repo()
			tip := g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "tip", "main^{tree}")
			for _, ref := range refs {
				g.run(repo, baseEnv(), "update-ref", "refs/heads/"+ref, "main")
			}
			g.run(repo, baseEnv(), "pack-refs", "--all")
			g.run(repo, baseEnv(), "checkout", "-q", "--detach")
			env := g.agentGlob(filepath.Join(repo, ".git"), []string{pattern})

			for _, ref := range refs[1:] {
				for _, name := range []string{ref, strings.ToUpper(ref)} {
					_, err := g.git(repo, env, "update-ref", "refs/heads/"+name, tip)
					full := "refs/heads/" + name
					want := landgate.Match(pattern, full) || landgate.Match(strings.ToLower(pattern), strings.ToLower(full))
					assert.Equal(t, want, err != nil, name)
				}
			}
		})
	}
}

func TestNamedPatternProtectsLikeAnExactName(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	g.run(repo, baseEnv(), "update-ref", "refs/heads/release/1.0", "main")
	g.run(repo, baseEnv(), "update-ref", "refs/heads/feat", g.run(repo, baseEnv(), "commit-tree", "-p", "main", "-m", "feat", "main^{tree}"))
	g.run(repo, baseEnv(), "checkout", "-q", "release/1.0")
	env := g.agentGlob(filepath.Join(repo, ".git"), []string{"refs/heads/release/*"})

	for _, move := range [][]string{
		{"commit", "--allow-empty", "-m", "on the branch"},
		{"branch", "release/9.9", "feat"},
		{"push", "-q", ".", "feat:release/1.0"},
	} {
		out, err := g.git(repo, env, move...)
		require.Error(t, err, move)
		assert.Contains(t, out, "git_land")
	}
	g.run(repo, env, "push", "-q", ".", "main:other")
}

func TestOwnBranchWorkPassesWithoutPatterns(t *testing.T) {
	g := newGitEnv(t)
	repo := g.repo()
	env := g.agent(filepath.Join(repo, ".git"))
	g.run(repo, baseEnv(), "branch", "tmp")

	g.run(repo, env, "switch", "-qc", "work")
	g.run(repo, env, "commit", "-q", "--allow-empty", "-m", "work")
	g.run(repo, env, "branch", "-m", "work2")
	g.run(repo, env, "worktree", "add", "-q", "-b", "sub", filepath.Join(t.TempDir(), "sub"))
	g.run(repo, env, "branch", "-D", "tmp")
}

func TestQueueMarkIsWrittenAndRemoved(t *testing.T) {
	dir := landgate.QueueDir(t.TempDir(), "/repo/.git")
	mark := filepath.Join(dir, "refs", "heads", "feat", "land")

	require.NoError(t, landgate.WriteQueueMark(dir, "feat/land", true))
	data, err := os.ReadFile(mark)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	require.NoError(t, landgate.WriteQueueMark(dir, "feat/land", false))
	require.NoError(t, landgate.WriteQueueMark(dir, "feat/land", false))
	_, err = os.Stat(mark)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Len(t, filepath.Base(dir), 16)
}
