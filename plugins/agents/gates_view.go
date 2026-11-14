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
	gateSheetID      = "gateSheet"
	gateRoundsMax    = 5
	gateWeek         = 7 * 24 * time.Hour

	actGateNew     = "gateNew"
	actGatePause   = "gatePause"
	actGateResume  = "gateResume"
	actGateEdit    = "gateEdit"
	actGateDelete  = "gateDelete"
	actGateReview  = "gateReview"
	actGateLanding = "gateLanding"
	actGateRepo    = "gateRepo"
	actGateRounds  = "gateRounds"
	actGatePaused  = "gatePaused"
	actGateCancel  = "gateCancel"
	actGateSave    = "gateSave"

	gateIcon        = "shield"
	gateOKIcon      = "checkmark.shield"
	gateWarnIcon    = "exclamationmark.shield"
	gatePausedIcon  = "pause.circle"
	gateHelpTitle   = "How gates work"
	gateNoLandings  = "no landings yet"
	gateNoHistory   = "No landings yet. The next landing into %s waits for a review."
	gateDeleteText  = "Delete the land gate on %s? Landings into %s then land without a review. Its verdict history goes too."
	gateSheetNote   = "Only you add, change or delete gates."
	gateHasGate     = "%s · has a gate"
	gateRepoTaken   = "Gated already"
	gateNoRepo      = "No folder of this workspace is a git repo."
	gateKindLabel   = "Fable review"
	gateKindLater   = "Command, graph and check gates come later."
	gateKindHint    = "a fresh Fable session reads the diff, read-only"
	gateDocsOnly    = "a change only in .md files lands without a review"
	gateGuards      = "landings into %s · a child's merge into its parent's branch"
	gateRoundsWords = "of changes, then you decide"
	gateRepoHint    = "%s · lands into %s"
)

var gateHelpFacts = [][2]string{
	{"Guards", "git_land into a gated repo's default branch, and a child's merge into its parent's branch, wait for a read-only Fable review of the diff"},
	{"Rounds", "after the gate's rounds of changes the landing waits for you: Land anyway or Stop, in Agents"},
	{"Docs only", gateDocsOnly},
	{"Repo", "one gate per repo; it covers every worktree of the repo"},
	{"Only you", "add, change, pause or delete gates; agents can read them"},
}

type gateScreen struct {
	Row      gateRow
	Repo     gateRepo
	History  []gateVerdict
	Picked   string
	Sessions map[string]bool
	Now      time.Time
	Loc      *time.Location
}

type gateDraft struct {
	Edit   string
	Name   string
	Repo   string
	Rounds int
	Paused bool
	Err    string
	Repos  []gateRepo
	Gated  map[string]bool
}

type gateVerdictLook struct {
	Word string
	Tone string
	Cell string
	Icon string
}

func gatesSidebar(rows []gateRow, selected string, now time.Time) sdui.Node {
	items := []sdui.TreeItem{automation.HelpItem(gateHelpTitle)}
	for _, r := range rows {
		items = append(items, gateItem(r, now))
	}
	return sdui.Tree(gatesTreeID, items, cmp.Or(selected, gateHelpID))
}

