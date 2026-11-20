package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"clubmate/plugins/internal/toolgate"
)

const (
	ask        = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"Clubmate gate"}}`
	batchEvent = "PostToolBatch"
	textMax    = 9000
	cutFormat  = "\n…\nThe gate's message is cut here; the whole of it is in %s.\n"
)

type call struct {
	Tool  string          `json:"tool_name"`
	Input json.RawMessage `json:"tool_input"`
	ID    string          `json:"tool_use_id"`
	Cwd   string          `json:"cwd"`
}

type batch struct {
	Calls []struct {
		ID string `json:"tool_use_id"`
	} `json:"tool_calls"`
}

type batchOutput struct {
	Hook batchContext `json:"hookSpecificOutput"`
}

type batchContext struct {
	Event   string `json:"hookEventName"`
	Context string `json:"additionalContext"`
}

func main() {
	run(os.Args[1:], os.Stdin, os.Stdout)
}

func run(args []string, stdin io.Reader, stdout io.Writer) {
	if len(args) > 0 && args[0] == toolgate.BatchArg {
		tell(args[1:], stdin, stdout)
		return
	}
	if asks(args, stdin) {
		_, _ = stdout.Write([]byte(ask))
	}
}

func asks(args []string, stdin io.Reader) bool {
	raw, err := io.ReadAll(stdin)
	var c *call
	if err != nil || json.Unmarshal(raw, &c) != nil || c == nil || c.Tool == "" || len(args) != 1 {
		return true
	}
	dir := args[0]
	own := matches(dir, c)
	place, there := cwdMatches(dir, c)
	if !own && !there {
		return false
	}
	if !there {
		place = toolgate.Place{}
	}
	mark(dir, c.ID, place)
	return true
}

func matches(dir string, c *call) bool {
	rules, ok := readRules(dir)
	return !ok || len(toolgate.All(rules, c.Tool, c.Input)) > 0
}

func cwdMatches(dir string, c *call) (toolgate.Place, bool) {
	if filepath.Base(dir) == toolgate.AskAllDir || c.Cwd == "" {
		return toolgate.Place{}, false
	}
	place, found := toolgate.PlaceOf(c.Cwd)
	if !found {
		return toolgate.Place{}, false
	}
	other := filepath.Join(filepath.Dir(dir), toolgate.HookDirName(place.Repo))
	if other == dir {
		return toolgate.Place{}, false
	}
	if _, err := os.Stat(filepath.Join(other, toolgate.RulesFile)); errors.Is(err, fs.ErrNotExist) {
		return toolgate.Place{}, false
	}
	return place, matches(other, c)
}

func readRules(dir string) ([]toolgate.Rule, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, toolgate.RulesFile))
	if err != nil {
		return nil, false
	}
	var rules *[]toolgate.Rule
	if json.Unmarshal(raw, &rules) != nil || rules == nil {
		return nil, false
	}
	return *rules, !slices.ContainsFunc(*rules, unusable)
}

func unusable(r toolgate.Rule) bool {
	if _, err := path.Match(r.Tool, ""); err != nil || r.Tool == "" || r.How != "" && !slices.Contains(toolgate.Hows, r.How) {
		return true
	}
	if r.How == toolgate.HowRegex {
		_, err := regexp.Compile(r.Text)
		return err != nil
	}
	return false
}

func tell(args []string, stdin io.Reader, stdout io.Writer) {
	raw, err := io.ReadAll(stdin)
	var b batch
	if err != nil || len(args) != 1 || json.Unmarshal(raw, &b) != nil {
		return
	}
	var texts []string
	first := ""
	for _, c := range b.Calls {
		if text, ok := claim(args[0], c.ID); ok && !slices.Contains(texts, text) {
			texts = append(texts, text)
			first = cmp.Or(first, c.ID)
		}
	}
	if len(texts) == 0 {
		return
	}
	out, err := json.Marshal(batchOutput{Hook: batchContext{Event: batchEvent, Context: capped(args[0], first, strings.Join(texts, "\n\n"))}})
	if err == nil {
		_, _ = stdout.Write(out)
	}
}

func claim(dir, id string) (string, bool) {
	if !toolgate.SafeID(id) {
		return "", false
	}
	from := filepath.Join(dir, toolgate.FindingsDir, id)
	data, err := os.ReadFile(from)
	if err != nil || len(data) == 0 {
		return "", false
	}
	told := filepath.Join(dir, toolgate.ToldDir)
	if os.MkdirAll(told, 0o700) != nil || os.Rename(from, filepath.Join(told, id)) != nil {
		return "", false
	}
	return string(data), true
}

func capped(dir, id, text string) string {
	if len(text) <= textMax {
		return text
	}
	full := filepath.Join(dir, toolgate.MessagesDir, "batch-"+id)
	if os.MkdirAll(filepath.Dir(full), 0o700) != nil || os.WriteFile(full, []byte(text), 0o600) != nil {
		full = "(it could not be written)"
	}
	note := fmt.Sprintf(cutFormat, full)
	end := textMax - len(note)
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + note
}

func mark(dir, id string, place toolgate.Place) {
	if !toolgate.SafeID(id) {
		return
	}
	marks := filepath.Join(dir, toolgate.MarksDir)
	if os.MkdirAll(marks, 0o700) != nil {
		return
	}
	var data []byte
	if place.Repo != "" {
		data, _ = json.Marshal(place)
	}
	_ = os.WriteFile(filepath.Join(marks, id), data, 0o600)
}
