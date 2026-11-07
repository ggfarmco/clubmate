package main

import (
	"fmt"
	"hash/fnv"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/sdui"
)

const (
	reposNode     = "repos"
	reposKeysNode = "keys"
	reposRowPart  = 1
	reposGap      = 6

	actReposToggle = "reposToggle"
	actReposRow    = "reposRow"
	actReposShow   = "reposShow"
	actReposKey    = "reposKey"
	actRepoFix     = "repoFix"
	actRepoSession = "repoSession"
	actRepoCode    = "repoCode"
	actRepoLeave   = "repoLeave"

	landsByIconPR  = "arrow.triangle.pull"
	landsByIconTop = "arrow.triangle.merge"
	landsByIconOn  = "arrow.triangle.branch"

	reposBranchNote  = "in each"
	reposFooter      = "Lands in order %s · a repo waits until the ones before it landed"
	reposHintFix     = "↑↓ repo · ↩ ask it to fix"
	reposHintOpen    = "↑↓ repo · ↩ open"
	reposCappedRule  = "connected while you were away"
	repoHoldsFormat  = "Leaving it unlanded holds %s too."
	repoLeaveConfirm = "Leave %s unlanded? Its branch stays."
)

var (
	reposColumns = []string{"Repo", "Lands by", "State"}
	reposKeys    = []string{"up", "down", "k", "j", "return"}
	prNumberRe   = regexp.MustCompile(`(\d+)\D*$`)
	landsByWords = map[string]string{landsByPR: "pull request", landsByBranch: "stays on its branch"}
	landsByIcons = map[string]string{landsByPR: landsByIconPR, landsByMerge: landsByIconTop, landsByBranch: landsByIconOn}
)

type reposScreenData struct {
	Kids   map[string]record
	Choice reposChoice
}

type reposChoice struct {
	chosen bool
	folded bool
	card   string
	rows   map[string]repoRowChoice
	cursor string
}

type repoRowChoice struct {
	open bool
	key  string
}

func (c reposChoice) clone() reposChoice {
	c.rows = maps.Clone(c.rows)
	return c
}

func (c reposChoice) collapsed(t taskRecord) bool {
	key := t.failedKey()
	if c.chosen && c.card == key {
		return c.folded
	}
	return key == ""
}

func (c *reposChoice) toggleCard(t taskRecord) {
	c.chosen, c.folded, c.card = true, !c.collapsed(t), t.failedKey()
}

func (c *reposChoice) openCard(t taskRecord) {
	c.chosen, c.folded, c.card = true, false, t.failedKey()
}

func (c reposChoice) rowOpen(r taskRepo) bool {
	if row, ok := c.rows[repoRowID(r.Path)]; ok && row.key == r.foldKey() {
		return row.open
	}
	return r.State == landFailed
}

func (c *reposChoice) toggleRow(r taskRepo) {
	if c.rows == nil {
		c.rows = map[string]repoRowChoice{}
	}
	c.rows[repoRowID(r.Path)] = repoRowChoice{open: !c.rowOpen(r), key: r.foldKey()}
}

func (c reposChoice) current(t taskRecord) int {
	if i := t.rowIndex(c.cursor); c.cursor != "" && i >= 0 {
		return i
	}
	return max(slices.IndexFunc(t.Repos, func(r taskRepo) bool { return r.State == landFailed }), 0)
}

func (r taskRepo) foldKey() string {
	return r.State + "/" + r.Since.String()
}

func (t taskRecord) failedKey() string {
	var keys []string
	for _, r := range t.Repos {
		if r.State == landFailed {
			keys = append(keys, r.Path+"/"+r.foldKey())
		}
	}
	return strings.Join(keys, "\n")
}

func (t taskRecord) rowIndex(id string) int {
	return slices.IndexFunc(t.Repos, func(r taskRepo) bool { return repoRowID(r.Path) == id })
}

func (t taskRecord) ranks() map[int]int {
	ranks := map[int]int{}
	for _, r := range t.Repos {
		if _, ok := ranks[r.Order]; !ok {
			ranks[r.Order] = len(ranks) + 1
		}
	}
	return ranks
}

func (t taskRecord) holder(i int) int {
	return slices.IndexFunc(t.Repos, func(r taskRepo) bool { return r.Order < t.Repos[i].Order && !r.landed() })
}

func (t taskRecord) holds(i int) []string {
	var names []string
	for _, r := range t.Repos {
		if r.Order > t.Repos[i].Order && !r.landed() && r.State != landLeft {
			names = append(names, r.Name)
		}
	}
	return names
}

func repoRowID(path string) string {
	h := fnv.New64a()
	h.Write([]byte(path))
	return strconv.FormatUint(h.Sum64(), 36)
}

func reposCardID(sid string) string {
	return sid + "/" + reposNode
}

