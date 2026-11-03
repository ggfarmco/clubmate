package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type designRepo struct {
	t     *testing.T
	dir   string
	notes string
}

func newDesignRepo(t *testing.T) *designRepo {
	r := &designRepo{t: t, dir: t.TempDir(), notes: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.git("config", "user.email", "t@example.com")
	r.git("config", "user.name", "t")
	r.commit("README.md", "chore: init")
	return r
}

func (r *designRepo) git(args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", r.dir}, args...)...).CombinedOutput()
	require.NoError(r.t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (r *designRepo) write(file, body string) {
	r.t.Helper()
	path := filepath.Join(r.dir, file)
	require.NoError(r.t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(r.t, os.WriteFile(path, []byte(body), 0o644))
}

func (r *designRepo) commit(file, msg string) {
	r.t.Helper()
	r.write(file, msg)
	r.git("add", file)
	r.git("commit", "-q", "-m", msg)
}

func (r *designRepo) runDir() string {
	return filepath.Join(r.dir, ".worktrees", "design-check", "gr-t1")
}

func (r *designRepo) script(name string, env []string, args ...string) (string, int) {
	r.t.Helper()
	path, err := filepath.Abs("../../scripts/design/" + name)
	require.NoError(r.t, err)
	cmd := exec.Command(path, args...)
	cmd.Dir = r.dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLUBMATE_") && !strings.HasPrefix(kv, "DESIGN_CHECK_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "CLUBMATE_GRAPH_RUN=gr_t1", "DESIGN_CHECK_NOTES="+r.notes, "DESIGN_CHECK_TODAY=2026-11-04")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	require.NoError(r.t, err, string(out))
	return string(out), 0
}

func (r *designRepo) readRun(file string) string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.runDir(), file))
	require.NoError(r.t, err)
	return string(data)
}

func TestDesignCapturePlan(t *testing.T) {
	setup := func(t *testing.T) *designRepo {
		r := newDesignRepo(t)
		manifest := filepath.Join(r.notes, "design-check", "2026-11-03", "manifest.json")
		require.NoError(t, os.MkdirAll(filepath.Dir(manifest), 0o755))
		require.NoError(t, os.WriteFile(manifest, []byte(`{"head":"`+r.git("rev-parse", "HEAD")+`"}`), 0o644))
		return r
	}

	t.Run("a changed plugin view picks its boards", func(t *testing.T) {
		r := setup(t)
		r.commit("plugins/cron/view.go", "fix(cron): wording")
		r.commit("docs/cron.md", "docs: cron")
		out, code := r.script("capture.sh", nil, "--plan")
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, "changed: plugins/cron/view.go -> Cron-1280")
		assert.Equal(t, "Cron-1280\n", r.readRun("boards"))
	})

	t.Run("nothing to capture exits 3", func(t *testing.T) {
		r := setup(t)
		r.commit("internal/core/core.go", "fix(core): no view")
		out, code := r.script("capture.sh", nil)
		assert.Equal(t, 3, code, out)
		assert.Contains(t, out, "nothing to capture")
	})

	t.Run("Sunday captures every board", func(t *testing.T) {
		r := setup(t)
		out, code := r.script("capture.sh", []string{"DESIGN_CHECK_TODAY=2026-11-08"}, "--dry-run")
		require.Equal(t, 0, code, out)
		assert.Contains(t, out, "mode: all (Sunday)")
		assert.Contains(t, out, "would capture on simulator cm-design-gr-t1:")
		assert.Len(t, strings.Fields(r.readRun("boards")), 39)
	})

	t.Run("an unknown board in the boards file is refused", func(t *testing.T) {
		r := setup(t)
		require.NoError(t, os.MkdirAll(r.runDir(), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(r.runDir(), "boards"), []byte("Cron-1280\nCron-9999\n"), 0o644))
		out, code := r.script("capture.sh", nil, "--dry-run")
		assert.Equal(t, 1, code, out)
		assert.Contains(t, out, "unknown board Cron-9999")
	})
}

func TestDesignLandAndRevert(t *testing.T) {
	r := newDesignRepo(t)
	r.git("checkout", "-q", "-b", "design/2026-11-04-cron")
	r.commit("plugins/cron/view.go", "fix(cron): wording")
	r.git("checkout", "-q", "main")

	r.write("plugins/cron/view.go", "someone else's work")
	out, code := r.script("land.sh", nil, "design/2026-11-04-cron", "gr_t1")
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "plugins/cron/view.go has uncommitted changes")
	require.NoError(t, os.Remove(filepath.Join(r.dir, "plugins/cron/view.go")))

	out, code = r.script("land.sh", nil, "design/2026-11-04-cron", "gr_t1")
	require.Equal(t, 0, code, out)
	landed := r.git("rev-parse", "HEAD")
	assert.Equal(t, "fix(cron): wording", r.git("log", "-1", "--format=%s"))
	assert.Equal(t, landed+" design/2026-11-04-cron\n", r.readRun("merged"))

	out, code = r.script("tests.sh", nil, "--dry-run")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, ":go jsonrpc2\n")

	out, code = r.script("revert.sh", nil)
	require.Equal(t, 0, code, out)
	assert.Equal(t, "README.md", r.git("ls-tree", "-r", "--name-only", "HEAD"))
	data, err := os.ReadFile(filepath.Join(r.dir, ".worktrees", "design-check", "landed"))
	require.NoError(t, err)
	assert.Empty(t, string(data))
}

func TestDesignCleanup(t *testing.T) {
	r := newDesignRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(r.runDir(), "shots"), 0o755))
	landedTree := filepath.Join(r.dir, ".worktrees", "design", "landed")
	dirtyTree := filepath.Join(r.dir, ".worktrees", "design", "dirty")
	r.git("worktree", "add", "-q", "-b", "design/landed", landedTree)
	r.git("-C", landedTree, "commit", "-q", "--allow-empty", "-m", "fix: landed")
	squashedTree := filepath.Join(r.dir, ".worktrees", "design", "squashed")
	r.git("worktree", "add", "-q", "-b", "design/squashed", squashedTree)
	require.NoError(t, os.WriteFile(filepath.Join(squashedTree, "fix.txt"), []byte("fix"), 0o644))
	r.git("-C", squashedTree, "add", "fix.txt")
	r.git("-C", squashedTree, "commit", "-q", "-m", "fix: squashed by hand")
	r.commit("docs/other.md", "docs: someone else")
	r.git("cherry-pick", "design/squashed")
	r.git("worktree", "add", "-q", "-b", "design/dirty", dirtyTree)
	require.NoError(t, os.WriteFile(filepath.Join(dirtyTree, "wip.txt"), []byte("wip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, ".worktrees", "design-check", "landed"), []byte("abc123 design/landed\n"), 0o644))

	out, code := r.script("cleanup.sh", nil, "--dry-run")
	require.Equal(t, 0, code, out)
	assert.DirExists(t, r.runDir())
	assert.DirExists(t, landedTree)

	out, code = r.script("cleanup.sh", nil)
	require.Equal(t, 0, code, out)
	assert.NoDirExists(t, r.runDir())
	assert.NoDirExists(t, landedTree)
	assert.Contains(t, out, "removed design/landed (landed as abc123)")
	assert.NoDirExists(t, squashedTree)
	assert.Contains(t, out, "removed design/squashed (its commits are on main)")
	assert.DirExists(t, dirtyTree)
	assert.Contains(t, out, "uncommitted changes")
	assert.Equal(t, "design/dirty\nmain", r.git("branch", "--format=%(refname:short)"))
}
