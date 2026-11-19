package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/toolgate"
)

const (
	gateIntoLike         = "like"
	gateSheetNote        = "Holds agents only; what you do yourself is never gated."
	gateNoRepo           = "No folder of this workspace is a git repo."
	gateRepoHint         = "%s · default branch %s"
	gateHoldsLine        = "An agent's landing waits for the graph."
	gateMatchTaken       = "%s is there already"
	gateBaseLine         = "Only a branch that Clubmate made from a parent's worktree has a parent's branch: its landing into that branch waits for the graph. A branch made with git has none and lands as before; to hold every landing into a feature branch, use any branch."
	gateAnyLine          = "Every landing, also a child's merge into its parent's branch."
	gateLikeLine         = "A pattern: * stays within one / segment, ** crosses them, so release/* holds release/1.0 and release/** also release/1.0/hotfix."
	gateLikeKeyword      = "default, base and any are in the menu: pick it from there"
	gateLikeHint         = "release/*"
	gateUndecided        = "%s can't decide a landing: no node reports approved and changes"
	gateUnreadableGraphs = "%s can't be read: open them in Graphs to see why"
	gateSentence         = "Holds an agent's %s in %s; %s decides."
	gateToolIcon         = "terminal"
	gatePointToolCall    = "Tool call"
	gateHoldsToolLine    = "An agent's tool call. The agent waits inside it while the graph runs."
	gateHoldsTool        = "an agent's tool call"
	gateCallCustom       = "custom"
	gateCallCustomLabel  = "Custom…"
	gateToolTextHint     = "make deploy"
	gateToolCallWord     = "%s call"
	gateUnhookedMany     = "%d sessions mid-turn pick this gate up when their turn ends"
	gateUnhookedOne      = "1 session mid-turn picks this gate up when its turn ends"
	gateUndecidedTool    = "%s can't decide a tool call: no node reports approved and changes"
	gatePausedLine       = "a paused gate holds nothing new; what it holds stays held"
	gatePausedToolLine   = "a paused gate takes no new call; what it holds stays held"
)

var gateCallPresets = []string{"git push", "gh pr create"}

type gateDraft struct {
	Edit      string
	Name      string
	Point     string
	Repo      string
	Into      string
	Like      bool
	Sessions  []string
	WroteMode string
	WroteLike string
	Graph     string
	Rounds    int
	Paused    bool
	Call      string
	Tool      string
	How       string
	Text      string
	Unhooked  int
	Err       string
	Repos     []gateRepo
	Gates     []gateConfig
	Choices   []graphs.GateGraphInfo
}

func (d gateDraft) primary() string {
	if d.Edit != "" {
		return d.Repo
	}
	i := slices.IndexFunc(d.Repos, func(r gateRepo) bool { return r.Path == d.Repo })
	if i < 0 {
		return ""
	}
	return d.Repos[i].Primary
}

func (d gateDraft) branch() string {
	i := slices.IndexFunc(d.Repos, func(r gateRepo) bool { return r.Path == d.Repo || r.Primary == d.Repo })
	if i < 0 {
		return ""
	}
	return d.Repos[i].Branch
}

func (d gateDraft) taken() (gateConfig, bool) {
	primary := d.primary()
	i := slices.IndexFunc(d.Gates, func(g gateConfig) bool {
		return g.ID != d.Edit && g.Repo == primary && g.Point == gatePointLand && g.Into == d.Into
	})
	if i < 0 {
		return gateConfig{}, false
	}
	return d.Gates[i], true
}

func (d gateDraft) choice() (graphs.GateGraphInfo, bool) {
	i := slices.IndexFunc(d.Choices, func(c graphs.GateGraphInfo) bool { return c.Name == d.Graph })
	if i < 0 {
		return graphs.GateGraphInfo{}, false
	}
	return d.Choices[i], d.Choices[i].Decides
}

func (d gateDraft) freeInto() string {
	for _, into := range []string{gateIntoDefault, gateIntoBase, gateIntoAny} {
		d.Into = into
		if _, taken := d.taken(); !taken {
			return into
		}
	}
	return gateIntoDefault
}

