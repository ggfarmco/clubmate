package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/automation"
	"clubmate/plugins/internal/landgate"
)

const (
	gatesSection     = "agents.gates"
	gatesSidebarSlot = gatesSection + ".sidebar"
	gatesMainSlot    = gatesSection + ".main"
	gatesTreeID      = "gates"
	gateHelpID       = "help"
	gateVerdictsID   = "gateVerdicts"
	gateFindingsID   = "gateFindings"
	gateLiveID       = "gateLive"
	gateLiveNote     = "Findings show here when it decides; the lander gets them in its transcript."
	gateSheetID      = "gateSheet"
	gateRoundsMax    = 5
	gateWeek         = 7 * 24 * time.Hour

	actGateNew       = "gateNew"
	actGatePause     = "gatePause"
	actGateResume    = "gateResume"
	actGateEdit      = "gateEdit"
	actGateDelete    = "gateDelete"
	actGateReview    = "gateReview"
	actGateLanding   = "gateLanding"
	actGateRepo      = "gateRepo"
	actGateInto      = "gateInto"
	actGateLike      = "gateLike"
	actGatePick      = "gatePick"
	actGateRounds    = "gateRounds"
	actGatePaused    = "gatePaused"
	actGateCancel    = "gateCancel"
	actGateSave      = "gateSave"
	actGateTrust     = "gateTrust"
	actGateOpenGraph = "gateOpenGraph"

	gateIcon        = "shield"
	gateLandIcon    = "arrow.triangle.merge"
	gateRepoIcon    = "folder"
	gateRepoPrefix  = "gate-repo:"
	gateCardPrefix  = "card:"
	gateTrustID     = "gateTrust"
	gateShortHash   = 6
	gateHelpTitle   = "How gates work"
	gateNoLandings  = "no landings yet"
	gateNoHistory   = "No landings yet. The next landing this gate holds waits for %s."
	gateDeleteText  = "Delete the gate %s on %s? Landings it holds then land without a review. Its verdict history goes too."
	gateDocsOnly    = "a change only in .md files lands without a review"
	gateHolds       = "an agent's landing (git_land)"
	gateRoundsWords = "of changes, then you decide"
	gateEnds        = "ends approved · changes"
	gateVerdictDoes = "approves or asks for changes"
	gateTrustTitle  = "%s changed: trust the new version?"
	gateTrustText   = "This gate pinned %s. The file is now %s. Until you trust it, the gate runs nothing: a landing it holds is refused, and runs through the new version when its agent calls git_land again."
	gateTrustLost   = "The pinned version wasn't kept, so only the new one can be shown:"
	gateTrustOther  = "Its content changed in a way this card doesn't list; read the file before you trust it."
	gateUnreadable  = "%s can't be read: %s"
	gateTrustPhone  = "Trust it on the Mac, where the change can be read; Pause works here."
	gateSummaryRow  = "summary"
	gateSummaryWord = "Summary · "
)

var gateHelpFacts = [][2]string{
	{"Holds", "an agent's git_land that a gate matches waits until the gate's graph decides the change (Fable review, built in, by default)"},
	{"Into", "which landings a gate holds: into the default branch, into a worktree's base (the branch it was made from), into any branch, or into branches like release/*; when several gates match, the most specific decides"},
	{"Decided by", "a graph from the repo's main checkout, your own graphs or the built-in ones; the gate pins its content, and a changed file waits until you trust it on the gate's page"},
	{"Rounds", "after the gate's rounds of changes the landing waits for you: Land anyway, One more round or Stop, in Agents"},
	{"Docs only", gateDocsOnly},
	{"Only you", "add, change, pause or delete gates; agents can read them"},
}

type gateScreen struct {
	Row      gateRow
	Lines    map[string]string
	Repo     gateRepo
	Graph    *graphs.GateGraphInfo
	GraphErr string
	Pinned   *graphs.GateGraphInfo
	Mac      bool
	History  []gateVerdict
	Picked   string
	Sessions map[string]bool
	Asks     map[string]sdui.Node
	Now      time.Time
	Loc      *time.Location
}

type gateLook struct {
	Text  string
	Tone  string
	Faint bool
}

type gateVerdictLook struct {
	Word string
	Tone string
	Cell string
	Icon string
}

