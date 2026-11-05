package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

func TestClaudeProjectDir(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/Users/a/my.repo_x", "-Users-a-my-repo-x"},
		{"/Users/cali/.config/nvim", "-Users-cali--config-nvim"},
		{
			"/private/var/folders/s0/ct9rsqss6dn7wgxm3_15s4ph0000gn/T/bashrule.1iaW",
			"-private-var-folders-s0-ct9rsqss6dn7wgxm3-15s4ph0000gn-T-bashrule-1iaW",
		},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, claudeProjectDir(tt.path))
		})
	}
}

func TestMemoryDirsSkipsMissing(t *testing.T) {
	home := t.TempDir()
	mkdir(t, home, ".claude/projects/-w-a/memory")
	dirs := memoryDirs(workspaceOf("/w/a", "/w/b"), home)
	assert.Equal(t, []string{filepath.Join(home, ".claude/projects/-w-a/memory")}, dirs)
}

func TestMemoryDirsKeepsOneDirForFoldersThatEscapeAlike(t *testing.T) {
	home := t.TempDir()
	mkdir(t, home, ".claude/projects/-w-a/memory")
	dirs := memoryDirs(workspaceOf("/w/a", "/w.a"), home)
	assert.Len(t, dirs, 1)
}

func TestMemoryDirsFollowsAutoMemoryDirectory(t *testing.T) {
	home := t.TempDir()
	mkdir(t, home, ".claude/projects/-w-a/memory")
	mkdir(t, home, "notes")
	write(t, filepath.Join(home, ".claude/settings.json"), `{"autoMemoryDirectory":"~/notes"}`)
	assert.Equal(t, []string{filepath.Join(home, "notes")}, memoryDirs(workspaceOf("/w/a"), home))
}

func TestSandboxHoldsNoGit(t *testing.T) {
	_, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	assert.NoFileExists(t, filepath.Join(m.Dir, ".git"))
	assert.NoDirExists(t, filepath.Join(m.Dir, ".git"))
}

func TestChangedListsEditedAddedAndRemovedMarkdown(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"edit.md": "a", "gone.md": "b", "same.md": "c"})
	write(t, filepath.Join(real, "edit.md"), "A")
	require.NoError(t, os.Remove(filepath.Join(real, "gone.md")))
	write(t, filepath.Join(real, "new.md"), "d")
	write(t, filepath.Join(real, ".DS_Store"), "x")

	got, err := m.changed(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.Join(real, "edit.md"),
		filepath.Join(real, "gone.md"),
		filepath.Join(real, "new.md"),
	}, got)
}

func TestApplyWritesEditsAndDeletes(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "old.md": "b"})
	editInSandbox(t, m, "MEMORY.md", "A")
	removeInSandbox(t, m, "old.md")
	declare(t, m)
	patch, err := m.apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "A", read(t, real, "MEMORY.md"))
	assert.NoFileExists(t, filepath.Join(real, "old.md"))
	assert.NotEmpty(t, patch)
}

func TestApplyFillsAnEmptyMemoryDir(t *testing.T) {
	real, m := sandboxWith(t, nil)
	editInSandbox(t, m, "MEMORY.md", "A")
	declare(t, m)
	_, err := m.apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "A", read(t, real, "MEMORY.md"))
}

func TestApplySendsEachSlotToItsDir(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	write(t, filepath.Join(first, "MEMORY.md"), "1")
	write(t, filepath.Join(second, "MEMORY.md"), "2")
	m, err := newMemorySandbox(t.Context(), t.TempDir(), []string{first, second})
	require.NoError(t, err)
	for slot, dir := range m.Dirs {
		if dir == second {
			write(t, filepath.Join(m.Dir, "memory", slot, "MEMORY.md"), "two")
		}
	}
	declare(t, m)
	_, err = m.apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "1", read(t, first, "MEMORY.md"))
	assert.Equal(t, "two", read(t, second, "MEMORY.md"))
}

func TestApplyIgnoresEditsNobodyDeclared(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "n.md": "b"})
	editInSandbox(t, m, "n.md", "B")
	declare(t, m)
	editInSandbox(t, m, "n.md", "not declared")
	editInSandbox(t, m, "MEMORY.md", "not declared")
	_, err := m.apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "B", read(t, real, "n.md"))
	assert.Equal(t, "a", read(t, real, "MEMORY.md"))
}

