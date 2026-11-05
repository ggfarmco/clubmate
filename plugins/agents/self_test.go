package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/acp"
)

func TestSessionMetaAppendsSelfAfterHint(t *testing.T) {
	m := sessionMeta(record{ID: "a"}, "", selfParts{Prompt: "be brief", PluginDir: "/s"})
	assert.Equal(t, askUserText+"\n\nbe brief", m.SystemPrompt.Append)
	assert.Equal(t, []acp.ClaudeCodePlugin{{Type: "local", Path: "/s"}}, m.ClaudeCode.Options.Plugins)
}

func TestSessionMetaChildGetsSelfAfterOpenItems(t *testing.T) {
	m := sessionMeta(record{ID: "a", ParentSession: "p"}, "", selfParts{Prompt: "be brief"})
	assert.Equal(t, openItemsText+"\n\nbe brief", m.SystemPrompt.Append)
}

func TestSessionMetaChildWithoutSelfGetsOpenItems(t *testing.T) {
	m := sessionMeta(record{ID: "a", ParentSession: "p"}, "", selfParts{})
	assert.Equal(t, openItemsText, m.SystemPrompt.Append)
}

func TestSessionMetaServiceIgnoresSelf(t *testing.T) {
	m := sessionMeta(record{ID: "a", SystemPrompt: "serve"}, "", selfParts{Prompt: "be brief", PluginDir: "/s"})
	assert.Equal(t, "serve", m.SystemPrompt.Append)
	assert.Empty(t, m.ClaudeCode.Options.Plugins)
}

func TestSelfDoesNotRestrict(t *testing.T) {
	m := sessionMeta(record{ID: "a"}, "", selfParts{Prompt: "be brief"})
	assert.Nil(t, m.ClaudeCode.Options.SettingSources)
	assert.False(t, m.ClaudeCode.Options.StrictMcpConfig)
}

func TestPromptOfMissingRepoIsEmpty(t *testing.T) {
	x := newSelfStore(filepath.Join(t.TempDir(), "gone"))
	assert.Empty(t, x.prompt())
	assert.Empty(t, x.skills())
}

func TestBlankPromptIsNoSelf(t *testing.T) {
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(t.Context()))
	require.NoError(t, os.WriteFile(filepath.Join(x.dir, "prompt.md"), []byte(" \n\t\n"), 0o600))
	assert.Empty(t, x.prompt())
}

func TestPartsWaitForAnApply(t *testing.T) {
	x := newSelfStore(t.TempDir())
	x.mu.Lock()
	done := make(chan selfParts)
	go func() { done <- x.parts() }()
	select {
	case <-done:
		t.Fatal("read the self during an apply")
	case <-time.After(50 * time.Millisecond):
	}
	x.mu.Unlock()
	<-done
}

func TestEnsureClearsAnInterruptedPick(t *testing.T) {
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(t.Context()))
	write(t, filepath.Join(x.dir, "prompt.md"), "<<<<<<< half")
	selfGit(t, x, "add", "prompt.md")

	require.NoError(t, x.ensure(t.Context()))

	assert.Empty(t, x.prompt())
}

func TestEnsureKeepsWhatIsThere(t *testing.T) {
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(t.Context()))
	assert.Equal(t, "1", selfGit(t, x, "rev-list", "--count", "HEAD"))
	require.NoError(t, os.WriteFile(filepath.Join(x.dir, "prompt.md"), []byte("be brief"), 0o600))
	require.NoError(t, x.ensure(t.Context()))
	assert.Equal(t, "be brief", x.prompt())
	assert.Equal(t, "1", selfGit(t, x, "rev-list", "--count", "HEAD"))
	manifest, err := os.ReadFile(filepath.Join(x.dir, ".claude-plugin", "plugin.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"self"}`, string(manifest))
}

func TestEnsureFinishesAnInterruptedStart(t *testing.T) {
	x := newSelfStore(t.TempDir())
	require.NoError(t, os.MkdirAll(x.dir, 0o700))
	require.NoError(t, exec.Command("git", "-C", x.dir, "init", "-q").Run())
	require.NoError(t, x.ensure(t.Context()))
	assert.Equal(t, "1", selfGit(t, x, "rev-list", "--count", "HEAD"))
}

func TestResetEmptiesPromptAndSkillsOnly(t *testing.T) {
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(t.Context()))
	skill := filepath.Join(x.dir, "skills", "tidy")
	require.NoError(t, os.MkdirAll(skill, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("tidy"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(x.dir, "prompt.md"), []byte("be brief"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(x.dir, "lessons.md"), []byte("rejected"), 0o600))
	assert.Equal(t, []string{"tidy"}, x.skills())

	require.NoError(t, x.reset(t.Context()))

	assert.Empty(t, x.prompt())
	assert.Empty(t, x.skills())
	lessons, err := os.ReadFile(filepath.Join(x.dir, "lessons.md"))
	require.NoError(t, err)
	assert.Equal(t, "rejected", string(lessons))
	assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
}

func selfGit(t *testing.T, x *selfStore, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", x.dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}
