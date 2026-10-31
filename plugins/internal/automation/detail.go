package automation

import (
	"encoding/json"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	ActPause  = "pause"
	ActResume = "resume"
	ActRun    = "run"
	ActDelete = "delete"
	ActReveal = "reveal"

	propsID = "props"
)

type DetailAction struct {
	Action   string
	Label    string
	Icon     string
	Variant  string
	Shortcut string
	Confirm  string
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
	var badges []sdui.HeaderBadge
	if !enabled {
		badges = append(badges, sdui.HeaderBadge{Text: "paused"})
	}
	if r, ok := LastRun(history); ok && r.Status == StatusError {
		badges = append(badges, sdui.HeaderBadge{Text: "failed", Tone: "danger"})
	}
	return badges
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

func Does(command string, args json.RawMessage) []sdui.Node {
	rows := []sdui.Node{Property("Does", command, "")}
	var fields map[string]any
	if json.Unmarshal(args, &fields) != nil || len(fields) == 0 {
		return rows
	}
	for _, key := range []string{"prompt", "text"} {
		if s, ok := fields[key].(string); ok && strings.TrimSpace(s) != "" {
			rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.Text(strings.TrimSpace(s))))
			delete(fields, key)
			break
		}
	}
	if len(fields) > 0 {
		rest, _ := json.Marshal(fields)
		rows = append(rows, sdui.HStack(sdui.MutedText(""), sdui.MutedText(string(rest))))
	}
	return rows
}
