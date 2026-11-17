package graphs

import (
	"context"
	"encoding/json"
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
}

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
	return gateInfo(g, from), nil
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
	info := GateGraphInfo{Name: g.Name, Title: g.Title, From: originWord(from), Hash: graphHash(g), Decides: g.decides(), Head: jsonText(head), Edges: jsonText(g.Edges)}
	for _, n := range g.Nodes {
		rest := n
		rest.Prompt, rest.Run, rest.Command, rest.Args, rest.Model, rest.Effort = "", "", "", nil, "", ""
		node := GateNodeInfo{ID: n.ID, Title: n.label(), Step: n.step(), Model: n.Model, Effort: n.Effort, Verdict: n.decides(), Other: jsonText(rest)}
		if node.Step {
			node.Command = commandLine(&n)
		} else {
			node.Prompt = n.Prompt
		}
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
