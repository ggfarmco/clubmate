package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	maxNumbered = 9
	previousKey = "previous/"

	actJump  = "jump"
	actLast  = "last"
	actNeeds = "needs"
)

var (
	errNoPrevious = errors.New("no previous session")
	errNoNeeds    = errors.New("nothing needs you")
)

type jumpArgs struct {
	N int `json:"n"`
}

func numberable(row sessionRow) bool {
	return row.Nest == nil && row.Rec.Run == nil && !row.Rec.isAside() && !row.Rec.restricted()
}

func numberedSet(roots []sessionRow) map[string]bool {
	set := map[string]bool{}
	for _, row := range roots {
		if numberable(row) {
			set[row.Rec.ID] = true
		}
	}
	return set
}

func numberRows(items []sdui.TreeItem, numbered map[string]bool) {
	n := 0
	runs := map[string]bool{}
	var walk func([]sdui.TreeItem)
	walk = func(items []sdui.TreeItem) {
		for i := range items {
			if n == maxNumbered {
				return
			}
			switch id := items[i].ID; {
			case strings.HasPrefix(id, groupPrefix):
				walk(items[i].Children)
			case strings.HasPrefix(id, runPrefix) && items[i].Open == openRun:
				if run := runOf(id); !runs[run] {
					runs[run] = true
					n++
					items[i].Number = n
				}
			case numbered[id]:
				n++
				items[i].Number = n
			}
		}
	}
	walk(items)
}

func sentTree(sidebar []byte) ([]sdui.TreeItem, string, bool) {
	var root sdui.Node
	if json.Unmarshal(sidebar, &root) != nil {
		return nil, "", false
	}
	tree, ok := findTree(root)
	if !ok {
		return nil, "", false
	}
	raw, err := json.Marshal(tree.Props["items"])
	if err != nil {
		return nil, "", false
	}
	var items []sdui.TreeItem
	if json.Unmarshal(raw, &items) != nil {
		return nil, "", false
	}
	selected, _ := tree.Props["selected"].(string)
	return items, selected, true
}

func numberedRow(items []sdui.TreeItem, n int) string {
	for _, item := range items {
		if item.Number == n {
			return item.ID
		}
		if strings.HasPrefix(item.ID, groupPrefix) {
			if id := numberedRow(item.Children, n); id != "" {
				return id
			}
		}
	}
	return ""
}

func needsOrder(infos []sessionInfo) []int {
	var order []int
	for i, info := range infos {
		if info.NeedsYou && !info.Hidden && (info.Kind == "" || info.Kind == kindRun) {
			order = append(order, i)
		}
	}
	since := func(i int) time.Time {
		if t := infos[i].NeedsYouSince; t != nil {
			return *t
		}
		return time.Time{}
	}
	slices.SortStableFunc(order, func(x, y int) int {
		return cmp.Or(compareSince(since(x), since(y)), cmp.Compare(infos[x].ID, infos[y].ID))
	})
	return order
}

func setNeedsRanks(infos []sessionInfo) {
	for rank, i := range needsOrder(infos) {
		infos[i].NeedsRank = rank + 1
	}
}

func needsIDs(infos []sessionInfo) []string {
	order := needsOrder(infos)
	ids := make([]string, 0, len(order))
	for _, i := range order {
		id := infos[i].ID
		if infos[i].Kind == kindRun {
			id = runPrefix + id
		}
		ids = append(ids, id)
	}
	return ids
}

func stepNeeds(ids []string, selected string, by int) string {
	if len(ids) == 0 || by == 0 {
		return ""
	}
	at := slices.Index(ids, selected)
	if at < 0 {
		if by > 0 {
			return ids[0]
		}
		return ids[len(ids)-1]
	}
	return ids[((at+by)%len(ids)+len(ids))%len(ids)]
}

func (a *app) jumpCommand(ctx context.Context, cmd pluginsdk.CommandRun, args jumpArgs) (any, error) {
	if a.bySession(cmd) {
		return nil, errUserAction
	}
	return nil, a.jump(ctx, cmd.WorkspaceID, args.N)
}