func gatesSidebar(rows []gateRow, selected string, now time.Time) sdui.Node {
	items := []sdui.TreeItem{automation.HelpItem(gateHelpTitle)}
	for _, g := range gateGroups(rows) {
		repo := sdui.TreeItem{ID: gateRepoPrefix + g.Repo, Label: filepath.Base(g.Repo), Icon: gateRepoIcon, Detail: strconv.Itoa(len(g.Rows)), HasChildren: true, Expanded: true}
		for _, r := range g.Rows {
			repo.Children = append(repo.Children, gateItem(r, now, r.Name))
		}
		items = append(items, repo)
	}
	return sdui.Tree(gatesTreeID, items, cmp.Or(selected, gateHelpID))
}

func gateDock(rows []gateRow, now time.Time) []sdui.TreeItem {
	items := []sdui.TreeItem{{ID: dockGateHelp, Label: gateHelpTitle, Icon: "info.circle", Open: openGate}}
	for _, g := range gateGroups(rows) {
		repo := sdui.TreeItem{ID: gateRepoPrefix + g.Repo, Label: filepath.Base(g.Repo), Icon: gateRepoIcon, Expanded: true}
		for _, r := range g.Rows {
			item := gateItem(r, now, strings.TrimPrefix(r.Name, gateNameLanding))
			item.ID, item.Open, item.Menu = gatePrefix+r.ID, openGate, gateRemoveMenu(r.Name, r.Repo)
			if item.Detail == gateNoLandings {
				item.Detail, item.DetailFaint = "", false
			}
			repo.Children = append(repo.Children, item)
		}
		items = append(items, repo)
	}
	return items
}

type gateGroup struct {
	Repo string
	Rows []gateRow
}

func gateGroups(rows []gateRow) []gateGroup {
	var out []gateGroup
	for _, r := range rows {
		i := slices.IndexFunc(out, func(g gateGroup) bool { return g.Repo == r.Repo })
		if i < 0 {
			out = append(out, gateGroup{Repo: r.Repo})
			i = len(out) - 1
		}
		out[i].Rows = append(out[i].Rows, r)
	}
	slices.SortStableFunc(out, func(x, y gateGroup) int {
		return cmp.Compare(strings.ToLower(filepath.Base(x.Repo)), strings.ToLower(filepath.Base(y.Repo)))
	})
	return out
}

func gateItem(r gateRow, now time.Time, label string) sdui.TreeItem {
	look := r.look(now)
	return sdui.TreeItem{ID: r.ID, Label: label, Icon: gateLandIcon, Detail: look.Text, DetailTone: look.Tone, DetailFaint: look.Faint, Menu: gateMenu(r)}
}

func (r gateRow) look(now time.Time) gateLook {
	asking, landing := r.landingCounts()
	switch {
	case r.State == gateStateTrust:
		return gateLook{Text: "trust?", Tone: "warning"}
	case asking > 0:
		return gateLook{Text: strconv.Itoa(asking) + " asking", Tone: tonePlain}
	case landing > 0:
		return gateLook{Text: landingCount(landing), Tone: tonePlain}
	case r.Paused:
		return gateLook{Text: gateStatePaused, Faint: true}
	case r.Last == nil:
		return gateLook{Text: gateNoLandings, Faint: true}
	}
	v := verdictLook(*r.Last)
	return gateLook{Text: v.Word + " " + shortAge(now.Sub(r.Last.At), r.Last.At), Tone: v.Tone}
}

func (r gateRow) landingCounts() (asking, landing int) {
	for _, l := range r.Landings {
		switch l.State {
		case gateStateNeedsMe:
			asking++
		case gateStateReview:
			landing++
		}
	}
	return asking, landing
}

func shortAge(d time.Duration, at time.Time) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d < gateWeek:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}
	return at.Local().Format("Jan 2")
}

func gateMenu(r gateRow) []sdui.MenuEntry {
	toggle := sdui.MenuEntry{Action: actGatePause, Label: "Pause"}
	if r.Paused {
		toggle = sdui.MenuEntry{Action: actGateResume, Label: "Resume"}
	}
	return []sdui.MenuEntry{
		toggle,
		{Action: actGateEdit, Label: "Edit…"},
		{Action: actGateDelete, Label: "Delete", Confirm: gateDeleteConfirm(r.Name, r.Repo)},
	}
}

