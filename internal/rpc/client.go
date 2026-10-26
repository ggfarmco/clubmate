package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/sourcegraph/jsonrpc2"
)

type Client struct {
	conn *jsonrpc2.Conn
}

func Dial(ctx context.Context, socket string) (*Client, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socket, err)
	}
	handler := jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) {
		return nil, &jsonrpc2.Error{Code: codeMethodNotFound, Message: "client accepts no calls"}
	})
	conn := jsonrpc2.NewConn(context.WithoutCancel(ctx), jsonrpc2.NewPlainObjectStream(nc), handler)
	return &Client{conn: conn}, nil
}

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	err := c.conn.Call(ctx, method, params, result)
	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		return &RemoteError{Code: int(rpcErr.Code), Message: rpcErr.Message}
	}
	return err
}

func (c *Client) Close() error {
	return c.conn.Close()
}
