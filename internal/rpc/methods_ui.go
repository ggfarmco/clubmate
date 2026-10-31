package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/project"
	"clubmate/internal/schema"
	"clubmate/internal/sdui"
	pkgsdui "clubmate/pkg/sdui"
)

const (
	notifyUIUpdated = "ui.updated"
	notifyUIClosed  = "ui.closed"
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
}

type Command struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Plugin      string         `json:"plugin"`
	Hidden      bool           `json:"hidden"`
	Confirm     string         `json:"confirm"`
	Args        *schema.Object `json:"args,omitempty"`
	Result      *schema.Object `json:"result,omitempty"`
	Agent       bool           `json:"agent"`
	Remote      bool           `json:"remote"`
	Scope       string         `json:"scope,omitempty"`
	Destructive bool           `json:"destructive"`
	Provides    []string       `json:"provides,omitempty"`
	Timeout     int            `json:"timeout,omitempty"`
}

type uiState struct {
	store    *sdui.Store
	sections *sdui.Sections
	router   Router
}

type projectParams struct {
	ProjectID string `json:"projectId"`
}

type renderParams struct {
	ProjectID string       `json:"projectId"`
	Slot      string       `json:"slot"`
	Tree      pkgsdui.Node `json:"tree"`
}

type focusParams struct {
	ProjectID string `json:"projectId"`
	Section   string `json:"section"`
	Slot      string `json:"slot"`
	Field     bool   `json:"field"`
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
	ProjectID string          `json:"projectId"`
	Slot      string          `json:"slot"`
	NodeID    string          `json:"nodeId"`
	Action    string          `json:"action"`
	Value     json.RawMessage `json:"value"`
}

type capabilitiesResult struct {
	Capabilities map[string]string `json:"capabilities"`
}

type commandRunParams struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Args      json.RawMessage `json:"args"`
	Agent     bool            `json:"agent,omitempty"`
	Caller    string          `json:"caller,omitempty"`
	Via       string          `json:"via,omitempty"`
}

type uiUpdatedParams struct {
	Subscription string        `json:"subscription"`
	View         sdui.SlotView `json:"view"`
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
	}
}

func (s *Server) uiSections(context.Context, empty) ([]sdui.Section, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	return nonNil(u.sections.List()), nil
}

func (s *Server) uiGet(_ context.Context, p projectParams) ([]sdui.SlotView, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	if err := optionalID(projectPrefix, p.ProjectID); err != nil {
		return nil, err
	}
	return nonNil(u.store.Get(p.ProjectID)), nil
}

func (s *Server) uiSubscribe(ctx context.Context, p projectParams) (subscriptionParams, error) {
	u, err := s.uiState()
	if err != nil {
		return subscriptionParams{}, err
	}
	if err := optionalID(projectPrefix, p.ProjectID); err != nil {
		return subscriptionParams{}, err
	}
	sub, err := u.store.Subscribe(p.ProjectID)
	if err != nil {
		return subscriptionParams{}, err
	}
	leave := func() {}
	target := &focusTarget{conn: connFrom(ctx)}
	if p.ProjectID != "" {
		leaveCount := s.presence.join(s.bus, s.logger, p.ProjectID)
		leave = func() {
			s.presence.dropTarget(p.ProjectID, target)
			leaveCount()
		}
	}
	sid, err := subscriptionsFrom(ctx).add(kindUI, sync.OnceFunc(func() {
		sub.Close()
		leave()
	}), forwardUI(ctx, connFrom(ctx), sub))
	if err == nil && p.ProjectID != "" {
		s.presence.addTarget(p.ProjectID, target, sid)
	}
	return subscriptionParams{Subscription: sid}, err
}

func (s *Server) uiPresence(context.Context, empty) ([]string, error) {
	return nonNil(s.presence.projects()), nil
}

func (s *Server) uiFocus(ctx context.Context, p focusParams) (deliveredResult, error) {
	u, err := s.uiState()
	if err != nil {
		return deliveredResult{}, err
	}
	if _, err := s.svc.GetProject(ctx, p.ProjectID); err != nil {
		if errors.Is(err, project.ErrNotFound) {
			return deliveredResult{}, invalidParams(fmt.Sprintf("unknown project: %q", p.ProjectID))
		}
		return deliveredResult{}, err
	}
	if _, ok := u.sections.Owner(p.Section); !ok {
		return deliveredResult{}, invalidParams(fmt.Sprintf("unknown section: %q", p.Section))
	}
	if scope, _ := u.sections.SlotScope(p.Section + ".main"); scope == sdui.ScopeApp {
		return deliveredResult{}, invalidParams(fmt.Sprintf("section %q is app-scope", p.Section))
	}
	if !slices.Contains([]string{"", p.Section + ".sidebar", p.Section + ".main"}, p.Slot) {
		return deliveredResult{}, invalidParams(fmt.Sprintf("invalid slot: %q", p.Slot))
	}
	if p.Field && p.Slot != p.Section+".sidebar" {
		return deliveredResult{}, invalidParams(fmt.Sprintf("field needs slot %q", p.Section+".sidebar"))
	}
	var wg sync.WaitGroup
	var delivered atomic.Int32
	for _, t := range s.presence.focusTargets(p.ProjectID) {
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
	if err := s.requireSlotScope(ctx, u, p.Slot, p.ProjectID); err != nil {
		return revResult{}, err
	}
	rev, err := u.store.Render(ctx, p.ProjectID, p.Slot, caller.PluginID, p.Tree)
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
	if err := s.requireSlotScope(ctx, u, p.Slot, p.ProjectID); err != nil {
		return nil, err
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
	needsProject := prim.needsProject
	if !isPrimitive {
		needsProject = c.Scope != sdui.ScopeApp
	}
	scope := ""
	if needsProject {
		if err := s.requireProject(ctx, p.ProjectID); err != nil {
			return nil, err
		}
		scope = p.ProjectID
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
		if scope, err = prim.scope(ctx, s.svc, p.ProjectID, p.Args); err != nil {
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
		return s.runPrimitive(ctx, prim, p.ProjectID, p.Args)
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

func (s *Server) requireSlotScope(ctx context.Context, u *uiState, slot, projectID string) error {
	scope, _ := u.sections.SlotScope(slot)
	if scope != sdui.ScopeApp {
		return s.requireProject(ctx, projectID)
	}
	if projectID != "" {
		return invalidParams(fmt.Sprintf("slot %q is app-scope and takes an empty projectId", slot))
	}
	return nil
}

func (s *Server) requireProject(ctx context.Context, projectID string) error {
	if err := requireID(projectPrefix, projectID); err != nil {
		return err
	}
	_, err := s.svc.GetProject(ctx, projectID)
	return err
}

func forwardUI(ctx context.Context, conn *jsonrpc2.Conn, sub *sdui.Subscription) forwardFunc {
	return func(sid string, done <-chan struct{}) {
		for {
			select {
			case <-done:
				return
			case view, ok := <-sub.C():
				if !ok {
					notifyDropped(ctx, conn, notifyUIClosed, sid, sub.Err())
					return
				}
				if err := conn.Notify(ctx, notifyUIUpdated, uiUpdatedParams{Subscription: sid, View: view}); err != nil {
					return
				}
			}
		}
	}
}

func invalidParams(msg string) error {
	return &jsonrpc2.Error{Code: codeInvalidParams, Message: msg}
}
