package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
)

const (
	pickSession    = "Start an agent session with + New"
	homePickTitle  = "Start a session about anything"
	pickIcon       = "sparkles"
	setupIcon      = "exclamationmark.circle"
	signInIcon     = "key"
	setupButtonGap = 8
	homePickText   = "Agents in Home can read every workspace's backlog, sessions and wiki, and never change them. Try \"What waits for me today?\""
	newSessionNode = "main/new"
	newSessionText = "New session"
	starterPrefix  = "starter/"
	starterNote    = "It sees the file and selection you're on. Ask anything, or start from one of these."
	commandRunes   = 300
	composerHint   = "/ for commands, @ for files"
	waitingBadge   = "waiting for you"
	installingNote = "Installing %s adapter…"
	queuedNote     = "Queued, sent when the turn ends"
	heldNote       = "Queued, not sent"
	queuePrefix    = "queue/"
	detailActivity = "Activity"
	detailContext  = "Context"
	detailNotify   = "Notify"
	detailReview   = "Review"

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
	View         transcriptView
	Workspace    string
	Pending      map[string]bool
	WindowFirst  string
	RestoreNote  string
	Anchor       string
	AnchorRev    int
	Expanded     map[string]bool
	Repos        reposScreenData
	Attachments  []pendingAttachment
	Queue        []queuedPrompt
	Sending      *entry
	Composer     string
	Steers       bool
	NeedsAuth    bool
	Continue     bool
	SignIn       string
	NodeProblem  string
	Installing   bool
	Changes      bool
	EditorReview string
	ReviewBases  []string
	ReviewBase   string
	Advisor      string
	Branch       string
	Parent       string
	Crumb        *repoCrumb
	Folders      pluginsdk.Workspace
	Children     string
	Row          sessionRow
	Share        map[string]float64
	Now          time.Time
	FromTitles   map[string]string
	Context      *contextScreen
	Code         *codePanel
	Review       *reviewProgress
	ReviewStates map[string]map[int64]string
	ReviewsOpen  map[string]bool
	Open         openCardData
	OpenDraft    openDraft
	Prefills     map[string]map[int]elicitPrefill
	Pinned       *sdui.Node
	Asides       optionAsides
	GateReview   *reviewHead
	Held         *heldBlock
	Calls        map[string]map[string]callLive
	Back         string
	SendBack     *sendBackView
	CloseAsk     string
	AsideCount   int
	ForkedFrom   string
	Beside       bool
	Guide        guideView
	Suggestion   string
	NeedsYou     string
	OfferTurn    int
	Notes        map[int]string
}

type codePanel struct {
	Back     sdui.Node
	Worktree string
	File     string
	Chips    []contextChip
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
	var children []sdui.Node
	if sc.Crumb != nil {
		children = append(children, crumbNode(sid, *sc.Crumb))
	}
	if sc.Back != "" {
		children = append(children, backBar(sc.Back))
	}
	children = append(children, header(sc))
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
	if sc.Pinned != nil {
		children = append(children, *sc.Pinned)
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
		reopen := failureButton(sid, reopenAction(sid, sc.Continue, ownsTask(rec)))
		closed := "The session is closed; its transcript stays readable."
		if rec.AutoClosed {
			closed = "Closed automatically an hour after it finished; its transcript stays readable."
		}
		children = append(children, sdui.HStack(sdui.MutedText(closed), reopen))
		if sc.RestoreNote != "" {
			children = append(children, sdui.WithTone(sdui.Text(sc.RestoreNote), "danger"))
		}
		return sdui.VStack(children...)
	}
	if card, ok := dockedPermission(sc); ok {
		children = append(children, card)
	}
	if len(sc.Queue) > 0 {
		children = append(children, queueView(sc))
	}
	if sc.SendBack != nil {
		children = append(children, sendBackPanel(sid, *sc.SendBack))
	}
	return sdui.VStack(append(children, composer(sc))...)
}

func pickSessionView(home bool, setup setupState, first bool) sdui.Node {
	add := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(newSessionNode, newSessionText, "plus"), actNew), sdui.VariantPrimary), "cmd+n")
	if n, ok := setupEmpty(setup, add); ok {
		return n
	}
	if home {
		return sdui.EmptyState(homePickText, sdui.EmptyProps{Title: homePickTitle, Icon: pickIcon, Action: &add, Center: true})
	}
	if first {
		return sdui.EmptyState(firstRunText, sdui.EmptyProps{Title: firstRunTitle, Icon: pickIcon, Action: &add, Note: setupCaption(setup)})
	}
	return sdui.EmptyState(pickSession, sdui.EmptyProps{Icon: pickIcon, Action: &add, Center: true})
}

