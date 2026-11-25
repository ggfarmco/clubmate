package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/graphs"
	"clubmate/plugins/internal/automation"
)

var gateSheetActions = []string{actGateRepo, actGateHolds, actGateSession, actGateWrote, actGateWroteLike, actGateInto, actGateLike, actGateCall, actGateTool, actGateHow, actGateText, actGatePick, actGateRounds, actGatePaused, actGateCancel, actGateSave, actGateOpenTaken}

func isGateSheetAction(action string) bool {
	return slices.Contains(gateSheetActions, action)
}

func (a *app) renderGates(ctx context.Context, p *workspaceUI, w pluginsdk.Workspace, slots []string) error {
	if !slices.Contains(slots, gatesSidebarSlot) && !slices.Contains(slots, gatesMainSlot) {
		return nil
	}
	gates, repos, err := a.workspaceGates(ctx, w.ID)
	if err != nil {
		return err
	}
	var rows []gateRow
	for _, g := range gates {
		rows = append(rows, a.gateRow(ctx, w.ID, g))
	}
	sortGateRows(rows)
	a.uiMu.Lock()
	selected, picked := p.gate, p.gatePick
	a.uiMu.Unlock()
	i := slices.IndexFunc(rows, func(r gateRow) bool { return r.ID == selected })
	if i < 0 {
		selected = ""
	}
	now := time.Now()
	if slices.Contains(slots, gatesSidebarSlot) {
		if err := a.push(ctx, p, w.ID, gatesSidebarSlot, gatesSidebar(rows, selected, now)); err != nil {
			return err
		}
	}
	if !slices.Contains(slots, gatesMainSlot) {
		return nil
	}
	return a.push(ctx, p, w.ID, gatesMainSlot, a.gateMainView(ctx, w.ID, rows, repos, selected, picked, now, false))
}

func sortGateRows(rows []gateRow) {
	slices.SortStableFunc(rows, func(x, y gateRow) int {
		return cmp.Or(gateOrder(x.gateConfig, y.gateConfig), cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)))
	})
}

func gateOrder(x, y gateConfig) int {
	return cmp.Or(cmp.Compare(pointOrder(x.Point), pointOrder(y.Point)), cmp.Compare(intoOrder(x.Into), intoOrder(y.Into)))
}

func intoOrder(into string) int {
	switch into {
	case gateIntoDefault:
		return 0
	case gateIntoBase:
		return 1
	case gateIntoAny:
		return 3
	}
	return 2
}

func (a *app) openRepoGate(ctx context.Context, workspaceID, repo string) error {
	gates, _, err := a.workspaceGates(ctx, workspaceID)
	if err != nil {
		return err
	}
	gates = slices.DeleteFunc(gates, func(g gateConfig) bool { return g.Repo != repo })
	if len(gates) == 0 {
		return nil
	}
	rows := make([]gateRow, 0, len(gates))
	for _, g := range gates {
		rows = append(rows, gateRow{gateConfig: g, Name: a.gateMatchName(ctx, g)})
	}
	sortGateRows(rows)
	return a.selectPage(ctx, workspaceID, gatePrefix+rows[0].ID)
}

func (a *app) gateCrumb(ctx context.Context, gateID string) string {
	gates, err := a.gates(ctx)
	if i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.ID == gateID }); err == nil && i >= 0 {
		return " / " + filepath.Base(gates[i].Repo)
	}
	return ""
}

func (a *app) gatePage(ctx context.Context, workspace, gateID, picked string) (sdui.Node, error) {
	gates, repos, err := a.workspaceGates(ctx, workspace)
	if err != nil {
		return sdui.Node{}, err
	}
	var rows []gateRow
	for _, g := range gates {
		rows = append(rows, a.gateRow(ctx, workspace, g))
	}
	sortGateRows(rows)
	return a.gateMainView(ctx, workspace, rows, repos, gateID, picked, time.Now(), true), nil
}

