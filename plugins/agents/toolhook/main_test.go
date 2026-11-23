package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/toolgate"
)

const askLine = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"Clubmate gate"}}`

func hookDir(t *testing.T, rules string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "marks"), 0o700))
	if rules != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "rules.json"), []byte(rules), 0o600))
	}
	return dir
}

func hook(dir, stdin string) string {
	var out bytes.Buffer
	run([]string{dir}, strings.NewReader(stdin), &out)
	return out.String()
}

func marks(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "marks"))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestToolHookAsksAndMarksOnMatch(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, `[{"id":"gt_1","tool":"Bash","how":"command","text":"git push"}]`)

	assert.Equal(t, askLine, hook(dir, `{"tool_name":"Bash","tool_input":{"command":"cd x && git push origin a","description":"push"},"tool_use_id":"toolu_1"}`))
	assert.Empty(t, hook(dir, `{"tool_name":"Bash","tool_input":{"command":"ls"},"tool_use_id":"toolu_2"}`))
	assert.Equal(t, []string{"toolu_1"}, marks(t, dir))
}

func TestToolHookAsksWhenRulesUnreadable(t *testing.T) {
	t.Parallel()
	ls := `{"tool_name":"Bash","tool_input":{"command":"ls"},"tool_use_id":"toolu_1"}`
	for name, rules := range map[string]string{
		"missing":   "",
		"invalid":   "[{",
		"null":      "null",
		"bad regex": `[{"id":"gt_1","tool":"Write","how":"regex","text":"("}]`,
		"bad glob":  `[{"id":"gt_1","tool":"Bash[","how":"contains","text":"x"}]`,
		"bad how":   `[{"id":"gt_1","tool":"Bash","how":"glob","text":"x"}]`,
		"no tool":   `[{"id":"gt_1","how":"contains","text":"x"}]`,
	} {
		dir := hookDir(t, rules)
		assert.Equal(t, askLine, hook(dir, ls), name)
		assert.Equal(t, []string{"toolu_1"}, marks(t, dir), name)
	}
}

func TestToolHookAsksWhenTheCallIsUnreadable(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, `[{"id":"gt_1","tool":"Bash","how":"command","text":"git push"}]`)

	for _, stdin := range []string{"not json", "{}", "null", `{"tool_input":{"command":"ls"},"tool_use_id":"toolu_1"}`} {
		assert.Equal(t, askLine, hook(dir, stdin), stdin)
	}
	assert.Equal(t, askLine, hook(dir, `{"tool_name":"Bash","tool_input":{"command":"git push"},"tool_use_id":"../escape"}`))
	assert.Empty(t, marks(t, dir))
}

func TestToolHookRecreatesMarks(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, `[{"id":"gt_1","tool":"Bash","how":"command","text":"git push"}]`)
	require.NoError(t, os.Remove(filepath.Join(dir, "marks")))

	assert.Equal(t, askLine, hook(dir, `{"tool_name":"Bash","tool_input":{"command":"git push"},"tool_use_id":"toolu_1"}`))
	assert.Equal(t, []string{"toolu_1"}, marks(t, dir))
}

func TestBatchHookPrintsFindingsOnce(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, "")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "findings"), 0o700))
	for id, text := range map[string]string{"toolu_1": "fix x", "toolu_2": "fix x", "toolu_3": "fix y"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "findings", id), []byte(text), 0o600))
	}
	batch := func(stdin string) string {
		var out bytes.Buffer
		run([]string{"batch", dir}, strings.NewReader(stdin), &out)
		return out.String()
	}

	got := batch(`{"hook_event_name":"PostToolBatch","tool_calls":[{"tool_use_id":"toolu_1"},{"tool_use_id":"toolu_2"},{"tool_use_id":"toolu_9"},{"tool_use_id":"../x"}]}`)

	assert.JSONEq(t, `{"hookSpecificOutput":{"hookEventName":"PostToolBatch","additionalContext":"fix x"}}`, got)
	assert.FileExists(t, filepath.Join(dir, "told", "toolu_1"))
	assert.FileExists(t, filepath.Join(dir, "told", "toolu_2"))
	assert.NoFileExists(t, filepath.Join(dir, "findings", "toolu_1"))
	assert.Empty(t, batch(`{"tool_calls":[{"tool_use_id":"toolu_1"}]}`))
	assert.Empty(t, batch(`not json`))
	assert.FileExists(t, filepath.Join(dir, "findings", "toolu_3"))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestBatchHookClaimsFindingsOnlyAfterPrintingThem(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, "")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "findings"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "findings", "toolu_1"), []byte("fix x"), 0o600))
	stdin := `{"tool_calls":[{"tool_use_id":"toolu_1"}]}`

	run([]string{"batch", dir}, strings.NewReader(stdin), failingWriter{})

	assert.FileExists(t, filepath.Join(dir, "findings", "toolu_1"))
	assert.NoFileExists(t, filepath.Join(dir, "told", "toolu_1"))
	var out bytes.Buffer
	run([]string{"batch", dir}, strings.NewReader(stdin), &out)
	assert.Contains(t, out.String(), "fix x")
	assert.FileExists(t, filepath.Join(dir, "told", "toolu_1"))
	assert.NoFileExists(t, filepath.Join(dir, "findings", "toolu_1"))
}

func TestBatchHookLeavesNoToldFileForFindingsThePluginClaimedFirst(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, "")

	claim(dir, "toolu_1")

	assert.NoFileExists(t, filepath.Join(dir, "told", "toolu_1"))
}

func TestBatchHookCapsItsText(t *testing.T) {
	t.Parallel()
	dir := hookDir(t, "")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "findings"), 0o700))
	long := strings.Repeat("é", 3000)
	for _, id := range []string{"toolu_1", "toolu_2"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "findings", id), []byte(id+long), 0o600))
	}
	var out bytes.Buffer

	run([]string{"batch", dir}, strings.NewReader(`{"tool_calls":[{"tool_use_id":"toolu_1"},{"tool_use_id":"toolu_2"}]}`), &out)

	var got batchOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.LessOrEqual(t, len(got.Hook.Context), 9000)
	full := filepath.Join(dir, "messages", "batch-toolu_1")
	assert.True(t, strings.HasSuffix(got.Hook.Context, "the whole of it is in "+full+".\n"))
	raw, err := os.ReadFile(full)
	require.NoError(t, err)
	assert.Equal(t, "toolu_1"+long+"\n\ntoolu_2"+long, string(raw))
}

func TestToolHookAddsTheRulesOfTheRepoItCallsIn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	push := `[{"id":"gt_1","tool":"Bash","how":"command","text":"git push"}]`
	repo := func(rules string) string {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o700))
		place, ok := toolgate.PlaceOf(dir)
		require.True(t, ok)
		if rules != "" {
			hooks := filepath.Join(root, toolgate.HookDirName(place.Repo))
			require.NoError(t, os.MkdirAll(hooks, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(hooks, "rules.json"), []byte(rules), 0o600))
		}
		return place.Repo
	}
	own, gated, lax, broken, plain := repo(`[]`), repo(push), repo(`[]`), repo(`[{`), repo("")
	dir := filepath.Join(root, toolgate.HookDirName(own))
	require.NoError(t, os.Mkdir(filepath.Join(gated, "sub"), 0o700))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(gated, link))
	call := func(id, cwd string) string {
		return `{"tool_name":"Bash","tool_input":{"command":"git push"},"tool_use_id":"` + id + `","cwd":"` + cwd + `"}`
	}
	markOf := func(id string) string {
		raw, err := os.ReadFile(filepath.Join(dir, "marks", id))
		require.NoError(t, err)
		return string(raw)
	}
	inGated := `{"repo":"` + gated + `","tree":"` + gated + `"}`

	assert.Empty(t, hook(dir, call("toolu_1", own)))
	assert.Empty(t, hook(dir, call("toolu_2", plain)))
	assert.Empty(t, hook(dir, call("toolu_3", lax)))
	assert.Equal(t, askLine, hook(dir, call("toolu_4", filepath.Join(gated, "sub"))))
	assert.JSONEq(t, inGated, markOf("toolu_4"))
	assert.Equal(t, askLine, hook(dir, call("toolu_5", link)))
	assert.JSONEq(t, inGated, markOf("toolu_5"))
	assert.Equal(t, askLine, hook(dir, call("toolu_6", broken)))
	assert.Equal(t, askLine, hook(filepath.Join(root, "all"), call("toolu_7", own)))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "rules.json"), []byte(push), 0o600))
	for id, cwd := range map[string]string{"toolu_8": plain, "toolu_9": lax, "toolu_10": filepath.Join(own, "gone")} {
		assert.Equal(t, askLine, hook(dir, call(id, cwd)), cwd)
		assert.Empty(t, markOf(id), cwd)
	}
}
