package claudectx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type File struct {
	Path    string
	Kind    string
	Content string
	Nested  bool
}

type Entry struct {
	Name string
	Text string
	Uses int
}

type Context struct {
	Instructions []File
	Skills       []Entry
	MCP          []Entry
	NeedsAuth    []string
	Failed       []string
	Hooks        []Entry
	Subagents    []Entry
	Tools        []Entry
	Deferred     []Entry
}

type Reader struct {
	mu   sync.Mutex
	path string
	off  int64
	st   state
}

var ErrNoLog = errors.New("no Claude log for the session")

func NewReader(path string) *Reader {
	return &Reader{path: path, st: newState()}
}

func Find(configDir, sessionID string) (string, error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\*?[`) {
		return "", ErrNoLog
	}
	matches, err := filepath.Glob(filepath.Join(configDir, "projects", "*", sessionID+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", ErrNoLog
	}
	return matches[0], nil
}

func ConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func (r *Reader) Read() (Context, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.Open(r.path)
	if err != nil {
		return Context{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Context{}, err
	}
	if info.Size() < r.off {
		r.off, r.st = 0, newState()
	}
	if _, err := f.Seek(r.off, io.SeekStart); err != nil {
		return Context{}, err
	}
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Context{}, fmt.Errorf("read %s: %w", r.path, err)
		}
		r.off += int64(len(line))
		r.st.apply(line)
	}
	return r.st.context(), nil
}

type state struct {
	files     ordered[File]
	skills    ordered[Entry]
	mcp       ordered[Entry]
	hooks     ordered[Entry]
	subagents ordered[Entry]
	deferred  ordered[Entry]
	tools     []Entry
	needsAuth []string
	failed    []string
	uses      map[string]int
	skillUses map[string]int
	agentUses map[string]int
}

type ordered[T any] struct {
	keys []string
	vals map[string]T
}

type line struct {
	Type        string          `json:"type"`
	IsSidechain bool            `json:"isSidechain"`
	Attachment  json.RawMessage `json:"attachment"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Shape struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	} `json:"shape"`
}

type attachment struct {
	Type    string          `json:"type"`
	Files   []fileJSON      `json:"files"`
	Path    string          `json:"path"`
	Content json.RawMessage `json:"content"`
	Names   []string        `json:"names"`

	AddedNames   []string `json:"addedNames"`
	AddedBlocks  []string `json:"addedBlocks"`
	RemovedNames []string `json:"removedNames"`
	AddedTypes   []string `json:"addedTypes"`
	AddedLines   []string `json:"addedLines"`
	NeedsAuth    []string `json:"needsAuthMcpServers"`
	Failed       []string `json:"failedMcpServers"`
	HookName     string   `json:"hookName"`
}

type fileJSON struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type toolUseJSON struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Input struct {
		Skill        string `json:"skill"`
		SubagentType string `json:"subagent_type"`
	} `json:"input"`
}

var markers = [][]byte{[]byte(`"type":"attachment"`), []byte(`"type":"api-request-shape"`), []byte(`"type":"tool_use"`)}

func newState() state {
	return state{
		files:     newOrdered[File](),
		skills:    newOrdered[Entry](),
		mcp:       newOrdered[Entry](),
		hooks:     newOrdered[Entry](),
		subagents: newOrdered[Entry](),
		deferred:  newOrdered[Entry](),
		uses:      map[string]int{},
		skillUses: map[string]int{},
		agentUses: map[string]int{},
	}
}

func (s *state) apply(raw []byte) {
	if !slices.ContainsFunc(markers, func(m []byte) bool { return bytes.Contains(raw, m) }) {
		return
	}
	var l line
	if json.Unmarshal(raw, &l) != nil || l.IsSidechain {
		return
	}
	switch l.Type {
	case "attachment":
		var a attachment
		if json.Unmarshal(l.Attachment, &a) == nil {
			s.attach(a)
		}
	case "api-request-shape":
		if len(l.Shape.Tools) == 0 {
			return
		}
		s.tools = s.tools[:0]
		for _, t := range l.Shape.Tools {
			s.tools = append(s.tools, Entry{Name: t.Name, Text: t.Description})
		}
	case "assistant":
		var blocks []toolUseJSON
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			return
		}
		for _, b := range blocks {
			if b.Type != "tool_use" {
				continue
			}
			s.uses[b.Name]++
			if b.Input.Skill != "" {
				s.skillUses[b.Input.Skill]++
			}
			if b.Input.SubagentType != "" {
				s.agentUses[b.Input.SubagentType]++
			}
		}
	}
}

