package rpc

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/schema"
	"clubmate/internal/sdui"
	"clubmate/internal/workspace"
)

const (
	notifyUIUpdated = "ui.updated"
	notifyUIFocus   = "ui.focus"
)

var (
	errNoUI           = errors.New("ui not configured")
	errUnknownSection = errors.New("unknown section")
	errUnknownCommand = errors.New("unknown command")
)

type Router interface {
	UIEvent(ctx context.Context, pluginID string, ev json.RawMessage) (json.RawMessage, error)
	Command(ctx context.Context, c Command, run json.RawMessage) (json.RawMessage, error)
	Commands() []Command
	Resolve(id string) (Command, bool)
	Capabilities() map[string]string
	Pprof(ctx context.Context, pluginID string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error)
}

type CommandDef struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Hidden      bool           `json:"hidden"`
	Confirm     string         `json:"confirm"`
	Args        *schema.Object `json:"args,omitempty"`
	Result      *schema.Object `json:"result,omitempty"`
	Agent       bool           `json:"agent"`
	Remote      bool           `json:"remote"`
	Scope       string         `json:"scope,omitempty"`
	Destructive bool           `json:"destructive"`
	Provides    []string       `json:"provides,omitempty"`
	Handles     []string       `json:"handles,omitempty"`
	Timeout     int            `json:"timeout,omitempty"`
}

type Command struct {
	CommandDef
	Plugin  string `json:"plugin"`
	Running bool   `json:"running"`
}

type uiState struct {
	store    *sdui.Store
	sections *sdui.Sections
	router   Router
}

type workspaceParams struct {
	WorkspaceID string `json:"workspaceId"`
}

type uiSubscribeParams struct {
	WorkspaceID string `json:"workspaceId"`
	Delta       bool   `json:"delta,omitempty"`
	Append      bool   `json:"append,omitempty"`
	Keep        bool   `json:"keep,omitempty"`
}

type renderParams struct {
	WorkspaceID string          `json:"workspaceId"`
	Slot        string          `json:"slot"`
	Tree        json.RawMessage `json:"tree"`
}

type focusParams struct {
	WorkspaceID string `json:"workspaceId"`
	Section     string `json:"section"`
	Slot        string `json:"slot"`
	Field       bool   `json:"field"`
}

type uiFocusParams struct {
	Subscription string `json:"subscription"`
	Section      string `json:"section"`
	Slot         string `json:"slot"`
	Field        bool   `json:"field,omitempty"`
}

type deliveredResult struct {
	Delivered int `json:"delivered"`
}

type revResult struct {
	Rev int64 `json:"rev"`
}

type uiEventParams struct {
	WorkspaceID string          `json:"workspaceId"`
	Slot        string          `json:"slot"`
	NodeID      string          `json:"nodeId"`
	Action      string          `json:"action"`
	Value       json.RawMessage `json:"value"`
	Agent       bool            `json:"agent,omitempty"`
}

type capabilitiesResult struct {
	Capabilities map[string]string `json:"capabilities"`
}

type commandRunParams struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	Args        json.RawMessage `json:"args"`
	Agent       bool            `json:"agent,omitempty"`
	Caller      string          `json:"caller,omitempty"`
	Terminal    string          `json:"terminal,omitempty"`
	Via         string          `json:"via,omitempty"`
}

func (s *Server) SetUI(store *sdui.Store, sections *sdui.Sections, router Router) {
	s.ui.Store(&uiState{store: store, sections: sections, router: router})
}

func (s *Server) uiMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"ui.sections":          method(s.uiSections),
		"ui.get":               method(s.uiGet),
		"ui.subscribe":         method(s.uiSubscribe),
		"ui.unsubscribe":       method(s.uiUnsubscribe),
		"ui.presence":          method(s.uiPresence),
		"ui.focus":             method(s.uiFocus),
		"ui.render":            method(s.uiRender),
		"ui.approve":           method(s.uiApprove),
		"ui.event":             method(s.uiEvent),
		"command.list":         method(s.commandList),
		"command.run":          method(s.commandRun),
		"command.capabilities": method(s.commandCapabilities),
		"command.broadcast":    method(s.commandBroadcast),
	}
}

func (s *Server) uiSections(context.Context, empty) ([]sdui.Section, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	return nonNil(u.sections.List()), nil
}

func (s *Server) uiGet(_ context.Context, p workspaceParams) ([]sdui.SlotView, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	if err := optionalID(workspacePrefix, p.WorkspaceID); err != nil {
		return nil, err
	}
	return nonNil(u.store.Get(p.WorkspaceID)), nil
}