func reposKeysID(sid string) string {
	return reposCardID(sid) + "/" + reposKeysNode
}

func reposCard(sc screen) (sdui.Node, bool) {
	rec := sc.View.Session
	if rec.Task == nil || len(rec.Task.Repos) == 0 || sc.Code != nil {
		return sdui.Node{}, false
	}
	p, bars := reposData(sc)
	card := sdui.Repos(reposCardID(rec.ID), p, bars...)
	return sdui.WithHints(sdui.WithAction(sdui.Keys(reposKeysID(rec.ID), reposKeys, card), actReposKey), p.Hint), true
}

func reposData(sc screen) (sdui.ReposProps, []sdui.Node) {
	rec := sc.View.Session
	task := *rec.Task
	choice := sc.Repos.Choice
	cursor := choice.current(task)
	ranks := task.ranks()
	p := sdui.ReposProps{
		Title:      counted(len(task.Repos), "repo"),
		Summary:    task.segments(),
		Branch:     taskBranchPrefix + task.Slug,
		BranchNote: reposBranchNote,
		Columns:    reposColumns,
		Footer:     task.footer(ranks),
		Hint:       reposHintOpen,
		Collapsed:  choice.collapsed(task),
		Action:     actReposToggle,
		RowAction:  actReposRow,
	}
	var bars []sdui.Node
	if text, bar, ok := reposArchiveFooter(rec, sc.Repos.Kids); ok {
		p.Footer, p.FooterAction = text, bar.ID
		bars = append(bars, bar)
	}
	for i, r := range task.Repos {
		kid := sc.Repos.kid(r)
		word, tone, icon := repoStateWord(task, i, kid)
		id := repoRowID(r.Path)
		rule, source := r.rule()
		row := sdui.ReposRow{
			ID: id, Order: ranks[r.Order], Name: r.Name, LandsBy: rule, Lands: landsByIcons[r.LandsBy], Source: source,
			State: word, Tone: tone, Icon: icon, Cursor: i == cursor, Tail: strings.TrimRight(r.Tail, "\n"),
		}
		bar, hasBar := repoBar(rec, task, i, kid, i == cursor)
		row.Opens = hasBar || row.Tail != ""
		row.Open = row.Opens && choice.rowOpen(r)
		if row.Open && hasBar {
			row.Actions, row.Note = bar.ID, r.holdsNote(task, i)
			bars = append(bars, bar)
		}
		if i == cursor && canFix(rec, r, kid) {
			p.Hint = reposHintFix
		}
		p.Rows = append(p.Rows, row)
	}
	return p, bars
}

func (d reposScreenData) kid(r taskRepo) *record {
	if kid, ok := d.Kids[r.Session]; ok {
		return &kid
	}
	return nil
}

func canFix(rec record, r taskRepo, kid *record) bool {
	return !rec.closed() && r.State == landFailed && !r.FixAsked && kid != nil && !kid.closed()
}

func (r taskRepo) rule() (string, string) {
	if r.Capped {
		return landsByWords[landsByPR], reposCappedRule
	}
	return r.ruleWords(), r.Source
}

func (r taskRepo) ruleWords() string {
	if r.LandsBy == landsByMerge {
		return mergeWords(r.Into)
	}
	return landsByWords[r.LandsBy]
}

func mergeWords(branch string) string {
	if branch == "" {
		return "merge"
	}
	return "merge to " + branch
}

func (r taskRepo) holdsNote(t taskRecord, i int) string {
	held := t.holds(i)
	if len(held) == 0 || r.landed() || r.State == landLeft {
		return ""
	}
	return fmt.Sprintf(repoHoldsFormat, strings.Join(held, ", "))
}

func repoBar(rec record, t taskRecord, i int, kid *record, cursor bool) (sdui.Node, bool) {
	if rec.closed() {
		return sdui.Node{}, false
	}
	r := t.Repos[i]
	base := reposCardID(rec.ID) + "/" + repoRowID(r.Path)
	button := func(name, label, action, variant string) sdui.Node {
		return sdui.WithVariant(sdui.WithAction(sdui.Button(base+"/"+name, label, ""), action), variant)
	}
	var row []sdui.Node
	if canFix(rec, r, kid) {
		fix := button("fix", "Ask it to fix", actRepoFix, sdui.VariantSecondary)
		if cursor {
			fix = sdui.WithHint(sdui.WithVariant(fix, sdui.VariantPrimary), "return")
		}
		row = append(row, fix)
	}
	if kid != nil {
		row = append(row, button("session", "Open its session", actRepoSession, sdui.VariantSecondary))
		if !kid.closed() {
			row = append(row, button("code", "Open in Code", actRepoCode, sdui.VariantGhost))
		}
	}
	if !r.landed() && r.State != landLeft {
		text := fmt.Sprintf(repoLeaveConfirm, r.Name)
		if held := t.holds(i); len(held) > 0 {
			text += " " + fmt.Sprintf(repoHoldsFormat, strings.Join(held, ", "))
		}
		row = append(row, sdui.Spacer(), sdui.WithConfirm(button("leave", "Leave it unlanded…", actRepoLeave, sdui.VariantGhost), text))
	}
	if len(row) == 0 {
		return sdui.Node{}, false
	}
	bar := sdui.WithGap(sdui.HStack(row...), reposGap)
	bar.ID = base + "/actions"
	return bar, true
}

