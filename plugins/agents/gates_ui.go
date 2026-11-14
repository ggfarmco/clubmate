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
	"clubmate/plugins/internal/automation"
)

var gateSheetActions = []string{actGateRepo, actGateRounds, actGatePaused, actGateCancel, actGateSave}

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
	slices.SortStableFunc(rows, func(x, y gateRow) int { return cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
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
	main := gatesHelp(rows)
	if i >= 0 {
		r := rows[i]
		repo := repos[slices.IndexFunc(repos, func(x gateRepo) bool { return x.Primary == r.Repo })]
		history := a.gateHistory(ctx, w.ID, r.ID)
		sessions := map[string]bool{}
		for _, s := range a.workspaceSessions(w.ID) {
			sessions[s.snapshot().ID] = true
		}
		main = gateMain(gateScreen{Row: r, Repo: repo, History: history, Picked: picked, Sessions: sessions, Now: now, Loc: time.Local})
	}
	return a.push(ctx, p, w.ID, gatesMainSlot, main)
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
	d := gateDraft{Rounds: gateRoundsDefault, Gated: map[string]bool{}}
	for _, g := range gates {
		d.Gated[g.Repo] = true
		if g.ID == edit {
			d.Edit, d.Name, d.Repo, d.Rounds, d.Paused = g.ID, gateName(g), g.Repo, g.Rounds, g.Paused
		}
	}
	if edit != "" && d.Edit == "" {
		return errGateUnknown
	}
	if edit == "" {
		w, err := a.host.GetWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		d.Repos = a.workspaceRepos(ctx, w)
		if i := slices.IndexFunc(d.Repos, func(r gateRepo) bool { return !d.Gated[r.Primary] }); i >= 0 {
			d.Repo = d.Repos[i].Path
		} else if len(d.Repos) > 0 {
			d.Repo = d.Repos[0].Path
		}
	}
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.gateSheet = &d
	a.uiMu.Unlock()
	return nil
}

func (a *app) gatesAct(ctx context.Context, ev pluginsdk.UIEvent) error {
	p := a.workspaceState(ev.WorkspaceID)
	target, fromButton := automation.ActionTarget(ev.NodeID, ev.Action)
	id := target
	if !fromButton && ev.NodeID != gateVerdictsID {
		if err := json.Unmarshal(ev.Value, &id); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
	}
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
	case actGateDelete:
		if err := a.deleteGate(ctx, id); err != nil {
			return err
		}
		a.uiMu.Lock()
		if p.gate == id {
			p.gate, p.gatePick = "", ""
		}
		a.uiMu.Unlock()
		return nil
	case actGateReview, actGateLanding:
		var row string
		if err := json.Unmarshal(ev.Value, &row); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		return a.openGateSession(ctx, ev.WorkspaceID, gateRowSession(row))
	}
	return nil
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
	case actGateRepo:
		if err := json.Unmarshal(ev.Value, &d.Repo); err != nil {
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
		g, err := a.addGate(ctx, gateAddArgs{Repo: d.Repo, Kind: gateKindReview, Rounds: d.Rounds})
		if err == nil {
			a.gatesChanged()
		}
		return g.ID, err
	}
	return d.Edit, a.updateGate(ctx, gateUpdateArgs{ID: d.Edit, Paused: &d.Paused, Rounds: &d.Rounds})
}

func gateName(g gateConfig) string {
	return filepath.Base(g.Repo)
}
