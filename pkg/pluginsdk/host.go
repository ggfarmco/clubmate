package pluginsdk

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/contract"
	"clubmate/pkg/sdui"
)

type (
	Workspace   = contract.Workspace
	Folder      = contract.Folder
	Session     = contract.TerminalSession
	ProcessInfo = contract.ProcessInfo
	ProcessNode = contract.ProcessNode
	ProcessRoot = contract.ProcessRoot
	KillRequest = contract.KillRequest
	Event       = contract.Event
)

const (
	busReadLimit   = 1000
	renderCacheCap = 64
)

type Host struct {
	conn     *jsonrpc2.Conn
	pluginID func() string

	renderMu   sync.Mutex
	rendered   map[slotRef]renderedTree
	renderTick uint64
}

func (h *Host) PluginID() string {
	return h.pluginID()
}

func (h *Host) Call(ctx context.Context, method string, params, result any) error {
	return h.conn.Call(ctx, method, params, result)
}

func (h *Host) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var out []Workspace
	err := h.Call(ctx, "workspace.list", struct{}{}, &out)
	return out, err
}

func (h *Host) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	var out Workspace
	err := h.Call(ctx, "workspace.get", idParams{ID: id}, &out)
	return out, err
}

func (h *Host) CreateWorkspace(ctx context.Context, name string, repos []string) (Workspace, error) {
	var out Workspace
	err := h.Call(ctx, "workspace.create", createWorkspaceParams{Name: name, Repos: repos}, &out)
	return out, err
}

func (h *Host) SetWorktree(ctx context.Context, workspaceID, repo, path string) (Workspace, error) {
	var out Workspace
	err := h.Call(ctx, "workspace.worktree.set", worktreeSetParams{ID: workspaceID, Repo: repo, Path: path}, &out)
	return out, err
}

func (h *Host) CheckWorktrees(ctx context.Context, workspaceID string) (Workspace, error) {
	var out Workspace
	err := h.Call(ctx, "workspace.worktree.check", idParams{ID: workspaceID}, &out)
	return out, err
}

func (h *Host) DeleteWorkspace(ctx context.Context, id string) error {
	return h.Call(ctx, "workspace.delete", idParams{ID: id}, nil)
}

func (h *Host) OpenWorkspace(ctx context.Context, id string) error {
	return h.Call(ctx, "workspace.show", idParams{ID: id}, nil)
}

func (h *Host) Focus(ctx context.Context, workspaceID, section, slot string) error {
	return h.Call(ctx, "ui.focus", map[string]string{"workspaceId": workspaceID, "section": section, "slot": slot}, nil)
}

func (h *Host) FocusField(ctx context.Context, workspaceID, section string) error {
	return h.Call(ctx, "ui.focus", map[string]any{"workspaceId": workspaceID, "section": section, "slot": section + ".sidebar", "field": true}, nil)
}

func (h *Host) CreateSession(ctx context.Context, req SessionRequest) (Session, error) {
	var out Session
	err := h.Call(ctx, "session.create", req, &out)
	return out, err
}

func (h *Host) Presence(ctx context.Context) ([]string, error) {
	var out []string
	err := h.Call(ctx, "ui.presence", struct{}{}, &out)
	return out, err
}

func (h *Host) SelectedSection(ctx context.Context, workspaceID string) (string, error) {
	var out struct {
		Section string `json:"section"`
	}
	err := h.Call(ctx, "ui.state.section", map[string]string{"workspaceId": workspaceID}, &out)
	return out.Section, err
}

func (h *Host) RunCommand(ctx context.Context, id, workspaceID string, args any, out any) error {
	return h.Call(ctx, "command.run", runCommandParams{ID: id, WorkspaceID: workspaceID, Args: args}, out)
}

func (h *Host) Broadcast(ctx context.Context, topic, workspaceID string, args any) error {
	return h.Call(ctx, "command.broadcast", broadcastParams{Topic: topic, WorkspaceID: workspaceID, Args: args}, nil)
}