func setupEmpty(st setupState, add sdui.Node) (sdui.Node, bool) {
	var p sdui.EmptyProps
	text := ""
	p.Icon = setupIcon
	switch st.State {
	case setupNodeMissing, setupNodeOld:
		p.Title, text, p.Note = nodeTitle, nodeNeedText, nodeBrewHint
		if st.State == setupNodeOld {
			text = "Found Node " + shortVersion(st.Found.Node) + " at " + st.path + ". " + nodeNeedText
		}
		if st.Fix != nil && st.Fix.URL != "" {
			p.Note = nodeURLHint
		}
	case setupSignedOut:
		p.Title, text, p.Note, p.Icon = signInEmpty, signInText, signInHint, signInIcon
	case setupGitMissing:
		p.Title, text, p.Note = gitTitle, gitText, gitHint
	default:
		return sdui.Node{}, false
	}
	if st.Fix != nil {
		fix := sdui.WithVariant(sdui.WithAction(sdui.Button(setupFixNode, st.Fix.Label, ""), actSetupFix), sdui.VariantPrimary)
		if st.State == setupGitMissing {
			fix = sdui.WithGap(sdui.HStack(add, sdui.WithVariant(sdui.WithAction(sdui.Button(setupFixNode, st.Fix.Label, ""), actSetupFix), sdui.VariantSecondary)), setupButtonGap)
		}
		p.Action = &fix
	}
	return sdui.EmptyState(text, p), true
}

func setupCaption(st setupState) string {
	switch st.State {
	case setupChecking:
		return checkingText
	case setupOK:
		parts := []string{"Claude Code"}
		if st.Found.Node != "" {
			parts = append(parts, "Node "+shortVersion(st.Found.Node))
		}
		if st.Found.Git != "" {
			parts = append(parts, "git "+shortVersion(st.Found.Git))
		}
		return strings.Join(parts, " · ")
	}
	return ""
}

func codeView(recs []record, sc screen, panel codePanel) sdui.Node {
	sc.Code = &panel
	if unstartedStep(sc) {
		return sdui.VStack(header(sc), stepView(sc, recs), stepComposer(sc))
	}
	return mainView(sc)
}

func startable(sc screen) bool {
	rec := sc.View.Session
	return sc.Code != nil && len(sc.View.Entries) == 0 && len(sc.Queue) == 0 && !rec.closed() && rec.Status != statusError && !turnActive(rec)
}

func newChatTitle(branch string) string {
	if branch == "" {
		return newChat
	}
	return newChat + " in " + branch
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
		sdui.Node{Type: "text", Props: map[string]any{"text": newChatTitle(sc.Branch), "style": "title"}},
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
	return sdui.CenteredTranscript(sid+"/transcript", append(nodes, sdui.VStack(buttons...))...)
}