func repoStateWord(t taskRecord, i int, kid *record) (string, string, string) {
	r := t.Repos[i]
	switch r.State {
	case landMerged:
		if r.PR != "" {
			return "merged · " + prLabel(r.PR), "success", statusIcons[statusDone]
		}
		return "merged", "success", statusIcons[statusDone]
	case landOnBranch:
		return "on its branch", "success", statusIcons[statusDone]
	case landPROpen:
		return prLabel(r.PR) + " open", "", landsByIconPR
	case landReady:
		if r.Request != "" {
			return "ready to land", "warning", statusIcons[statusWaiting]
		}
		return "ready to land", "", statusIcons[statusIdle]
	case landFailed:
		return "failed", "danger", statusIcons[statusError]
	case landLeft:
		return "left unlanded", "", statusIcons[statusIdle]
	case landAsking:
		if kid != nil && needsYou(*kid) {
			return "asking", "warning", statusIcons[statusWaiting]
		}
		return "asking", "", statusIcons[statusAsksParent]
	case landRunning:
		if r.Landing {
			return "landing", "", statusIcons[statusRunning]
		}
		return "running", "", statusIcons[statusRunning]
	}
	return waitsWord(t, i), "", statusIcons[statusIdle]
}

func waitsWord(t taskRecord, i int) string {
	r := t.Repos[i]
	word := "waits"
	switch h := t.holder(i); {
	case h >= 0 && strings.HasPrefix(r.For, t.Repos[h].Name):
		word += " for " + strconv.Itoa(t.ranks()[t.Repos[h].Order])
	case r.For != "":
		word += " for " + r.For
	}
	if r.Finished {
		return "on its branch · " + word
	}
	return word
}

func prLabel(pr string) string {
	if m := prNumberRe.FindStringSubmatch(pr); m != nil {
		return "PR #" + m[1]
	}
	return "PR"
}

func (t taskRecord) segments() []sdui.ReposSegment {
	count := func(match func(taskRepo) bool) int {
		n := 0
		for _, r := range t.Repos {
			if match(r) {
				n++
			}
		}
		return n
	}
	state := func(s string) func(taskRepo) bool {
		return func(r taskRepo) bool { return r.State == s }
	}
	waiting := func(r taskRepo) bool { return r.State == landReady && r.Request != "" }
	ready := func(r taskRepo) bool { return r.State == landReady && r.Request == "" }
	var out []sdui.ReposSegment
	add := func(n int, word, tone string) {
		if n > 0 {
			out = append(out, sdui.ReposSegment{Text: strconv.Itoa(n) + " " + word, Tone: tone})
		}
	}
	add(count(state(landMerged)), "merged", "")
	add(count(state(landOnBranch)), "on its branch", "")
	if prs := count(state(landPROpen)); prs > 1 {
		add(prs, "PRs open, in order", "")
	} else {
		add(prs, "PR open", "")
	}
	add(count(waiting), "ready to land", "warning")
	add(count(ready), "ready to land", "")
	add(count(state(landFailed)), "failed", "danger")
	add(count(state(landRunning)), "running", "")
	add(count(state(landAsking)), "asking", "")
	add(count(state(landWaits)), "waits", "")
	add(count(state(landLeft)), "left", "")
	return out
}

func (t taskRecord) footer(ranks map[int]int) string {
	if len(ranks) < 2 {
		return ""
	}
	steps := make([]string, 0, len(ranks))
	for rank := 1; rank <= len(ranks); rank++ {
		steps = append(steps, strconv.Itoa(rank))
	}
	return fmt.Sprintf(reposFooter, strings.Join(steps, " → "))
}

func reposPill(rec record) (sdui.Node, bool) {
	if rec.Task == nil || len(rec.Task.Repos) == 0 {
		return sdui.Node{}, false
	}
	failed := 0
	for _, r := range rec.Task.Repos {
		if r.State == landFailed {
			failed++
		}
	}
	detail, tone := "", ""
	if failed > 0 {
		detail, tone = strconv.Itoa(failed)+" failed", "danger"
	}
	button := sdui.WithAction(sdui.Button(reposCardID(rec.ID)+"/pill", counted(len(rec.Task.Repos), "repo"), repoIcon), actReposShow)
	return sdui.WithPill(button, detail, tone), true
}
