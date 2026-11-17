package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clubmate/pkg/contract"
	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/api"
)

const (
	changedTopic = "awake.changed"
	settingsSlot = "awake.settings"
	holdPrefix   = "ah"
	tickEvery    = 5 * time.Second
	reseedEvery  = time.Minute
	maxReason    = 80

	keySettings = "settings"
	keyChoice   = "choice"
	keyHolds    = "holds"
	keyLid      = "lid"
)

type host interface {
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
	Publish(ctx context.Context, topic string, payload any) (pluginsdk.Event, error)
	Render(ctx context.Context, workspaceID, slot string, n sdui.Node) (int64, error)
	RunCommand(ctx context.Context, id, workspaceID string, args any, out any) error
	ListWorkspaces(ctx context.Context) ([]pluginsdk.Workspace, error)
}

type awake struct {
	h      host
	mu     sync.Mutex
	pubMu  sync.Mutex
	k      *keeper
	power  func() power
	last   string
	seeded time.Time
}

type agentSession struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Active bool   `json:"active"`
}

type setArgs struct {
	Mode  string `json:"mode"`
	Hours int    `json:"hours"`
}

type holdArgs struct {
	Reason  string `json:"reason"`
	Minutes int    `json:"minutes"`
	PID     int    `json:"pid"`
}

type holdResult struct {
	Hold  string `json:"hold"`
	Until int64  `json:"until"`
}

type releaseArgs struct {
	Hold string `json:"hold"`
}