func gateItem(r gateRow, now time.Time) sdui.TreeItem {
	item := sdui.TreeItem{ID: r.ID, Label: r.Name, Icon: gateIcon, Menu: gateMenu(r)}
	switch r.State {
	case gateStateNeedsMe:
		item.Icon, item.Tone = gateWarnIcon, "warning"
		item.Badges = []sdui.HeaderBadge{{Text: "needs you", Tone: "warning"}}
	case gateStatePaused:
		item.Icon, item.Detail = gatePausedIcon, gateStatePaused
	case gateStateReview:
		item.Detail, item.DetailTone = gateStateReview, "warning"
	default:
		if r.Last == nil {
			item.Detail, item.DetailFaint = gateNoLandings, true
			break
		}
		look := verdictLook(*r.Last)
		item.Detail, item.DetailTone = look.Word+" · "+shortAge(now.Sub(r.Last.At), r.Last.At), look.Tone
		if r.Last.By == landgate.ByReviewer {
			item.Icon, item.Tone = gateVerdictIcon(r.Last.Outcome), look.Tone
		}
	}
	return item
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

func gateVerdictIcon(outcome string) string {
	if outcome == gateChanges {
		return gateWarnIcon
	}
	return gateOKIcon
}

func gateMenu(r gateRow) []sdui.MenuEntry {
	toggle := sdui.MenuEntry{Action: actGatePause, Label: "Pause"}
	if r.Paused {
		toggle = sdui.MenuEntry{Action: actGateResume, Label: "Resume"}
	}
	return []sdui.MenuEntry{
		toggle,
		{Action: actGateEdit, Label: "Edit…"},
		{Action: actGateDelete, Label: "Delete", Confirm: gateDeleteConfirm(r.Name, "")},
	}
}

func gateDeleteConfirm(name, branch string) string {
	return fmt.Sprintf(gateDeleteText, name, cmp.Or(branch, "its default branch"))
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
	icon := gateIcon
	if r.Paused {
		badges[0].Text, icon = gateStatePaused, gatePausedIcon
	}
	if r.State == gateStateNeedsMe {
		badges = append(badges, sdui.HeaderBadge{Text: "needs you", Tone: "warning"})
	}
	actions := []sdui.Node{
		automation.ActionNode(r.ID, gatePauseAction(r.Paused)),
		automation.ActionNode(r.ID, automation.DetailAction{Action: actGateEdit, Label: "Edit", Icon: "pencil", Variant: sdui.VariantSecondary, Shortcut: "e"}),
		automation.ActionNode(r.ID, automation.DetailAction{Action: actGateDelete, Label: "Delete", Icon: "trash", Variant: sdui.VariantDanger, Shortcut: "cmd+delete", Confirm: gateDeleteConfirm(r.Name, sc.Repo.Branch)}),
	}
	header := sdui.Header(r.Name, sdui.HeaderProps{Icon: icon, Meta: gateMeta(sc), Badges: badges}, actions...)
	branch := cmp.Or(sc.Repo.Branch, "the default branch")
	props := automation.Properties(
		automation.Property("Repo", r.Name, tildePath(r.Repo)),
		automation.Property("Guards", fmt.Sprintf(gateGuards, branch), ""),
		automation.Property("Kind", gateKindLabel, gateKindHint),
		automation.Property("Rounds", roundsWords(r.Rounds), ""),
		automation.Property("Docs only", gateDocsOnly, ""),
	)
	body := []sdui.Node{props}
	rows, picked := gateTableRows(sc)
	if len(rows) == 0 {
		body = append(body, sdui.MutedText(fmt.Sprintf(gateNoHistory, branch)))
		return sdui.VStack(header, sdui.Scroll(sdui.VStack(body...)))
	}
	columns := []sdui.Column{
		{ID: "when", Title: "When", Width: 110},
		{ID: "verdict", Title: "Verdict", Width: 90},
		{ID: "landing", Title: "Landing"},
		{ID: "into", Title: "Into", Width: 120, Truncate: "middle", Priority: 1},
	}
	body = append(body, sdui.MutedText(weekLine(sc.History, sc.Now)), sdui.WithAction(sdui.Table(gateVerdictsID, columns, rows, picked, sdui.Sort{}), "select"))
	if card, ok := findingsCard(sc, picked); ok {
		body = append(body, card)
	}
	return sdui.VStack(header, sdui.Scroll(sdui.VStack(body...)))
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
		return "Verdicts · none in the last 7 days"
	}
	parts := []string{"Verdicts · last 7 days"}
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
		word, link, open := "reviewing", "Open review", l.Reviewer
		if l.State == gateStateNeedsMe {
			word, link, open = "needs you", "Open landing", l.Lander
		}
		add(gateRowID(l.Since, open), []sdui.Cell{{Text: when(l.Since)}, {Text: word, Tone: "warn"}, sessionLink(sc, landing, open, link, l.State == gateStateNeedsMe), {Text: l.Into}})
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
	if len(v.Findings) == 0 && len(v.Deferred) == 0 {
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
	return sdui.WithID(sdui.Card(cmp.Or(look.Tone, "neutral"), look.Icon, lines...), gateFindingsID), true
}

func gateSheetView(d gateDraft) sdui.Node {
	var rows []sdui.Node
	title := "New gate"
	save := "Add gate"
	disabled := false
	if d.Edit == "" {
		repo, err := gateRepoField(d)
		rows = append(rows, repo...)
		disabled = err
	} else {
		title, save = "Edit gate · "+d.Name, "Save"
		rows = append(rows, automation.Property("Repo", d.Name, ""))
	}
	kind, hint := sdui.Select(gateSheetID+"/kind", []sdui.Option{{Value: gateKindReview, Label: gateKindLabel}}, gateKindReview), gateKindLater
	if d.Edit != "" {
		kind, hint = sdui.Text(gateKindLabel), gateKindHint
	}
	rows = append(rows, sdui.HStack(sdui.MutedText("Kind"), kind, sdui.MutedText(hint)))
	rows = append(rows, sdui.HStack(sdui.MutedText("Rounds"), sdui.WithAction(sdui.Select(gateSheetID+"/rounds", roundOptions(d.Rounds), strconv.Itoa(d.Rounds)), actGateRounds), sdui.MutedText(gateRoundsWords)))
	if d.Edit != "" {
		rows = append(rows, sdui.HStack(sdui.MutedText("Paused"), sdui.WithAction(sdui.Toggle(gateSheetID+"/paused", d.Paused), actGatePaused), sdui.MutedText("a paused gate gates nothing")))
	}
	rows = append(rows, automation.Property("Docs only", gateDocsOnly, ""))
	cancel := sdui.WithVariant(sdui.WithAction(sdui.Button(gateSheetID+"/cancel", "Cancel", ""), actGateCancel), sdui.VariantSecondary)
	ok := sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(gateSheetID+"/save", save, ""), actGateSave), sdui.VariantPrimary), "cmd+return")
	if disabled {
		ok = sdui.WithDisabled(ok)
	}
	note, tone := gateSheetNote, ""
	if d.Err != "" {
		note, tone = d.Err, "danger"
	}
	return sdui.Sheet(gateSheetID, sdui.SheetProps{Title: title, Icon: gateIcon, Cancel: actGateCancel, Note: note, NoteTone: tone},
		automation.Properties(rows...),
		sdui.HStack(cancel, ok),
	)
}

