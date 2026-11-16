package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	keyManual   = "manual"
	keyHolds    = "holds"
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
	last   string
	seeded time.Time
}

type agentSession struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Active bool   `json:"active"`
}

type setArgs struct {
	On      bool `json:"on"`
	Minutes int  `json:"minutes"`
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
	Agents  *bool `json:"agents"`
	Battery *bool `json:"battery"`
	Display *bool `json:"display"`
}

func main() {
	a := &awake{k: newKeeper(assertIOKit, time.Now, pidAlive)}
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
		a.ticks(ctx)
	}()
	return nil
}

func (a *awake) start(ctx context.Context) {
	a.seedAll(ctx)
	a.mu.Lock()
	a.k.prune()
	a.k.apply()
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
	_, err = a.h.StorageGet(ctx, keyManual, &a.k.manual)
	logError("load manual", err)
	_, err = a.h.StorageGet(ctx, keyHolds, &a.k.holds)
	logError("load holds", err)
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
	reseed := a.k.settings.Agents && a.k.now().Sub(a.seeded) >= reseedEvery
	a.mu.Unlock()
	if reseed {
		a.seedAll(ctx)
	}
	a.mu.Lock()
	a.k.prune()
	a.k.apply()
	a.mu.Unlock()
	a.changed(ctx)
}

func (a *awake) shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.k.holder.release()
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
		return a.setManual(ctx, args.On, args.Minutes), nil
	case "awake.on":
		return a.setManual(ctx, true, 0), nil
	case "awake.off":
		return a.setManual(ctx, false, 0), nil
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

func (a *awake) setManual(ctx context.Context, on bool, minutes int) stateView {
	a.mu.Lock()
	a.k.dirty = true
	a.k.manual = manualHold{}
	if on {
		a.k.manual.On = true
		if minutes > 0 {
			a.k.manual.Until = a.k.now().Add(time.Duration(minutes) * time.Minute).Unix()
		}
	}
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
	if args.Agents != nil {
		a.k.settings.Agents = *args.Agents
	}
	if args.Battery != nil {
		a.k.settings.Battery = *args.Battery
	}
	if args.Display != nil {
		a.k.settings.Display = *args.Display
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
		}
	}
	a.k.active[workspace] = n
	a.k.keepSessions(workspace, open)
}

func (a *awake) uiEvent(ctx context.Context, ev pluginsdk.UIEvent) (any, error) {
	if ev.Slot != settingsSlot {
		return nil, fmt.Errorf("unknown slot %q", ev.Slot)
	}
	var on bool
	if err := json.Unmarshal(ev.Value, &on); err != nil {
		return nil, fmt.Errorf("%s: %w", ev.Action, err)
	}
	switch ev.Action {
	case "manual":
		return a.setManual(ctx, on, 0), nil
	case "agents":
		return a.setSettings(ctx, settingsArgs{Agents: &on}), nil
	case "battery":
		return a.setSettings(ctx, settingsArgs{Battery: &on}), nil
	case "display":
		return a.setSettings(ctx, settingsArgs{Display: &on}), nil
	}
	return nil, fmt.Errorf("unknown action %q", ev.Action)
}

func (a *awake) changed(ctx context.Context) stateView {
	a.pubMu.Lock()
	defer a.pubMu.Unlock()
	a.mu.Lock()
	v := a.k.view()
	s, m, holds := a.k.settings, a.k.manual, append([]agentHold{}, a.k.holds...)
	persist := a.k.dirty
	a.k.dirty = false
	raw, _ := json.Marshal(v)
	same := string(raw) == a.last
	a.last = string(raw)
	a.mu.Unlock()
	if persist {
		logError("store settings", a.h.StorageSet(ctx, keySettings, s))
		logError("store manual", a.h.StorageSet(ctx, keyManual, m))
		logError("store holds", a.h.StorageSet(ctx, keyHolds, holds))
	}
	if same {
		return v
	}
	_, err := a.h.Publish(ctx, changedTopic, v)
	logError("publish "+changedTopic, err)
	logError("render settings", a.render(ctx, v))
	return v
}

func (a *awake) render(ctx context.Context, v stateView) error {
	_, err := a.h.Render(ctx, "", settingsSlot, sdui.VStack(
		sdui.SettingRow("Keep the Mac awake", strings.TrimSuffix(v.Detail, " "+lidNote), sdui.WithAction(sdui.Toggle("manual", v.Manual), "manual")),
		sdui.SettingRow("While agents work", "Any agent session running or waiting for you keeps the Mac awake.", sdui.WithAction(sdui.Toggle("agents", v.Agents), "agents")),
		sdui.SettingRow("On battery too", "Off: agents keep the Mac awake only on power, so a question left overnight doesn't drain the battery. Your own hold and agents' holds work on battery anyway.", sdui.WithAction(sdui.Toggle("battery", v.Battery), "battery")),
		sdui.SettingRow("Keep the screen on", "Off: the screen sleeps as usual while the Mac stays awake.", sdui.WithAction(sdui.Toggle("display", v.Display), "display")),
		sdui.MutedText("A closed lid still sleeps a laptop on battery; on power it stays awake with the lid closed only with an external display, keyboard and mouse."),
	))
	return err
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
