package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	pickSession    = "Start an agent session with + New"
	noCodeSession  = "No agent session in this worktree"
	codeSelectNode = "code/session"
	codeNewNode    = "code/new"
	maxCodeOptions = 50
	commandRunes   = 300
	installingNote = "Installing %s adapter…"
	queuedNote     = "Queued, sent when the turn ends"
	heldNote       = "Queued, not sent"
	queuePrefix    = "queue/"
	mergeConfirm   = "Ask the agent to run the tests, squash its branch into main as one commit and remove its worktree and branch?"

	advisorHelp = "Advisor model for new sessions in this worktree"
	finishHelp  = "What the agent does once the task is done"
	afkHelp     = "Away mode: the agent works without asking you and reports when done; child sessions follow it"
)

var (
	agentLabels    = map[string]string{agentClaude: "Claude"}
	advisorChoices = []advisorChoice{
		{label: "Default"},
		{value: "fable", label: "Fable"},
		{value: "opus", label: "Opus"},
		{value: "sonnet", label: "Sonnet"},
	}
	afkChoices = []advisorChoice{
		{value: afkOff, label: "Off"},
		{value: afkOn, label: "On"},
	}
	finishChoices = []advisorChoice{
		{value: finishAsk, label: "Ask"},
		{value: finishMerge, label: "Merge to main"},
	}
)

type screen struct {
	View        transcriptView
	Pending     map[string]bool
	Window      int
	Expanded    map[string]bool
	Attachments []pendingAttachment
	Queue       []queuedPrompt
	Composer    string
	Steers      bool
	NeedsAuth   bool
	Continue    bool
	SignIn      string
	NodeProblem string
	Installing  bool
	Changes     bool
	Advisor     string
	Parent      string
	FromTitles  map[string]string
}

type pendingAttachment struct {
	ID string
	attachment
}

type advisorChoice struct {
	value string
	label string
}

func mainView(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	children := []sdui.Node{header(sc)}
	if sc.Installing {
		children = append(children, sdui.MutedText(fmt.Sprintf(installingNote, agentLabel(rec.Agent))))
	}
	if sc.NodeProblem != "" && rec.Status == statusError {
		return sdui.VStack(append(children, sdui.Empty(sc.NodeProblem))...)
	}
	if sc.SignIn != "" {
		children = append(children, sdui.MutedText("Sign in to "+agentLabel(rec.Agent)+" in the terminal below; the session retries when it exits."), sdui.Surface(sc.SignIn))
		return sdui.VStack(children...)
	}
	children = append(children, transcript(sc))
	if rec.Closed && rec.HandoffTo != "" {
		next := sdui.WithAction(sdui.Button(sid+"/"+handoffToNode, "Open new session", "arrow.turn.down.right"), actOpenHandoff)
		children = append(children, sdui.HStack(sdui.MutedText("The session was handed off to a new session."), next))
		return sdui.VStack(children...)
	}
	if rec.Closed {
		reopen := sdui.WithAction(sdui.Button(sid+"/reopen", "Reopen", "arrow.clockwise"), actRetry)
		if sc.Continue {
			reopen = continueButton(sid)
		}
		children = append(children, sdui.HStack(sdui.MutedText("The session is closed; its transcript stays readable."), reopen))
		return sdui.VStack(children...)
	}
	if len(sc.Queue) > 0 {
		children = append(children, queueView(sc))
	}
	return sdui.VStack(append(children, composer(sc))...)
}

func codeView(recs []record, bound string, sc *screen) sdui.Node {
	var opts []sdui.Option
	for _, rec := range recs {
		if len(opts) < maxCodeOptions && (!rec.Closed || rec.ID == bound) {
			opts = append(opts, sdui.Option{Value: rec.ID, Label: cmp.Or(rec.Title, "New session")})
		}
	}
	if sc != nil && !slices.ContainsFunc(opts, func(o sdui.Option) bool { return o.Value == bound }) {
		opts = append(opts, sdui.Option{Value: bound, Label: cmp.Or(sc.View.Session.Title, "New session")})
	}
	var bar []sdui.Node
	if len(opts) > 0 {
		bar = append(bar, sdui.WithAction(sdui.Select(codeSelectNode, opts, bound), actCodeSelect))
	}
	bar = append(bar, sdui.WithAction(sdui.Button(codeNewNode, "New", "plus"), actCodeNew))
	body := sdui.Empty(noCodeSession)
	if sc != nil {
		body = mainView(*sc)
	}
	return sdui.VStack(sdui.HStack(bar...), body)
}

