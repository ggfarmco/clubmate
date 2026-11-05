package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	graphsDir       = ".clubmate/graphs"
	userGraphsDir   = "~/" + graphsDir
	builtinDir      = "builtin"
	graphExt        = ".json"
	maxAgentNodes   = 8
	maxAllNodes     = 16
	maxEdges        = 32
	defaultMaxTurns = 20
	maxMaxTurns     = 200
	defaultMaxSteps = 50
	maxMaxSteps     = 500
	maxMaxFires     = 50
	defaultTimeout  = 600
	maxTimeout      = 7200
	inMain          = "main"
)

const (
	outPass = "pass"
	outFail = "fail"
)

const (
	onDone    = "done"
	onError   = "error"
	onAsks    = "asks"
	onOutcome = "outcome"
)

//go:embed builtin/*.json
var builtinGraphs embed.FS

var (
	nameRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)
	edgeKinds     = []string{onDone, onError, onAsks, onOutcome}
	errNoGraph    = errors.New("graph is required")
)

type Graph struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MaxTurns    int    `json:"maxTurns,omitempty"`
	MaxSteps    int    `json:"maxSteps,omitempty"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}

type Node struct {
	ID       string            `json:"id"`
	Role     string            `json:"role,omitempty"`
	Prompt   string            `json:"prompt,omitempty"`
	Run      string            `json:"run,omitempty"`
	Command  string            `json:"command,omitempty"`
	Args     json.RawMessage   `json:"args,omitempty"`
	In       string            `json:"in,omitempty"`
	Timeout  int               `json:"timeout,omitempty"`
	Exits    map[string]string `json:"exits,omitempty"`
	Start    bool              `json:"start,omitempty"`
	Branch   string            `json:"branch,omitempty"`
	Afk      bool              `json:"afk,omitempty"`
	Outcomes []string          `json:"outcomes,omitempty"`
	End      []string          `json:"end,omitempty"`
}

type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	On       string `json:"on,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Text     string `json:"text,omitempty"`
	MaxFires int    `json:"maxFires,omitempty"`
}

