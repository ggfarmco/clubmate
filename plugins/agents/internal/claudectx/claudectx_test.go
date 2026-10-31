package claudectx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/internal/claudectx"
)

func TestReadBuildsTheCurrentContextAndFollowsAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects", "-repo", "sid-1.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	write(t, path,
		attachment(map[string]any{"type": "instructions", "files": []map[string]any{
			{"path": "/home/.claude/CLAUDE.md", "type": "User", "content": "be short"},
			{"path": "/repo/CLAUDE.md", "type": "Project", "content": "agents only when asked"},
		}}),
		attachment(map[string]any{"type": "skill_listing", "isInitial": true, "names": []string{"codex:rescue", "ui"},
			"content": "- codex:rescue: Delegate to Codex\n- ui: Build views\nTRIGGER on screenshots"}),
		attachment(map[string]any{"type": "mcp_instructions_delta", "addedNames": []string{"clubmate", "linear"},
			"addedBlocks": []string{"## clubmate\nDrive the app"}}),
		attachment(map[string]any{"type": "deferred_tools_delta", "addedNames": []string{"CronCreate", "mcp__clubmate__agents_new"},
			"needsAuthMcpServers": []string{"slack"}}),
		attachment(map[string]any{"type": "hook_additional_context", "hookName": "SessionStart", "content": []string{"explanatory style"}}),
		attachment(map[string]any{"type": "agent_listing_delta", "addedTypes": []string{"Explore"}, "addedLines": []string{"- Explore: Read-only search"}}),
		map[string]any{"type": "api-request-shape", "shape": map[string]any{"tools": []map[string]any{
			{"name": "Bash", "description": "Run a command"}, {"name": "Skill", "description": "Invoke a skill"},
		}}},
		toolUse("Bash", map[string]any{"command": "ls"}),
		toolUse("Skill", map[string]any{"skill": "ui"}),
	)
	r := claudectx.NewReader(path)

	got, err := r.Read()

	require.NoError(t, err)
	assert.Equal(t, []claudectx.File{
		{Path: "/home/.claude/CLAUDE.md", Kind: "User", Content: "be short"},
		{Path: "/repo/CLAUDE.md", Kind: "Project", Content: "agents only when asked"},
	}, got.Instructions)
	assert.Equal(t, []claudectx.Entry{
		{Name: "codex:rescue", Text: "Delegate to Codex"},
		{Name: "ui", Text: "Build views\nTRIGGER on screenshots", Uses: 1},
	}, got.Skills)
	assert.Equal(t, []claudectx.Entry{{Name: "clubmate", Text: "Drive the app"}, {Name: "linear"}}, got.MCP)
	assert.Equal(t, []string{"slack"}, got.NeedsAuth)
	assert.Equal(t, []claudectx.Entry{{Name: "SessionStart", Text: "explanatory style"}}, got.Hooks)
	assert.Equal(t, []claudectx.Entry{{Name: "Explore", Text: "Read-only search"}}, got.Subagents)
	assert.Equal(t, []claudectx.Entry{{Name: "Bash", Text: "Run a command", Uses: 1}, {Name: "Skill", Text: "Invoke a skill", Uses: 1}}, got.Tools)
	assert.Equal(t, []claudectx.Entry{{Name: "CronCreate"}, {Name: "mcp__clubmate__agents_new"}}, got.Deferred)

	appendLines(t, path,
		attachment(map[string]any{"type": "instructions", "changed": true, "files": []map[string]any{
			{"path": "/repo/CLAUDE.md", "type": "Project", "content": "run independent parts as agents"},
		}}),
		attachment(map[string]any{"type": "nested_memory", "path": "/repo/sub/CLAUDE.md",
			"content": map[string]any{"path": "/repo/sub/CLAUDE.md", "type": "Project", "content": "sub rules"}}),
		attachment(map[string]any{"type": "mcp_instructions_delta", "removedNames": []string{"linear"}}),
		toolUse("mcp__clubmate__agents_new", map[string]any{}),
		toolUse("Agent", map[string]any{"subagent_type": "Explore"}),
	)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"attachment","attachment":{"type":"hook_additional`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got, err = r.Read()

	require.NoError(t, err)
	assert.Equal(t, []claudectx.File{
		{Path: "/home/.claude/CLAUDE.md", Kind: "User", Content: "be short"},
		{Path: "/repo/CLAUDE.md", Kind: "Project", Content: "run independent parts as agents"},
		{Path: "/repo/sub/CLAUDE.md", Kind: "Project", Content: "sub rules", Nested: true},
	}, got.Instructions)
	assert.Equal(t, []claudectx.Entry{{Name: "clubmate", Text: "Drive the app"}}, got.MCP)
	assert.Equal(t, []claudectx.Entry{{Name: "CronCreate"}, {Name: "mcp__clubmate__agents_new", Uses: 1}}, got.Deferred)
	assert.Equal(t, []claudectx.Entry{{Name: "Explore", Text: "Read-only search", Uses: 1}}, got.Subagents)

	found, err := claudectx.Find(dir, "sid-1")
	require.NoError(t, err)
	assert.Equal(t, path, found)
	_, err = claudectx.Find(dir, "missing")
	assert.ErrorIs(t, err, claudectx.ErrNoLog)
}

func attachment(a map[string]any) map[string]any {
	return map[string]any{"type": "attachment", "attachment": a}
}

func toolUse(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{"content": []map[string]any{
		{"type": "tool_use", "name": name, "input": input},
	}}}
}

func write(t *testing.T, path string, lines ...map[string]any) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	appendLines(t, path, lines...)
}

func appendLines(t *testing.T, path string, lines ...map[string]any) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	defer f.Close()
	for _, l := range lines {
		b, err := json.Marshal(l)
		require.NoError(t, err)
		_, err = f.Write(append(b, '\n'))
		require.NoError(t, err)
	}
}