func TestApplyWithNothingDeclaredIgnoresADaySession(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	write(t, filepath.Join(real, "MEMORY.md"), "changed by a day session")
	patch, err := m.apply(t.Context())
	require.NoError(t, err)
	assert.Empty(t, patch)
}

func TestApplyDropsWholeProjectWhenAFileMoved(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "n.md": "b"})
	editInSandbox(t, m, "n.md", "B")
	declare(t, m)
	write(t, filepath.Join(real, "MEMORY.md"), "changed by a day session")
	_, err := m.apply(t.Context())
	assert.ErrorIs(t, err, errMemoryMoved)
	assert.Equal(t, "b", read(t, real, "n.md"))
}

func TestApplyRefusesNonMarkdown(t *testing.T) {
	_, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	editInSandbox(t, m, "run.sh", "x")
	declare(t, m)
	_, err := m.apply(t.Context())
	assert.ErrorIs(t, err, errMemoryPath)
}

func TestApplyRefusesPathsOutsideItsSlots(t *testing.T) {
	for _, name := range []string{"notes.md", "memory/9/x.md"} {
		t.Run(name, func(t *testing.T) {
			_, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
			write(t, filepath.Join(m.Dir, name), "x")
			declare(t, m)
			_, err := m.apply(t.Context())
			assert.ErrorIs(t, err, errMemoryPath)
		})
	}
}

func TestApplyRefusesACommittedSymlink(t *testing.T) {
	_, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	require.NoError(t, os.Symlink("/etc/hosts", sandboxPath(t, m, "link.md")))
	declare(t, m)
	_, err := m.apply(t.Context())
	assert.ErrorIs(t, err, errMemoryPath)
}

func TestApplyRefusesWritingThroughASymlinkedDir(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(real, "sub")))
	editInSandbox(t, m, "sub/x.md", "x")
	declare(t, m)
	_, err := m.apply(t.Context())
	assert.ErrorIs(t, err, errMemoryPath)
	assert.NoFileExists(t, filepath.Join(outside, "x.md"))
}

func TestApplyRefusesATreeThatClimbsOutOfItsSlot(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	escaping := filepath.Join(t.TempDir(), "evil.md")
	write(t, escaping, "x")
	blob := gitOut(t, m, "", "hash-object", "-w", escaping)
	up := gitOut(t, m, "100644 blob "+blob+"\tevil.md\n", "mktree")
	slot := gitOut(t, m, "040000 tree "+up+"\t..\n", "mktree")
	memory := gitOut(t, m, "040000 tree "+slot+"\t0\n", "mktree")
	root := gitOut(t, m, "040000 tree "+memory+"\tmemory\n", "mktree")
	commit := gitOut(t, m, "", "commit-tree", root, "-p", "HEAD", "-m", "climb")
	gitOut(t, m, "", "update-ref", "HEAD", commit)
	_, err := m.apply(t.Context())
	assert.ErrorIs(t, err, errMemoryPath)
	assert.NoFileExists(t, filepath.Join(filepath.Dir(real), "evil.md"))
}

func TestApplyIgnoresAGitFilePlantedInTheSandbox(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	editInSandbox(t, m, "MEMORY.md", "A")
	declare(t, m)
	write(t, filepath.Join(m.Dir, ".git"), "gitdir: "+t.TempDir()+"\n")
	_, err := m.apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "A", read(t, real, "MEMORY.md"))
}

func TestApplyRestoresTheBaselineWhenACopyFails(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "old.md": "b"})
	locked := filepath.Join(real, "locked")
	require.NoError(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	editInSandbox(t, m, "MEMORY.md", "A")
	removeInSandbox(t, m, "old.md")
	editInSandbox(t, m, "locked/new.md", "x")
	declare(t, m)
	_, err := m.apply(t.Context())
	assert.Error(t, err)
	assert.Equal(t, "a", read(t, real, "MEMORY.md"))
	assert.Equal(t, "b", read(t, real, "old.md"))
}