func header(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	meta := []string{agentLabel(rec.Agent), "⑂ " + filepath.Base(rec.Worktree)}
	if name := modeName(sc.View.Modes, rec.Mode); name != "" {
		meta = append(meta, name)
	}
	if name := configName(sc.View.Configs, acp.CategoryModel); name != "" {
		meta = append(meta, name)
	}
	if name := effortName(sc.View.Configs); name != "" {
		meta = append(meta, name)
	}
	row := []sdui.Node{
		{Type: "text", Props: map[string]any{"text": cmp.Or(rec.Title, "New session"), "style": "title"}},
		sdui.MutedText(strings.Join(meta, " · ")),
	}
	if sc.Parent != "" {
		row = append(row, sdui.WithAction(sdui.Button(sid+"/parent", "from "+sc.Parent, "arrow.turn.up.left"), actSelectParent))
	}
	if rec.HandoffFrom != "" {
		row = append(row, sdui.WithAction(sdui.Button(sid+"/"+handoffFromNode, "Earlier session", "clock.arrow.circlepath"), actOpenHandoff))
	}
	if sc.Changes {
		row = append(row, sdui.WithAction(sdui.Button(sid+"/review", "Review changes", "doc.text.magnifyingglass"), actReview))
	}
	if mergeable(rec, sc.Changes) {
		row = append(row, sdui.WithConfirm(sdui.WithAction(sdui.Button(sid+"/merge", "Merge to main", "arrow.triangle.merge"), actMerge), mergeConfirm))
	}
	if turnActive(rec) {
		row = append(row, sdui.WithAction(sdui.Button(sid+"/stop", "Stop", "stop.fill"), actStop))
	}
	children := []sdui.Node{sdui.HStack(row...)}
	if rec.Status != statusError {
		return sdui.VStack(children...)
	}
	problem := []sdui.Node{sdui.MutedText(cmp.Or(firstLine(rec.Error), "The agent failed"))}
	switch {
	case sc.NeedsAuth:
		problem = append(problem, sdui.WithAction(sdui.Button(sid+"/signin", "Sign in", "person.badge.key"), actSignIn))
	case sc.Continue:
		problem = append(problem, continueButton(sid))
	default:
		problem = append(problem, sdui.WithAction(sdui.Button(sid+"/retry", "Retry", "arrow.clockwise"), actRetry))
	}
	return sdui.VStack(append(children, sdui.HStack(problem...))...)
}

func continueButton(sid string) sdui.Node {
	return sdui.WithAction(sdui.Button(sid+"/continue", "Hand off to a new session", "arrow.turn.down.right"), actContinue)
}

func queueView(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	head := []sdui.Node{sdui.MutedText(queuedNote)}
	if !turnActive(rec) {
		head = []sdui.Node{sdui.MutedText(heldNote), sdui.WithAction(sdui.Button(sid+"/"+queuePrefix+"send", "Send queued", "paperplane"), actSendQueued)}
	}
	rows := []sdui.Node{sdui.HStack(head...)}
	for _, q := range sc.Queue {
		text := strings.TrimSpace(q.Text)
		if n := len(q.Attachments); n > 0 {
			text = strings.Join(nonEmpty(text, strconv.Itoa(n)+" attached"), " · ")
		}
		rows = append(rows, sdui.HStack(sdui.MutedText(text), sdui.WithAction(sdui.Button(sid+"/"+queuePrefix+q.ID, "Remove", "xmark"), actUnqueue)))
	}
	return sdui.VStack(rows...)
}