func gateDeleteConfirm(name, repo string) string {
	return fmt.Sprintf(gateDeleteText, cmp.Or(name, "gate"), filepath.Base(repo))
}

func verdictLook(v gateVerdict) gateVerdictLook {
	switch {
	case v.By == landgate.ByDocs:
		return gateVerdictLook{Word: "docs only", Icon: "doc.text"}
	case v.By == landgate.ByUser:
		return gateVerdictLook{Word: "you: land", Tone: "accent", Icon: "person"}
	case v.Outcome == gateChanges:
		return gateVerdictLook{Word: "changes", Tone: "warning", Cell: "warn", Icon: "exclamationmark.triangle"}
	}
	return gateVerdictLook{Word: "approved", Tone: "success", Cell: "ok", Icon: "checkmark"}
}

func gatesHelp(rows []gateRow) sdui.Node {
	button := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(gateHelpID+"/"+actGateNew, "New gate", "plus"), actGateNew), sdui.VariantPrimary), "cmd+n")
	header := sdui.Header("Gates", sdui.HeaderProps{Icon: automation.HelpIcon, Meta: gatesCount(rows)}, button)
	return sdui.VStack(header, sdui.Scroll(automation.Facts(gateHelpFacts)))
}

func gatesCount(rows []gateRow) string {
	if len(rows) == 0 {
		return "no gates in this workspace yet: its landings need no review"
	}
	parts := []string{counted(len(rows), "gate")}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.State]++
	}
	for _, s := range []string{gateStatePaused, gateStateReview, gateStateNeedsMe} {
		if counts[s] > 0 {
			parts = append(parts, strconv.Itoa(counts[s])+" "+strings.ReplaceAll(s, "-", " "))
		}
	}
	return strings.Join(parts, " · ")
}

func gateMain(sc gateScreen) sdui.Node {
	r := sc.Row
	badges := []sdui.HeaderBadge{{Text: gateStateActive}}
	if r.Paused {
		badges[0].Text = gateStatePaused
	}
	if r.State == gateStateTrust {
		badges = append(badges, sdui.HeaderBadge{Text: "trust?", Tone: "warning"})
	}
	if asking, _ := r.landingCounts(); asking > 0 {
		badges = append(badges, sdui.HeaderBadge{Text: "needs you", Tone: "warning"})
	}
	actions := []sdui.Node{
		automation.ActionNode(r.ID, gatePauseAction(r.Paused)),
		automation.ActionNode(r.ID, automation.DetailAction{Action: actGateEdit, Label: "Edit", Icon: "pencil", Variant: sdui.VariantSecondary, Shortcut: "e"}),
		automation.ActionNode(r.ID, automation.DetailAction{Action: actGateDelete, Label: "Delete", Icon: "trash", Variant: sdui.VariantDanger, Shortcut: "cmd+delete", Confirm: gateDeleteConfirm(r.Name, r.Repo)}),
	}
	header := sdui.Header(r.Name, sdui.HeaderProps{Icon: gateLandIcon, Meta: gateMeta(sc), Badges: badges}, actions...)
	var body []sdui.Node
	if r.State == gateStateTrust {
		body = append(body, trustCard(sc))
	}
	body = append(body, gateFacts(sc))
	rows, picked := gateTableRows(sc)
	if len(rows) == 0 {
		body = append(body, sdui.MutedText(fmt.Sprintf(gateNoHistory, graphTitle(sc.Graph, r.Graph))))
		return sdui.VStack(header, sdui.Scroll(sdui.VStack(body...)))
	}
	columns := []sdui.Column{
		{ID: "when", Title: "When", Width: 110},
		{ID: "verdict", Title: "Verdict", Width: 90},
		{ID: "landing", Title: "Landing"},
		{ID: "into", Title: "Into", Width: 120, Truncate: "middle", Priority: 1},
	}
	body = append(body, sdui.MutedText(weekLine(sc.History, sc.Now)), sdui.WithAction(sdui.Table(gateVerdictsID, columns, rows, picked, sdui.Sort{}), "select"))
	if card, ok := liveCard(sc, picked); ok {
		body = append(body, card)
	} else if card, ok := findingsCard(sc, picked); ok {
		body = append(body, card)
	}
	return sdui.VStack(header, sdui.Scroll(sdui.VStack(body...)))
}