func (h *Host) ListSessions(ctx context.Context, workspaceID string, mine bool) ([]Session, error) {
	params := listSessionsParams{WorkspaceID: workspaceID}
	if mine {
		params.Owner = h.PluginID()
	}
	var out []Session
	err := h.Call(ctx, "session.list", params, &out)
	return out, err
}

func (h *Host) KillSession(ctx context.Context, id string) error {
	return h.Call(ctx, "session.kill", idParams{ID: id}, nil)
}

func (h *Host) RemoveSession(ctx context.Context, id string) error {
	return h.Call(ctx, "session.remove", idParams{ID: id}, nil)
}

func (h *Host) ProcessTree(ctx context.Context, workspaceID, sessionID string) ([]ProcessNode, error) {
	var out []ProcessNode
	err := h.Call(ctx, "process.tree", processTreeParams{WorkspaceID: workspaceID, SessionID: sessionID}, &out)
	return out, err
}

func (h *Host) KillProcesses(ctx context.Context, req KillRequest) ([]int, error) {
	var out killResult
	err := h.Call(ctx, "process.kill", req, &out)
	return out.Killed, err
}

func (h *Host) RegisterProcess(ctx context.Context, workspaceID string, pid int, label string) (ProcessRoot, error) {
	var out ProcessRoot
	err := h.Call(ctx, "process.register", registerProcessParams{WorkspaceID: workspaceID, PID: pid, Label: label}, &out)
	return out, err
}

func (h *Host) UnregisterProcess(ctx context.Context, pid int) error {
	return h.Call(ctx, "process.unregister", pidParams{PID: pid}, nil)
}

func (h *Host) ProcessRoots(ctx context.Context, workspaceID string) ([]ProcessRoot, error) {
	var out []ProcessRoot
	err := h.Call(ctx, "process.roots", workspaceParams{WorkspaceID: workspaceID}, &out)
	return out, err
}

func (h *Host) Publish(ctx context.Context, topic string, payload any) (Event, error) {
	var out Event
	err := h.Call(ctx, "bus.publish", publishParams{Topic: topic, Payload: payload}, &out)
	return out, err
}

func (h *Host) Subscribe(ctx context.Context, pattern string, after int64) (string, error) {
	var out subscribeResult
	err := h.Call(ctx, "bus.subscribe", subscribeParams{Pattern: pattern, After: after}, &out)
	return out.Subscription, err
}

func (h *Host) WatchedSections(ctx context.Context, until int64) (map[string][]string, error) {
	out := map[string][]string{}
	var after int64
	for {
		var page []Event
		if err := h.Call(ctx, "bus.read", busReadParams{Pattern: contract.TopicRemoteWatching, After: after, Limit: busReadLimit}, &page); err != nil {
			return nil, err
		}
		for _, ev := range page {
			if ev.Seq > until {
				return out, nil
			}
			var w contract.RemoteWatching
			if json.Unmarshal(ev.Payload, &w) != nil || w.Workspace == "" {
				continue
			}
			if len(w.Sections) == 0 {
				delete(out, w.Workspace)
			} else {
				out[w.Workspace] = w.Sections
			}
		}
		if len(page) < busReadLimit {
			return out, nil
		}
		after = page[len(page)-1].Seq
	}
}

func (h *Host) WatchFS(ctx context.Context, workspaceID string) (string, error) {
	var out watchResult
	err := h.Call(ctx, "fs.watch", workspaceParams{WorkspaceID: workspaceID}, &out)
	return out.Watch, err
}

func (h *Host) UnwatchFS(ctx context.Context, watch string) error {
	return h.Call(ctx, "fs.unwatch", watchResult{Watch: watch}, nil)
}

func (h *Host) Render(ctx context.Context, workspaceID, slot string, n sdui.Node) (int64, error) {
	tree, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}
	return h.renderOwned(ctx, workspaceID, slot, tree)
}

