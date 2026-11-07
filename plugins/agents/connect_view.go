package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/sdui"
)

const (
	connectShown = 3

	connectTitleOne    = "Connect a repo?"
	connectTitleMany   = "Connect %d repos?"
	connectCaptionOne  = "%s wants a folder this workspace doesn't have"
	connectCaptionMany = "%s found folders this workspace doesn't have · asks for all of them at once"
	connectLabel       = "Connect"
	cloneLabel         = "Clone and connect"
	connectHint        = "Space un-ticks the focused repo · ↑↓ moves · the card has the keys until you click the composer"
	connectEffectMany  = "Each becomes a folder of **%s** and stays. %s starts a child session in each, on a new branch, with that repo's own CLAUDE.md and memory; a repo's own rules win over the plan."
	connectSkippedLine = "Un-ticked repos are skipped, not left pending. "
	connectWithout     = "Don't connect: %s goes on without %s and says what is missing."
	connectWithOnly    = "Don't connect: %s goes on with %s only and says what is missing."
	connectCloneFormat = "Clone %s into %s?"
	stateChangedFormat = "%s changed"
	stateBehindFormat  = "%d behind origin/%s"
	stateCleanFormat   = "%s · clean"
	stateOnFormat      = "on %s · clean · last commit %s"
	repoBranchFormat   = "new branch `%s` · %s's own CLAUDE.md and memory"
	repoNoBranchFormat = "new branch · %s's own CLAUDE.md and memory"
	bringsNothing      = "no instructions, graphs or hooks"
	bringsNoGraphs     = "no graphs or hooks"
	trustBoth          = "steps and the hook stay off until you trust them"
	trustBothLong      = "graph steps and the hook stay off until you trust them"
	trustSteps         = "graph steps stay off until you trust them"
	trustHook          = "the hook stays off until you trust it"
	rulesWinNote       = "its rules win over the plan"

	landCaptionFormat = "%d of %d in order · ask before merging"
	landMergeFormat   = "Merge `%s` into `%s`"
	landPRFormat      = "Open a pull request from `%s` into `%s`"
	landRuleFormat    = "By its rule: %s."
	landBeforeFormat  = "%s already landed"
	landAfterOne      = "%s waits for this one"
	landAfterMany     = "%s wait for this one"
	landHintText      = "Review opens the diff for this repo; Merge lands it now. The card has the keys until you click the composer. No time limit."
	landHoldsFormat   = " Leaving it unlanded holds %s too."
	landLeaveConfirm  = "Leave %s unlanded?"
	landBadgeFormat   = "%d to land"
	landReviewLabel   = "Review…"
	landMergeLabel    = "Merge"
	landLeaveLabel    = "Leave it unlanded…"
	repoFolderIcon    = "folder"
	landHeadNode      = "land"
	connectHeadNode   = "connect"
)

type repoFacts struct {
	Remote        string    `json:"remote"`
	Branch        string    `json:"branch"`
	Clean         bool      `json:"clean"`
	Dirty         int       `json:"dirty"`
	Behind        int       `json:"behind"`
	DefaultBranch string    `json:"defaultBranch"`
	LastCommit    time.Time `json:"lastCommit"`
	Brings        struct {
		Graphs       []string `json:"graphs"`
		Hooks        []string `json:"hooks"`
		Instructions []string `json:"instructions"`
		Skills       []string `json:"skills"`
	} `json:"brings"`
	Trusted struct {
		Hooks  bool     `json:"hooks"`
		Graphs []string `json:"graphs"`
	} `json:"trusted"`
}

func requestCard(sc screen) (sdui.Node, bool) {
	rec := sc.View.Session
	if i := slices.IndexFunc(rec.ConnectRequests, connectRequest.waits); i >= 0 {
		return connectCard(sc, rec.ConnectRequests[i]), true
	}
	if rec.Task == nil {
		return sdui.Node{}, false
	}
	if i := slices.IndexFunc(rec.Task.Repos, func(r taskRepo) bool { return r.Request != "" }); i >= 0 {
		return landCard(rec, i), true
	}
	return sdui.Node{}, false
}

func connectCard(sc screen, req connectRequest) sdui.Node {
	rec := sc.View.Session
	agent := agentLabel(rec.Agent)
	repos := slices.DeleteFunc(slices.Clone(req.Repos), func(r connectRepo) bool { return r.State != repoPending })
	single := len(repos) == 1
	p := sdui.ConnectRepoProps{Title: fmt.Sprintf(connectTitleMany, len(repos)), Caption: fmt.Sprintf(connectCaptionMany, agent), Connect: connectLabel, Seen: req.seen()}
	if single {
		p.Title, p.Caption = connectTitleOne, fmt.Sprintf(connectCaptionOne, agent)
	}
	if !slices.ContainsFunc(repos, func(r connectRepo) bool { return r.URL == "" }) {
		p.Connect = cloneLabel
	}
	for i, r := range repos {
		row := connectRow(r, single, sc.Now)
		row.Folded = i >= connectShown
		p.Repos = append(p.Repos, row)
	}
	if len(repos) > connectShown {
		p.More = counted(len(repos)-connectShown, "more repo")
	}
	p.Declined = declinedLine(rec, repos, agent)
	if single {
		p.Effects = connectEffects(sc, repos[0], p.Repos[0], agent)
	} else {
		p.Effect = fmt.Sprintf(connectEffectMany, sc.Workspace, agent)
		p.Hint = connectHint
		p.Declined = connectSkippedLine + p.Declined
	}
	return sdui.WithDock(sdui.ConnectRepoCard(rec.ID+"/"+connectHeadNode+"/"+req.ID, req.ID, p))
}