func gateFacts(sc gateScreen) sdui.Node {
	r := sc.Row
	return automation.Properties(
		automation.Property("Repo", filepath.Base(r.Repo), tildePath(r.Repo)),
		automation.Property("Holds", gateHolds, ""),
		automation.Property("Match", matchWords(r.Into, sc.Repo.Branch), ""),
		sdui.HStack(sdui.MutedText("Graph"), graphFact(sc)),
		automation.Property("Rounds", roundsWords(r.Rounds), ""),
		automation.Property("Docs only", gateDocsOnly, ""),
	)
}

func matchWords(into, branch string) string {
	branch = cmp.Or(branch, "the default branch")
	switch {
	case into == gateIntoDefault:
		return "landings into " + branch + ", the default branch"
	case into == gateIntoBase:
		return "landings into a worktree's base, the branch it was made from; only worktrees made from a branch have one"
	case into == gateIntoAny:
		return "every landing: into " + branch + ", into a base, and a child's merge into its parent's branch"
	case isGlob(into):
		return "landings into a branch like " + into
	}
	return "landings into " + into
}

func graphFact(sc gateScreen) sdui.Node {
	r := sc.Row
	if sc.Graph == nil {
		return sdui.MutedText(fmt.Sprintf(gateUnreadable, r.Graph, cmp.Or(sc.GraphErr, "no such graph")))
	}
	g := *sc.Graph
	head := []sdui.Node{sdui.Text(graphTitle(sc.Graph, r.Graph)), sdui.MutedText(originWords(g.From, r.Repo) + " · pinned " + shortHash(r.Pin))}
	if sc.Mac {
		head = append(head, gateGraphButton(r.ID))
	}
	lines := []sdui.Node{sdui.HStack(head...)}
	for i, n := range g.Nodes {
		lines = append(lines, sdui.HStack(sdui.MutedText(strconv.Itoa(i+1)), sdui.Text(n.Title), sdui.MutedText(nodeDoes(n))))
	}
	lines = append(lines, sdui.MutedText(gateEnds))
	return sdui.VStack(lines...)
}

func nodeDoes(n graphs.GateNodeInfo) string {
	if n.Step {
		return "step · " + n.Command
	}
	parts := []string{"agent"}
	if n.Model != "" {
		parts = append(parts, n.Model)
	}
	if n.Effort != "" {
		parts = append(parts, n.Effort+" effort")
	}
	if n.Verdict {
		parts = append(parts, gateVerdictDoes)
	}
	return strings.Join(parts, " · ")
}

func graphTitle(g *graphs.GateGraphInfo, name string) string {
	if g == nil {
		return name
	}
	return cmp.Or(g.Title, g.Name)
}

func originWords(from, repo string) string {
	switch from {
	case graphs.FromBuiltin:
		return "built in"
	case graphs.FromUser:
		return "yours"
	}
	return "in " + filepath.Base(repo)
}

func shortHash(h string) string {
	return h[:min(len(h), gateShortHash)]
}

func gateGraphButton(target string) sdui.Node {
	return automation.ActionNode(target, automation.DetailAction{Action: actGateOpenGraph, Label: "Open graph", Variant: sdui.VariantGhost})
}

func gatePauseAction(paused bool) automation.DetailAction {
	if paused {
		return automation.DetailAction{Action: actGateResume, Label: "Resume", Icon: "play", Variant: sdui.VariantSecondary, Shortcut: "p"}
	}
	return automation.DetailAction{Action: actGatePause, Label: "Pause", Icon: "pause", Variant: sdui.VariantSecondary, Shortcut: "p"}
}

func gateMeta(sc gateScreen) string {
	var parts []string
	if sc.Row.Last != nil {
		parts = append(parts, "last: "+verdictLook(*sc.Row.Last).Word+" "+ago(sc.Row.Last.At, sc.Now))
	}
	reviewing := 0
	for _, l := range sc.Row.Landings {
		if l.State == gateStateReview {
			reviewing++
		}
	}
	if reviewing > 0 {
		parts = append(parts, strconv.Itoa(reviewing)+" reviewing")
	}
	return strings.Join(parts, " · ")
}