func TestReverseRestores(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	editInSandbox(t, m, "MEMORY.md", "A")
	declare(t, m)
	patch, _ := m.apply(t.Context())
	require.NoError(t, reverseMemory(t.Context(), patch, m.Dirs))
	assert.Equal(t, "a", read(t, real, "MEMORY.md"))
}

func TestReverseUndoesAddsDeletesAndBinaryEdits(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"bin.md": "a\x00b", "old.md": "b"})
	editInSandbox(t, m, "bin.md", "c\x00d")
	removeInSandbox(t, m, "old.md")
	editInSandbox(t, m, "new.md", "n")
	declare(t, m)
	patch, err := m.apply(t.Context())
	require.NoError(t, err)
	require.NoError(t, reverseMemory(t.Context(), patch, m.Dirs))
	assert.Equal(t, "a\x00b", read(t, real, "bin.md"))
	assert.Equal(t, "b", read(t, real, "old.md"))
	assert.NoFileExists(t, filepath.Join(real, "new.md"))
}

func TestReverseChangesNothingWhenThePatchNoLongerFits(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "n.md": "b"})
	editInSandbox(t, m, "MEMORY.md", "A")
	editInSandbox(t, m, "n.md", "B")
	declare(t, m)
	patch, err := m.apply(t.Context())
	require.NoError(t, err)
	write(t, filepath.Join(real, "MEMORY.md"), "changed by a day session")
	assert.Error(t, reverseMemory(t.Context(), patch, m.Dirs))
	assert.Equal(t, "changed by a day session", read(t, real, "MEMORY.md"))
	assert.Equal(t, "B", read(t, real, "n.md"))
}

func TestPatchSurvivesGitConfig(t *testing.T) {
	for _, kv := range [][2]string{{"color.ui", "always"}, {"diff.context", "0"}} {
		t.Run(kv[0], func(t *testing.T) {
			real, m := sandboxWith(t, map[string]string{"MEMORY.md": "1\n2\n3\n4\n5\n"})
			_, err := sandboxGit(t.Context(), m.Dir, "config", kv[0], kv[1])
			require.NoError(t, err)
			editInSandbox(t, m, "MEMORY.md", "1\n2\nX\n4\n5\n")
			declare(t, m)
			patch, err := m.apply(t.Context())
			require.NoError(t, err)
			assert.NotContains(t, string(patch), "\x1b")
			require.NoError(t, reverseMemory(t.Context(), patch, m.Dirs))
			assert.Equal(t, "1\n2\n3\n4\n5\n", read(t, real, "MEMORY.md"))
		})
	}
}

func sandboxWith(t *testing.T, files map[string]string) (string, memorySandbox) {
	t.Helper()
	real := t.TempDir()
	for name, content := range files {
		write(t, filepath.Join(real, name), content)
	}
	m, err := newMemorySandbox(t.Context(), t.TempDir(), []string{real})
	require.NoError(t, err)
	return real, m
}

func sandboxPath(t *testing.T, m memorySandbox, name string) string {
	t.Helper()
	require.Len(t, m.Dirs, 1)
	for slot := range m.Dirs {
		return filepath.Join(m.Dir, "memory", slot, name)
	}
	return ""
}

func editInSandbox(t *testing.T, m memorySandbox, name, content string) {
	t.Helper()
	write(t, sandboxPath(t, m, name), content)
}

func removeInSandbox(t *testing.T, m memorySandbox, name string) {
	t.Helper()
	require.NoError(t, os.Remove(sandboxPath(t, m, name)))
}

func declare(t *testing.T, m memorySandbox) {
	t.Helper()
	_, err := sandboxGit(t.Context(), m.Dir, "add", "-A")
	require.NoError(t, err)
	_, err = sandboxGit(t.Context(), m.Dir, "commit", "-qm", "declare")
	require.NoError(t, err)
}

func gitOut(t *testing.T, m memorySandbox, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", slices.Concat([]string{"-C", m.Dir}, sandboxGitArgs(m.Dir), args)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(data)
}

func mkdir(t *testing.T, base, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(base, name), 0o700))
}

func workspaceOf(paths ...string) pluginsdk.Workspace {
	var w pluginsdk.Workspace
	for _, path := range paths {
		w.Folders = append(w.Folders, pluginsdk.Folder{Path: path})
	}
	return w
}
