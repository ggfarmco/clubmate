package graphs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	FromBuiltin = "builtin"
	FromUser    = "user"
	FromRepo    = "repo"
)

type GateGraphInfo struct {
	Name    string         `json:"name"`
	Title   string         `json:"title,omitempty"`
	From    string         `json:"from"`
	Hash    string         `json:"hash"`
	Decides bool           `json:"decides"`
	Head    string         `json:"head,omitempty"`
	Edges   string         `json:"edges,omitempty"`
	Nodes   []GateNodeInfo `json:"nodes"`
	Error   string         `json:"error,omitempty"`
	Changed time.Time      `json:"changed,omitzero"`
	Shape   int            `json:"shape,omitempty"`
}

const gateInfoShape = 1

type GateNodeInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Step    bool   `json:"step,omitempty"`
	Command string `json:"command,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Verdict bool   `json:"verdict,omitempty"`
	Other   string `json:"other,omitempty"`
}

func (e *Engine) GateGraphInfo(_ context.Context, repo, name string) (GateGraphInfo, error) {
	g, from, err := loadGraph(gateDirs(repo), e.a.home, name)
	if err != nil {
		return GateGraphInfo{}, err
	}
	info := gateInfo(g, from)
	info.Changed = graphFileTime(gateDirs(repo), e.a.home, name)
	return info, nil
}

func graphFileTime(dirs []string, home, name string) time.Time {
	for _, dir := range append(slices.Clip(dirs), home) {
		if dir == "" {
			continue
		}
		st, err := os.Stat(filepath.Join(dir, graphsDir, name+graphExt))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return time.Time{}
		}
		return st.ModTime()
	}
	return time.Time{}
}

func (e *Engine) GateGraphStamp(repo, name string) string {
	for _, dir := range append(gateDirs(repo), e.a.home) {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, graphsDir, name+graphExt)
		if info, err := os.Stat(path); err == nil {
			return fmt.Sprintf("%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())
		}
	}
	return ""
}

func (e *Engine) GateGraphChoices(_ context.Context, repo string) []GateGraphInfo {
	files := listGraphs(gateDirs(repo), e.a.home)
	out := make([]GateGraphInfo, 0, len(files))
	for _, f := range files {
		from := fromProject
		switch {
		case f.Builtin:
			from = fromBuiltin
		case f.User:
			from = fromUser
		}
		if f.spec == nil {
			out = append(out, GateGraphInfo{Name: f.Name, From: originWord(from), Error: f.Error})
			continue
		}
		out = append(out, gateInfo(f.spec, from))
	}
	return out
}

func gateInfo(g *Graph, from origin) GateGraphInfo {
	head := struct {
		Title       string `json:"title,omitempty"`
		Description string `json:"description,omitempty"`
		MaxTurns    int    `json:"maxTurns,omitempty"`
		MaxSteps    int    `json:"maxSteps,omitempty"`
	}{g.Title, g.Description, g.MaxTurns, g.MaxSteps}
	info := GateGraphInfo{Name: g.Name, Title: g.Title, From: originWord(from), Hash: graphHash(g), Decides: g.decides(), Head: jsonText(head), Edges: jsonText(g.Edges), Shape: gateInfoShape}
	for _, n := range g.Nodes {
		rest := n
		rest.Prompt, rest.Model, rest.Effort = "", "", ""
		node := GateNodeInfo{ID: n.ID, Title: n.label(), Step: n.step(), Prompt: n.Prompt, Model: n.Model, Effort: n.Effort, Verdict: n.decides()}
		if node.Step {
			node.Command = commandLine(&n)
			if n.Run != "" {
				rest.Run = ""
			} else {
				rest.Command, rest.Args = "", nil
			}
		}
		node.Other = jsonText(rest)
		info.Nodes = append(info.Nodes, node)
	}
	return info
}

func jsonText(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func originWord(from origin) string {
	switch from {
	case fromBuiltin:
		return FromBuiltin
	case fromUser:
		return FromUser
	}
	return FromRepo
}

func (i GateGraphInfo) AsShape(shape int) GateGraphInfo {
	if shape >= i.Shape {
		return i
	}
	i.Shape = shape
	i.Nodes = slices.Clone(i.Nodes)
	for k, n := range i.Nodes {
		var rest Node
		if json.Unmarshal([]byte(n.Other), &rest) == nil {
			rest.Run, rest.Command, rest.Args = "", "", nil
			n.Other = jsonText(rest)
		}
		if n.Step {
			n.Prompt = ""
		}
		i.Nodes[k] = n
	}
	return i
}
