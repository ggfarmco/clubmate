package automation

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	ActPause  = "pause"
	ActResume = "resume"
	ActRun    = "run"
	ActDelete = "delete"
	ActReveal = "reveal"
	ActJSON   = "json"

	propsID     = "props"
	promptLines = 3
	agentNew    = "agents.new"
	agentEmit   = "triggers.emit"
)

var (
	idRe        = regexp.MustCompile(`\b(agt|gr)_[0-9a-zA-Z]+\b`)
	graphHeader = regexp.MustCompile(`^\[Agent graph [^\]\n]*\]\s*`)
)

type DetailAction struct {
	Action   string
	Label    string
	Icon     string
	Variant  string
	Shortcut string
	Confirm  string
}

type Open struct {
	JSON bool
}

func ActionNode(id string, a DetailAction) sdui.Node {
	n := sdui.WithAction(sdui.Button(id+"/"+a.Action, a.Label, a.Icon), a.Action)
	if a.Variant != "" {
		n = sdui.WithVariant(n, a.Variant)
	}
	if a.Shortcut != "" {
		n = sdui.WithShortcut(n, a.Shortcut)
	}
	if a.Confirm != "" {
		n = sdui.WithConfirm(n, a.Confirm)
	}
	return n
}

func ActionTarget(nodeID, action string) (string, bool) {
	return strings.CutSuffix(nodeID, "/"+action)
}

func RunAction(variant string) DetailAction {
	return DetailAction{Action: ActRun, Label: "Run now", Icon: "play.fill", Variant: variant, Shortcut: "cmd+r"}
}

func PauseAction(enabled bool) DetailAction {
	if enabled {
		return DetailAction{Action: ActPause, Label: "Pause", Icon: "pause", Variant: sdui.VariantSecondary, Shortcut: "p"}
	}
	return DetailAction{Action: ActResume, Label: "Resume", Icon: "play", Variant: sdui.VariantSecondary, Shortcut: "p"}
}

func DeleteAction(confirm string) DetailAction {
	return DetailAction{Action: ActDelete, Label: "Delete", Icon: "trash", Variant: sdui.VariantDanger, Shortcut: "cmd+delete", Confirm: confirm}
}

func StateBadges(enabled bool, history []Run) []sdui.HeaderBadge {
	badges := []sdui.HeaderBadge{{Text: "active"}}
	if !enabled {
		badges[0].Text = "paused"
	}
	if Failed(history) {
		badges = append(badges, sdui.HeaderBadge{Text: "failed", Tone: "danger"})
	}
	return badges
}

func Failed(history []Run) bool {
	r, ok := LastRun(history)
	return ok && r.Status == StatusError
}

func ListIcon(enabled bool, history []Run, icon string) (string, string) {
	switch {
	case Failed(history):
		return "xmark.circle", "danger"
	case !enabled:
		return "pause.circle", ""
	}
	return icon, ""
}

func Property(key, value, machine string) sdui.Node {
	row := []sdui.Node{sdui.MutedText(key), sdui.Text(value)}
	if machine != "" {
		row = append(row, sdui.MutedText(machine))
	}
	return sdui.HStack(row...)
}

func Properties(rows ...sdui.Node) sdui.Node {
	return sdui.Form(propsID, rows...)
}

func Until(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d < time.Minute:
		return "in less than a minute"
	case d < time.Hour:
		return fmt.Sprintf("in %d min", int(d.Round(time.Minute)/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %d h", int(d.Round(time.Hour)/time.Hour))
	}
	return fmt.Sprintf("in %d days", int(d.Round(24*time.Hour)/(24*time.Hour)))
}

func Names(text string, titles map[string]string) string {
	return idRe.ReplaceAllStringFunc(text, func(id string) string {
		if t := titles[id]; t != "" {
			return t
		}
		if strings.HasPrefix(id, "gr_") {
			return "a graph run"
		}
		return "a closed session"
	})
}

func Does(id, command string, args json.RawMessage, titles map[string]string, open Open) []sdui.Node {
	var fields map[string]any
	_ = json.Unmarshal(args, &fields)
	rows := []sdui.Node{Property("Does", doesLine(command, fields, titles), "")}
	if prompt := Names(promptOf(fields), titles); prompt != "" {
		rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.Card("", "", sdui.WithLines(sdui.Text(prompt), promptLines))))
	}
	toggle := DetailAction{Action: ActJSON, Label: "Show as JSON", Icon: "chevron.right", Variant: sdui.VariantGhost}
	if open.JSON {
		toggle.Label, toggle.Icon = "Hide JSON", "chevron.down"
	}
	rows = append(rows, sdui.HStack(sdui.MutedText(""), ActionNode(id, toggle)))
	if open.JSON {
		raw, _ := json.MarshalIndent(map[string]any{"command": command, "args": fields}, "", "  ")
		rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.MutedText(string(raw))))
	}
	return rows
}

func doesLine(command string, fields map[string]any, titles map[string]string) string {
	str := func(key string) string {
		s, _ := fields[key].(string)
		return strings.TrimSpace(s)
	}
	switch command {
	case agentNew:
		parts := []string{"Start an agent", cmp.Or(Capitalize(str("agent")), "Claude")}
		if branch := str("branch"); branch != "" {
			return strings.Join(append(parts, "new session in worktree "+branch), " · ")
		}
		return strings.Join(append(parts, "new session"), " · ")
	case PromptCommand:
		return "Send a prompt to " + Names(cmp.Or(str("session"), "a session"), titles)
	case agentEmit:
		return "Emit the event “" + str("name") + "”"
	}
	return "Run the command " + command
}

func promptOf(fields map[string]any) string {
	for _, key := range []string{"prompt", "text"} {
		if s, ok := fields[key].(string); ok && strings.TrimSpace(s) != "" {
			return graphHeader.ReplaceAllString(strings.TrimSpace(s), "")
		}
	}
	return ""
}

func Capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
