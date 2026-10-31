package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/agentcall"
)

type Registry[P Entry[P]] struct {
	Mu             *sync.Mutex
	Items          map[string]P
	Noun           string
	Sessions       func(ctx context.Context, workspace string) ([]agentcall.Session, error)
	Projects       func(ctx context.Context) ([]pluginsdk.Project, error)
	Render         func(ctx context.Context, workspace string) error
	Save           func(ctx context.Context, p P) error
	Drop           func(ctx context.Context, p P) error
	Changed        func()
	SessionDeleted func(session string)
	Log            func(error)
	renderMu       sync.Mutex
	dirty          map[string]bool
}

func (r *Registry[P]) BusEvent(ctx context.Context, ev pluginsdk.Event) {
	switch ev.Topic {
	case TopicAgentsChanged:
		var p changedPayload
		if !r.decode(ev, &p) {
			return
		}
		if p.Kind == changeDeleted && r.SessionDeleted != nil {
			r.SessionDeleted(p.Session)
		}
		if (p.Kind == changeStatus || p.Kind == changeDeleted) && r.Owns(p.Session) {
			r.Reconcile(ctx, p.Workspace)
			r.Flush(ctx)
		}
	case TopicAgentsReady:
		r.ReconcileAll(ctx)
		r.RenderAll(ctx)
	case TopicProjectCreated:
		var p createdPayload
		if !r.decode(ev, &p) {
			return
		}
		r.Invalidate(p.Project.ID)
		r.Flush(ctx)
	case TopicProjectDeleted:
		var p deletedPayload
		if !r.decode(ev, &p) {
			return
		}
		r.DropWorkspace(ctx, p.ID)
	}
}

func (r *Registry[P]) ReconcileAll(ctx context.Context) {
	r.Mu.Lock()
	workspaces := map[string]bool{}
	for _, p := range r.Items {
		if act := p.Action(); act.Owner != "" {
			workspaces[act.Workspace] = true
		}
	}
	r.Mu.Unlock()
	for ws := range workspaces {
		r.Reconcile(ctx, ws)
	}
}

func (r *Registry[P]) Reconcile(ctx context.Context, workspace string) {
	r.Mu.Lock()
	owned := map[string]string{}
	for id, p := range r.Items {
		if act := p.Action(); act.Workspace == workspace && act.Owner != "" {
			owned[id] = act.Owner
		}
	}
	r.Mu.Unlock()
	if len(owned) == 0 {
		return
	}
	sessions, err := r.Sessions(ctx, workspace)
	if err != nil {
		r.Log(err)
		return
	}
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for id, owner := range owned {
		p, ok := r.Items[id]
		if !ok || p.Action().Owner != owner {
			continue
		}
		if s, alive := agentcall.Find(sessions, owner); !alive || s.Closed {
			r.Log(r.Drop(ctx, p))
		}
	}
}

func (r *Registry[P]) Prune(ctx context.Context) {
	r.Mu.Lock()
	placed := map[string]string{}
	for id, p := range r.Items {
		if ws := p.Action().Workspace; ws != "" {
			placed[id] = ws
		}
	}
	r.Mu.Unlock()
	if len(placed) == 0 {
		return
	}
	projects, err := r.Projects(ctx)
	if err != nil {
		r.Log(fmt.Errorf("prune %ss of deleted workspaces: %w", r.Noun, err))
		return
	}
	live := map[string]bool{}
	for _, p := range projects {
		live[p.ID] = true
	}
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for id, workspace := range placed {
		if p, ok := r.Items[id]; ok && !live[workspace] {
			r.Log(r.Drop(ctx, p))
			delete(r.dirty, workspace)
		}
	}
}

func (r *Registry[P]) DropWorkspace(ctx context.Context, workspace string) {
	projects, err := r.Projects(ctx)
	if err != nil {
		r.Log(fmt.Errorf("check deleted project %s: %w", workspace, err))
		return
	}
	if slices.ContainsFunc(projects, func(p pluginsdk.Project) bool { return p.ID == workspace }) {
		r.Log(fmt.Errorf("ignore %s for live project %s", TopicProjectDeleted, workspace))
		return
	}
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for _, p := range r.Items {
		if p.Action().Workspace == workspace {
			r.Log(r.Drop(ctx, p))
		}
	}
	delete(r.dirty, workspace)
}

func (r *Registry[P]) Owns(session string) bool {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for _, p := range r.Items {
		if session != "" && p.Action().Owner == session {
			return true
		}
	}
	return false
}

func (r *Registry[P]) Lookup(cmd pluginsdk.CommandRun, id string) (P, error) {
	p, ok := r.Items[id]
	if !ok || p.Action().Workspace != cmd.ProjectID {
		var zero P
		return zero, fmt.Errorf("unknown %s %q", r.Noun, id)
	}
	return p, nil
}

