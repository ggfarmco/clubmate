package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
)

const (
	writeTimeout       = 30 * time.Second
	codeMethodNotFound = -32601
)

var ErrClosed = errors.New("acp: connection closed")

type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("acp error %d: %s", e.Code, e.Message)
}

type Handler interface {
	Notify(method string, params json.RawMessage)
	Request(ctx context.Context, id, method string, params json.RawMessage) (any, error)
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    error
}

type Conn struct {
	r       io.Reader
	w       io.Writer
	handler Handler
	badLine func([]byte)

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[string]chan response
	queue   []message
	ended   bool
	wake    chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func NewConn(r io.Reader, w io.Writer, h Handler, badLine func([]byte)) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{r: r, w: w, handler: h, badLine: badLine, pending: map[string]chan response{}, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (c *Conn) Start() {
	go c.read()
	go c.dispatch()
}

func (c *Conn) Done() <-chan struct{} {
	return c.done
}

func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.ended {
		c.mu.Unlock()
		return ErrClosed
	}
	c.nextID++
	id := strconv.FormatInt(c.nextID, 10)
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(message{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: mustRaw(params)}); err != nil {
		c.forget(id)
		return err
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		if result == nil || len(res.result) == 0 {
			return nil
		}
		return json.Unmarshal(res.result, result)
	case <-ctx.Done():
		c.forget(id)
		return ctx.Err()
	}
}

func (c *Conn) Notify(method string, params any) error {
	return c.send(message{JSONRPC: "2.0", Method: method, Params: mustRaw(params)})
}

func (c *Conn) forget(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Conn) send(m message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if d, ok := c.w.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = d.SetWriteDeadline(time.Now().Add(writeTimeout))
	}
	if _, err := c.w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrClosed, err)
	}
	return nil
}

func (c *Conn) read() {
	br := bufio.NewReader(c.r)
	for {
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var m message
			if json.Unmarshal(trimmed, &m) != nil || (m.Method == "" && len(m.ID) == 0) {
				if c.badLine != nil {
					c.badLine(trimmed)
				}
			} else {
				c.push(m, false)
			}
		}
		if err != nil {
			c.push(message{}, true)
			return
		}
	}
}

func (c *Conn) push(m message, end bool) {
	c.mu.Lock()
	if end {
		c.ended = true
	} else {
		c.queue = append(c.queue, m)
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Conn) dispatch() {
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			if c.ended {
				pending := c.pending
				c.pending = map[string]chan response{}
				c.mu.Unlock()
				for _, ch := range pending {
					ch <- response{err: ErrClosed}
				}
				c.cancel()
				close(c.done)
				return
			}
			c.mu.Unlock()
			<-c.wake
			continue
		}
		m := c.queue[0]
		c.queue[0] = message{}
		c.queue = c.queue[1:]
		c.mu.Unlock()
		c.deliver(m)
	}
}

func (c *Conn) deliver(m message) {
	switch {
	case m.Method == "":
		id := idKey(m.ID)
		c.mu.Lock()
		ch, ok := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if !ok {
			return
		}
		if m.Error != nil {
			ch <- response{err: m.Error}
			return
		}
		ch <- response{result: m.Result}
	case len(m.ID) == 0:
		c.handler.Notify(m.Method, m.Params)
	default:
		go c.serve(m)
	}
}

func (c *Conn) serve(m message) {
	result, err := c.handler.Request(c.ctx, idKey(m.ID), m.Method, m.Params)
	reply := message{JSONRPC: "2.0", ID: m.ID}
	if err != nil {
		var rpcErr *Error
		if !errors.As(err, &rpcErr) {
			rpcErr = &Error{Code: CodeInternal, Message: err.Error()}
		}
		reply.Error = rpcErr
	} else if reply.Result = mustRaw(result); reply.Result == nil {
		reply.Result = json.RawMessage("null")
	}
	_ = c.send(reply)
}

func MethodNotFound(method string) error {
	return &Error{Code: codeMethodNotFound, Message: "method not found: " + method}
}

func RequestKey(raw json.RawMessage) string {
	return idKey(raw)
}

func idKey(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(bytes.TrimSpace(raw))
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