func (s *state) attach(a attachment) {
	switch a.Type {
	case "instructions":
		for _, f := range a.Files {
			s.files.set(f.Path, File{Path: f.Path, Kind: f.Type, Content: f.Content})
		}
	case "nested_memory":
		var f fileJSON
		if json.Unmarshal(a.Content, &f) == nil {
			s.files.set(a.Path, File{Path: a.Path, Kind: f.Type, Content: f.Content, Nested: true})
		}
	case "skill_listing":
		var text string
		if json.Unmarshal(a.Content, &text) == nil {
			for _, e := range listing(text, a.Names) {
				s.skills.set(e.Name, e)
			}
		}
	case "mcp_instructions_delta":
		blocks := map[string]string{}
		for _, b := range a.AddedBlocks {
			head, body, _ := strings.Cut(b, "\n")
			blocks[strings.TrimPrefix(head, "## ")] = body
		}
		for _, n := range a.AddedNames {
			s.mcp.set(n, Entry{Name: n, Text: blocks[n]})
		}
		s.mcp.remove(a.RemovedNames)
	case "deferred_tools_delta":
		for _, n := range a.AddedNames {
			s.deferred.set(n, Entry{Name: n})
		}
		s.deferred.remove(a.RemovedNames)
		s.needsAuth, s.failed = a.NeedsAuth, a.Failed
	case "hook_additional_context":
		var parts []string
		if json.Unmarshal(a.Content, &parts) == nil {
			s.hooks.set(a.HookName, Entry{Name: a.HookName, Text: strings.Join(parts, "\n\n")})
		}
	case "agent_listing_delta":
		for i, t := range a.AddedTypes {
			text := ""
			if i < len(a.AddedLines) {
				text = strings.TrimPrefix(a.AddedLines[i], "- "+t+": ")
			}
			s.subagents.set(t, Entry{Name: t, Text: text})
		}
	}
}

func (s *state) context() Context {
	return Context{
		Instructions: s.files.list(),
		Skills:       counted(s.skills.list(), s.skillUses),
		MCP:          s.mcp.list(),
		NeedsAuth:    slices.Clone(s.needsAuth),
		Failed:       slices.Clone(s.failed),
		Hooks:        s.hooks.list(),
		Subagents:    counted(s.subagents.list(), s.agentUses),
		Tools:        counted(slices.Clone(s.tools), s.uses),
		Deferred:     counted(s.deferred.list(), s.uses),
	}
}

func listing(text string, names []string) []Entry {
	byLength := slices.Clone(names)
	slices.SortFunc(byLength, func(a, b string) int { return len(b) - len(a) })
	var out []Entry
	for l := range strings.SplitSeq(text, "\n") {
		item, ok := strings.CutPrefix(l, "- ")
		if !ok {
			if len(out) > 0 {
				out[len(out)-1].Text += "\n" + l
			}
			continue
		}
		name, desc, _ := strings.Cut(item, ": ")
		for _, n := range byLength {
			if rest, ok := strings.CutPrefix(item, n+": "); ok {
				name, desc = n, rest
				break
			}
		}
		out = append(out, Entry{Name: name, Text: desc})
	}
	return out
}

func counted(entries []Entry, uses map[string]int) []Entry {
	for i := range entries {
		entries[i].Uses = uses[entries[i].Name]
	}
	return entries
}

func newOrdered[T any]() ordered[T] {
	return ordered[T]{vals: map[string]T{}}
}

func (o *ordered[T]) set(key string, v T) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func (o *ordered[T]) remove(keys []string) {
	for _, k := range keys {
		delete(o.vals, k)
	}
	o.keys = slices.DeleteFunc(o.keys, func(k string) bool { return slices.Contains(keys, k) })
}

func (o *ordered[T]) list() []T {
	if len(o.keys) == 0 {
		return nil
	}
	out := make([]T, 0, len(o.keys))
	for _, k := range o.keys {
		out = append(out, o.vals[k])
	}
	return out
}