func gateRepoField(d gateDraft) ([]sdui.Node, bool) {
	if len(d.Repos) == 0 {
		return []sdui.Node{automation.Property("Repo", gateNoRepo, "")}, true
	}
	options := make([]sdui.Option, 0, len(d.Repos))
	var chosen gateRepo
	for _, r := range d.Repos {
		label := filepath.Base(r.Primary)
		if d.Gated[r.Primary] {
			label = fmt.Sprintf(gateHasGate, label)
		}
		options = append(options, sdui.Option{Value: r.Path, Label: label})
		if r.Path == d.Repo {
			chosen = r
		}
	}
	field := sdui.WithAction(sdui.Select(gateSheetID+"/repo", options, d.Repo), actGateRepo)
	taken := d.Gated[chosen.Primary]
	if taken {
		field = sdui.WithError(field, gateRepoTaken)
	}
	hint := fmt.Sprintf(gateRepoHint, tildePath(chosen.Primary), cmp.Or(chosen.Branch, "its default branch"))
	return []sdui.Node{sdui.HStack(sdui.MutedText("Repo"), field, sdui.MutedText(hint))}, taken || chosen.Primary == ""
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

func gateChip(state string) (string, string) {
	switch state {
	case gateStateReview:
		return "review…", "warning"
	case gateChanges:
		return gateChanges, "warning"
	case gateApproved:
		return gateApproved, "success"
	}
	return "", ""
}
