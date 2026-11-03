package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	actApprove    = "approve"
	actContinue   = "continue"
	actFullOutput = "fullOutput"
	actOutput     = "output"
	optOnce       = "once"
	optAlways     = "always"
	tailLines     = 12
)

var userOnly = []string{actApprove, actContinue, actTrust, actTrustConfirm}

type runExtras struct {
	pending  map[string]*pendingInfo
	selected string
}

func nodeKind(g *Graph, id string) string {
	if g == nil {
		return ""
	}
	n := g.node(id)
	switch {
	case n == nil:
		return ""
	case !n.step():
		return sdui.KindAgent
	case g.check(id):
		return sdui.KindCheck
	}
	return sdui.KindAction
}

func stepSince(st *StepMove) string {
	switch {
	case st == nil:
	case st.Status == stepRunning && !st.StartedAt.IsZero():
		return st.StartedAt.UTC().Format(time.RFC3339)
	case st.Status == stepWaiting && !st.WaitingAt.IsZero():
		return st.WaitingAt.UTC().Format(time.RFC3339)
	}
	return ""
}

func edgeLabel(r *Run, i int, count int, held bool) string {
	if r.Spec == nil || i >= len(r.Spec.Edges) || r.Spec.Edges[i].MaxFires == 0 {
		return ""
	}
	limit := r.Spec.Edges[i].MaxFires + r.Extra[i]
	if held {
		return fmt.Sprintf("×%d/%d · waits for you", limit, limit)
	}
	return fmt.Sprintf("×%d/%d", min(count, limit), limit)
}

func (r *Run) heldEdge(i int) bool {
	for _, h := range r.Held {
		if h.Edge == i {
			return true
		}
	}
	return false
}

func capCard(r *Run, n *runNeed) sdui.Node {
	e := r.Spec.Edges[n.Edge]
	limit := e.MaxFires + r.Extra[n.Edge]
	text := fmt.Sprintf("%s → %s reached its limit of %s. Continue allows one more round; Stop run ends the run.", roleOf(r, e.From), roleOf(r, e.To), rounds(limit))
	return sdui.Card("warning", "exclamationmark.circle", sdui.WithTone(sdui.Text(text), "warning"))
}

func approvalCard(r *Run, n *runNeed, p *pendingInfo) sdui.Node {
	role := roleOf(r, n.Node)
	options := []sdui.PermissionOption{{ID: optOnce, Label: "Allow once", Kind: "allow_once", Shortcut: "opt+1"}}
	if p != nil && p.Rule != "" {
		options = append(options, sdui.PermissionOption{ID: optAlways, Label: "Always allow…", Kind: "allow_always", Shortcut: "opt+2", Rule: p.Rule, Scope: "in the main checkout (" + p.Main + ")"})
	}
	options = append(options, sdui.PermissionOption{ID: reasonRejectDecision, Label: "Reject", Kind: "reject_once", Shortcut: "opt+3"})
	card := sdui.WithAction(sdui.PermissionCard(r.ID+"/"+actApprove, n.Node, sdui.PermissionCardProps{
		Text:    "Run " + n.Command + "?",
		Title:   "Run a command?",
		Command: n.Command,
		Caption: role + " · a step of " + cmp.Or(r.Title, r.Graph),
		Options: options,
	}), actApprove)
	where := "Reject ends the step with error · rejected"
	if p != nil && p.Dir != "" {
		where = "in " + p.Dir + " · not in the allow list · " + where
	}
	return sdui.VStack(card, sdui.MutedText(where))
}

func (r *Run) outputStep(selected string) (string, int) {
	pick := func(node string) int {
		for i := len(r.Moves) - 1; i >= 0; i-- {
			m := r.Moves[i]
			if m.Node == node && m.Step != nil && m.Step.Status != stepWaiting && m.Step.Reason != reasonMerged {
				return i
			}
		}
		return -1
	}
	if selected != "" {
		if i := pick(selected); i >= 0 {
			return selected, i
		}
	}
	for i := len(r.Moves) - 1; i >= 0; i-- {
		if m := r.Moves[i]; m.Step != nil && m.Step.Status != stepWaiting && m.Step.Reason != reasonMerged {
			return m.Node, i
		}
	}
	return "", -1
}

func (r *Run) round(node string, move int) (int, int) {
	n, at := 0, 0
	for i, m := range r.Moves {
		if m.Node == node && m.Step != nil && m.Step.Reason != reasonMerged {
			n++
			if i == move {
				at = n
			}
		}
	}
	return at, n
}