func (a *app) gateMainView(ctx context.Context, workspace string, rows []gateRow, repos []gateRepo, selected, picked string, now time.Time, mac bool) sdui.Node {
	i := slices.IndexFunc(rows, func(r gateRow) bool { return r.ID == selected })
	if i < 0 {
		return gatesHelp(rows)
	}
	r := rows[i]
	repo := repos[slices.IndexFunc(repos, func(x gateRepo) bool { return x.Primary == r.Repo })]
	history := a.gateHistory(ctx, workspace, r.ID)
	sessions := map[string]bool{}
	for _, s := range a.workspaceSessions(workspace) {
		sessions[s.snapshot().ID] = true
	}
	lines := map[string]string{}
	for _, l := range r.Landings {
		if s := a.session(l.Reviewer); s != nil && l.State == gateStateReview {
			lines[l.Reviewer] = s.latestLine()
		}
	}
	asks := map[string]sdui.Node{}
	for _, l := range r.Landings {
		if l.Run == "" {
			continue
		}
		if card, ok := a.graphs.NeedCard(l.Run); ok {
			asks[l.Run] = card
		}
	}
	sc := gateScreen{Row: r, Repo: repo, History: history, Picked: picked, Sessions: sessions, Lines: lines, Asks: asks, Mac: mac, Now: now, Loc: time.Local}
	if info, err := a.graphs.GateGraphInfo(ctx, r.Repo, r.Graph); err == nil {
		sc.Graph = &info
	} else {
		sc.GraphErr = err.Error()
	}
	if r.State == gateStateTrust {
		sc.Pinned = a.pinnedGraph(ctx, r.gateConfig)
	}
	return gateMain(sc)
}

func (a *app) gateSheetTree(p *workspaceUI) (sdui.Node, bool) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.gateSheet == nil {
		return sdui.Node{}, false
	}
	return sdui.VStack(gateSheetView(*p.gateSheet)), true
}

func (a *app) gatesNewCommand(ctx context.Context, cmd pluginsdk.CommandRun, _ struct{}) (any, error) {
	if !a.userWord(cmd) {
		return nil, errGateUser
	}
	if err := a.openGateSheet(ctx, cmd.WorkspaceID, ""); err != nil {
		return nil, err
	}
	return nil, a.render(ctx, cmd.WorkspaceID)
}

func (a *app) openGateSheet(ctx context.Context, workspaceID, edit string) error {
	gates, err := a.gates(ctx)
	if err != nil {
		return err
	}
	d := gateDraft{Rounds: gateRoundsDefault, Graph: gateGraphDefault, Gates: gates}
	if edit != "" {
		i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.ID == edit })
		if i < 0 {
			return errGateUnknown
		}
		g := gates[i]
		d.Edit, d.Point, d.Repo, d.Into, d.Graph, d.Rounds, d.Paused = g.ID, g.Point, g.Repo, g.Into, g.Graph, g.Rounds, g.Paused
		d.Repos = []gateRepo{a.gateRepo(ctx, g.Repo)}
		d.Name = gateNameOf(g.Into, d.branch())
		d.Like = !slices.Contains([]string{gateIntoDefault, gateIntoBase, gateIntoAny}, g.Into)
		if g.Point == gatePointTool {
			d.Name, d.Like = toolGateName(g), false
			d.Call = gateCallOf(g)
			if d.Call == gateCallCustom {
				d.Tool, d.How, d.Text = g.Tool, g.How, g.Text
			}
		}
		if g.Point == gatePointTurn {
			d.Name, d.Like = doneGateName(g.Wrote), false
			d.Sessions, d.WroteMode = slices.Clone(g.Sessions), wroteMode(g.Wrote)
			if d.WroteMode == gateWroteLike {
				d.WroteLike = strings.Join(g.Wrote, ", ")
			}
		}
	} else {
		w, err := a.host.GetWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		d.Repos = a.workspaceRepos(ctx, w)
		if len(d.Repos) > 0 {
			d.Repo = d.Repos[0].Path
		}
		d.Point, d.Into = gatePointLand, d.freeInto()
	}
	d.Choices = a.graphs.GateGraphChoices(ctx, d.primary())
	d.Unhooked = a.sheetUnhooked(ctx, gates, d.primary())
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.gateSheet = &d
	a.uiMu.Unlock()
	return nil
}