func (s *Server) uiSubscribe(ctx context.Context, p uiSubscribeParams) (subscriptionParams, error) {
	u, err := s.uiState()
	if err != nil {
		return subscriptionParams{}, err
	}
	if err := optionalID(workspacePrefix, p.WorkspaceID); err != nil {
		return subscriptionParams{}, err
	}
	sub, err := u.store.Subscribe(p.WorkspaceID)
	if err != nil {
		return subscriptionParams{}, err
	}
	leave := func() {}
	target := &focusTarget{conn: connFrom(ctx)}
	if p.WorkspaceID != "" {
		leaveCount := s.presence.join(s.bus, s.logger, p.WorkspaceID)
		leave = func() {
			s.presence.dropTarget(p.WorkspaceID, target)
			leaveCount()
		}
	}
	sid, err := subscriptionsFrom(ctx).add(kindUI, sync.OnceFunc(func() {
		sub.Close()
		leave()
	}), forwardUI(ctx, connFrom(ctx), sub, p.Delta, sdui.DeltaOptions{Appends: p.Append, Keeps: p.Keep}))
	if err == nil && p.WorkspaceID != "" {
		s.presence.addTarget(p.WorkspaceID, target, sid)
	}
	return subscriptionParams{Subscription: sid}, err
}

func (s *Server) uiPresence(context.Context, empty) ([]string, error) {
	return nonNil(s.presence.workspaces()), nil
}

func (s *Server) uiFocus(ctx context.Context, p focusParams) (deliveredResult, error) {
	u, err := s.uiState()
	if err != nil {
		return deliveredResult{}, err
	}
	if _, err := s.svc.GetWorkspace(ctx, p.WorkspaceID); err != nil {
		if errors.Is(err, workspace.ErrNotFound) {
			return deliveredResult{}, invalidParams(fmt.Sprintf("unknown workspace: %q", p.WorkspaceID))
		}
		return deliveredResult{}, err
	}
	if _, ok := u.sections.Owner(p.Section); !ok {
		return deliveredResult{}, invalidParams(fmt.Sprintf("unknown section: %q", p.Section))
	}
	if scope, _ := u.sections.SlotScope(p.Section + ".main"); scope == sdui.ScopeApp {
		return deliveredResult{}, invalidParams(fmt.Sprintf("section %q is app-scope", p.Section))
	}
	if !slices.Contains(focusSlots(u.sections, p.Section), p.Slot) {
		return deliveredResult{}, invalidParams(fmt.Sprintf("invalid slot: %q", p.Slot))
	}
	if p.Field && p.Slot != p.Section+".sidebar" {
		return deliveredResult{}, invalidParams(fmt.Sprintf("field needs slot %q", p.Section+".sidebar"))
	}
	var wg sync.WaitGroup
	var delivered atomic.Int32
	for _, t := range s.presence.focusTargets(p.WorkspaceID) {
		wg.Go(func() {
			params := uiFocusParams{Subscription: t.sid, Section: p.Section, Slot: p.Slot, Field: p.Field}
			if t.conn.Notify(ctx, notifyUIFocus, params) == nil {
				delivered.Add(1)
			}
		})
	}
	wg.Wait()
	return deliveredResult{Delivered: int(delivered.Load())}, nil
}

func (s *Server) uiUnsubscribe(ctx context.Context, p subscriptionParams) (empty, error) {
	return empty{}, subscriptionsFrom(ctx).cancel(kindUI, p.Subscription)
}

func (s *Server) uiRender(ctx context.Context, p renderParams) (revResult, error) {
	caller := CallerFrom(ctx)
	if !caller.IsPlugin() {
		return revResult{}, invalidParams("ui.render requires a plugin caller")
	}
	u, err := s.uiState()
	if err != nil {
		return revResult{}, err
	}
	if _, ok := u.sections.SlotSection(p.Slot); !ok {
		return revResult{}, invalidParams(fmt.Sprintf("invalid slot: %q", p.Slot))
	}
	if owner, _ := u.sections.SlotOwner(p.Slot); owner != caller.PluginID {
		return revResult{}, invalidParams(fmt.Sprintf("slot %q is not owned by plugin %q", p.Slot, caller.PluginID))
	}
	if err := s.requireSlotScope(ctx, u, p.Slot, p.WorkspaceID); err != nil {
		return revResult{}, err
	}
	rev, err := u.store.RenderJSON(ctx, p.WorkspaceID, p.Slot, caller.PluginID, p.Tree)
	return revResult{Rev: rev}, err
}

func (s *Server) uiEvent(ctx context.Context, p uiEventParams) (json.RawMessage, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	if p.Action == "" {
		return nil, invalidParams("action is required")
	}
	section, ok := u.sections.SlotSection(p.Slot)
	if !ok {
		return nil, invalidParams(fmt.Sprintf("invalid slot: %q", p.Slot))
	}
	owner, ok := u.sections.SlotOwner(p.Slot)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownSection, section)
	}
	if err := s.requireSlotScope(ctx, u, p.Slot, p.WorkspaceID); err != nil {
		return nil, err
	}
	if p.Agent && confirmGated(u.store.Get(p.WorkspaceID), p.Slot, p.Action) {
		return nil, &jsonrpc2.Error{Code: codeDenied, Message: fmt.Sprintf("action %q needs the user's confirmation", p.Action)}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return u.router.UIEvent(ctx, owner, raw)
}

