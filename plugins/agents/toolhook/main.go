package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
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
	ask         = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"Clubmate gate"}}`
	rulesFile   = "rules.json"
	marksDir    = "marks"
	batchArg    = "batch"
	batchEvent  = "PostToolBatch"
	findingsDir = "findings"
	toldDir     = "told"
	messagesDir = "messages"
	textMax     = 9000
	cutFormat   = "\n…\nThe gate's message is cut here; the whole of it is in %s.\n"
)

var hows = []string{"", toolgate.HowCommand, toolgate.HowStarts, toolgate.HowContains, toolgate.HowRegex}

type call struct {
	Tool  string          `json:"tool_name"`
	Input json.RawMessage `json:"tool_input"`
	ID    string          `json:"tool_use_id"`
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
	if len(args) > 0 && args[0] == batchArg {
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
	rules, ok := readRules(dir)
	if ok && len(toolgate.All(rules, c.Tool, c.Input)) == 0 {
		return false
	}
	mark(dir, c.ID)
	return true
}

func readRules(dir string) ([]toolgate.Rule, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, rulesFile))
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
	if _, err := path.Match(r.Tool, ""); err != nil || r.Tool == "" || !slices.Contains(hows, r.How) {
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
	if !safeID(id) {
		return "", false
	}
	from := filepath.Join(dir, findingsDir, id)
	data, err := os.ReadFile(from)
	if err != nil || len(data) == 0 {
		return "", false
	}
	told := filepath.Join(dir, toldDir)
	if os.MkdirAll(told, 0o700) != nil || os.Rename(from, filepath.Join(told, id)) != nil {
		return "", false
	}
	return string(data), true
}

func capped(dir, id, text string) string {
	if len(text) <= textMax {
		return text
	}
	full := filepath.Join(dir, messagesDir, "batch-"+id)
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

func safeID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, `/\`)
}

func mark(dir, id string) {
	if !safeID(id) {
		return
	}
	marks := filepath.Join(dir, marksDir)
	if os.MkdirAll(marks, 0o700) != nil {
		return
	}
	if f, err := os.Create(filepath.Join(marks, id)); err == nil {
		_ = f.Close()
	}
}