func connectRow(r connectRepo, single bool, now time.Time) sdui.ConnectRepoRow {
	row := sdui.ConnectRepoRow{Key: r.key(), Name: r.Name, Path: tildePath(r.Path), Reason: r.Reason, LandsBy: r.landsByWords(), Source: fromSource(r.Source)}
	if r.URL != "" {
		row.Clone, row.Path, row.Remote = true, tildePath(r.Into), remoteHost(r.URL)
		row.State = fmt.Sprintf(connectCloneFormat, row.Remote, tildePath(filepath.Dir(r.Into)))
		return row
	}
	var facts repoFacts
	_ = json.Unmarshal(r.Facts, &facts)
	row.Remote = remoteHost(facts.Remote)
	row.State, row.Warn = facts.stateLine(single, now)
	if r.Note != "" {
		row.State, row.Warn = r.Note, true
	}
	row.Brings, row.Trust = facts.bringsLine(single)
	return row
}

func (r connectRepo) landsByWords() string {
	if r.LandsBy != landsByMerge {
		return landsByWords[r.LandsBy]
	}
	var facts repoFacts
	_ = json.Unmarshal(r.Facts, &facts)
	return mergeWords(facts.DefaultBranch)
}

func (f repoFacts) stateLine(single bool, now time.Time) (string, bool) {
	var warn []string
	if !f.Clean {
		warn = append(warn, fmt.Sprintf(stateChangedFormat, counted(f.Dirty, "file")))
	}
	if f.Behind > 0 {
		warn = append(warn, fmt.Sprintf(stateBehindFormat, f.Behind, f.DefaultBranch))
	}
	switch {
	case len(warn) > 0:
		return strings.Join(warn, " · "), true
	case single && !f.LastCommit.IsZero():
		return fmt.Sprintf(stateOnFormat, f.Branch, ago(f.LastCommit, now)), false
	}
	return fmt.Sprintf(stateCleanFormat, f.Branch), false
}

func (f repoFacts) bringsLine(single bool) (string, string) {
	b := f.Brings
	var own, runs []string
	if len(b.Instructions) > 0 {
		own = append(own, b.Instructions[0])
	}
	if n := len(b.Skills); n > 0 {
		own = append(own, counted(n, "skill"))
	}
	switch n := len(b.Graphs); {
	case n == 1 && !single:
		runs = append(runs, "1 graph **"+b.Graphs[0]+"**")
	case n > 0:
		runs = append(runs, counted(n, "graph"))
	}
	if n := len(b.Hooks); n > 0 {
		runs = append(runs, counted(n, "worktree hook"))
	}
	var text string
	switch {
	case single:
		text = "Brings " + joinAnd(append(own, runs...))
		if len(own)+len(runs) == 0 {
			text = "Brings nothing"
		}
	case len(own)+len(runs) == 0:
		text = bringsNothing
	case len(runs) == 0:
		text = strings.Join(own, ", ") + " · " + bringsNoGraphs
	default:
		text = strings.Join(slices.DeleteFunc([]string{strings.Join(own, ", "), strings.Join(runs, " · ")}, func(s string) bool { return s == "" }), " · ")
	}
	return text, f.trustNote(single)
}

func (f repoFacts) trustNote(long bool) string {
	graphs := slices.ContainsFunc(f.Brings.Graphs, func(g string) bool { return !slices.Contains(f.Trusted.Graphs, g) })
	hooks := len(f.Brings.Hooks) > 0 && !f.Trusted.Hooks
	switch {
	case graphs && hooks && long:
		return trustBothLong
	case graphs && hooks:
		return trustBoth
	case graphs:
		return trustSteps
	case hooks:
		return trustHook
	}
	return ""
}

func connectEffects(sc screen, repo connectRepo, row sdui.ConnectRepoRow, agent string) []sdui.ConnectEffect {
	out := []sdui.ConnectEffect{
		{Text: "Becomes a folder of **" + sc.Workspace + "**", Note: "stays after the task · remove it any time"},
		{Text: agent + " starts a child session there", Note: fmt.Sprintf(repoNoBranchFormat, repo.Name)},
	}
	if task := sc.View.Session.Task; task != nil && task.Slug != "" {
		out[1].Note = fmt.Sprintf(repoBranchFormat, taskBranchPrefix+task.Slug, repo.Name)
	}
	if row.LandsBy != "" {
		out = append(out, sdui.ConnectEffect{Text: "Lands by " + row.LandsBy, Note: strings.Join(nonEmpty(row.Source, rulesWinNote), " · ")})
	}
	if row.Brings != "" {
		out = append(out, sdui.ConnectEffect{Text: row.Brings, Note: row.Trust})
	}
	return out
}