func (s *Server) commandList(context.Context, empty) ([]Command, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	return append(primitiveCommands(), u.router.Commands()...), nil
}

func (s *Server) commandRun(ctx context.Context, p commandRunParams) (json.RawMessage, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	prim, isPrimitive := primitives[p.ID]
	c := prim.cmd
	if !isPrimitive {
		var ok bool
		if c, ok = u.router.Resolve(p.ID); !ok {
			return nil, fmt.Errorf("%w: %q", errUnknownCommand, p.ID)
		}
	}
	if p.Agent && !c.Agent {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, p.ID)
	}
	needsWorkspace := prim.needsWorkspace
	if !isPrimitive {
		needsWorkspace = c.Scope != sdui.ScopeApp
	}
	scope := ""
	if needsWorkspace {
		if err := s.requireWorkspace(ctx, p.WorkspaceID); err != nil {
			return nil, err
		}
		scope = p.WorkspaceID
	}
	if err := schema.Validate(c.Args, p.Args); err != nil {
		return nil, invalidParams(err.Error())
	}
	if p.Agent && (isPrimitive || c.Args != nil) {
		if p.Args, err = exactArgs(c, p.Args); err != nil {
			return nil, err
		}
	}
	if isPrimitive && prim.scope != nil {
		if scope, err = prim.scope(ctx, s.svc, p.WorkspaceID, p.Args); err != nil {
			return nil, err
		}
	}
	if p.Agent && c.Destructive {
		if p.Args, err = s.approve(ctx, c, scope, sessionArg(isPrimitive), p.Args); err != nil {
			return nil, err
		}
	}
	if isPrimitive {
		if p.Agent {
			ctx = withAgentCall(ctx)
		}
		ctx = withOrigin(ctx, p.Caller, p.Terminal)
		return s.runPrimitive(ctx, prim, p.WorkspaceID, p.Args)
	}
	p.ID = c.ID
	p.Via = CallerFrom(ctx).PluginID
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return u.router.Command(ctx, c, raw)
}

func (s *Server) commandCapabilities(context.Context, empty) (capabilitiesResult, error) {
	u, err := s.uiState()
	if err != nil {
		return capabilitiesResult{}, err
	}
	caps := u.router.Capabilities()
	for name := range caps {
		if _, ok := primitives[name]; ok {
			caps[name] = name
		}
	}
	return capabilitiesResult{Capabilities: caps}, nil
}

func (s *Server) uiState() (*uiState, error) {
	u := s.ui.Load()
	if u == nil {
		return nil, errNoUI
	}
	return u, nil
}

func (s *Server) requireSlotScope(ctx context.Context, u *uiState, slot, workspaceID string) error {
	scope, _ := u.sections.SlotScope(slot)
	if scope != sdui.ScopeApp {
		return s.requireWorkspace(ctx, workspaceID)
	}
	if workspaceID != "" {
		return invalidParams(fmt.Sprintf("slot %q is app-scope and takes an empty workspaceId", slot))
	}
	return nil
}

func (s *Server) requireWorkspace(ctx context.Context, workspaceID string) error {
	if err := requireID(workspacePrefix, workspaceID); err != nil {
		return err
	}
	_, err := s.svc.GetWorkspace(ctx, workspaceID)
	return err
}

func focusSlots(sections *sdui.Sections, section string) []string {
	var slots []string
	if sections.InRail(section) {
		slots = append(slots, "", section+".sidebar", section+".main")
	}
	popup := section + ".popup"
	if owner, ok := sections.SlotSection(popup); ok && owner == section {
		slots = append(slots, popup)
	}
	return slots
}

func forwardUI(ctx context.Context, conn *jsonrpc2.Conn, sub *sdui.Subscription, delta bool, opts sdui.DeltaOptions) forwardFunc {
	return func(sid string, done <-chan struct{}) {
		var tracker *sdui.DeltaTracker
		if delta {
			tracker = sdui.NewDeltaTracker(opts)
		}
		for {
			select {
			case <-done:
				return
			case <-sub.Ready():
				views, ok := sub.Take()
				if !ok {
					return
				}
				for _, view := range views {
					if tracker != nil {
						view = tracker.Frame(view)
					}
					if err := conn.Notify(ctx, notifyUIUpdated, uiUpdatedFrame(sid, view)); err != nil {
						return
					}
				}
			}
		}
	}
}

func uiUpdatedFrame(sid string, view sdui.SlotView) json.RawMessage {
	b := make([]byte, 0, len(view.Tree)+len(view.Delta)+len(sid)+224)
	b = append(b, `{"subscription":`...)
	b, _ = jsontext.AppendQuote(b, sid)
	b = append(b, `,"view":`...)
	b = view.AppendJSON(b)
	return append(b, '}')
}

func invalidParams(msg string) error {
	return &jsonrpc2.Error{Code: codeInvalidParams, Message: msg}
}