func (h *Host) RenderJSON(ctx context.Context, workspaceID, slot string, tree json.RawMessage) (int64, error) {
	return h.renderOwned(ctx, workspaceID, slot, slices.Clone(tree))
}

func (h *Host) renderOwned(ctx context.Context, workspaceID, slot string, tree json.RawMessage) (int64, error) {
	key := slotRef{workspace: workspaceID, slot: slot}
	next, err := sdui.IndexTree(tree)
	if prev, ok := h.lastRender(key); ok && err == nil {
		if delta, ok := next.DeltaFrom(prev.index, sdui.DeltaOptions{Appends: true, Keeps: true}); ok {
			if rev, err := h.render(ctx, renderParams{WorkspaceID: workspaceID, Slot: slot, Base: prev.rev, Delta: delta}); err == nil {
				h.remember(key, renderedTree{rev: rev, index: next})
				return rev, nil
			}
		}
	}
	rev, err := h.render(ctx, renderParams{WorkspaceID: workspaceID, Slot: slot, Tree: tree})
	if err == nil && next != nil {
		h.remember(key, renderedTree{rev: rev, index: next})
	}
	return rev, err
}

func (h *Host) UISections(ctx context.Context) ([]json.RawMessage, error) {
	var out []json.RawMessage
	err := h.Call(ctx, "ui.sections", struct{}{}, &out)
	return out, err
}

func (h *Host) UIGet(ctx context.Context, workspaceID string) ([]SlotView, error) {
	var out []SlotView
	err := h.Call(ctx, "ui.get", workspaceParams{WorkspaceID: workspaceID}, &out)
	return out, err
}

func (h *Host) UISubscribe(ctx context.Context, p UISubscribe) (string, error) {
	var out subscribeResult
	err := h.Call(ctx, "ui.subscribe", p, &out)
	return out.Subscription, err
}

func (h *Host) UIUnsubscribe(ctx context.Context, subscription string) error {
	return h.Call(ctx, "ui.unsubscribe", subscribeResult{Subscription: subscription}, nil)
}

func (h *Host) UIEvent(ctx context.Context, ev UIEvent) (json.RawMessage, error) {
	var out json.RawMessage
	err := h.Call(ctx, "ui.event", ev, &out)
	return out, err
}

func (h *Host) Approve(ctx context.Context, workspaceID, title, command string, args map[string]string, body string) error {
	return h.Call(ctx, "ui.approve", approveParams{WorkspaceID: workspaceID, Title: title, Command: command, Args: args, Body: body}, nil)
}

func (h *Host) StorageGet(ctx context.Context, key string, out any) (bool, error) {
	var res storageGetResult
	if err := h.Call(ctx, "storage.get", storageKeyParams{Key: key}, &res); err != nil {
		return false, err
	}
	if !res.Found {
		return false, nil
	}
	return true, json.Unmarshal(res.Value, out)
}

func (h *Host) StorageSet(ctx context.Context, key string, v any) error {
	return h.Call(ctx, "storage.set", storageSetParams{Key: key, Value: v}, nil)
}

func (h *Host) StorageDelete(ctx context.Context, key string) error {
	return h.Call(ctx, "storage.delete", storageKeyParams{Key: key}, nil)
}

func (h *Host) StorageDeletePrefix(ctx context.Context, prefix string) error {
	return h.Call(ctx, "storage.deletePrefix", storageListParams{Prefix: prefix}, nil)
}

func (h *Host) StorageList(ctx context.Context, prefix string) ([]string, error) {
	var res storageListResult
	err := h.Call(ctx, "storage.list", storageListParams{Prefix: prefix}, &res)
	return res.Keys, err
}

type processTreeParams struct {
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId"`
}

type killResult struct {
	Killed []int `json:"killed"`
}

type createWorkspaceParams struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

type idParams struct {
	ID string `json:"id"`
}

