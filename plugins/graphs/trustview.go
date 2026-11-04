package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/automation"
)

const (
	actTrust        = "trust"
	actTrustCancel  = "trustCancel"
	actTrustConfirm = "trustConfirm"
	dateLayout      = "Jan 2"
	shortHash       = 6
)

type trustView struct {
	untrusted  bool
	changed    []string
	hash       string
	last       *trustedSteps
	confirming bool
}

type fieldChange struct {
	field string
	was   string
	now   string
}

func stepDiff(was, now Node) []fieldChange {
	var out []fieldChange
	add := func(field, x, y string) {
		if x != y {
			out = append(out, fieldChange{field: field, was: x, now: y})
		}
	}
	add("command", commandLine(&was), commandLine(&now))
	add("folder", cmp.Or(was.In, "the run's folder"), cmp.Or(now.In, "the run's folder"))
	add("timeout", roundDuration(was.timeout()), roundDuration(now.timeout()))
	add("exits", exitsLine(was.Exits), exitsLine(now.Exits))
	return out
}

func commandLine(n *Node) string {
	if n.Run != "" {
		return n.Run
	}
	if len(n.Args) == 0 {
		return n.Command
	}
	return n.Command + " " + string(n.Args)
}

func exitsLine(exits map[string]string) string {
	keys := slices.Sorted(maps.Keys(exits))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" → "+exits[k])
	}
	return strings.Join(parts, ", ")
}

func (tv trustView) lastStep(id string) (Node, bool) {
	if tv.last == nil {
		return Node{}, false
	}
	i := slices.IndexFunc(tv.last.Steps, func(n Node) bool { return n.ID == id })
	if i < 0 {
		return Node{}, false
	}
	return tv.last.Steps[i], true
}

func trustNotice(tv trustView) sdui.Node {
	text := "Its steps run shell commands as you: review them, then Trust to run it."
	if tv.last != nil {
		text = "The file changed after you trusted it (" + tv.last.At.Local().Format(dateLayout) + "). Its steps run shell commands as you: review what changed, then Trust to run it again."
	}
	return sdui.Card("warning", "exclamationmark.circle", sdui.WithTone(sdui.Text(text), "warning"))
}

func trustDefinition(definition string, tv trustView) string {
	if tv.last == nil || tv.hash == "" {
		return definition + " · never trusted"
	}
	return definition + " · changed · trusted version " + tv.last.Hash[:min(shortHash, len(tv.last.Hash))] + ", now " + tv.hash[:min(shortHash, len(tv.hash))]
}

func changedCard(spec *Graph, tv trustView) (sdui.Node, bool) {
	if tv.last == nil || len(tv.changed) == 0 {
		return sdui.Node{}, false
	}
	var rows []sdui.Node
	steps := 0
	for _, id := range tv.changed {
		if id == "edges" {
			rows = append(rows, sdui.MutedText("Edges · the routing changed: an edge or an end was added, removed or rewired"))
			continue
		}
		steps++
		n := spec.node(id)
		was, ok := tv.lastStep(id)
		if !ok {
			rows = append(rows, sdui.MutedText(n.label()+" · new step"), sdui.WithTone(sdui.MonoText("+ "+commandLine(n)), "success"))
			continue
		}
		for _, c := range stepDiff(was, *n) {
			rows = append(rows, sdui.MutedText(n.label()+" · "+c.field), sdui.WithTone(sdui.MonoText("− "+c.was), "danger"), sdui.WithTone(sdui.MonoText("+ "+c.now), "success"))
		}
	}
	what := strconv.Itoa(steps) + " steps"
	if steps == 1 {
		what = "1 step"
	}
	head := sdui.HStack(sdui.Text("What changed since you trusted it"), sdui.MutedText(what+" · the rest is the same"))
	foot := sdui.MutedText("Steps run in the run's folder with your shell environment. A command outside the allow list still asks before it runs.")
	return sdui.Card("", "terminal", append(append([]sdui.Node{head}, rows...), foot)...), true
}