func roundsWords(n int) string {
	return counted(n, "round") + " " + gateRoundsWords
}

func weekLine(history []gateVerdict, now time.Time) string {
	counts := map[string]int{}
	total := 0
	for _, v := range history {
		if now.Sub(v.At) > gateWeek {
			break
		}
		counts[verdictLook(v).Word]++
		total++
	}
	if total == 0 {
		return "History · none in the last 7 days"
	}
	parts := []string{"History · last 7 days"}
	for _, w := range []string{"approved", "changes", "docs only", "you: land"} {
		if counts[w] > 0 {
			parts = append(parts, w+" "+strconv.Itoa(counts[w]))
		}
	}
	return strings.Join(parts, " · ")
}

func gateRowID(at time.Time, open string) string {
	return strconv.FormatInt(at.UnixNano(), 10) + "/" + open
}

func gateRowSession(rowID string) string {
	_, sid, _ := strings.Cut(rowID, "/")
	return sid
}

func gateTableRows(sc gateScreen) ([]sdui.TableRow, string) {
	when := func(t time.Time) string { return t.In(sc.Loc).Format("Jan 2 15:04") }
	var rows []sdui.TableRow
	ids := map[string]bool{}
	add := func(id string, cells []sdui.Cell) {
		if !ids[id] {
			ids[id] = true
			rows = append(rows, sdui.TableRow{ID: id, Cells: cells})
		}
	}
	for _, l := range sc.Row.Landings {
		landing := landingWords(l.Title, l.Source, l.Round, len(l.Findings))
		word, link, open := "reviewing", "Open review", l.open()
		if l.State == gateStateNeedsMe {
			word, link = "needs you", "Open landing"
		}
		add(l.rowID(), []sdui.Cell{{Text: when(l.Since)}, {Text: word, Tone: "warn"}, sessionLink(sc, landing, open, link, l.State == gateStateNeedsMe), {Text: l.Into}})
	}
	for _, v := range sc.History {
		look := verdictLook(v)
		round := v.Round
		if v.By == landgate.ByDocs {
			round = 0
		}
		landing := landingWords(v.Title, v.Source, round, len(v.Findings))
		if v.By == landgate.ByUser && v.Round > 0 {
			landing = cmp.Or(v.Title, v.Source, "the staged change") + " · after " + counted(v.Round, "round")
		}
		add(gateRowID(v.At, v.Reviewer), []sdui.Cell{{Text: when(v.At)}, {Text: look.Word, Tone: look.Cell}, sessionLink(sc, landing, v.Reviewer, "Open review", false), {Text: v.Into}})
		if v.Summary != "" && v.Outcome == gateApproved && v.By == landgate.ByReviewer {
			id := gateRowID(v.At, gateSummaryRow)
			ids[id] = true
			rows = append(rows, sdui.TableRow{ID: id, Depth: 1, Muted: true, Cells: []sdui.Cell{{}, {}, {Text: gateSummaryWord + v.Summary}, {}}})
		}
	}
	picked := sc.Picked
	if !ids[picked] {
		picked = ""
	}
	return rows, picked
}

func landingWords(title, source string, round, findings int) string {
	parts := []string{cmp.Or(title, source, "the staged change")}
	if round > 0 {
		parts = append(parts, "round "+strconv.Itoa(round))
	}
	if findings > 0 {
		parts = append(parts, counted(findings, "finding"))
	}
	return strings.Join(parts, " · ")
}

func sessionLink(sc gateScreen, text, sid, label string, landing bool) sdui.Cell {
	if sid == "" || !sc.Sessions[sid] {
		return sdui.Cell{Text: text}
	}
	action := actGateReview
	if landing {
		action = actGateLanding
	}
	return sdui.Cell{Text: text, Action: action, Label: label}
}