func (a *app) gatesAct(ctx context.Context, ev pluginsdk.UIEvent) error {
	if graphs.NeedAction(ev.Action) {
		ev.Slot = graphsMainOld
		_, err := a.graphs.PageEvent(ctx, ev)
		a.kick(ev.WorkspaceID)
		return err
	}
	p := a.workspaceState(ev.WorkspaceID)
	target, fromButton := automation.ActionTarget(ev.NodeID, ev.Action)
	id := target
	if !fromButton && ev.NodeID != gateVerdictsID {
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	}
	id = strings.TrimPrefix(id, gateCardPrefix)
	switch ev.Action {
	case "select":
		var row string
		if err := json.Unmarshal(ev.Value, &row); err != nil {
			return fmt.Errorf("select value: %w", err)
		}
		a.uiMu.Lock()
		defer a.uiMu.Unlock()
		if ev.NodeID == gateVerdictsID {
			p.gatePick = row
			return nil
		}
		if strings.HasPrefix(row, gateRepoPrefix) {
			return nil
		}
		row = strings.TrimPrefix(row, gateTrustPrefix)
		if row == gateHelpID {
			row = ""
		}
		if p.gate != row {
			p.gate, p.gatePick = row, ""
		}
		return nil
	case actGateNew:
		return a.openGateSheet(ctx, ev.WorkspaceID, "")
	case actGateEdit:
		return a.openGateSheet(ctx, ev.WorkspaceID, id)
	case actGatePause, actGateResume:
		paused := ev.Action == actGatePause
		return a.updateGate(ctx, gateUpdateArgs{ID: id, Paused: &paused})
	case actGateTrust:
		gateID, hash, ok := strings.Cut(id, "@")
		if !ok {
			return errTrustStale
		}
		return a.updateGate(ctx, gateUpdateArgs{ID: gateID, Trust: &hash})
	case actGateOpenGraph:
		gates, err := a.gates(ctx)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(gates, func(g gateConfig) bool { return g.ID == id })
		if i < 0 {
			return errGateUnknown
		}
		return a.selectPage(ctx, ev.WorkspaceID, graphPrefix+gates[i].Graph)
	case actGateDelete:
		if err := a.deleteGate(ctx, ev.WorkspaceID, id); err != nil {
			return err
		}
		a.uiMu.Lock()
		if p.gate == id {
			p.gate, p.gatePick = "", ""
		}
		a.uiMu.Unlock()
		return nil
	case actGateReview, actGateLanding:
		sid, err := gateOpens(ev)
		if err != nil {
			return err
		}
		if ev.Action == actGateReview {
			a.uiMu.Lock()
			from := p.selected
			if !strings.HasPrefix(from, gatePrefix) {
				from = ""
				if p.gate != "" {
					from = gatePrefix + p.gate
				}
			}
			a.uiMu.Unlock()
			return a.openReviewer(ctx, ev.WorkspaceID, sid, from)
		}
		return a.openGateSession(ctx, ev.WorkspaceID, sid)
	}
	return nil
}

