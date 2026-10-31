package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	graphsDir       = ".clubmate/graphs"
	graphExt        = ".json"
	maxNodes        = 8
	maxEdges        = 32
	defaultMaxTurns = 20
	maxMaxTurns     = 200
)

const (
	onDone    = "done"
	onError   = "error"
	onAsks    = "asks"
	onOutcome = "outcome"
)

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
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}

type Node struct {
	ID       string   `json:"id"`
	Role     string   `json:"role,omitempty"`
	Prompt   string   `json:"prompt"`
	Start    bool     `json:"start,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Outcomes []string `json:"outcomes,omitempty"`
	End      []string `json:"end,omitempty"`
}

type Edge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	On      string `json:"on,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Text    string `json:"text,omitempty"`
}

type graphFile struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Nodes       int    `json:"nodes"`
	Error       string `json:"error,omitempty"`
}

func loadGraph(dirs []string, name string) (*Graph, string, error) {
	if name == "" {
		return nil, "", errNoGraph
	}
	if !nameRe.MatchString(name) {
		return nil, "", fmt.Errorf("graph %q: use lowercase letters, digits, - and _", name)
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, graphsDir, name+graphExt)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("read graph %s: %w", path, err)
		}
		g, err := parseGraph(name, data)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		return g, path, nil
	}
	return nil, "", fmt.Errorf("no graph %q in %s/ of %s", name, graphsDir, strings.Join(dirs, ", "))
}

func listGraphs(dirs []string) []graphFile {
	seen := map[string]bool{}
	var files []graphFile
	for _, dir := range dirs {
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
			f := graphFile{Name: name, Path: path}
			data, err := os.ReadFile(path)
			if err == nil {
				var g *Graph
				if g, err = parseGraph(name, data); err == nil {
					f.Title, f.Description, f.Nodes = g.Title, g.Description, len(g.Nodes)
				}
			}
			if err != nil {
				f.Error = err.Error()
			}
			files = append(files, f)
		}
	}
	slices.SortFunc(files, func(x, y graphFile) int { return strings.Compare(x.Name, y.Name) })
	return files
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
	if len(g.Nodes) > maxNodes {
		return fmt.Errorf("nodes: at most %d, since a session runs at most %d child sessions at once", maxNodes, maxNodes)
	}
	if len(g.Edges) > maxEdges {
		return fmt.Errorf("edges: at most %d", maxEdges)
	}
	if g.MaxTurns < 0 || g.MaxTurns > maxMaxTurns {
		return fmt.Errorf("maxTurns: between 1 and %d, or omit for %d", maxMaxTurns, defaultMaxTurns)
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
		if strings.TrimSpace(n.Prompt) == "" {
			return fmt.Errorf("node %s: prompt is required", n.ID)
		}
		if n.Start {
			starts++
		}
	}
	if starts == 0 {
		return errors.New("nodes: mark at least one node with start: true")
	}
	for _, n := range g.Nodes {
		if err := checkPlaceholders(n.Prompt, nodes, false); err != nil {
			return fmt.Errorf("node %s: prompt: %w", n.ID, err)
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(n.Branch, -1) {
			if m[1] != "run" && m[1] != "graph" {
				return fmt.Errorf("node %s: branch: {{%s}}: use run or graph", n.ID, m[1])
			}
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
		case e.On == onOutcome && !slices.Contains(from.Outcomes, e.Outcome):
			return fmt.Errorf("edges[%d].outcome %q: not among node %s's outcomes %v", i, e.Outcome, e.From, from.Outcomes)
		case e.On != onOutcome && e.Outcome != "":
			return fmt.Errorf("edges[%d].outcome: only for on: outcome", i)
		case e.On == onOutcome && slices.Contains(from.End, e.Outcome):
			return fmt.Errorf("edges[%d].outcome %q: ends the run (node %s's end), so no edge can fire on it", i, e.Outcome, e.From)
		}
		if err := checkPlaceholders(e.Text, nodes, true); err != nil {
			return fmt.Errorf("edges[%d].text: %w", i, err)
		}
	}
	for id, n := range nodes {
		for _, o := range n.Outcomes {
			if strings.TrimSpace(o) == "" || strings.Contains(o, "{{") {
				return fmt.Errorf("node %s: outcome %q: give a plain word", id, o)
			}
		}
		for _, o := range n.End {
			if !slices.Contains(n.Outcomes, o) {
				return fmt.Errorf("node %s: end %q: not among its outcomes %v", id, o, n.Outcomes)
			}
		}
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

func (g *Graph) maxTurns() int {
	if g.MaxTurns == 0 {
		return defaultMaxTurns
	}
	return g.MaxTurns
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