func findingsCard(sc gateScreen, picked string) (sdui.Node, bool) {
	if picked == "" {
		return sdui.Node{}, false
	}
	at, _, _ := strings.Cut(picked, "/")
	i := slices.IndexFunc(sc.History, func(v gateVerdict) bool { return strconv.FormatInt(v.At.UnixNano(), 10) == at })
	if i < 0 {
		return sdui.Node{}, false
	}
	v := sc.History[i]
	open := v.Reviewer != "" && sc.Sessions[v.Reviewer]
	if len(v.Findings) == 0 && len(v.Deferred) == 0 && v.Summary == "" && !open {
		return sdui.Node{}, false
	}
	look := verdictLook(v)
	title := look.Word
	if v.Round > 0 {
		title = "Round " + strconv.Itoa(v.Round) + " · " + look.Word
	}
	lines := []sdui.Node{sdui.Text(title), sdui.MutedText(cmp.Or(v.Source, "the staged change") + " → " + v.Into)}
	for _, f := range v.Findings {
		lines = append(lines, sdui.Text("· "+f))
	}
	if len(v.Deferred) > 0 {
		lines = append(lines, sdui.MutedText("Deferred: "+strings.Join(v.Deferred, " · ")))
	}
	if v.Summary != "" {
		lines = append(lines, sdui.MutedText(gateSummaryWord+v.Summary))
	}
	if open {
		lines = append(lines, sdui.HStack(sdui.Spacer(), gateCardButton(v.Reviewer, actGateReview, landingOpenReview, landingOpenKey)))
	}
	return sdui.WithID(sdui.Card("neutral", look.Icon, lines...), gateFindingsID), true
}

func (l gateLanding) open() string {
	if l.State == gateStateNeedsMe {
		return l.Lander
	}
	return l.Reviewer
}

func (l gateLanding) rowID() string {
	return gateRowID(l.Since, l.open())
}

func liveCard(sc gateScreen, picked string) (sdui.Node, bool) {
	i := slices.IndexFunc(sc.Row.Landings, func(l gateLanding) bool {
		return (l.State == gateStateReview || l.Ask != "") && (picked == "" || l.rowID() == picked)
	})
	if i < 0 {
		return sdui.Node{}, false
	}
	l := sc.Row.Landings[i]
	state, tone := gateStateReview, "accent"
	if l.Ask != "" {
		state, tone = landingAsking, toneWarning
	}
	title := fmt.Sprintf(reviewRoundLabel, max(l.Round, 1))
	title = strings.TrimSuffix(title, " review") + " · " + state
	subject := landReview{Into: l.Into, Source: l.Source}.subject() + " · " + elapsed(sc.Now.Sub(l.Since))
	lines := []sdui.Node{sdui.Text(title), sdui.MutedText(subject)}
	ask, asks := sc.Asks[l.Run]
	switch {
	case l.Ask != "":
		lines = append(lines, sdui.WithTone(sdui.Text(l.Ask), toneWarning))
	case sc.Lines[l.Reviewer] != "":
		lines = append(lines, sdui.Text(sc.Lines[l.Reviewer]))
	}
	if !asks {
		lines = append(lines, sdui.MutedText(gateLiveNote))
	}
	var buttons []sdui.Node
	if sc.Sessions[l.Reviewer] {
		buttons = append(buttons, gateCardButton(l.Reviewer, actGateReview, landingOpenReview, landingOpenKey))
	}
	if sc.Sessions[l.Lander] {
		buttons = append(buttons, gateCardButton(l.Lander, actGateLanding, "Open landing", ""))
	}
	if len(buttons) > 0 {
		lines = append(lines, sdui.WithGap(sdui.HStack(append([]sdui.Node{sdui.Spacer()}, buttons...)...), openGap))
	}
	card := sdui.WithID(sdui.Card(tone, landingIcon, lines...), gateLiveID)
	if asks && l.Ask != "" {
		return sdui.VStack(card, ask), true
	}
	return card, true
}

func gateCardButton(sid, action, label, key string) sdui.Node {
	return automation.ActionNode(sid, automation.DetailAction{Action: action, Label: label, Variant: sdui.VariantSecondary, Shortcut: key})
}

func roundOptions(current int) []sdui.Option {
	out := make([]sdui.Option, 0, gateRoundsMax+1)
	for n := 1; n <= max(gateRoundsMax, current); n++ {
		if n > gateRoundsMax && n != current {
			continue
		}
		out = append(out, sdui.Option{Value: strconv.Itoa(n), Label: strconv.Itoa(n)})
	}
	return out
}