type graphFile struct {
	Name        string        `json:"name"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Path        string        `json:"path"`
	Nodes       int           `json:"nodes"`
	Builtin     bool          `json:"builtin,omitempty"`
	User        bool          `json:"user,omitempty"`
	Untrusted   bool          `json:"untrusted,omitempty"`
	Hash        string        `json:"hash,omitempty"`
	Changed     []string      `json:"changed,omitempty"`
	Trusted     *trustedSteps `json:"trusted,omitempty"`
	Error       string        `json:"error,omitempty"`

	spec *Graph
}

type origin int

const (
	fromProject origin = iota
	fromUser
	fromBuiltin
)

func loadGraph(dirs []string, home, name string) (*Graph, origin, error) {
	if name == "" {
		return nil, fromProject, errNoGraph
	}
	if !nameRe.MatchString(name) {
		return nil, fromProject, fmt.Errorf("graph %q: use lowercase letters, digits, - and _", name)
	}
	for i, dir := range append(slices.Clip(dirs), home) {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, graphsDir, name+graphExt)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fromProject, fmt.Errorf("read graph %s: %w", path, err)
		}
		from := fromProject
		if i == len(dirs) {
			from = fromUser
		}
		g, err := parseGraph(name, data)
		if err != nil {
			return nil, from, fmt.Errorf("%s: %w", path, err)
		}
		return g, from, nil
	}
	path := builtinPath(name)
	data, err := builtinGraphs.ReadFile(path)
	if err != nil {
		return nil, fromProject, fmt.Errorf("no graph %q in %s/ of %s, in %s, nor built in", name, graphsDir, strings.Join(dirs, ", "), userGraphsDir)
	}
	g, err := parseGraph(name, data)
	if err != nil {
		return nil, fromBuiltin, fmt.Errorf("%s: %w", path, err)
	}
	return g, fromBuiltin, nil
}

func graphDir(dirs []string, name string) string {
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, graphsDir, name+graphExt)); err == nil {
			return dir
		}
	}
	return ""
}

func listGraphs(dirs []string, home string) []graphFile {
	seen := map[string]bool{}
	var files []graphFile
	for i, dir := range append(slices.Clip(dirs), home) {
		if dir == "" {
			continue
		}
		paths, err := filepath.Glob(filepath.Join(dir, graphsDir, "*"+graphExt))
		if err != nil {
			continue
		}
		for _, path := range paths {
			name := strings.TrimSuffix(filepath.Base(path), graphExt)
			if seen[name] || !nameRe.MatchString(name) {
				continue
			}
			seen[name] = true
			data, err := os.ReadFile(path)
			files = append(files, describeGraph(graphFile{Name: name, Path: path, User: i == len(dirs)}, data, err))
		}
	}
	entries, _ := builtinGraphs.ReadDir(builtinDir)
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), graphExt)
		if seen[name] {
			continue
		}
		path := builtinPath(name)
		data, err := builtinGraphs.ReadFile(path)
		files = append(files, describeGraph(graphFile{Name: name, Path: path, Builtin: true}, data, err))
	}
	slices.SortFunc(files, func(x, y graphFile) int { return strings.Compare(x.Name, y.Name) })
	return files
}

func describeGraph(f graphFile, data []byte, err error) graphFile {
	if err == nil {
		var g *Graph
		if g, err = parseGraph(f.Name, data); err == nil {
			f.Title, f.Description, f.Nodes, f.spec = g.Title, g.Description, len(g.Nodes), g
		}
	}
	if err != nil {
		f.Error = err.Error()
	}
	return f
}

func builtinPath(name string) string {
	return builtinDir + "/" + name + graphExt
}

func parseGraph(name string, data []byte) (*Graph, error) {
	var g Graph
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("invalid graph JSON: %w", err)
	}
	g.Name = name
	if err := g.validate(); err != nil {
		return nil, err
	}
	return &g, nil
}

func (g *Graph) validate() error {
	if len(g.Nodes) == 0 {
		return errors.New("nodes: at least one node is required")
	}
	if len(g.Nodes) > maxAllNodes {
		return fmt.Errorf("nodes: at most %d nodes", maxAllNodes)
	}
	agents := 0
	for _, n := range g.Nodes {
		if !n.step() {
			agents++
		}
	}
	if agents > maxAgentNodes {
		return fmt.Errorf("nodes: at most %d agent nodes, since a session runs at most %d child sessions at once", maxAgentNodes, maxAgentNodes)
	}
	if len(g.Edges) > maxEdges {
		return fmt.Errorf("edges: at most %d", maxEdges)
	}
	if g.MaxTurns < 0 || g.MaxTurns > maxMaxTurns {
		return fmt.Errorf("maxTurns: between 1 and %d, or omit for %d", maxMaxTurns, defaultMaxTurns)
	}
	if g.MaxSteps < 0 || g.MaxSteps > maxMaxSteps {
		return fmt.Errorf("maxSteps: between 1 and %d, or omit for %d", maxMaxSteps, defaultMaxSteps)
	}
	nodes := map[string]*Node{}
	starts := 0
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if !nameRe.MatchString(n.ID) {
			return fmt.Errorf("nodes[%d].id %q: use lowercase letters, digits, - and _", i, n.ID)
		}
		if nodes[n.ID] != nil {
			return fmt.Errorf("nodes[%d].id %q: duplicate", i, n.ID)
		}
		nodes[n.ID] = n
		if n.Start {
			starts++
		}
	}
	if starts == 0 {
		return errors.New("nodes: mark at least one node with start: true")
	}
	for _, n := range g.Nodes {
		if err := n.validate(nodes); err != nil {
			return fmt.Errorf("node %s: %w", n.ID, err)
		}
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.On == "" {
			e.On = onDone
		}
		from, to := nodes[e.From], nodes[e.To]
		switch {
		case from == nil:
			return fmt.Errorf("edges[%d].from: no node %q", i, e.From)
		case to == nil:
			return fmt.Errorf("edges[%d].to: no node %q", i, e.To)
		case !slices.Contains(edgeKinds, e.On):
			return fmt.Errorf("edges[%d].on %q: one of %s", i, e.On, strings.Join(edgeKinds, ", "))
		case e.On == onAsks && from.step():
			return fmt.Errorf("edges[%d].on: a step doesn't ask", i)
		case e.On == onOutcome && !slices.Contains(from.outcomeWords(), e.Outcome):
			return fmt.Errorf("edges[%d].outcome %q: not among node %s's outcomes %v", i, e.Outcome, e.From, from.outcomeWords())
		case e.On != onOutcome && e.Outcome != "":
			return fmt.Errorf("edges[%d].outcome: only for on: outcome", i)
		case e.On == onOutcome && slices.Contains(from.End, e.Outcome):
			return fmt.Errorf("edges[%d].outcome %q: ends the run (node %s's end), so no edge can fire on it", i, e.Outcome, e.From)
		case e.MaxFires < 0 || e.MaxFires > maxMaxFires:
			return fmt.Errorf("edges[%d].maxFires: between 1 and %d, or omit for no limit", i, maxMaxFires)
		case to.step() && e.Text != "":
			return fmt.Errorf("edges[%d].text: a step takes no message", i)
		}
		if err := checkPlaceholders(e.Text, nodes, true); err != nil {
			return fmt.Errorf("edges[%d].text: %w", i, err)
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(e.Text, -1) {
			switch {
			case m[1] == "payload.output" && !from.step():
				return fmt.Errorf("edges[%d].text: {{payload.output}}: payload.output is only in edges from a step", i)
			case m[1] == "payload.reply" && from.step():
				return fmt.Errorf("edges[%d].text: {{payload.reply}}: payload.reply is only in edges from an agent", i)
			}
		}
	}
	for id, n := range nodes {
		ways := g.waysOut(id)
		for _, o := range n.End {
			if !slices.Contains(ways, o) {
				return fmt.Errorf("node %s: end %q: not among its outcomes %v", id, o, ways)
			}
		}
	}
	return nil
}

func (n *Node) validate(nodes map[string]*Node) error {
	kinds := 0
	for _, v := range []string{n.Prompt, n.Run, n.Command} {
		if strings.TrimSpace(v) != "" {
			kinds++
		}
	}
	if kinds != 1 {
		return errors.New("give one of prompt, run or command")
	}
	if !n.step() {
		if err := checkPlaceholders(n.Prompt, nodes, false); err != nil {
			return fmt.Errorf("prompt: %w", err)
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(n.Branch, -1) {
			if m[1] != "run" && m[1] != "graph" {
				return fmt.Errorf("branch: {{%s}}: use run or graph", m[1])
			}
		}
		for _, o := range n.Outcomes {
			if strings.TrimSpace(o) == "" || strings.Contains(o, "{{") {
				return fmt.Errorf("outcome %q: give a plain word", o)
			}
		}
		if n.In != "" || n.Timeout != 0 || len(n.Exits) > 0 || len(n.Args) > 0 {
			return errors.New("in, timeout, exits and args are only for steps")
		}
		return nil
	}
	switch {
	case n.Start:
		return errors.New("a step can't be a start node")
	case len(n.Outcomes) > 0:
		return errors.New("a step's outcomes come from its exit code: use exits")
	case n.Branch != "" || n.Afk:
		return errors.New("branch and afk are only for agent nodes")
	case n.Timeout < 0 || n.Timeout > maxTimeout:
		return fmt.Errorf("timeout: between 1 and %d seconds, or omit for %d", maxTimeout, defaultTimeout)
	case len(n.Args) > 0 && n.Command == "":
		return errors.New("args: only with command")
	case placeholderRe.MatchString(n.Run):
		return errors.New("run: no placeholders; a step reads the CLUBMATE_GRAPH_* environment variables")
	case n.In != "" && n.In != inMain && (nodes[n.In] == nil || nodes[n.In].step()):
		return fmt.Errorf("in %q: in: main or an agent node id", n.In)
	}
	for code, word := range n.Exits {
		if c, err := strconv.Atoi(code); err != nil || c < 1 || c > 255 || strconv.Itoa(c) != code {
			return fmt.Errorf("exits %q: exit code 1–255", code)
		}
		switch {
		case word == outPass || word == outFail || word == onDone || word == onError:
			return fmt.Errorf("exits %q: %s: pass and fail are built in, done and error are taken", code, word)
		case strings.TrimSpace(word) == "" || strings.Contains(word, "{{"):
			return fmt.Errorf("exits %q: give a plain word", code)
		}
	}
	for _, m := range placeholderRe.FindAllStringSubmatch(string(n.Args), -1) {
		if m[1] == "run" || m[1] == "graph" {
			continue
		}
		if id, ok := strings.CutPrefix(m[1], "node."); ok && nodes[id] != nil {
			continue
		}
		return fmt.Errorf("args: {{%s}}: use run, graph or node.<id>", m[1])
	}
	return nil
}

func (g *Graph) node(id string) *Node {
	i := slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == id })
	if i < 0 {
		return nil
	}
	return &g.Nodes[i]
}

func (g *Graph) hasEdge(from, on string) bool {
	return slices.ContainsFunc(g.Edges, func(e Edge) bool { return e.From == from && e.On == on })
}

func (g *Graph) check(id string) bool {
	n := g.node(id)
	return n != nil && n.step() && g.hasEdge(id, onOutcome)
}

func (g *Graph) owned(e Edge) bool {
	from, to := g.node(e.From), g.node(e.To)
	return e.MaxFires > 0 || from != nil && from.step() || to != nil && to.step()
}

func (g *Graph) waysOut(id string) []string {
	n := g.node(id)
	if n.step() && !g.check(id) {
		return []string{onDone}
	}
	return n.outcomeWords()
}

func (g *Graph) maxSteps() int {
	if g.MaxSteps == 0 {
		return defaultMaxSteps
	}
	return g.MaxSteps
}

func (g *Graph) maxTurns() int {
	if g.MaxTurns == 0 {
		return defaultMaxTurns
	}
	return g.MaxTurns
}

func (n *Node) step() bool {
	return n.Run != "" || n.Command != ""
}

func (n *Node) outcomeWords() []string {
	if !n.step() {
		return n.Outcomes
	}
	codes := slices.Collect(maps.Keys(n.Exits))
	slices.SortFunc(codes, func(x, y string) int {
		a, _ := strconv.Atoi(x)
		b, _ := strconv.Atoi(y)
		return a - b
	})
	words := []string{outPass, outFail}
	for _, c := range codes {
		words = append(words, n.Exits[c])
	}
	return words
}

func (n *Node) timeout() time.Duration {
	if n.Timeout == 0 {
		return defaultTimeout * time.Second
	}
	return time.Duration(n.Timeout) * time.Second
}

func envName(id string) string {
	return strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
}

func (n *Node) label() string {
	if n.Role != "" {
		return n.Role
	}
	return n.ID
}

func checkPlaceholders(s string, nodes map[string]*Node, edge bool) error {
	for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
		if graphPlaceholder(m[1], nodes) || edge && triggerPlaceholder(m[1]) {
			continue
		}
		if edge {
			return fmt.Errorf("{{%s}}: use input, run, graph, node.<id> of a node in the graph, topic, seq, payload or payload.<field>", m[1])
		}
		return fmt.Errorf("{{%s}}: use input, run, graph or node.<id> of a node in the graph", m[1])
	}
	return nil
}

func graphPlaceholder(name string, nodes map[string]*Node) bool {
	if name == "input" || name == "run" || name == "graph" {
		return true
	}
	id, ok := strings.CutPrefix(name, "node.")
	return ok && nodes[id] != nil
}

func triggerPlaceholder(name string) bool {
	return name == "topic" || name == "seq" || name == "payload" || strings.HasPrefix(name, "payload.")
}