func trustConfirm(id, title string, spec *Graph, tv trustView) sdui.Node {
	intro := "Clubmate runs these commands as you when the graph reaches them."
	if tv.last != nil {
		intro += " Changed since you trusted it on " + tv.last.At.Local().Format(dateLayout) + ":"
	}
	children := []sdui.Node{sdui.Text(intro)}
	for _, n := range spec.Nodes {
		if !n.step() {
			continue
		}
		kind := nodeKind(spec, n.ID)
		row := []sdui.Node{sdui.Text(n.label()), sdui.MutedText(kind)}
		tone := ""
		var notes []string
		if tv.last != nil && slices.Contains(tv.changed, n.ID) {
			tone = "warning"
			row = append(row, sdui.Badge(tagChanged, "warning"))
			if was, ok := tv.lastStep(n.ID); ok && commandLine(&was) != commandLine(&n) {
				notes = append(notes, "was: "+commandLine(&was))
			}
		}
		if n.Timeout > 0 {
			notes = append(notes, "timeout "+roundDuration(time.Duration(n.Timeout)*time.Second))
		}
		card := []sdui.Node{sdui.HStack(row...), sdui.MonoText("$ " + commandLine(&n))}
		if len(notes) > 0 {
			card = append(card, sdui.MutedText(strings.Join(notes, " · ")))
		}
		children = append(children, sdui.Card(tone, "", card...))
	}
	children = append(children,
		sdui.MutedText("A command outside the allow list still asks before it runs. Trust lasts until the file changes again."),
		sdui.HStack(
			automation.ActionNode(id, automation.DetailAction{Action: actTrustCancel, Label: "Cancel", Variant: sdui.VariantSecondary}),
			automation.ActionNode(id, automation.DetailAction{Action: actTrustConfirm, Label: "Trust", Variant: sdui.VariantPrimary, Shortcut: "cmd+return"}),
		),
	)
	return sdui.Sheet(id+"/sheet", sdui.SheetProps{Title: "Trust " + title + "?", Icon: "checkmark", Tone: "warning", Cancel: actTrustCancel}, children...)
}

func (a *app) trustFor(ctx context.Context, workspace string, dirs []string, g graphFile, spec *Graph) trustView {
	if spec == nil {
		return trustView{}
	}
	root := a.rootOf(ctx, workspace, trustDir(dirs, g.Name))
	untrusted, changed, err := a.trustState(ctx, root, spec)
	if err != nil || !untrusted {
		logError(err)
		return trustView{}
	}
	tv := trustView{untrusted: true, changed: changed, hash: stepsHash(spec)}
	if last, ok, err := a.lastTrusted(ctx, root(), g.Name); err == nil && ok {
		tv.last = &last
	} else {
		logError(err)
	}
	a.mu.Lock()
	tv.confirming = a.trusting[workspace] == g.Name
	a.mu.Unlock()
	return tv
}

func (a *app) trustEvent(ctx context.Context, ev pluginsdk.UIEvent, graph string) error {
	a.mu.Lock()
	switch ev.Action {
	case actTrust:
		a.trusting[ev.WorkspaceID] = graph
	case actTrustCancel:
		delete(a.trusting, ev.WorkspaceID)
	}
	a.touch(ev.WorkspaceID)
	a.mu.Unlock()
	if ev.Action != actTrustConfirm {
		a.flush(ctx)
		return nil
	}
	a.mu.Lock()
	hash := a.shownHash[ev.WorkspaceID+"/"+graph]
	confirming := a.trusting[ev.WorkspaceID] == graph
	delete(a.shownHash, ev.WorkspaceID+"/"+graph)
	a.mu.Unlock()
	if !confirming || hash == "" {
		return fmt.Errorf("trust %s: look at its steps again", graph)
	}
	args, err := json.Marshal(trustArgs{Graph: graph, Hash: hash})
	if err != nil {
		return err
	}
	if _, err := a.command(ctx, nil, pluginsdk.CommandRun{ID: "graphs.trust", WorkspaceID: ev.WorkspaceID, Args: args}); err != nil {
		return fmt.Errorf("trust %s: %w", graph, err)
	}
	a.mu.Lock()
	delete(a.trusting, ev.WorkspaceID)
	a.touch(ev.WorkspaceID)
	a.mu.Unlock()
	a.flush(ctx)
	return nil
}

func roundDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return strconv.Itoa(int(d.Hours())) + " h"
	case d >= time.Minute && d%time.Minute == 0:
		return strconv.Itoa(int(d.Minutes())) + " min"
	}
	return humanDuration(d)
}