func gateOpens(ev pluginsdk.UIEvent) (string, error) {
	if sid, fromButton := automation.ActionTarget(ev.NodeID, ev.Action); fromButton {
		return sid, nil
	}
	var row string
	if err := json.Unmarshal(ev.Value, &row); err != nil {
		return "", fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return gateRowSession(row), nil
}

func (a *app) openGateSession(ctx context.Context, workspaceID, sid string) error {
	if _, err := a.lookup(workspaceID, sid); err != nil {
		return err
	}
	if err := a.selectSession(ctx, workspaceID, sid); err != nil {
		return err
	}
	a.setSection(workspaceID, sectionID)
	return a.host.Focus(ctx, workspaceID, sectionID, mainSlot)
}

func (a *app) gateSheetAct(ctx context.Context, ev pluginsdk.UIEvent) error {
	p := a.workspaceState(ev.WorkspaceID)
	a.uiMu.Lock()
	if p.gateSheet == nil {
		a.uiMu.Unlock()
		return nil
	}
	d := *p.gateSheet
	a.uiMu.Unlock()
	d.Err = ""
	switch ev.Action {
	case actGateCancel:
		a.uiMu.Lock()
		p.gateSheet = nil
		a.uiMu.Unlock()
		return nil
	case actGateOpenTaken:
		target, ok := automation.ActionTarget(ev.NodeID, ev.Action)
		id, cut := strings.CutPrefix(target, gateSheetID+"/")
		if !ok || !cut || id == "" {
			return fmt.Errorf("%s %q: %w", ev.Action, ev.NodeID, errGateUnknown)
		}
		a.uiMu.Lock()
		p.gateSheet, p.gate, p.gatePick = nil, id, ""
		a.uiMu.Unlock()
		return a.selectPage(ctx, ev.WorkspaceID, gatePrefix+id)
	case actGateRepo:
		if err := json.Unmarshal(ev.Value, &d.Repo); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		d.Choices = a.graphs.GateGraphChoices(ctx, d.primary())
		if _, ok := d.choice(); !ok {
			d.Graph = gateGraphDefault
		}
		d.Unhooked = a.sheetUnhooked(ctx, d.Gates, d.primary())
	case actGateHolds:
		var v string
		if err := json.Unmarshal(ev.Value, &v); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		if d.Edit != "" || v == d.Point {
			break
		}
		d.Point, d.Like = v, false
		switch v {
		case gatePointTurn:
			d.Into, d.Sessions, d.WroteMode, d.WroteLike = "", slices.Clone(doneSessionsDefault), gateWrotePlans, ""
		case gatePointTool:
			d.Into, d.Tool, d.How, d.Text = "", "", "", ""
			d.Call = d.freeCall()
		default:
			d.Point, d.Into = gatePointLand, d.freeInto()
		}
	case actGateSession:
		var on bool
		if err := json.Unmarshal(ev.Value, &on); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		kind := strings.TrimPrefix(ev.NodeID, gateSheetID+"/session/")
		d.Sessions = slices.DeleteFunc(slices.Clone(d.Sessions), func(k string) bool { return k == kind })
		if on {
			d.Sessions = append(d.Sessions, kind)
			slices.Sort(d.Sessions)
		}
	case actGateWrote:
		if err := json.Unmarshal(ev.Value, &d.WroteMode); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGateWroteLike:
		if err := json.Unmarshal(ev.Value, &d.WroteLike); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGateInto:
		var v string
		if err := json.Unmarshal(ev.Value, &v); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		d.Like = v == gateIntoLike
		if d.Like {
			v = ""
		}
		d.Into = v
	case actGateLike:
		if err := json.Unmarshal(ev.Value, &d.Into); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		d.Into = strings.TrimSpace(d.Into)
	case actGateCall:
		if err := json.Unmarshal(ev.Value, &d.Call); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGateTool:
		if err := json.Unmarshal(ev.Value, &d.Tool); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		d.How = toolHowAfter(d.customTool(), d.How)
	case actGateHow:
		if err := json.Unmarshal(ev.Value, &d.How); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGateText:
		if err := json.Unmarshal(ev.Value, &d.Text); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGatePick:
		if err := json.Unmarshal(ev.Value, &d.Graph); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGateRounds:
		var v string
		if err := json.Unmarshal(ev.Value, &v); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		d.Rounds = n
	case actGatePaused:
		if err := json.Unmarshal(ev.Value, &d.Paused); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	case actGateSave:
		gateID, err := a.saveGateDraft(ctx, d)
		if err != nil {
			d.Err = err.Error()
			break
		}
		a.uiMu.Lock()
		p.gateSheet, p.gate, p.gatePick = nil, gateID, ""
		a.uiMu.Unlock()
		return nil
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if p.gateSheet != nil {
		p.gateSheet = &d
	}
	return nil
}

func (a *app) saveGateDraft(ctx context.Context, d gateDraft) (string, error) {
	if d.Edit == "" {
		args := gateAddArgs{Repo: d.Repo, Point: cmp.Or(d.Point, gatePointLand), Into: d.Into, Graph: d.Graph, Rounds: d.Rounds}
		switch d.Point {
		case gatePointTurn:
			args.Sessions, args.Wrote = d.Sessions, d.wrote()
		case gatePointTool:
			t, err := d.toolConfig()
			if err != nil {
				return "", err
			}
			args.Tool, args.How, args.Text = t.Tool, t.How, t.Text
		}
		g, err := a.addGate(ctx, args)
		if err == nil {
			a.gatesChanged()
		}
		return g.ID, err
	}
	args := gateUpdateArgs{ID: d.Edit, Paused: &d.Paused, Rounds: &d.Rounds, Graph: &d.Graph}
	if d.Point == gatePointTool {
		t, err := d.toolConfig()
		if err != nil {
			return d.Edit, err
		}
		if i := slices.IndexFunc(d.Gates, func(g gateConfig) bool { return g.ID == d.Edit }); i < 0 || !sameTool(d.Gates[i], t) {
			args.Tool, args.How, args.Text = &t.Tool, &t.How, &t.Text
		}
	} else if d.Point != gatePointTurn {
		args.Into = &d.Into
	} else if i := slices.IndexFunc(d.Gates, func(g gateConfig) bool { return g.ID == d.Edit }); i < 0 || !sameDone(d.Gates[i], gateConfig{Sessions: slices.Sorted(slices.Values(d.Sessions)), Wrote: d.wrote()}) {
		wrote := d.wrote()
		args.Sessions, args.Wrote = &d.Sessions, &wrote
	}
	return d.Edit, a.updateGate(ctx, args)
}

func gateName(g gateConfig) string {
	return filepath.Base(g.Repo)
}

func (a *app) sheetUnhooked(ctx context.Context, gates []gateConfig, primary string) int {
	if primary == "" {
		return 0
	}
	if slices.Contains(toolGatedRepos(gates), primary) {
		return a.unhooked(primary)
	}
	n := 0
	for _, s := range a.allSessions() {
		rec, live, _, _ := s.hookView()
		if !live || rec.closed() || !hookable(rec) || !s.working() || !a.hooksLater(s) {
			continue
		}
		if p, err := a.hookPrimary(ctx, rec.Worktree); err == nil && p == primary {
			n++
		}
	}
	return n
}