func declinedLine(rec record, repos []connectRepo, agent string) string {
	var others []string
	if rec.Task != nil {
		for _, r := range rec.Task.Repos {
			if !slices.ContainsFunc(repos, func(c connectRepo) bool { return c.Name == r.Name }) {
				others = append(others, r.Name)
			}
		}
	}
	if len(others) > 0 {
		return fmt.Sprintf(connectWithOnly, agent, joinAnd(others))
	}
	if len(repos) == 1 {
		return fmt.Sprintf(connectWithout, agent, repos[0].Name)
	}
	return fmt.Sprintf(connectWithout, agent, "them")
}

func landCard(rec record, i int) sdui.Node {
	task := rec.Task
	r := task.Repos[i]
	var before, after []string
	for _, other := range task.Repos {
		switch {
		case other.Name == r.Name:
		case other.landed():
			before = append(before, other.Name)
		case other.Order > r.Order:
			after = append(after, other.Name)
		}
	}
	return sdui.WithDock(sdui.LandRequestCard(rec.ID+"/"+landHeadNode+"/"+r.Request, r.Request, sdui.LandRequestProps{
		Title:   fmt.Sprintf(landOneFormat, r.Name),
		Caption: fmt.Sprintf(landCaptionFormat, i+1, len(task.Repos)),
		Name:    r.Name,
		Detail:  r.landLine(),
		Rule:    r.ruleLine(before, after),
		Hint:    landHint(after),
		Confirm: leaveQuestion(r.Name, after),
		Options: []sdui.PermissionOption{
			{ID: landActionReview, Label: landReviewLabel, Kind: "allow_once", Shortcut: "opt+1"},
			{ID: landActionMerge, Label: landMergeLabel, Shortcut: "opt+2"},
			{ID: landActionLeave, Label: landLeaveLabel, Kind: "reject_once", Shortcut: "opt+3"},
		},
	}))
}

func (r taskRepo) landLine() string {
	format := landMergeFormat
	if r.LandsBy == landsByPR || r.Capped {
		format = landPRFormat
	}
	line := fmt.Sprintf(format, r.Branch, r.Into)
	if r.Commits > 0 {
		line += " · " + counted(r.Commits, "commit")
	}
	return line
}

func (r taskRepo) ruleLine(before, after []string) string {
	rule := r.ruleWords()
	if r.Source != "" {
		rule += " (" + strings.TrimPrefix(r.Source, "from ") + ")"
	}
	line := []string{fmt.Sprintf(landRuleFormat, rule)}
	var then []string
	if len(before) > 0 {
		then = append(then, fmt.Sprintf(landBeforeFormat, joinAnd(before)))
	}
	if len(after) > 0 {
		then = append(then, fmt.Sprintf(plural(len(after), landAfterOne, landAfterMany), joinAnd(after)))
	}
	if len(then) > 0 {
		line = append(line, strings.Join(then, "; ")+".")
	}
	return strings.Join(line, " ")
}

func landHint(after []string) string {
	if len(after) == 0 {
		return landHintText
	}
	return landHintText + fmt.Sprintf(landHoldsFormat, joinAnd(after))
}

func leaveQuestion(name string, after []string) string {
	text := fmt.Sprintf(landLeaveConfirm, name)
	if len(after) > 0 {
		text += fmt.Sprintf(landHoldsFormat, joinAnd(after))
	}
	return text
}

func landBadge(rec record) string {
	if rec.Task == nil || len(rec.Task.requests()) == 0 {
		return ""
	}
	return fmt.Sprintf(landBadgeFormat, len(rec.Task.requests()))
}

func (a *app) landAct(ctx context.Context, workspaceID string, args landAnswerArgs) error {
	res, err := a.answerLand(ctx, workspaceID, args)
	if err != nil || args.Action != landActionReview {
		return err
	}
	kid := a.session(res.Session)
	if kid == nil {
		return errNoCheckpoint
	}
	if a.reviews.progress(res.Session) != nil {
		return a.openReview(ctx, workspaceID, res.Session, "")
	}
	return a.review(ctx, workspaceID, kid, 0)
}

func fromSource(source string) string {
	if source == "" || strings.HasPrefix(source, "from ") {
		return source
	}
	return "from " + source
}

func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || path == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

func remoteHost(remote string) string {
	host := strings.TrimSuffix(remote, ".git")
	if _, rest, ok := strings.Cut(host, "://"); ok {
		host = rest
	}
	if _, rest, ok := strings.Cut(host, "@"); ok {
		host = strings.Replace(rest, ":", "/", 1)
	}
	return host
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
