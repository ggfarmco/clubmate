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
	noCodeTitle    = "No agent session in this worktree"
	noCodeSession  = "Start one to ask about the code you're on."
	codeSelectNode = "code/session"
	codeNewNode    = "code/new"
	starterPrefix  = "starter/"
	starterNote    = "Ask anything, @ to attach a file, or start from one of these."
	maxCodeOptions = 50
	commandRunes   = 300
	composerHint   = "/ for commands, @ for files"
	waitingBadge   = "waiting for you"
	installingNote = "Installing %s adapter…"
	queuedNote     = "Queued, sent when the turn ends"
	heldNote       = "Queued, not sent"
	queuePrefix    = "queue/"
	mergeConfirm   = "Ask the agent to run the tests, squash its branch into main as one commit and remove its worktree and branch?"

	advisorHelp = "Advisor model for new sessions in this worktree"
	finishHelp  = "What the agent does once the task is done"
	afkLabel    = "AFK"
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
	toggleOptions = []sdui.Option{{Value: "true", Label: "On"}, {Value: "false", Label: "Off"}}
	finishChoices = []advisorChoice{
		{value: finishAsk, label: "Ask"},
		{value: finishMerge, label: "Merge to main"},
	}
)

type screen struct {
	View        transcriptView
	Workspace   string
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
	Branch      string
	Parent      string
	Children    string
	FromTitles  map[string]string
	Context     *contextScreen
	Code        *codePanel
}

type codePanel struct {
	Picker sdui.Node
	New    sdui.Node
	File   string
}

type starter struct {
	label  string
	prompt string
	attach bool
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
	switch {
	case sc.Context != nil:
		children = append(children, contextView(sc))
	case startable(sc):
		children = append(children, starterView(sc))
	default:
		children = append(children, transcript(sc))
	}
	if rec.closed() && rec.HandoffTo != "" {
		next := sdui.WithAction(sdui.Button(sid+"/"+handoffToNode, "Open new session", "arrow.turn.down.right"), actOpenHandoff)
		children = append(children, sdui.HStack(sdui.MutedText("The session was handed off to a new session."), next))
		return sdui.VStack(children...)
	}
	if rec.closed() {
		reopen := sdui.WithAction(sdui.Button(sid+"/reopen", "Reopen", "arrow.clockwise"), actRetry)
		if sc.Continue {
			reopen = continueButton(sid)
		}
		children = append(children, sdui.HStack(sdui.MutedText("The session is closed; its transcript stays readable."), reopen))
		return sdui.VStack(children...)
	}
	if card, ok := dockedPermission(sc); ok {
		children = append(children, card)
	}
	if len(sc.Queue) > 0 {
		children = append(children, queueView(sc))
	}
	return sdui.VStack(append(children, composer(sc))...)
}

func codeView(recs []record, bound string, sc *screen, file string) sdui.Node {
	if sc == nil {
		add := sdui.WithVariant(sdui.WithAction(sdui.Button(codeNewNode, "New", "plus"), actCodeNew), "ghost")
		return sdui.EmptyState(noCodeSession, sdui.EmptyProps{Title: noCodeTitle, Action: &add})
	}
	add := sdui.WithIconOnly(sdui.WithVariant(sdui.WithAction(sdui.Button(codeNewNode, "New session", "plus"), actCodeNew), "ghost"))
	var opts []sdui.Option
	for _, rec := range recs {
		if len(opts) < maxCodeOptions && (!rec.closed() || rec.ID == bound) {
			opts = append(opts, sdui.Option{Value: rec.ID, Label: cmp.Or(rec.Title, "New session")})
		}
	}
	if !slices.ContainsFunc(opts, func(o sdui.Option) bool { return o.Value == bound }) {
		opts = append(opts, sdui.Option{Value: bound, Label: cmp.Or(sc.View.Session.Title, "New session")})
	}
	v := *sc
	v.Code = &codePanel{Picker: sdui.WithAction(sdui.Select(codeSelectNode, opts, bound), actCodeSelect), New: add, File: file}
	if unstartedStep(v) {
		return sdui.VStack(header(v), stepView(v, recs))
	}
	return mainView(v)
}

func startable(sc screen) bool {
	rec := sc.View.Session
	return sc.Code != nil && len(sc.View.Entries) == 0 && len(sc.Queue) == 0 && !rec.closed() && rec.Status != statusError && !turnActive(rec)
}

func starters(file string) []starter {
	if file == "" {
		return []starter{
			{label: "Explain how this worktree is laid out", prompt: "Explain how this worktree is laid out."},
			{label: "Review the last commit", prompt: "Review the last commit."},
			{label: "Review the uncommitted changes", prompt: "Review the uncommitted changes."},
		}
	}
	name := filepath.Base(file)
	return []starter{
		{label: "Explain " + name, prompt: "Explain this file.", attach: true},
		{label: "Review the last commit", prompt: "Review the last commit."},
		{label: "Write a test for " + name, prompt: "Write a test for this file.", attach: true},
	}
}