func gateSheetView(d gateDraft) sdui.Node {
	rows := []sdui.Node{}
	title, save := "New gate", "Create gate"
	disabled := false
	if d.Edit == "" {
		repo, missing := gateRepoField(d)
		rows = append(rows, repo)
		disabled = missing
	} else {
		title, save = "Edit gate · "+d.Name, "Save"
		rows = append(rows, automation.Property("Repo", filepath.Base(d.Repo), tildePath(d.Repo)))
	}
	rows = append(rows, gateHoldsField(d)...)
	match := gateIntoField
	switch d.Point {
	case gatePointTurn:
		match = gateDoneFields
	case gatePointTool:
		match = gateToolFields
	}
	fields, ok := match(d)
	rows = append(rows, fields...)
	disabled = disabled || !ok
	graph, ok := gateGraphField(d)
	rows = append(rows, graph...)
	disabled = disabled || !ok
	rows = append(rows, sdui.HStack(sdui.MutedText("Rounds"), sdui.WithAction(sdui.Select(gateSheetID+"/rounds", roundOptions(d.Rounds), strconv.Itoa(d.Rounds)), actGateRounds), sdui.MutedText(gateRoundsWords)))
	if d.Edit != "" {
		paused := gatePausedLine
		if d.Point == gatePointTool {
			paused = gatePausedToolLine
		}
		rows = append(rows, sdui.HStack(sdui.MutedText("Paused"), sdui.WithAction(sdui.Toggle(gateSheetID+"/paused", d.Paused), actGatePaused), sdui.MutedText(paused)))
	}
	if !heldPoint(d.Point) {
		rows = append(rows, automation.Property("Docs only", gateDocsOnly, ""))
	}
	cancel := sdui.WithVariant(sdui.WithAction(sdui.Button(gateSheetID+"/cancel", "Cancel", ""), actGateCancel), sdui.VariantSecondary)
	ok2 := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(gateSheetID+"/save", save, ""), actGateSave), sdui.VariantPrimary), "cmd+return")
	if disabled {
		ok2 = sdui.WithDisabled(ok2)
	}
	note, tone := gateSheetNote, ""
	if !disabled || d.Point == gatePointTurn && !validKinds(d.Sessions) {
		note = gateSentenceOf(d)
	}
	if d.Err != "" {
		note, tone = d.Err, "danger"
	}
	return sdui.Sheet(gateSheetID, sdui.SheetProps{Title: title, Icon: pointIcon(d.Point), Cancel: actGateCancel, Note: note, NoteTone: tone},
		automation.Properties(rows...),
		sdui.HStack(cancel, ok2),
	)
}

func gateRepoField(d gateDraft) (sdui.Node, bool) {
	if len(d.Repos) == 0 {
		return automation.Property("Repo", gateNoRepo, ""), true
	}
	options := make([]sdui.Option, 0, len(d.Repos))
	for _, r := range d.Repos {
		label := filepath.Base(r.Primary)
		if n := countGates(d.Gates, r.Primary); n > 0 {
			label += " · " + counted(n, "gate")
		}
		options = append(options, sdui.Option{Value: r.Path, Label: label})
	}
	field := sdui.WithAction(sdui.Select(gateSheetID+"/repo", options, d.Repo), actGateRepo)
	primary := d.primary()
	hint := fmt.Sprintf(gateRepoHint, tildePath(primary), cmp.Or(d.branch(), "unknown"))
	return sdui.HStack(sdui.MutedText("Repo"), field, sdui.MutedText(hint)), primary == ""
}

func countGates(gates []gateConfig, primary string) int {
	n := 0
	for _, g := range gates {
		if g.Repo == primary {
			n++
		}
	}
	return n
}

