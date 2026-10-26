package pluginsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/sourcegraph/jsonrpc2"
)

const ProtocolVersion = 1

const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type UIEvent struct {
	ProjectID string          `json:"projectId"`
	Slot      string          `json:"slot"`
	NodeID    string          `json:"nodeId"`
	Action    string          `json:"action"`
	Value     json.RawMessage `json:"value"`
}

type CommandRun struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Args      json.RawMessage `json:"args"`
}

type Plugin struct {
	Init     func(ctx context.Context, h *Host) error
	UIEvent  func(ctx context.Context, h *Host, ev UIEvent) (any, error)
	Command  func(ctx context.Context, h *Host, cmd CommandRun) (any, error)
	BusEvent func(ctx context.Context, h *Host, sub string, ev Event)
	Shutdown func(ctx context.Context, h *Host)
}

type stdio struct {
	io.Reader
	io.Writer
}

func (stdio) Close() error {
	return os.Stdin.Close()
}

func Run(p Plugin) error {
	err := RunConn(context.Background(), stdio{Reader: os.Stdin, Writer: os.Stdout}, p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plugin:", err)
	}
	return err
}

func RunConn(ctx context.Context, rwc io.ReadWriteCloser, p Plugin) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rt := &runtime{plugin: p, stop: make(chan struct{}), initErr: make(chan error, 1)}
	conn := jsonrpc2.NewConn(ctx, jsonrpc2.NewPlainObjectStream(rwc), jsonrpc2.AsyncHandler(rt))
	var err error
	select {
	case <-conn.DisconnectNotify():
	case <-rt.stop:
	case err = <-rt.initErr:
	}
	cancel()
	_ = conn.Close()
	return err
}

type runtime struct {
	plugin   Plugin
	pluginID atomic.Value
	stop     chan struct{}
	stopOnce sync.Once
	initErr  chan error
}

type initializeParams struct {
	Protocol int    `json:"protocol"`
	PluginID string `json:"pluginId"`
}

type initializeResult struct {
	Protocol int `json:"protocol"`
}

type busEventParams struct {
	Subscription string `json:"subscription"`
	Event        Event  `json:"event"`
}

func (rt *runtime) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	h := &Host{conn: conn, pluginID: rt.currentID}
	result, after, err := rt.dispatch(ctx, h, req)
	if !req.Notif {
		reply(ctx, conn, req, result, err)
	}
	if after != nil {
		after()
	}
}

func (rt *runtime) dispatch(ctx context.Context, h *Host, req *jsonrpc2.Request) (any, func(), error) {
	switch req.Method {
	case "plugin.initialize":
		return rt.initialize(ctx, h, req)
	case "plugin.ping":
		return "pong", nil, nil
	case "plugin.shutdown":
		if rt.plugin.Shutdown != nil {
			rt.plugin.Shutdown(ctx, h)
		}
		return struct{}{}, func() { rt.stopOnce.Do(func() { close(rt.stop) }) }, nil
	case "ui.event":
		if rt.plugin.UIEvent == nil {
			return nil, nil, methodNotFound(req.Method)
		}
		var ev UIEvent
		if err := decode(req, &ev); err != nil {
			return nil, nil, err
		}
		result, err := rt.plugin.UIEvent(ctx, h, ev)
		return result, nil, err
	case "command.run":
		if rt.plugin.Command == nil {
			return nil, nil, methodNotFound(req.Method)
		}
		var cmd CommandRun
		if err := decode(req, &cmd); err != nil {
			return nil, nil, err
		}
		result, err := rt.plugin.Command(ctx, h, cmd)
		return result, nil, err
	case "bus.event":
		rt.busEvent(ctx, h, req)
		return nil, nil, nil
	case "bus.closed":
		return nil, nil, nil
	}
	return nil, nil, methodNotFound(req.Method)
}

func (rt *runtime) initialize(ctx context.Context, h *Host, req *jsonrpc2.Request) (any, func(), error) {
	var p initializeParams
	if err := decode(req, &p); err != nil {
		return nil, nil, err
	}
	if p.Protocol != ProtocolVersion {
		return nil, nil, &jsonrpc2.Error{Code: codeInvalidParams, Message: fmt.Sprintf("unsupported protocol %d, want %d", p.Protocol, ProtocolVersion)}
	}
	rt.pluginID.Store(p.PluginID)
	return initializeResult{Protocol: ProtocolVersion}, func() { rt.runInit(ctx, h) }, nil
}

func (rt *runtime) runInit(ctx context.Context, h *Host) {
	if rt.plugin.Init == nil {
		return
	}
	if err := rt.plugin.Init(ctx, h); err != nil {
		rt.initErr <- fmt.Errorf("init: %w", err)
	}
}

func (rt *runtime) busEvent(ctx context.Context, h *Host, req *jsonrpc2.Request) {
	if rt.plugin.BusEvent == nil {
		return
	}
	var p busEventParams
	if err := decode(req, &p); err != nil {
		fmt.Fprintln(os.Stderr, "plugin: bad bus.event:", err)
		return
	}
	rt.plugin.BusEvent(ctx, h, p.Subscription, p.Event)
}

func (rt *runtime) currentID() string {
	id, _ := rt.pluginID.Load().(string)
	return id
}

func reply(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request, result any, err error) {
	if err == nil {
		_ = conn.Reply(ctx, req.ID, result)
		return
	}
	var rpcErr *jsonrpc2.Error
	if !errors.As(err, &rpcErr) {
		rpcErr = &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: err.Error()}
	}
	_ = conn.ReplyWithError(ctx, req.ID, rpcErr)
}

func decode(req *jsonrpc2.Request, v any) error {
	if req.Params == nil {
		return nil
	}
	if err := json.Unmarshal(*req.Params, v); err != nil {
		return &jsonrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
	}
	return nil
}

func methodNotFound(method string) error {
	return &jsonrpc2.Error{Code: codeMethodNotFound, Message: "method not found: " + method}
}
