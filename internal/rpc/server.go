package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/kv"
)

const (
	Version = "0.1.0"

	acceptBackoffMin = 5 * time.Millisecond
	acceptBackoffMax = time.Second

	notifyClientsTimeout = time.Second
)

type handlerFunc func(ctx context.Context, params *json.RawMessage) (any, error)

type empty struct{}

type Server struct {
	svc        *core.Service
	bus        *bus.Bus
	kv         *kv.Store
	methods    map[string]handlerFunc
	pluginList atomic.Pointer[func() any]
	ui         atomic.Pointer[uiState]
	presence   presence
	logger     *slog.Logger
	shutdown   atomic.Pointer[func()]
	clientsMu  sync.Mutex
	clients    map[*jsonrpc2.Conn]struct{}
}

type ServerOption func(*Server)

func WithKV(store *kv.Store) ServerOption {
	return func(s *Server) { s.kv = store }
}

func WithLogger(l *slog.Logger) ServerOption {
	return func(s *Server) { s.logger = l }
}

func NewServer(svc *core.Service, b *bus.Bus, opts ...ServerOption) *Server {
	s := &Server{svc: svc, bus: b, clients: map[*jsonrpc2.Conn]struct{}{}, logger: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		opt(s)
	}
	s.methods = s.coreMethods()
	maps.Copy(s.methods, s.busMethods())
	maps.Copy(s.methods, s.storageMethods())
	maps.Copy(s.methods, s.uiStateMethods())
	maps.Copy(s.methods, s.pluginMethods())
	maps.Copy(s.methods, s.uiMethods())
	maps.Copy(s.methods, s.fsMethods())
	return s
}

func (s *Server) SetShutdown(fn func()) {
	s.shutdown.Store(&fn)
}

func Serve(ctx context.Context, l net.Listener, svc *core.Service) error {
	return NewServer(svc, nil).Serve(ctx, l)
}

func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	stopListener := context.AfterFunc(ctx, func() { l.Close() })
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	defer stopListener()
	var backoff time.Duration
	for {
		nc, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if !retryableAccept(err) {
				return fmt.Errorf("accept: %w", err)
			}
			backoff = nextBackoff(backoff)
			if !sleep(ctx, backoff) {
				return nil
			}
			continue
		}
		backoff = 0
		wg.Go(func() {
			_, done := s.open(ctx, jsonrpc2.NewPlainObjectStream(nc), Caller{})
			<-done
		})
	}
}

func (s *Server) ServeConn(ctx context.Context, stream jsonrpc2.ObjectStream, caller Caller) *jsonrpc2.Conn {
	conn, _ := s.open(ctx, stream, caller)
	return conn
}

func (s *Server) open(ctx context.Context, stream jsonrpc2.ObjectStream, caller Caller) (*jsonrpc2.Conn, <-chan struct{}) {
	subs := newSubscriptions()
	ctx = withSubscriptions(WithCaller(ctx, caller), subs)
	conn := jsonrpc2.NewConn(ctx, stream, jsonrpc2.AsyncHandler(s.handler()))
	if !caller.IsPlugin() {
		s.trackClient(conn)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-conn.DisconnectNotify()
		s.untrackClient(conn)
		subs.closeAll()
	}()
	return conn, done
}

func (s *Server) trackClient(conn *jsonrpc2.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[conn] = struct{}{}
}

func (s *Server) untrackClient(conn *jsonrpc2.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.clients, conn)
}

func (s *Server) notifyClients(method string, params any, except *jsonrpc2.Conn) int {
	s.clientsMu.Lock()
	conns := slices.Collect(maps.Keys(s.clients))
	s.clientsMu.Unlock()
	var wg sync.WaitGroup
	var sent atomic.Int32
	for _, conn := range conns {
		if conn == except {
			continue
		}
		wg.Go(func() {
			if conn.Notify(context.Background(), method, params) == nil {
				sent.Add(1)
			}
		})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(notifyClientsTimeout):
	}
	return int(sent.Load())
}

func (s *Server) handler() jsonrpc2.Handler {
	return jsonrpc2.HandlerWithError(func(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		fn, ok := s.methods[req.Method]
		if !ok {
			return nil, &jsonrpc2.Error{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
		}
		result, err := fn(withConn(ctx, conn), req.Params)
		if err != nil {
			return nil, toRPCError(err)
		}
		return result, nil
	}).SuppressErrClosed()
}

func retryableAccept(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
}

func nextBackoff(d time.Duration) time.Duration {
	if d == 0 {
		return acceptBackoffMin
	}
	return min(d*2, acceptBackoffMax)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func method[P, R any](fn func(ctx context.Context, p P) (R, error)) handlerFunc {
	return func(ctx context.Context, raw *json.RawMessage) (any, error) {
		var p P
		if raw != nil {
			if err := json.Unmarshal(*raw, &p); err != nil {
				return nil, &jsonrpc2.Error{Code: codeInvalidParams, Message: fmt.Sprintf("decode params: %v", err)}
			}
		}
		result, err := fn(ctx, p)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
