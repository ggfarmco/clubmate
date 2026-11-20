package main

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"clubmate/plugins/internal/toolgate"
)

const (
	gatePointTool    = "tool"
	gateNameTool     = "Tool call · "
	toolWordsCommand = "%s · a %s command"
	toolWordsStarts  = "%s · a command that starts with %s"
	toolWordsHas     = "%s · the %s contains %s"
	toolWordsRegex   = "%s · the %s matches /%s/"
	toolWordsAny     = "%s · any call"
	toolWordCommand  = "command"
	toolWordInput    = "input"
	toolHeldWrapped  = "%s · inside %s, held to be safe"
	toolHeldUnread   = "%s · in a command Clubmate can't parse, held to be safe"
)

var (
	errGateTool         = errors.New("tool is a tool name or a glob such as mcp__*")
	errGateRegex        = errors.New("text is not a valid regex")
	errGateHow          = errors.New("how is command, starts, contains or regex")
	errGateBashOnly     = errors.New("command and starts match Bash commands; use contains or regex for another tool")
	errGateCommandWords = errors.New("a command match names a binary and its subcommand, such as git push")
	errGateToolText     = errors.New("a Tool call gate names a command or a tool")
	errGateStartsOne    = errors.New("starts with names one command, without ; & | ( ), $(…), backticks or a heredoc")
	errGateStartsRead   = errors.New("starts with reads a command after VAR=x, sudo, time, env, nohup, exec, command and shell words, so it can't start with one")
	errGateCommandRead  = errors.New("a command match names the binary by its name, read after wrappers such as sudo, env or timeout, so it has no path or wrapper")
	errGateNeverMatches = errors.New("this gate would never match its own text: command names a binary and its subcommand words, without options, quotes, ; & or |")
	errGateToolMatch    = errors.New("a Tool call gate matches tool, how and text, not into, sessions or wrote")
	errGateToolOnly     = errors.New("tool, how and text are for a Tool call gate")
)

func toolMatch(tool, how, text string) (gateConfig, error) {
	tool = cmp.Or(strings.TrimSpace(tool), toolgate.Bash)
	if _, err := path.Match(tool, ""); err != nil {
		return gateConfig{}, errGateTool
	}
	if how != "" && !slices.Contains(toolgate.Hows, how) {
		return gateConfig{}, errGateHow
	}
	if strings.TrimSpace(text) == "" {
		if tool == toolgate.Bash || strings.ContainsAny(tool, `*?[\`) {
			return gateConfig{}, errGateToolText
		}
		return gateConfig{Point: gatePointTool, Tool: tool}, nil
	}
	how = cmp.Or(how, toolHowDefault(tool))
	switch how {
	case toolgate.HowStarts, toolgate.HowCommand:
		if tool != toolgate.Bash {
			return gateConfig{}, errGateBashOnly
		}
		words := strings.Fields(text)
		if how == toolgate.HowCommand && len(words) < 2 {
			return gateConfig{}, errGateCommandWords
		}
		if how == toolgate.HowStarts && !toolgate.OneCommand(text) {
			return gateConfig{}, errGateStartsOne
		}
		text = strings.Join(words, " ")
		if !toolgate.Accepts(how, text) {
			return gateConfig{}, refusal(how, text)
		}
	case toolgate.HowRegex:
		if _, err := regexp.Compile(text); err != nil {
			return gateConfig{}, fmt.Errorf("%w: %v", errGateRegex, err)
		}
	}
	return gateConfig{Point: gatePointTool, Tool: tool, How: how, Text: text}, nil
}

func refusal(how, text string) error {
	err := errGateNeverMatches
	switch {
	case toolgate.Readable(how, text) == text:
	case how == toolgate.HowStarts:
		err = errGateStartsRead
	default:
		err = errGateCommandRead
	}
	if s := toolgate.Suggest(how, text); s != "" {
		return fmt.Errorf("%w; write %q", err, s)
	}
	return err
}

func toolHowDefault(tool string) string {
	if tool == toolgate.Bash {
		return toolgate.HowStarts
	}
	return toolgate.HowContains
}

func toolHowAfter(tool, how string) string {
	if tool != toolgate.Bash && (how == toolgate.HowStarts || how == toolgate.HowCommand) {
		return ""
	}
	return how
}

func toolHeldWhy(g gateConfig, via string) string {
	switch via {
	case "":
		return ""
	case toolgate.ViaUnread:
		return fmt.Sprintf(toolHeldUnread, toolMatchName(g))
	}
	return fmt.Sprintf(toolHeldWrapped, toolMatchName(g), via)
}

func sameTool(x, y gateConfig) bool {
	return x.Tool == y.Tool && x.How == y.How && x.Text == y.Text
}

func toolMatchName(g gateConfig) string {
	switch {
	case g.Text == "":
		return g.Tool
	case g.How == toolgate.HowRegex:
		return "/" + g.Text + "/"
	}
	return g.Text
}

func toolGateName(g gateConfig) string {
	return gateNameTool + toolMatchName(g)
}

func toolMatchWords(g gateConfig) string {
	what := toolWordInput
	if g.Tool == toolgate.Bash {
		what = toolWordCommand
	}
	switch {
	case g.Text == "":
		return fmt.Sprintf(toolWordsAny, g.Tool)
	case g.How == toolgate.HowCommand:
		return fmt.Sprintf(toolWordsCommand, g.Tool, g.Text)
	case g.How == toolgate.HowStarts:
		return fmt.Sprintf(toolWordsStarts, g.Tool, g.Text)
	case g.How == toolgate.HowRegex:
		return fmt.Sprintf(toolWordsRegex, g.Tool, what, g.Text)
	}
	return fmt.Sprintf(toolWordsHas, g.Tool, what, g.Text)
}

func toolRulesOf(gates []gateConfig, primary string) []toolgate.Rule {
	gates = slices.DeleteFunc(slices.Clone(gates), func(g gateConfig) bool {
		return g.Repo != primary || g.Paused || g.Point != gatePointTool
	})
	slices.SortStableFunc(gates, func(x, y gateConfig) int { return x.CreatedAt.Compare(y.CreatedAt) })
	out := make([]toolgate.Rule, 0, len(gates))
	for _, g := range gates {
		out = append(out, toolgate.Rule{ID: g.ID, Tool: g.Tool, How: g.How, Text: g.Text})
	}
	return out
}
