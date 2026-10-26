package pluginsdk

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Repos     []string  `json:"repos"`
	CreatedAt time.Time `json:"createdAt"`
}

type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	Owner     string    `json:"owner"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd"`
	HostPID   int       `json:"hostPid"`
	ChildPID  int       `json:"childPid"`
	PGID      int       `json:"pgid"`
	StartedAt time.Time `json:"startedAt"`
	State     string    `json:"state"`
	ExitCode  *int      `json:"exitCode"`
}

type Event struct {
	Seq     int64           `json:"seq"`
	Topic   string          `json:"topic"`
	Source  string          `json:"source"`
	Time    time.Time       `json:"time"`
	Payload json.RawMessage `json:"payload"`
}

type Host struct {
	conn     *jsonrpc2.Conn
	pluginID func() string
}

func (h *Host) PluginID() string {
	return h.pluginID()
}

func (h *Host) Call(ctx context.Context, method string, params, result any) error {
	return h.conn.Call(ctx, method, params, result)
}

func (h *Host) ListProjects(ctx context.Context) ([]Project, error) {
	var out []Project
	err := h.Call(ctx, "project.list", struct{}{}, &out)
	return out, err
}

func (h *Host) GetProject(ctx context.Context, id string) (Project, error) {
	var out Project
	err := h.Call(ctx, "project.get", idParams{ID: id}, &out)
	return out, err
}

func (h *Host) CreateSession(ctx context.Context, projectID, title string, command []string) (Session, error) {
	var out Session
	err := h.Call(ctx, "session.create", createSessionParams{ProjectID: projectID, Title: title, Command: command}, &out)
	return out, err
}

func (h *Host) ListSessions(ctx context.Context, projectID string, mine bool) ([]Session, error) {
	params := listSessionsParams{ProjectID: projectID}
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

func (h *Host) StorageList(ctx context.Context, prefix string) ([]string, error) {
	var res storageListResult
	err := h.Call(ctx, "storage.list", storageListParams{Prefix: prefix}, &res)
	return res.Keys, err
}

type idParams struct {
	ID string `json:"id"`
}

type createSessionParams struct {
	ProjectID string   `json:"projectId"`
	Title     string   `json:"title"`
	Command   []string `json:"command"`
}

type listSessionsParams struct {
	ProjectID string `json:"projectId"`
	Owner     string `json:"owner"`
}

type publishParams struct {
	Topic   string `json:"topic"`
	Payload any    `json:"payload"`
}

type subscribeParams struct {
	Pattern string `json:"pattern"`
	After   int64  `json:"after"`
}

type subscribeResult struct {
	Subscription string `json:"subscription"`
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