func header(sc screen) sdui.Node {
	rec := sc.View.Session
	sid := rec.ID
	display := displayStatus(rec, sc.Children)
	row := sc.Row
	row.Rec, row.Children, row.Review = rec, sc.Children, sc.Review
	activity := rowActivity(row, sc.Now)
	if needsYou(rec) || rec.Status == statusError || reviewRow(row) {
		activity = ""
	}
	notify := notifyLabels[row.Notify]
	var head []string
	if h := sc.GateReview; h != nil {
		notify, head = "", h.meta()
	}
	meta := append(head, nonEmpty(afkDetail(rec), notify, activity)...)
	fill, full := usageParts(sc.View.Usage)
	if !full {
		meta = append(meta, nonEmpty(fill)...)
	}
	meta = append(meta, shareParts(sc.Share)...)
	details := headerDetails(head, rec, notify, activity, sc.View.Usage, sc.Share)
	var buttons []sdui.Node
	if turnActive(rec) {
		buttons = append(buttons, sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(sid+"/stop", "Stop", "stop.fill"), actStop), sdui.VariantGhost), stopKey))
	}
	if b, ok := contextButton(sc); ok && sc.Code == nil {
		buttons = append(buttons, b)
	}
	if t := sc.Guide.tour; t != nil && sc.Code == nil && !rec.isAside() {
		buttons = append(buttons, sdui.WithAction(sdui.TourStrip(sid+"/"+stripNode, *t), actTourOffer))
	}
	if sc.Changes && sc.EditorReview == reviewDiffview {
		buttons = append(buttons, reviewButton(sid, sc.ReviewBases, sc.ReviewBase))
	}
	if display == statusReview {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/"+reviewedNode, reviewedLabel, "checkmark"), actReviewed))
	}
	if sc.Review != nil {
		buttons = append(buttons, openReviewButton(sid))
	}
	if sc.Parent != "" {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/parent", "from "+sc.Parent, "arrow.turn.up.left"), actSelectParent))
	}
	if sc.ForkedFrom != "" {
		buttons = append(buttons, forkedFromButton(sid, sc.ForkedFrom))
	}
	if sc.AsideCount > 0 {
		buttons = append(buttons, asideNextButton(sid, sc.AsideCount))
	}
	if rec.HandoffFrom != "" {
		buttons = append(buttons, sdui.WithAction(sdui.Button(sid+"/"+handoffFromNode, "Earlier session", "clock.arrow.circlepath"), actOpenHandoff))
	}
	var top sdui.Node
	if rec.isAside() && sc.Code == nil {
		top = asideHeader(sc)
	} else if sc.Code != nil {
		icon := cmp.Or(statusIcons[display], statusIcons[statusIdle])
		if unstartedStep(sc) {
			icon = graphIcon
		}
		row := []sdui.Node{sc.Code.Back, sdui.Divider(), sdui.Icon(icon), sdui.WithLines(openStrong(cmp.Or(rec.Title, newSessionLabel)), 1)}
		if sc.Code.Worktree != "" {
			row = append(row, sdui.WithLines(sdui.MutedText(sc.Code.Worktree), 1))
		}
		top = chatBar(append(append(append(row, sdui.Spacer()), buttons...), hideButton())...)
	} else {
		var badges []sdui.HeaderBadge
		title := cmp.Or(rec.Title, "New session")
		asks := waitingBadge
		if h := sc.GateReview; h != nil {
			title, asks = h.Round.Label, reviewAsksYou
			if h.Badge != "" {
				badges = append(badges, sdui.HeaderBadge{Text: h.Badge, Tone: h.Tone})
			}
		}
		if l := sc.Row.Landing; l != nil && l.Held != "" {
			badges = append(badges, sdui.HeaderBadge{Text: l.Badge, Tone: l.BadgeTone})
		}
		if u := sc.Row.Ungated; u != nil {
			badges = append(badges, sdui.HeaderBadge{Text: u.Badge, Tone: toneNeutral})
		}
		if needsYou(rec) {
			badges = append(badges, sdui.HeaderBadge{Text: cmp.Or(landBadge(rec), asks), Tone: statusTones[statusWaiting]})
		}
		if rec.Status == statusError {
			badges = append(badges, sdui.HeaderBadge{Text: failureOf(rec).Badge, Tone: statusTones[statusError]})
		}
		if full {
			badges = append(badges, sdui.HeaderBadge{Text: fill, Tone: "warning"})
		}
		if sc.Review != nil {
			badges = append(badges, reviewBadge(sc.Review))
		}
		if rec.OpenCount > 0 {
			badges = append(badges, headerOpenBadge(rec))
		}
		if sc.Crumb != nil && sc.Crumb.Waits != "" {
			badges = append(badges, sdui.HeaderBadge{Text: sc.Crumb.Waits})
		}
		if pill, ok := reposPill(rec); ok {
			buttons = append(buttons, pill)
		}
		top = sdui.Header(title, sdui.HeaderProps{
			Icon:    statusIcons[display],
			Meta:    strings.Join(meta, " · "),
			Badges:  byUrgency(badges),
			Status:  display,
			Agent:   agentLabel(rec.Agent),
			Branch:  sc.Branch,
			Mode:    sessionModeLabel(sc.View),
			Details: details,
			Context: headerContext(sc.View.Usage),
			Compact: true,
		}, buttons...)
	}
	children := []sdui.Node{top}
	if l := sc.Row.Landing; l != nil && sc.Code == nil && l.Into != "" {
		children = append(children, landingLine(sid, *l))
	}
	if rec.Status != statusError {
		return sdui.VStack(children...)
	}
	problem := []sdui.Node{sdui.MutedText(failureOf(rec).Line)}
	for _, act := range failureActions(sid, sc.NeedsAuth, sc.Continue, rec.LimitWait != nil && !rec.LimitWait.Fired) {
		problem = append(problem, failureButton(sid, act))
	}
	return sdui.VStack(append(children, sdui.HStack(problem...))...)
}

func headerDetails(review []string, rec record, notify, activity string, u *acp.UsageUpdate, share map[string]float64) []sdui.HeaderDetail {
	var out []sdui.HeaderDetail
	add := func(label, value string) {
		if value != "" {
			out = append(out, sdui.HeaderDetail{Label: label, Value: value})
		}
	}
	add(detailActivity, activity)
	if fill, ok := contextFill(u); ok {
		add(detailContext, strconv.Itoa(fill)+"%")
	}
	for _, name := range usageWindows {
		add(shareLabels[name]+" window", shareAmount(share[name]))
	}
	if rec.AFK {
		add(afkLabel, "on")
	}
	add(detailNotify, notify)
	add(detailReview, strings.Join(review, " · "))
	return out
}

func headerContext(u *acp.UsageUpdate) *sdui.HeaderContext {
	fill, ok := contextFill(u)
	if !ok {
		return nil
	}
	c := sdui.HeaderContext{Fill: fill}
	if fill >= contextWarnPercent {
		c.Tone = "warning"
	}
	return &c
}

