package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	verdictAllow  = "allow"
	verdictAsk    = "ask"
	verdictDeny   = "deny"
	modeBypass    = "bypassPermissions"
	managedPath   = "/Library/Application Support/ClaudeCode/managed-settings.json"
	bashTool      = "Bash"
	localSettings = "settings.local.json"
)

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

type ruleSet struct {
	Allow       []string
	Ask         []string
	Deny        []string
	DefaultMode string
	NoBypass    bool
}

type settingsFile struct {
	Permissions struct {
		Allow       []string `json:"allow"`
		Ask         []string `json:"ask"`
		Deny        []string `json:"deny"`
		DefaultMode string   `json:"defaultMode"`
		NoBypass    string   `json:"disableBypassPermissionsMode"`
	} `json:"permissions"`
}

func loadRules(managed, home, dir string, project bool) ruleSet {
	var rs ruleSet
	paths := []struct {
		path    string
		trusted bool
	}{
		{filepath.Join(home, ".claude", "settings.json"), true},
		{filepath.Join(dir, ".claude", "settings.json"), false},
		{filepath.Join(dir, ".claude", localSettings), false},
		{managed, true},
	}
	for _, src := range paths {
		f, ok := readSettings(src.path)
		if !ok {
			continue
		}
		p := f.Permissions
		if src.trusted || project {
			rs.Allow = append(rs.Allow, p.Allow...)
		}
		rs.Ask = append(rs.Ask, p.Ask...)
		rs.Deny = append(rs.Deny, p.Deny...)
		if p.DefaultMode != "" && (src.trusted || p.DefaultMode != modeBypass) {
			rs.DefaultMode = p.DefaultMode
		}
		if src.path == managed && p.NoBypass == "disable" {
			rs.NoBypass = true
		}
	}
	if rs.NoBypass && rs.DefaultMode == modeBypass {
		rs.DefaultMode = ""
	}
	return rs
}

func readSettings(path string) (settingsFile, bool) {
	var f settingsFile
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, false
	}
	if err != nil {
		logError(fmt.Errorf("read %s: %w", path, err))
		return f, false
	}
	if err := json.Unmarshal(data, &f); err != nil {
		logError(fmt.Errorf("parse %s: %w", path, err))
		return f, false
	}
	return f, true
}

func (rs ruleSet) decide(command string) (string, string) {
	if rule, ok := rs.denied(command); ok {
		return verdictDeny, rule
	}
	for _, rule := range rs.Ask {
		if matchesAnyPart(rule, command) {
			return verdictAsk, rule
		}
	}
	if rule, ok := rs.suspicious(command); ok {
		return verdictAsk, rule
	}
	simple := simpleCommand(command)
	for _, rule := range rs.Allow {
		if rule == bashTool && (simple || len(rs.Deny)+len(rs.Ask) == 0) {
			return verdictAllow, rule
		}
		if simple && bashMatch(rule, command, false) {
			return verdictAllow, rule
		}
	}
	return verdictAsk, ""
}

func (rs ruleSet) denied(command string) (string, bool) {
	for _, rule := range rs.Deny {
		if matchesAnyPart(rule, command) {
			return rule, true
		}
	}
	return "", false
}

func (rs ruleSet) suspicious(command string) (string, bool) {
	if strings.ContainsAny(command, "'\"\\$`") && len(rs.Deny)+len(rs.Ask) > 0 {
		return "a quoted or expanded command", true
	}
	words := commandWords(command)
	for _, rule := range rs.Deny {
		need := ruleWords(rule)
		if len(need) > 0 && !slices.ContainsFunc(need, func(w string) bool { return !slices.Contains(words, w) }) {
			return rule, true
		}
	}
	return "", false
}

func ruleWords(rule string) []string {
	inner, ok := strings.CutPrefix(rule, bashTool+"(")
	if !ok {
		return nil
	}
	inner = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(inner, ")"), ":*"), "*")
	return strings.Fields(inner)
}

func commandWords(command string) []string {
	tokens := strings.FieldsFunc(command, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-/", r))
	})
	words := slices.Clone(tokens)
	for _, t := range tokens {
		if strings.Contains(t, "/") {
			words = append(words, filepath.Base(t))
		}
	}
	return words
}

func matchesAnyPart(rule, command string) bool {
	for _, part := range commandParts(command) {
		if bashMatch(rule, part, true) {
			return true
		}
	}
	return false
}

func commandParts(command string) []string {
	parts := strings.FieldsFunc(command, func(r rune) bool { return strings.ContainsRune(";&|\n`()$", r) })
	out := []string{command}
	for _, p := range parts {
		words := strings.Fields(p)
		for len(words) > 0 && strings.Contains(words[0], "=") {
			words = words[1:]
		}
		if len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}

func bashMatch(rule, command string, broad bool) bool {
	if rule == bashTool {
		return true
	}
	inner, ok := strings.CutPrefix(rule, bashTool+"(")
	if !ok || !strings.HasSuffix(inner, ")") {
		return false
	}
	inner = strings.TrimSuffix(inner, ")")
	prefix, isPrefix := strings.CutSuffix(inner, ":*")
	if !isPrefix {
		prefix, isPrefix = strings.CutSuffix(inner, " *")
	}
	if !isPrefix && strings.Contains(inner, "*") {
		if !broad {
			return false
		}
		prefix, _, _ = strings.Cut(inner, "*")
		prefix, isPrefix = strings.TrimSpace(prefix), true
	}
	if !isPrefix {
		return command == inner
	}
	if broad {
		return strings.HasPrefix(strings.TrimSpace(command), prefix)
	}
	return command == prefix || strings.HasPrefix(command, prefix+" ")
}

func simpleCommand(command string) bool {
	if command == "" {
		return false
	}
	quoted, word, first := false, 0, true
	for _, r := range command {
		switch {
		case quoted && r == '\'':
			quoted = false
		case quoted && (r < 0x20 || r > 0x7e):
			return false
		case quoted:
		case r == '\'':
			quoted = true
			word++
		case r == ' ':
			if word == 0 {
				return false
			}
			word, first = 0, false
		case r == '=' && first:
			return false
		case !plainWord.MatchString(string(r)):
			return false
		default:
			word++
		}
	}
	return !quoted && word > 0
}

func addAllowRule(dir, command string) error {
	if strings.ContainsAny(command, "*()") {
		return errors.New("always: a command with *, ( or ) would read as a pattern; allow it once instead")
	}
	path := filepath.Join(dir, ".claude", localSettings)
	for _, p := range []string{filepath.Dir(path), path} {
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("always: %s is a symlink; allow it once instead", p)
		}
	}
	doc := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	perms, _ := doc["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	rule := bashTool + "(" + command + ")"
	allow, _ := perms["allow"].([]any)
	if slices.Contains(allow, any(rule)) {
		return nil
	}
	perms["allow"] = append(allow, rule)
	doc["permissions"] = perms
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), localSettings+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(out, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