func (a *app) lastCommand(ctx context.Context, cmd pluginsdk.CommandRun) (any, error) {
	if a.bySession(cmd) {
		return nil, errUserAction
	}
	return nil, a.last(ctx, cmd.WorkspaceID)
}

func (a *app) needsCommand(ctx context.Context, cmd pluginsdk.CommandRun, args stepArgs) (any, error) {
	if a.bySession(cmd) {
		return nil, errUserAction
	}
	return nil, a.nextNeeds(ctx, cmd.WorkspaceID, args.By)
}

func (a *app) jumpEvent(ctx context.Context, ev pluginsdk.UIEvent) error {
	var n float64
	if ev.Action != actLast && json.Unmarshal(ev.Value, &n) != nil {
		return nil
	}
	switch ev.Action {
	case actJump:
		return a.jump(ctx, ev.WorkspaceID, int(n))
	case actNeeds:
		return a.nextNeeds(ctx, ev.WorkspaceID, int(n))
	}
	return a.last(ctx, ev.WorkspaceID)
}

func (a *app) jump(ctx context.Context, workspaceID string, n int) error {
	p := a.workspaceState(workspaceID)
	p.render.Lock()
	raw := p.sent[sidebarSlot]
	p.render.Unlock()
	items, _, ok := sentTree(raw)
	if !ok || n < 1 || n > maxNumbered {
		return nil
	}
	id := numberedRow(items, n)
	if id == "" {
		return nil
	}
	return a.open(ctx, workspaceID, id)
}

func (a *app) open(ctx context.Context, workspaceID, id string) error {
	if !pageID(id) {
		return a.reveal(ctx, workspaceID, id)
	}
	a.setSection(workspaceID, sectionID)
	if err := a.selectPage(ctx, workspaceID, id); err != nil {
		return err
	}
	a.kick(workspaceID)
	return a.host.Focus(ctx, workspaceID, sectionID, mainSlot)
}

func (a *app) nextNeeds(ctx context.Context, workspaceID string, by int) error {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	selected := p.selected
	a.uiMu.Unlock()
	next := stepNeeds(needsIDs(a.infos(workspaceID)), selected, by)
	if next == "" {
		return errNoNeeds
	}
	return a.open(ctx, workspaceID, next)
}

func (a *app) last(ctx context.Context, workspaceID string) error {
	prev, err := a.previous(ctx, workspaceID)
	if err != nil {
		return err
	}
	if !a.reachable(ctx, workspaceID, prev) {
		return errNoPrevious
	}
	return a.open(ctx, workspaceID, prev)
}

func (a *app) reachable(ctx context.Context, workspaceID, id string) bool {
	if id == "" {
		return false
	}
	if pageID(id) {
		return !a.deadPage(ctx, workspaceID, id)
	}
	s := a.session(id)
	if s == nil {
		return false
	}
	rec := s.snapshot()
	return rec.Workspace == workspaceID && !rec.closed()
}

func (a *app) previous(ctx context.Context, workspaceID string) (string, error) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	loaded, prev := p.prevSet, p.previous
	a.uiMu.Unlock()
	if loaded {
		return prev, nil
	}
	if _, err := a.host.StorageGet(ctx, previousKey+workspaceID, &prev); err != nil {
		return "", err
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.prevSet {
		p.previous, p.prevSet = prev, true
	}
	return p.previous, nil
}

func (a *app) remember(ctx context.Context, workspaceID, sid string) error {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	restored, old := p.restored, p.selected
	a.uiMu.Unlock()
	if !restored {
		if _, err := a.host.StorageGet(ctx, selectedKey+workspaceID, &old); err != nil {
			return err
		}
	}
	if old == "" || old == sid {
		return nil
	}
	a.uiMu.Lock()
	p.previous, p.prevSet = old, true
	a.uiMu.Unlock()
	return a.host.StorageSet(ctx, previousKey+workspaceID, old)
}
