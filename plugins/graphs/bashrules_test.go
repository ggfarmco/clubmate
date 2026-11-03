package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashRulesCorpus(t *testing.T) {
	f, err := os.Open("testdata/bash-rules.txt")
	require.NoError(t, err)
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		require.Len(t, fields, 3, sc.Text())
		command, err := strconv.Unquote(fields[2])
		require.NoError(t, err, sc.Text())
		var rs ruleSet
		for _, rule := range strings.Split(fields[1], ",") {
			switch {
			case rule == "":
			case strings.HasPrefix(rule, "deny:"):
				rs.Deny = append(rs.Deny, strings.TrimPrefix(rule, "deny:"))
			case strings.HasPrefix(rule, "ask:"):
				rs.Ask = append(rs.Ask, strings.TrimPrefix(rule, "ask:"))
			default:
				rs.Allow = append(rs.Allow, rule)
			}
		}
		got, _ := rs.decide(command)
		assert.Equal(t, fields[0], got, sc.Text())
	}
	require.NoError(t, sc.Err())
}

func writeSettings(t *testing.T, path string, v any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func TestManagedDenyWins(t *testing.T) {
	root := t.TempDir()
	managed, home, dir := filepath.Join(root, "managed.json"), filepath.Join(root, "home"), filepath.Join(root, "repo")
	writeSettings(t, managed, map[string]any{"permissions": map[string]any{"deny": []string{"Bash(git push:*)"}}})
	writeSettings(t, filepath.Join(dir, ".claude", "settings.local.json"), map[string]any{"permissions": map[string]any{"allow": []string{"Bash(git push:*)"}, "defaultMode": "bypassPermissions"}})
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"defaultMode": "auto"}})
	rs := loadRules(managed, home, dir, true)
	got, _ := rs.decide("git push origin main")
	assert.Equal(t, verdictDeny, got)
	assert.Equal(t, "auto", rs.DefaultMode)
}

func TestAlwaysAllowWritesExactRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.local.json")
	writeSettings(t, path, map[string]any{"model": "x"})
	require.NoError(t, addAllowRule(dir, "make test"))
	require.NoError(t, addAllowRule(dir, "make test"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got struct {
		Model       string `json:"model"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "x", got.Model)
	assert.Equal(t, []string{"Bash(make test)"}, got.Permissions.Allow)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestProjectSettingsCannotTurnOnBypass(t *testing.T) {
	root := t.TempDir()
	managed, home, dir := filepath.Join(root, "managed.json"), filepath.Join(root, "home"), filepath.Join(root, "repo")
	writeSettings(t, filepath.Join(dir, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"defaultMode": "bypassPermissions"}})
	writeSettings(t, filepath.Join(dir, ".claude", "settings.local.json"), map[string]any{"permissions": map[string]any{"defaultMode": "bypassPermissions"}})
	assert.Empty(t, loadRules(managed, home, dir, true).DefaultMode)
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{"permissions": map[string]any{"defaultMode": "bypassPermissions"}})
	assert.Equal(t, modeBypass, loadRules(managed, home, dir, true).DefaultMode)
	writeSettings(t, managed, map[string]any{"permissions": map[string]any{"disableBypassPermissionsMode": "disable"}})
	rs := loadRules(managed, home, dir, true)
	assert.Empty(t, rs.DefaultMode)
	assert.True(t, rs.NoBypass)
}

func TestAlwaysRefusesPatternCommands(t *testing.T) {
	for _, command := range []string{"rm *", "echo a)", "make test:*", "make test *"} {
		assert.Error(t, addAllowRule(t.TempDir(), command), command)
	}
}

func TestProjectAllowRulesNeedTrust(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	writeSettings(t, filepath.Join(dir, ".claude", "settings.local.json"), map[string]any{"permissions": map[string]any{"allow": []string{"Bash(make test)"}, "deny": []string{"Bash(rm:*)"}}})
	untrusted := loadRules(filepath.Join(root, "none.json"), filepath.Join(root, "home"), dir, false)
	assert.Empty(t, untrusted.Allow)
	assert.Equal(t, []string{"Bash(rm:*)"}, untrusted.Deny)
	assert.Equal(t, []string{"Bash(make test)"}, loadRules(filepath.Join(root, "none.json"), filepath.Join(root, "home"), dir, true).Allow)
}

func TestAlwaysAllowRefusesSymlinks(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
	file := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(file, ".claude"), 0o700))
	require.NoError(t, os.Symlink(victim, filepath.Join(file, ".claude", "settings.local.json")))
	folder := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Dir(victim), filepath.Join(folder, ".claude")))
	for _, dir := range []string{file, folder} {
		assert.Error(t, addAllowRule(dir, "make test"))
	}
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(victim), "settings.local.json"))
}