func (r *Registry[P]) Show(cmd pluginsdk.CommandRun) (P, error) {
	var args IDArgs
	if err := Decode(cmd, &args); err != nil {
		var zero P
		return zero, err
	}
	r.Mu.Lock()
	defer r.Mu.Unlock()
	p, err := r.Lookup(cmd, args.ID)
	if err != nil {
		return p, err
	}
	return p.Clone(), nil
}

func (r *Registry[P]) Authorized(ctx context.Context, cmd pluginsdk.CommandRun, id string) (P, error) {
	r.Mu.Lock()
	p, err := r.Lookup(cmd, id)
	r.Mu.Unlock()
	if err != nil {
		return p, err
	}
	act := p.Action()
	if Allowed(cmd, act, nil) || Allowed(cmd, act, r.Parents(ctx, act.Workspace)) {
		return p, nil
	}
	var zero P
	return zero, fmt.Errorf("%s %s belongs to another agent session", r.Noun, act.ID)
}

func (r *Registry[P]) Mutate(ctx context.Context, cmd pluginsdk.CommandRun, id string, change func(P) error) (P, error) {
	var zero P
	for {
		p, err := r.Authorized(ctx, cmd, id)
		if err != nil {
			return zero, err
		}
		updated := p.Clone()
		if err := change(updated); err != nil {
			return zero, err
		}
		saved, err := r.replace(ctx, p, updated)
		if err != nil {
			return zero, err
		}
		if saved {
			return updated.Clone(), nil
		}
	}
}

func (r *Registry[P]) Parents(ctx context.Context, workspace string) map[string]string {
	sessions, err := r.Sessions(ctx, workspace)
	if err != nil {
		return nil
	}
	return agentcall.Parents(sessions)
}

func (r *Registry[P]) InWorkspace(workspace string) []P {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	var items []P
	for _, p := range r.Items {
		if p.Action().Workspace == workspace {
			items = append(items, p.Clone())
		}
	}
	Sort(items)
	return items
}

func (r *Registry[P]) Titles(ctx context.Context, workspace string, items []P) map[string]string {
	if !slices.ContainsFunc(items, func(p P) bool { return p.Action().Owner != "" }) {
		return nil
	}
	sessions, err := r.Sessions(ctx, workspace)
	if err != nil {
		return nil
	}
	titles := make(map[string]string, len(sessions))
	for _, s := range sessions {
		titles[s.ID] = s.Title
	}
	return titles
}

func (r *Registry[P]) RenderAll(ctx context.Context) {
	projects, err := r.Projects(ctx)
	if err != nil {
		r.Log(fmt.Errorf("render %s sections: %w", r.Noun, err))
		return
	}
	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	r.Invalidate(ids...)
	r.Flush(ctx)
}

func (r *Registry[P]) Invalidate(workspaces ...string) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for _, ws := range workspaces {
		r.Touch(ws)
	}
}

func (r *Registry[P]) Touch(workspace string) {
	if workspace == "" {
		return
	}
	if r.dirty == nil {
		r.dirty = map[string]bool{}
	}
	r.dirty[workspace] = true
}

func (r *Registry[P]) Flush(ctx context.Context) {
	r.renderMu.Lock()
	defer r.renderMu.Unlock()
	r.Mu.Lock()
	dirty := r.dirty
	r.dirty = nil
	r.Mu.Unlock()
	for ws := range dirty {
		r.Log(r.Render(ctx, ws))
	}
}

func (r *Registry[P]) NotifyPaused(ctx context.Context, h *pluginsdk.Host, title string, act Action, lastError string) {
	req := notifyRequest{ProjectID: act.Workspace, Title: title, Message: act.Name + ": " + lastError, Kind: "stuck"}
	if err := h.Call(ctx, "notify.post", req, nil); err != nil {
		r.Log(fmt.Errorf("notify paused %s %s: %w", r.Noun, act.ID, err))
	}
}

func (r *Registry[P]) replace(ctx context.Context, current, updated P) (bool, error) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	id := updated.Action().ID
	if r.Items[id] != current {
		return false, nil
	}
	if err := r.Save(ctx, updated); err != nil {
		return false, err
	}
	r.Items[id] = updated
	r.Touch(updated.Action().Workspace)
	if r.Changed != nil {
		r.Changed()
	}
	return true, nil
}

func (r *Registry[P]) decode(ev pluginsdk.Event, v any) bool {
	if err := json.Unmarshal(ev.Payload, v); err != nil {
		r.Log(fmt.Errorf("decode %s: %w", ev.Topic, err))
		return false
	}
	return true
}