type settingsArgs struct {
	Battery *bool `json:"battery"`
	Display *bool `json:"display"`
	Lid     *bool `json:"lid"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == guardArg {
		runGuard(newClamshell())
		return
	}
	a := &awake{k: newKeeper(assertIOKit, newClamshell(), time.Now, pidAlive), power: readPower}
	pluginsdk.Main(pluginsdk.Plugin{
		Init: a.init,
		Command: func(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
			return a.command(ctx, cmd)
		},
		BusEvent: func(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) { a.busEvent(ctx, ev) },
		UIEvent: func(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
			return a.uiEvent(ctx, ev)
		},
		Shutdown: func(context.Context, *pluginsdk.Host) { a.shutdown() },
	})
}

func (a *awake) init(ctx context.Context, h *pluginsdk.Host) error {
	a.h = h
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"agents.*", "workspace.*"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	a.load(ctx)
	go func() {
		ctx := context.WithoutCancel(ctx)
		a.start(ctx)
		go watchPower(func() { a.tick(ctx) })
		a.ticks(ctx)
	}()
	return nil
}

func (a *awake) start(ctx context.Context) {
	a.seedAll(ctx)
	a.mu.Lock()
	a.k.power = a.power()
	a.k.prune()
	a.k.refresh()
	a.mu.Unlock()
	a.changed(ctx)
}

func (a *awake) load(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var s settings
	found, err := a.h.StorageGet(ctx, keySettings, &s)
	logError("load settings", err)
	if found && err == nil {
		a.k.settings = s
	}
	_, err = a.h.StorageGet(ctx, keyChoice, &a.k.choice)
	logError("load choice", err)
	_, err = a.h.StorageGet(ctx, keyHolds, &a.k.holds)
	logError("load holds", err)
	_, err = a.h.StorageGet(ctx, keyLid, &a.k.lidOn)
	logError("load lid", err)
}

func (a *awake) ticks(ctx context.Context) {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.tick(ctx)
		}
	}
}

func (a *awake) tick(ctx context.Context) {
	a.mu.Lock()
	reseed := a.k.choice.Mode == modeAgents && a.k.now().Sub(a.seeded) >= reseedEvery
	a.mu.Unlock()
	if reseed {
		a.seedAll(ctx)
	}
	p := a.power()
	a.mu.Lock()
	a.k.power = p
	a.k.prune()
	a.k.refresh()
	a.mu.Unlock()
	a.changed(ctx)
}

func (a *awake) shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.k.stop()
}

func (a *awake) command(ctx context.Context, cmd pluginsdk.CommandRun) (any, error) {
	switch cmd.ID {
	case "awake.state":
		return a.state(), nil
	case "awake.set":
		var args setArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return a.choose(ctx, args.Mode, args.Hours)
	case "awake.on":
		return a.choose(ctx, modeOn, 0)
	case "awake.agents":
		return a.choose(ctx, modeAgents, 0)
	case "awake.off":
		return a.choose(ctx, modeOff, 0)
	case "awake.toggle":
		return a.toggle(ctx), nil
	case "awake.hold":
		var args holdArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return a.hold(ctx, cmd.WorkspaceID, cmd.Caller, args)
	case "awake.release":
		var args releaseArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.release(ctx, cmd.WorkspaceID, args.Hold)
	case "awake.settings-set":
		var args settingsArgs
		if err := decode(cmd.Args, &args); err != nil {
			return nil, err
		}
		return a.setSettings(ctx, args), nil
	}
	return nil, fmt.Errorf("unknown command %q", cmd.ID)
}

func (a *awake) state() stateView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.k.view()
}

func (a *awake) choose(ctx context.Context, mode string, hours int) (stateView, error) {
	a.mu.Lock()
	err := a.k.choose(mode, hours)
	a.k.apply()
	a.mu.Unlock()
	if err != nil {
		return stateView{}, err
	}
	return a.changed(ctx), nil
}

func (a *awake) toggle(ctx context.Context) stateView {
	a.mu.Lock()
	mode := modeOn
	if a.k.choice.Mode != modeOff {
		mode = modeOff
	}
	_ = a.k.choose(mode, 0)
	a.k.apply()
	a.mu.Unlock()
	return a.changed(ctx)
}

func (a *awake) hold(ctx context.Context, workspace, caller string, args holdArgs) (holdResult, error) {
	reason := strings.TrimSpace(args.Reason)
	if reason == "" {
		return holdResult{}, errors.New("reason is required")
	}
	if utf8.RuneCountInString(reason) > maxReason {
		return holdResult{}, fmt.Errorf("reason is longer than %d characters", maxReason)
	}
	minutes := args.Minutes
	if minutes <= 0 {
		minutes = defaultHoldMinutes
	}
	minutes = min(minutes, maxHoldMinutes)
	a.mu.Lock()
	if args.PID > 0 && !a.k.alive(args.PID) {
		a.mu.Unlock()
		return holdResult{}, fmt.Errorf("process %d is not running", args.PID)
	}
	h := agentHold{
		ID:        id.New(holdPrefix),
		Reason:    reason,
		Until:     a.k.now().Add(time.Duration(minutes) * time.Minute).Unix(),
		PID:       args.PID,
		Session:   caller,
		Workspace: workspace,
	}
	a.k.holds = append(a.k.holds, h)
	a.k.dirty = true
	a.k.apply()
	a.mu.Unlock()
	a.changed(ctx)
	return holdResult{Hold: h.ID, Until: h.Until}, nil
}

func (a *awake) release(ctx context.Context, workspace, hold string) error {
	a.mu.Lock()
	found := false
	for i, h := range a.k.holds {
		if h.ID == hold && (workspace == "" || h.Workspace == workspace) {
			a.k.holds = append(a.k.holds[:i], a.k.holds[i+1:]...)
			found, a.k.dirty = true, true
			break
		}
	}
	a.k.apply()
	a.mu.Unlock()
	if !found {
		return fmt.Errorf("unknown hold %q", hold)
	}
	a.changed(ctx)
	return nil
}

func (a *awake) setSettings(ctx context.Context, args settingsArgs) stateView {
	a.mu.Lock()
	a.k.dirty = true
	if args.Battery != nil {
		a.k.settings.Battery = *args.Battery
	}
	if args.Display != nil {
		a.k.settings.Display = *args.Display
	}
	if args.Lid != nil {
		a.k.settings.Lid = *args.Lid
	}
	a.k.apply()
	a.mu.Unlock()
	return a.changed(ctx)
}

func (a *awake) busEvent(ctx context.Context, ev pluginsdk.Event) {
	switch ev.Topic {
	case api.TopicReady:
		a.seedAll(ctx)
	case api.TopicChanged:
		var p api.Changed
		if err := json.Unmarshal(ev.Payload, &p); err != nil || p.Workspace == "" {
			return
		}
		switch p.Kind {
		case api.ChangeCreated, api.ChangeStatus, api.ChangeDeleted:
		default:
			return
		}
		if p.Session != "" && (p.Status == api.StatusClosed || p.Kind == api.ChangeDeleted) {
			a.mu.Lock()
			a.k.dropSession(p.Session)
			a.k.apply()
			a.mu.Unlock()
		}
		a.seed(ctx, p.Workspace)
	case contract.TopicWorkspaceDeleted:
		var p contract.WorkspaceDeleted
		if err := json.Unmarshal(ev.Payload, &p); err != nil || p.Workspace == "" {
			return
		}
		a.mu.Lock()
		a.k.dropWorkspace(p.Workspace)
		a.k.apply()
		a.mu.Unlock()
	default:
		return
	}
	a.mu.Lock()
	a.k.apply()
	a.mu.Unlock()
	a.changed(ctx)
}

func (a *awake) seedAll(ctx context.Context) {
	list, err := a.h.ListWorkspaces(ctx)
	if err != nil {
		logError("list workspaces", err)
		return
	}
	a.mu.Lock()
	a.seeded = a.k.now()
	a.mu.Unlock()
	known := make(map[string]bool, len(list))
	for _, w := range list {
		known[w.ID] = true
		a.seed(ctx, w.ID)
	}
	a.mu.Lock()
	for ws := range a.k.active {
		if !known[ws] {
			a.k.dropWorkspace(ws)
		}
	}
	a.k.apply()
	a.mu.Unlock()
}

func (a *awake) seed(ctx context.Context, workspace string) {
	var list []agentSession
	err := a.h.RunCommand(ctx, "agents.sessions", workspace, struct{}{}, &list)
	a.mu.Lock()
	defer a.mu.Unlock()
	defer a.k.apply()
	if err != nil {
		delete(a.k.active, workspace)
		return
	}
	n := 0
	open := make(map[string]bool, len(list))
	for _, s := range list {
		if s.Active {
			n++
		}
		if s.Status != api.StatusClosed {
			open[s.ID] = true
			a.k.titles[s.ID] = s.Title
		}
	}
	a.k.active[workspace] = n
	a.k.keepSessions(workspace, open)
}

func (a *awake) uiEvent(ctx context.Context, ev pluginsdk.UIEvent) (any, error) {
	if ev.Slot != settingsSlot {
		return nil, fmt.Errorf("unknown slot %q", ev.Slot)
	}
	switch ev.Action {
	case "choice":
		var mode string
		if err := json.Unmarshal(ev.Value, &mode); err != nil {
			return nil, fmt.Errorf("choice: %w", err)
		}
		hours := 0
		if mode == modeHours {
			a.mu.Lock()
			hours = a.k.lastHours()
			a.mu.Unlock()
		}
		return a.choose(ctx, mode, hours)
	case "hours":
		var n json.Number
		if err := json.Unmarshal(ev.Value, &n); err != nil {
			return nil, fmt.Errorf("hours: %w", err)
		}
		hours, err := strconv.Atoi(n.String())
		if err != nil {
			return nil, fmt.Errorf("hours: %w", err)
		}
		return a.choose(ctx, modeHours, hours)
	case actionBattery, actionDisplay, actionLid:
		var on bool
		if err := json.Unmarshal(ev.Value, &on); err != nil {
			return nil, fmt.Errorf("%s: %w", ev.Action, err)
		}
		switch ev.Action {
		case actionBattery:
			return a.setSettings(ctx, settingsArgs{Battery: &on}), nil
		case actionDisplay:
			return a.setSettings(ctx, settingsArgs{Display: &on}), nil
		}
		return a.setSettings(ctx, settingsArgs{Lid: &on}), nil
	}
	return nil, fmt.Errorf("unknown action %q", ev.Action)
}

func (a *awake) changed(ctx context.Context) stateView {
	a.pubMu.Lock()
	defer a.pubMu.Unlock()
	a.mu.Lock()
	v := a.k.view()
	s, m, holds, lid := a.k.settings, a.k.choice, append([]agentHold{}, a.k.holds...), a.k.lidOn
	persist := a.k.dirty
	a.k.dirty = false
	raw, _ := json.Marshal(v)
	same := string(raw) == a.last
	a.last = string(raw)
	a.mu.Unlock()
	if persist {
		logError("store settings", a.h.StorageSet(ctx, keySettings, s))
		logError("store choice", a.h.StorageSet(ctx, keyChoice, m))
		logError("store holds", a.h.StorageSet(ctx, keyHolds, holds))
		logError("store lid", a.h.StorageSet(ctx, keyLid, lid))
	}
	if same {
		return v
	}
	_, err := a.h.Publish(ctx, changedTopic, v)
	logError("publish "+changedTopic, err)
	logError("render settings", a.render(ctx, v))
	return v
}

func decode(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid args: %w", err)
	}
	return nil
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "awake: %s: %v\n", what, err)
	}
}