func gateIntoField(d gateDraft) ([]sdui.Node, bool) {
	value := d.Into
	if d.Like {
		value = gateIntoLike
	}
	options := []sdui.Option{
		{Value: gateIntoDefault, Label: "the default branch (" + cmp.Or(d.branch(), "main") + ")"},
		{Value: gateIntoBase, Label: "the parent's branch"},
		{Value: gateIntoAny, Label: "any branch"},
		{Value: gateIntoLike, Label: "Branches like…"},
	}
	field := sdui.WithAction(sdui.Select(gateSheetID+"/into", options, value), actGateInto)
	ok := !d.Like || strings.TrimSpace(d.Into) != ""
	if d.Like && slices.Contains([]string{gateIntoDefault, gateIntoBase, gateIntoAny}, d.Into) {
		field, ok = sdui.WithError(field, gateLikeKeyword), false
	}
	if g, taken := d.taken(); taken && ok {
		field = sdui.WithError(field, fmt.Sprintf(gateMatchTaken, gateNameOf(g.Into, d.branch())))
		ok = false
	}
	row := []sdui.Node{sdui.MutedText("Into"), field}
	if d.Like {
		row = append(row, sdui.WithAction(sdui.WithLive(sdui.TextField(gateSheetID+"/like", d.Into, gateLikeHint)), actGateLike))
	}
	out := []sdui.Node{sdui.HStack(row...)}
	switch {
	case d.Like:
		out = append(out, sdui.MutedText(gateLikeLine))
	case d.Into == gateIntoBase:
		out = append(out, sdui.MutedText(gateBaseLine))
	case d.Into == gateIntoAny:
		out = append(out, sdui.MutedText(gateAnyLine))
	}
	return out, ok
}

func gateGraphField(d gateDraft) ([]sdui.Node, bool) {
	var options []sdui.Option
	var undecided []string
	var unreadable []string
	for _, c := range gateChoiceOrder(d.Choices) {
		if c.Error != "" {
			unreadable = append(unreadable, c.Name)
			continue
		}
		if !c.Decides {
			undecided = append(undecided, cmp.Or(c.Title, c.Name))
			continue
		}
		options = append(options, sdui.Option{Value: c.Name, Label: cmp.Or(c.Title, c.Name) + " · " + originWords(c.From, d.primary())})
	}
	out := []sdui.Node{sdui.HStack(sdui.MutedText("Decided by"), sdui.WithAction(sdui.Select(gateSheetID+"/graph", options, d.Graph), actGatePick))}
	chosen, ok := d.choice()
	if ok {
		out = append(out, sdui.MutedText(chainWords(chosen)))
	}
	if len(undecided) > 0 {
		words := gateUndecided
		switch d.Point {
		case gatePointTurn:
			words = gateUndecidedDone
		case gatePointTool:
			words = gateUndecidedTool
		}
		out = append(out, sdui.MutedText(fmt.Sprintf(words, strings.Join(undecided, ", "))))
	}
	if len(unreadable) > 0 {
		out = append(out, sdui.MutedText(fmt.Sprintf(gateUnreadableGraphs, strings.Join(unreadable, ", "))))
	}
	return out, ok
}

func gateChoiceOrder(choices []graphs.GateGraphInfo) []graphs.GateGraphInfo {
	rank := map[string]int{graphs.FromBuiltin: 0, graphs.FromUser: 1, graphs.FromRepo: 2}
	out := slices.Clone(choices)
	slices.SortStableFunc(out, func(x, y graphs.GateGraphInfo) int {
		if c := cmp.Compare(rank[x.From], rank[y.From]); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(cmp.Or(x.Title, x.Name)), strings.ToLower(cmp.Or(y.Title, y.Name)))
	})
	return out
}

func chainWords(g graphs.GateGraphInfo) string {
	parts := make([]string, 0, len(g.Nodes)+1)
	for _, n := range g.Nodes {
		parts = append(parts, n.Title)
	}
	return strings.Join(append(parts, "approved / changes"), " → ")
}

func gateSentenceOf(d gateDraft) string {
	chosen, _ := d.choice()
	switch d.Point {
	case gatePointTurn:
		return doneSentenceOf(d, cmp.Or(chosen.Title, chosen.Name), filepath.Base(d.primary()))
	case gatePointTool:
		g, _ := d.toolConfig()
		return fmt.Sprintf(gateSentence, toolSentenceWhat(g), filepath.Base(d.primary()), cmp.Or(chosen.Title, chosen.Name))
	}
	what := "l" + strings.TrimPrefix(gateNameOf(d.Into, d.branch()), "L")
	return fmt.Sprintf(gateSentence, what, filepath.Base(d.primary()), cmp.Or(chosen.Title, chosen.Name))
}

func (d gateDraft) call() string {
	return cmp.Or(d.Call, gateCallPresets[0])
}

func (d gateDraft) customTool() string {
	return cmp.Or(strings.TrimSpace(d.Tool), toolgate.Bash)
}

func (d gateDraft) customHow() string {
	tool := d.customTool()
	return cmp.Or(toolHowAfter(tool, d.How), toolHowDefault(tool))
}

