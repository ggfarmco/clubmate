package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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
	actStepOutput = "stepOutput"
	actOutput     = "output"
	actOpenTurn   = "openTurn"
	optOnce       = "once"
	optAlways     = "always"
	tailLines     = 12
)

var userOnly = []string{actApprove, actContinue, actTrust, actTrustConfirm}

type runExtras struct {
	pending     map[string]*pendingInfo
	selected    pick
	live        *liveOutput
	summarizing bool
	now         time.Time
	home        string
}

type pick struct {
	node  string
	move  int
	exact bool
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

func approvalCard(r *Run, n *runNeed, p *pendingInfo, home string) sdui.Node {
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
		where = "in " + homeShort(p.Dir, home) + mainSuffix(p) + " · not in the allow list · " + where
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

func (r *Run) shownMove(sel pick) (string, int) {
	if sel.exact && sel.move < len(r.Moves) && r.Moves[sel.move].Node == sel.node && r.closedTurn(sel.move) {
		return sel.node, sel.move
	}
	if n := r.node(sel.node); n != nil && !n.Step {
		if i := r.lastTurn(sel.node); i >= 0 {
			return sel.node, i
		}
	}
	return r.outputStep(sel.node)
}

func (r *Run) closedTurn(i int) bool {
	m := r.Moves[i]
	n := r.node(m.Node)
	return n != nil && !n.Step && m.Step == nil && !m.Open
}

func (r *Run) lastTurn(node string) int {
	for i := len(r.Moves) - 1; i >= 0; i-- {
		if r.Moves[i].Node == node && r.closedTurn(i) {
			return i
		}
	}
	return -1
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

func agentPanel(r *Run, node string, move int, summarizing bool) sdui.Node {
	m := r.Moves[move]
	role := roleOf(r, node)
	meta := []string{cmp.Or(r.moveOutcome(m), onDone)}
	if m.Turn > 0 {
		meta = append(meta, "turn "+strconv.Itoa(m.Turn))
	}
	if !m.Ended.IsZero() {
		meta = append(meta, humanDuration(m.Ended.Sub(m.At)))
	}
	var text sdui.Node
	var notes []sdui.Node
	switch {
	case m.Summary != "":
		text, notes = sdui.Text(m.Summary), []sdui.Node{sdui.MutedText("summary by Haiku")}
	case summarizing:
		text = sdui.MutedText("Summarizing…")
	case m.Reply != "":
		text, notes = sdui.WithLines(sdui.Text(m.Reply), 3), []sdui.Node{sdui.MutedText("the end of its reply")}
	default:
		text = sdui.MutedText("No summary of this turn.")
	}
	open := automation.ActionNode(r.ID, automation.DetailAction{Action: actOpenTurn, Label: "Open " + role, Icon: "sparkles", Variant: sdui.VariantSecondary})
	head := sdui.HStack(sdui.Text(role), sdui.MutedText(strings.Join(meta, " · ")))
	return sdui.Card("", statusIcon("done"), append([]sdui.Node{head, text, sdui.HStack(open)}, notes...)...)
}

func outputPanel(r *Run, node string, move int, live *liveOutput, summarizing bool, now time.Time) sdui.Node {
	st := r.Moves[move].Step
	role := roleOf(r, node)
	var meta []string
	if at, all := r.round(node, move); all > 1 {
		meta = append(meta, "round "+strconv.Itoa(at))
	}
	switch {
	case st.Status == stepRunning && !st.StartedAt.IsZero() && now.After(st.StartedAt):
		meta = append(meta, stepRunning+" · "+humanDuration(now.Sub(st.StartedAt)))
	case st.Status == stepRunning:
		meta = append(meta, stepRunning)
	case st.Outcome == onError:
		meta = append(meta, onError+" · "+cmp.Or(st.Reason, "failed"))
	default:
		meta = append(meta, st.Outcome)
	}
	if st.ExitCode != nil && st.Reason != "exit "+strconv.Itoa(*st.ExitCode) {
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
	tail, lines := strings.TrimRight(st.Tail, "\n"), st.Lines
	if live != nil {
		tail, lines = live.tail, live.lines
	}
	var body []sdui.Node
	switch {
	case st.Status == stepRunning && strings.TrimSpace(tail) == "":
		body = append(body, sdui.BlockText("Nothing printed yet."), sdui.HStack(full), sdui.MutedText(role+" is still running: the tail follows its output."))
	case strings.TrimSpace(tail) == "":
		body = append(body, sdui.BlockText("No output — the command printed nothing.\n"+st.Command+" in "+cmp.Or(st.Dir, "the run's folder")), sdui.HStack(full))
	default:
		split := strings.Split(tail, "\n")
		shown := split[max(0, len(split)-tailLines):]
		copyButton := sdui.WithCopy(sdui.WithVariant(sdui.Button(r.ID+"/copy", "Copy", "doc.on.doc"), sdui.VariantGhost), tail)
		body = append(body, sdui.BlockText(strings.Join(shown, "\n")), sdui.HStack(full, copyButton))
		var notes []string
		if r.Moves[move].Summary != "" {
			notes = append(notes, "summary by Haiku")
		}
		if all := max(lines, len(split)); len(shown) < all {
			notes = append(notes, fmt.Sprintf("last %d of %d lines", len(shown), all))
		}
		if st.Status == stepRunning {
			notes = append(notes, role+" is still running: the tail follows its output.")
		}
		if len(notes) > 0 {
			body = append(body, sdui.MutedText(strings.Join(notes, " · ")))
		}
	}
	top := []sdui.Node{head}
	switch {
	case r.Moves[move].Summary != "":
		top = append(top, sdui.Text(r.Moves[move].Summary))
	case summarizing && st.Status == stepFinished:
		top = append(top, sdui.MutedText("Summarizing…"))
	}
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

func (a *app) extras(r *Run) runExtras {
	a.mu.Lock()
	x := runExtras{pending: map[string]*pendingInfo{}, selected: a.stepSel[r.ID], now: a.now(), home: a.home}
	for k, p := range a.pending {
		if node, ok := strings.CutPrefix(k, r.ID+"/"); ok {
			x.pending[node] = pendingOf(p)
		}
	}
	_, move := r.shownMove(x.selected)
	x.summarizing = move >= 0 && a.summarizing[r.ID+"/"+strconv.Itoa(move)]
	a.mu.Unlock()
	if move >= 0 && r.Moves[move].Step != nil && r.Moves[move].Step.Status == stepRunning && r.Moves[move].Step.Output != "" {
		if live, ok := readLive(r.Moves[move].Step.Output); ok {
			x.live = &live
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

func (a *app) stepOutput(ctx context.Context, workspace, runID string) error {
	r, err := a.lookup(workspace, runID)
	if err != nil {
		return err
	}
	node := r.runningStep()
	if node == "" {
		return fmt.Errorf("graph run %q has no running step", runID)
	}
	a.mu.Lock()
	a.stepSel[runID] = pick{node: node}
	a.touch(workspace)
	a.mu.Unlock()
	a.flush(ctx)
	return a.fullOutput(ctx, workspace, runID, node)
}

func needDetail(r *Run, n *runNeed) string {
	if n.Kind == needStep {
		return "approve"
	}
	limit := r.Spec.Edges[n.Edge].MaxFires + r.Extra[n.Edge]
	return fmt.Sprintf("%d of %d", limit, limit)
}

func mainSuffix(p *pendingInfo) string {
	if p.Main != "" && filepath.Clean(p.Dir) == filepath.Clean(p.Main) {
		return " · the main checkout"
	}
	return ""
}

func homeShort(path, home string) string {
	if home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, filepath.Clean(home)); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
		return "~" + rest
	}
	return path
}