var badgeUrgency = map[string]int{"danger": 0, "warning": 1, "accent": 2}

func byUrgency(badges []sdui.HeaderBadge) []sdui.HeaderBadge {
	rank := func(b sdui.HeaderBadge) int {
		if r, ok := badgeUrgency[b.Tone]; ok {
			return r
		}
		return len(badgeUrgency)
	}
	slices.SortStableFunc(badges, func(a, b sdui.HeaderBadge) int { return rank(a) - rank(b) })
	return badges
}

func sessionModeLabel(v transcriptView) string {
	return modeWord(v.Session.Mode, v.Modes)
}

func modeWord(mode string, modes []acp.SessionMode) string {
	if i := slices.IndexFunc(modes, func(m acp.SessionMode) bool { return m.ID == mode }); i >= 0 {
		return modeLabel(modes[i])
	}
	return cmp.Or(modeLabels[mode], mode)
}

func failureButton(sid string, act failureAction) sdui.Node {
	return sdui.WithAction(sdui.Button(sid+"/"+act.ID, act.Label, act.Icon), act.action)
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
		path := relPath(a.Path, rec.Worktree)
		if a.Kind == "image" {
			path = a.Name
		}
		attachments = append(attachments, sdui.Attachment{ID: a.ID, Kind: a.Kind, Path: path, StartLine: a.StartLine, EndLine: a.EndLine})
	}
	contextLabel := ""
	if sc.Code != nil {
		chips := make([]sdui.Attachment, 0, len(sc.Code.Chips))
		for _, c := range sc.Code.Chips {
			chips = append(chips, c.chip)
		}
		attachments = append(chips, attachments...)
		contextLabel = composerContext(sc)
	}
	modes := modeOptions(v)
	if rec.isAside() {
		modes = nil
	}
	placeholder, hint := composerPlaceholder(sc), composerHint
	if w, ok := dockedQuiz(sc); ok {
		placeholder, hint = w.Labels.SayIt, w.Labels.AnswerHint
	}
	return sdui.Composer(sc.Composer, sdui.ComposerProps{
		Placeholder: placeholder,
		Hint:        hint,
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
		Context:     contextLabel,
		Draft:       composerDraftNode(rec.ComposerDraft),
		Suggestion:  sc.Suggestion,
		Continue:    continueLabel(sc),
	})
}

func composerContext(sc screen) string {
	switch {
	case !hasContext(sc.View.Session):
		return ""
	case sc.Context != nil:
		return transcriptLabel
	}
	return contextLabel
}

func modeOptions(v transcriptView) []sdui.Option {
	modes := make([]sdui.Option, 0, len(v.Modes))
	for _, m := range v.Modes {
		modes = append(modes, sdui.Option{Value: m.ID, Label: modeLabel(m)})
	}
	return modes
}

func composerPlaceholder(sc screen) string {
	rec := sc.View.Session
	agent := agentLabel(rec.Agent)
	switch {
	case rec.isAside() && !turnActive(rec):
		return asidePlaceholder
	case asked(sc):
		return "Answer " + agent + "…"
	case sc.Row.Landing != nil && sc.Row.Landing.Hint != "":
		return sc.Row.Landing.Hint
	case turnActive(rec) && !sc.Steers:
		return "Queue a message"
	case rec.OpenCount > 0:
		return "Message " + agent + "…" + withAnswers
	}
	return "Message " + agent + "…"
}

func asked(sc screen) bool {
	rec := sc.View.Session
	if rec.Status == statusDone && rec.Asks || slices.ContainsFunc(rec.ConnectRequests, connectRequest.waits) {
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
	handoff := make([]sdui.Option, 0, len(handoffChoices))
	for _, c := range handoffChoices {
		handoff = append(handoff, sdui.Option{Value: c.value, Label: c.label})
	}
	return append(out,
		sdui.Picker{ID: finishID, Title: "Finish", Help: finishHelp, Value: cmp.Or(sc.View.Session.Finish, finishAsk), Options: finish},
		sdui.Picker{ID: handoffID, Title: "Handoff", Help: handoffHelp, Value: cmp.Or(sc.View.Session.Handoff, handoffOff), Options: handoff},
		sdui.Picker{ID: afkID, Title: afkLabel, Help: afkHelp, Value: strconv.FormatBool(sc.View.Session.AFK), Options: toggleOptions, Toggle: true},
	)
}

var modeLabels = func() map[string]string {
	labels := make(map[string]string, len(api.ClaudeModes))
	for _, m := range api.ClaudeModes {
		labels[m.ID] = m.Label
	}
	return labels
}()

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

func composerDraftNode(d *composerDraft) *sdui.ComposerDraft {
	if d == nil {
		return nil
	}
	return &sdui.ComposerDraft{Text: d.Text, Rev: d.Rev}
}