func (d gateDraft) toolConfig() (gateConfig, error) {
	if d.call() != gateCallCustom {
		return toolMatch(toolgate.Bash, toolgate.HowCommand, d.call())
	}
	text := d.Text
	if d.customHow() == toolgate.HowContains {
		text = strings.TrimSpace(text)
	}
	return toolMatch(d.Tool, d.customHow(), text)
}

func (d gateDraft) takenTool() (gateConfig, bool) {
	want, err := d.toolConfig()
	if err != nil {
		return gateConfig{}, false
	}
	primary := d.primary()
	i := slices.IndexFunc(d.Gates, func(g gateConfig) bool {
		return g.ID != d.Edit && g.Repo == primary && g.Point == gatePointTool && sameTool(g, want)
	})
	if i < 0 {
		return gateConfig{}, false
	}
	return d.Gates[i], true
}

func (d gateDraft) freeCall() string {
	for _, c := range gateCallPresets {
		d.Call = c
		if _, taken := d.takenTool(); !taken {
			return c
		}
	}
	return gateCallPresets[0]
}

func gateCallOf(g gateConfig) string {
	if g.Tool == toolgate.Bash && g.How == toolgate.HowCommand && slices.Contains(gateCallPresets, g.Text) {
		return g.Text
	}
	return gateCallCustom
}

func toolSentenceWhat(g gateConfig) string {
	if g.Tool == toolgate.Bash {
		return toolMatchName(g)
	}
	return fmt.Sprintf(gateToolCallWord, g.Tool)
}

func gateToolFields(d gateDraft) ([]sdui.Node, bool) {
	items := make([]sdui.Item, 0, len(gateCallPresets)+1)
	for _, c := range gateCallPresets {
		items = append(items, sdui.Item{ID: c, Label: c})
	}
	items = append(items, sdui.Item{ID: gateCallCustom, Label: gateCallCustomLabel})
	out := []sdui.Node{sdui.HStack(sdui.MutedText("Call"), sdui.WithAction(sdui.Tabs(gateSheetID+"/call", items, d.call()), actGateCall))}
	g, err := d.toolConfig()
	ok := err == nil
	taken, isTaken := d.takenTool()
	if d.call() == gateCallCustom {
		text := sdui.WithAction(sdui.WithLive(sdui.TextField(gateSheetID+"/text", d.Text, gateToolTextHint)), actGateText)
		switch {
		case err != nil && (strings.TrimSpace(d.Text) != "" || d.customTool() != toolgate.Bash):
			text = sdui.WithError(text, err.Error())
		case isTaken:
			text = sdui.WithError(text, fmt.Sprintf(gateMatchTaken, toolGateName(taken)))
		}
		tool := sdui.WithAction(sdui.WithLive(sdui.TextField(gateSheetID+"/tool", d.Tool, toolgate.Bash)), actGateTool)
		how := sdui.WithAction(sdui.WithSegmented(sdui.Tabs(gateSheetID+"/how", gateHowItems(d), d.customHow())), actGateHow)
		out = append(out, sdui.HStack(sdui.MutedText(""), how), sdui.HStack(sdui.MutedText(""), tool, text))
	}
	if ok {
		out = append(out, sdui.MutedText("= "+toolMatchWords(g)))
	}
	if isTaken {
		ok = false
		if d.call() != gateCallCustom {
			out = append(out, sdui.WithTone(sdui.Text(fmt.Sprintf(gateMatchTaken, toolGateName(taken))), toneWarning))
		}
	}
	switch {
	case d.Unhooked == 1:
		out = append(out, sdui.MutedText(gateUnhookedOne))
	case d.Unhooked > 1:
		out = append(out, sdui.MutedText(fmt.Sprintf(gateUnhookedMany, d.Unhooked)))
	}
	return out, ok
}

func gateHowItems(d gateDraft) []sdui.Item {
	items := []sdui.Item{
		{ID: toolgate.HowStarts, Label: "starts with"},
		{ID: toolgate.HowContains, Label: "contains"},
		{ID: toolgate.HowRegex, Label: "regex"},
	}
	if d.customTool() != toolgate.Bash {
		return items[1:]
	}
	if d.customHow() == toolgate.HowCommand {
		items = append([]sdui.Item{{ID: toolgate.HowCommand, Label: "command"}}, items...)
	}
	return items
}