func outputPanel(r *Run, node string, move int) sdui.Node {
	st := r.Moves[move].Step
	role := roleOf(r, node)
	var meta []string
	if at, all := r.round(node, move); all > 1 {
		meta = append(meta, "round "+strconv.Itoa(at))
	}
	switch {
	case st.Status == stepRunning:
		meta = append(meta, stepRunning)
	case st.Outcome == onError:
		meta = append(meta, onError+" · "+cmp.Or(st.Reason, "failed"))
	default:
		meta = append(meta, st.Outcome)
	}
	if st.ExitCode != nil {
		meta = append(meta, "exit "+strconv.Itoa(*st.ExitCode))
	}
	if st.Status == stepFinished {
		meta = append(meta, humanDuration(time.Duration(st.Duration)*time.Millisecond))
	}
	head := sdui.HStack(sdui.Text(role), sdui.MutedText(strings.Join(meta, " · ")))
	full := automation.ActionNode(r.ID, automation.DetailAction{Action: actFullOutput, Label: "Full output", Icon: "doc.text", Variant: sdui.VariantSecondary})
	if st.Output == "" {
		full = sdui.WithDisabled(full)
	}
	tail := strings.TrimRight(st.Tail, "\n")
	var body []sdui.Node
	switch {
	case st.Status == stepRunning:
		body = append(body, sdui.MutedText(role+" is still running: Full output shows what it printed so far."), sdui.HStack(full))
	case strings.TrimSpace(tail) == "":
		body = append(body, sdui.Text("No output — the command printed nothing."), sdui.MonoText(st.Command+" in "+cmp.Or(st.Dir, "the run's folder")), sdui.HStack(full))
	default:
		lines := strings.Split(tail, "\n")
		shown := lines[max(0, len(lines)-tailLines):]
		copyButton := sdui.WithCopy(sdui.WithVariant(sdui.Button(r.ID+"/copy", "Copy", "doc.on.doc"), sdui.VariantGhost), tail)
		row := []sdui.Node{full, copyButton}
		if len(shown) < len(lines) {
			row = append(row, sdui.MutedText(fmt.Sprintf("last %d of %d lines", len(shown), len(lines))))
		}
		body = append(body, sdui.MonoText(strings.Join(shown, "\n")), sdui.HStack(row...))
	}
	top := []sdui.Node{head}
	if len(st.CheckFiles) > 0 {
		top = append(top, sdui.WithTone(sdui.Text(fmt.Sprintf(checkFilesNote, strings.Join(st.CheckFiles, ", "))), "warning"))
	}
	glyph := stepGlyph(stepStatus(st))
	return sdui.Card("", statusIcon(glyph), append(top, body...)...)
}

func stepGlyph(status string) string {
	switch status {
	case stepWaiting, stepRunning:
		return status
	case onError:
		return "error"
	case notStarted:
		return "idle"
	case outPass, onDone:
		return "done"
	}
	return "fail"
}

func (a *app) extras(runID string) runExtras {
	a.mu.Lock()
	defer a.mu.Unlock()
	x := runExtras{pending: map[string]*pendingInfo{}, selected: a.stepSel[runID]}
	for k, p := range a.pending {
		if node, ok := strings.CutPrefix(k, runID+"/"); ok {
			x.pending[node] = pendingOf(p)
		}
	}
	return x
}

type permissionValue struct {
	Request string `json:"request"`
	Option  string `json:"option"`
}

type openArgs struct {
	Path  string `json:"path"`
	Focus bool   `json:"focus"`
}

func (a *app) approveFromSection(ctx context.Context, ev pluginsdk.UIEvent, runID string) error {
	var v permissionValue
	if err := json.Unmarshal(ev.Value, &v); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	args, err := json.Marshal(approveArgs{ID: runID, Node: v.Request, Decision: v.Option})
	if err != nil {
		return err
	}
	_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: "graphs.approve", WorkspaceID: ev.WorkspaceID, Args: args})
	return err
}

func (a *app) continueFromSection(ctx context.Context, workspace, runID string) error {
	r, err := a.lookup(workspace, runID)
	if err != nil {
		return err
	}
	n := r.need()
	if n == nil || n.Kind != needCap {
		return errors.New("the run is not waiting at a round cap")
	}
	args, err := json.Marshal(continueArgs{ID: runID, Edge: n.Edge})
	if err != nil {
		return err
	}
	_, err = a.command(ctx, nil, pluginsdk.CommandRun{ID: "graphs.continue", WorkspaceID: workspace, Args: args})
	return err
}

func (a *app) fullOutput(ctx context.Context, workspace, runID, node string) error {
	r, err := a.lookup(workspace, runID)
	if err != nil {
		return err
	}
	_, move := r.outputStep(node)
	if move < 0 || r.Moves[move].Step.Output == "" {
		return fmt.Errorf("graph run %q has no output for %q", runID, cmp.Or(node, "its steps"))
	}
	args, err := json.Marshal(openArgs{Path: r.Moves[move].Step.Output, Focus: true})
	if err != nil {
		return err
	}
	_, err = a.runCommand(ctx, automation.CommandRequest{ID: "editor.open", WorkspaceID: workspace, Args: args})
	return err
}

func needDetail(r *Run, n *runNeed) string {
	if n.Kind == needStep {
		return "approve"
	}
	limit := r.Spec.Edges[n.Edge].MaxFires + r.Extra[n.Edge]
	return fmt.Sprintf("%d of %d", limit, limit)
}