func starterView(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	nodes := []sdui.Node{sdui.VStack(
		sdui.Node{Type: "text", Props: map[string]any{"text": cmp.Or(rec.Title, agentLabel(rec.Agent)) + " is idle", "style": "title"}},
		sdui.MutedText(starterNote),
	)}
	if sc.Code.File != "" {
		nodes = append(nodes, sdui.HStack(sdui.Icon("doc"), sdui.Badge(filepath.Base(sc.Code.File), "")))
	}
	var buttons []sdui.Node
	for i, s := range starters(sc.Code.File) {
		n := strconv.Itoa(i + 1)
		buttons = append(buttons, sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/"+starterPrefix+n, s.label, ""), actStarter), "ghost"))
	}
	return sdui.Transcript(sid+"/transcript", append(nodes, sdui.VStack(buttons...))...)
}

func header(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	display := displayStatus(rec, sc.Children)
	var meta []string
	if rec.AFK {
		meta = append(meta, afkLabel)
	}
	if display == childrenRunning || display == childrenWaiting {
		meta = append(meta, statusLabels[display])
	}
	fill, full := usageParts(sc.View.Usage)
	if !full {
		meta = append(meta, nonEmpty(fill)...)
	}
	var buttons []sdui.Node
	if turnActive(rec) {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/stop", "Stop", "stop.fill"), actStop))
	}
	if sc.Parent != "" {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/parent", "from "+sc.Parent, "arrow.turn.up.left"), actSelectParent))
	}
	if rec.HandoffFrom != "" {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/"+handoffFromNode, "Earlier session", "clock.arrow.circlepath"), actOpenHandoff))
	}
	if b, ok := contextButton(sc); ok {
		buttons = append(buttons, b)
	}
	if sc.Changes {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/review", "Review changes", "doc.text.magnifyingglass"), actReview))
	}
	if mergeable(rec, sc.Changes) {
		buttons = append(buttons, sdui.WithConfirm(sdui.WithAction(sdui.Button(sid+"/merge", "Merge to main", "arrow.triangle.merge"), actMerge), mergeConfirm))
	}
	var top sdui.Node
	if sc.Code != nil {
		row := append([]sdui.Node{sdui.Icon(cmp.Or(statusIcons[display], statusIcons[statusIdle])), sc.Code.Picker}, buttons...)
		top = sdui.HStack(append(row, sc.Code.New)...)
	} else {
		var badges []sdui.HeaderBadge
		if needsYou(rec) {
			badges = append(badges, sdui.HeaderBadge{Text: waitingBadge, Tone: statusTones[statusWaiting]})
		}
		if full {
			badges = append(badges, sdui.HeaderBadge{Text: fill, Tone: "warning"})
		}
		top = sdui.Header(cmp.Or(rec.Title, "New session"), sdui.HeaderProps{
			Icon:   statusIcons[display],
			Meta:   strings.Join(meta, " · "),
			Badges: badges,
			Status: display,
			Agent:  agentLabel(rec.Agent),
			Branch: sc.Branch,
			Mode:   sessionModeLabel(sc.View),
		}, buttons...)
	}
	children := []sdui.Node{top}
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

func sessionModeLabel(v transcriptView) string {
	if i := slices.IndexFunc(v.Modes, func(m acp.SessionMode) bool { return m.ID == v.Session.Mode }); i >= 0 {
		return modeLabel(v.Modes[i])
	}
	return cmp.Or(modeLabels[v.Session.Mode], v.Session.Mode)
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
		modes = append(modes, sdui.Option{Value: m.ID, Label: modeLabel(m)})
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
	return sdui.Composer(sc.Composer, sdui.ComposerProps{
		Placeholder: composerPlaceholder(sc),
		Hint:        composerHint,
		Agent:       agentLabel(rec.Agent),
		Modes:       modes,
		Mode:        rec.Mode,
		Pickers:     pickers(sc),
		Commands:    commands,
		Attach:      []string{"file", "selection"},
		Attachments: attachments,
		Running:     turnActive(rec),
		Steers:      sc.Steers,
		Docked:      docked(sc),
	})
}

func composerPlaceholder(sc screen) string {
	rec := sc.View.Session
	agent := agentLabel(rec.Agent)
	switch {
	case asked(sc):
		return "Answer " + agent + "…"
	case turnActive(rec) && !sc.Steers:
		return "Queue a message"
	}
	return "Message " + agent + "…"
}

func asked(sc screen) bool {
	rec := sc.View.Session
	if rec.Status == statusDone && rec.Asks {
		return true
	}
	return slices.ContainsFunc(sc.View.Entries, func(en *entry) bool { return en.Kind == entryElicitation && sc.Pending[en.ID] })
}

func pickers(sc screen) []sdui.Picker {
	var out []sdui.Picker
	for _, o := range sc.View.Configs {
		if !pickable(o) {
			continue
		}
		if o.Type == acp.ConfigBoolean {
			out = append(out, sdui.Picker{ID: o.ID, Title: o.Name, Help: cmp.Or(o.Description, o.Name), Value: o.Current(), Options: toggleOptions, Toggle: true})
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
	return append(out,
		sdui.Picker{ID: finishID, Title: "Finish", Help: finishHelp, Value: cmp.Or(sc.View.Session.Finish, finishAsk), Options: finish},
		sdui.Picker{ID: afkID, Title: afkLabel, Help: afkHelp, Value: strconv.FormatBool(sc.View.Session.AFK), Options: toggleOptions, Toggle: true},
	)
}

var modeLabels = map[string]string{
	"default":           "Ask before edits",
	"acceptEdits":       "Accept edits",
	"plan":              "Plan",
	"bypassPermissions": "Bypass permissions",
}

func modeLabel(m acp.SessionMode) string {
	return cmp.Or(modeLabels[m.ID], m.Name, m.ID)
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