type worktreeSetParams struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
	Path string `json:"path"`
}

type UISubscribe struct {
	WorkspaceID string `json:"workspaceId"`
	Presence    bool   `json:"presence"`
}

type SessionRequest struct {
	WorkspaceID string   `json:"workspaceId"`
	Title       string   `json:"title"`
	Command     []string `json:"command"`
	Cwd         string   `json:"cwd"`
	Env         []string `json:"env"`
	UnsetEnv    []string `json:"unsetEnv"`
	PathAppend  []string `json:"pathAppend"`
}

type runCommandParams struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Args        any    `json:"args"`
}

type broadcastParams struct {
	Topic       string `json:"topic"`
	WorkspaceID string `json:"workspaceId"`
	Args        any    `json:"args"`
}

type listSessionsParams struct {
	WorkspaceID string `json:"workspaceId"`
	Owner       string `json:"owner"`
}

type publishParams struct {
	Topic   string `json:"topic"`
	Payload any    `json:"payload"`
}

type subscribeParams struct {
	Pattern string `json:"pattern"`
	After   int64  `json:"after"`
}

type busReadParams struct {
	Pattern string `json:"pattern"`
	After   int64  `json:"after"`
	Limit   int    `json:"limit"`
}

type subscribeResult struct {
	Subscription string `json:"subscription"`
}

type renderParams struct {
	WorkspaceID string          `json:"workspaceId"`
	Slot        string          `json:"slot"`
	Tree        json.RawMessage `json:"tree,omitempty"`
	Base        int64           `json:"base,omitempty"`
	Delta       json.RawMessage `json:"delta,omitempty"`
}

type slotRef struct {
	workspace, slot string
}

type renderedTree struct {
	rev   int64
	index *sdui.TreeIndex
	used  uint64
}

type approveParams struct {
	WorkspaceID string            `json:"workspaceId"`
	Title       string            `json:"title"`
	Command     string            `json:"command"`
	Args        map[string]string `json:"args"`
	Body        string            `json:"body,omitempty"`
}

type workspaceParams struct {
	WorkspaceID string `json:"workspaceId"`
}

type registerProcessParams struct {
	WorkspaceID string `json:"workspaceId"`
	PID         int    `json:"pid"`
	Label       string `json:"label"`
}

type pidParams struct {
	PID int `json:"pid"`
}

type watchResult struct {
	Watch string `json:"watch"`
}

type renderResult struct {
	Rev int64 `json:"rev"`
}

type storageKeyParams struct {
	Key string `json:"key"`
}

type storageSetParams struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type storageListParams struct {
	Prefix string `json:"prefix"`
}

type storageGetResult struct {
	Value json.RawMessage `json:"value"`
	Found bool            `json:"found"`
}

type storageListResult struct {
	Keys []string `json:"keys"`
}

func (h *Host) render(ctx context.Context, p renderParams) (int64, error) {
	var out renderResult
	err := h.Call(ctx, "ui.render", p, &out)
	return out.Rev, err
}

func (h *Host) lastRender(key slotRef) (renderedTree, bool) {
	h.renderMu.Lock()
	defer h.renderMu.Unlock()
	r, ok := h.rendered[key]
	if ok {
		h.renderTick++
		r.used = h.renderTick
		h.rendered[key] = r
	}
	return r, ok
}

func (h *Host) remember(key slotRef, r renderedTree) {
	h.renderMu.Lock()
	defer h.renderMu.Unlock()
	if h.rendered == nil {
		h.rendered = map[slotRef]renderedTree{}
	}
	if r.rev <= h.rendered[key].rev {
		return
	}
	h.renderTick++
	r.used = h.renderTick
	h.rendered[key] = r
	if len(h.rendered) <= renderCacheCap {
		return
	}
	oldest := key
	for k, v := range h.rendered {
		if v.used < h.rendered[oldest].used {
			oldest = k
		}
	}
	delete(h.rendered, oldest)
}
