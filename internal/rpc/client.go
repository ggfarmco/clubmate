package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/paths"
)

const notificationBuffer = 64

type Client struct {
	conn  *jsonrpc2.Conn
	inbox *inbox
}

type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type inbox struct {
	notes   chan Notification
	dropped atomic.Int64
}

func Dial(ctx context.Context, socket string) (*Client, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	if err := verifyConn(nc); err != nil {
		nc.Close()
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	in := &inbox{notes: make(chan Notification, notificationBuffer)}
	conn := jsonrpc2.NewConn(context.WithoutCancel(ctx), jsonrpc2.NewPlainObjectStream(nc), in)
	return &Client{conn: conn, inbox: in}, nil
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	err := c.conn.Call(ctx, method, params, result)
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return &RemoteError{Code: int(rpcErr.Code), Message: rpcErr.Message}
	}
	return err
}

func (c *Client) Notifications() <-chan Notification {
	return c.inbox.notes
}

func (c *Client) Dropped() int64 {
	return c.inbox.dropped.Load()
}

func (c *Client) Done() <-chan struct{} {
	return c.conn.DisconnectNotify()
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (in *inbox) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if !req.Notif {
		_ = conn.ReplyWithError(ctx, req.ID, &jsonrpc2.Error{Code: codeMethodNotFound, Message: "client accepts no calls"})
		return
	}
	n := Notification{Method: req.Method}
	if req.Params != nil {
		n.Params = *req.Params
	}
	select {
	case in.notes <- n:
	default:
		in.dropped.Add(1)
	}
}

func verifyConn(nc net.Conn) error {
	uc, ok := nc.(*net.UnixConn)
	if !ok {
		return paths.ErrUntrustedPeer
	}
	return paths.VerifyPeer(uc)
}