func composer(sc screen) sdui.Node {
	v := sc.View
	rec := v.Session
	modes := make([]sdui.Option, 0, len(v.Modes))
	for _, m := range v.Modes {
		modes = append(modes, sdui.Option{Value: m.ID, Label: cmp.Or(m.Name, m.ID)})
	}
	commands := make([]sdui.Command, 0, len(v.Commands))
	for _, c := range v.Commands {
		var input struct {
			Hint string `json:"hint"`
		}
		_ = json.Unmarshal(c.Input, &input)
		commands = append(commands, sdui.Command{Name: c.Name, Description: clipRunes(c.Description, commandRunes), Hint: input.Hint})
	}
	attachments := make([]sdui.Attachment, 0, len(sc.Attachments))
	for _, a := range sc.Attachments {
		attachments = append(attachments, sdui.Attachment{ID: a.ID, Kind: a.Kind, Path: relPath(a.Path, rec.Worktree), StartLine: a.StartLine, EndLine: a.EndLine})
	}
	placeholder := "Ask " + agentLabel(rec.Agent) + ", @ to attach a file, / for commands…"
	if len(v.Turns) > 0 {
		placeholder = "Ask a follow-up, or type / for commands…"
	}
	return sdui.Composer(sc.Composer, sdui.ComposerProps{
		Placeholder: placeholder,
		Agent:       agentLabel(rec.Agent),
		Modes:       modes,
		Mode:        rec.Mode,
		Pickers:     pickers(sc),
		Commands:    commands,
		Attach:      []string{"file", "selection"},
		Attachments: attachments,
		Running:     turnActive(rec),
		Steers:      sc.Steers,
	})
}

func pickers(sc screen) []sdui.Picker {
	var out []sdui.Picker
	for _, o := range sc.View.Configs {
		if !pickable(o) {
			continue
		}
		choices := o.Choices()
		options := make([]sdui.Option, 0, len(choices))
		for _, c := range choices {
			options = append(options, sdui.Option{Value: c.Value, Label: cmp.Or(c.Name, c.Value)})
		}
		p := sdui.Picker{ID: o.ID, Help: cmp.Or(o.Description, o.Name), Value: o.Current(), Options: options}
		if o.Category != acp.CategoryModel {
			p.Title = o.Name
		}
		out = append(out, p)
	}
	if sc.View.Session.Agent != agentClaude {
		return out
	}
	options := make([]sdui.Option, 0, len(advisorChoices))
	for _, c := range advisorChoices {
		options = append(options, sdui.Option{Value: c.value, Label: c.label})
	}
	out = append(out, sdui.Picker{ID: advisorID, Title: "Advisor", Help: advisorHelp, Value: sc.Advisor, Options: options})
	if sc.View.Session.ParentSession != "" {
		return out
	}
	finish := make([]sdui.Option, 0, len(finishChoices))
	for _, c := range finishChoices {
		finish = append(finish, sdui.Option{Value: c.value, Label: c.label})
	}
	afk := make([]sdui.Option, 0, len(afkChoices))
	for _, c := range afkChoices {
		afk = append(afk, sdui.Option{Value: c.value, Label: c.label})
	}
	afkValue := afkOff
	if sc.View.Session.AFK {
		afkValue = afkOn
	}
	return append(out,
		sdui.Picker{ID: finishID, Title: "Finish", Help: finishHelp, Value: cmp.Or(sc.View.Session.Finish, finishAsk), Options: finish},
		sdui.Picker{ID: afkID, Title: "AFK", Help: afkHelp, Value: afkValue, Options: afk},
	)
}

func effortName(configs []acp.SessionConfigOption) string {
	name := configName(configs, acp.CategoryThoughtLevel)
	i := slices.IndexFunc(configs, func(o acp.SessionConfigOption) bool { return o.Category == acp.CategoryThoughtLevel })
	if name == "" || configs[i].Name == "" {
		return name
	}
	return name + " " + strings.ToLower(configs[i].Name)
}

func configName(configs []acp.SessionConfigOption, category string) string {
	for _, o := range configs {
		if o.Category != category {
			continue
		}
		if c, ok := o.Choice(o.Current()); ok {
			return cmp.Or(c.Name, c.Value)
		}
		return o.Current()
	}
	return ""
}

func modeName(modes []acp.SessionMode, id string) string {
	for _, m := range modes {
		if m.ID == id {
			return cmp.Or(m.Name, m.ID)
		}
	}
	return id
}

func turnActive(rec record) bool {
	return rec.Status == statusRunning || rec.Status == statusWaiting
}

func agentLabel(agent string) string {
	if label, ok := agentLabels[agent]; ok {
		return label
	}
	return agent
}

func firstCheckpoint(turns []turnInfo) turnInfo {
	var best turnInfo
	for _, t := range turns {
		if t.CheckpointSha != "" && (best.Turn == 0 || t.Turn < best.Turn) {
			best = t
		}
	}
	return best
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
