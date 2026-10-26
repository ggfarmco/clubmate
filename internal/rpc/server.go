package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/core"
	"clubmate/internal/project"
	"clubmate/internal/session"
)

const Version = "0.1.0"

type handlerFunc func(ctx context.Context, params *json.RawMessage) (any, error)

type idParams struct {
	ID string `json:"id"`
}

type createProjectParams struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

type addRepoParams struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type listSessionsParams struct {
	ProjectID string `json:"projectId"`
}

type pingResult struct {
	Version string `json:"version"`
}

type killResult struct {
	Killed []int `json:"killed"`
}

type empty struct{}

func Serve(ctx context.Context, l net.Listener, svc *core.Service) error {
	ctx, cancel := context.WithCancel(ctx)
	stopListener := context.AfterFunc(ctx, func() { l.Close() })
	handler := newHandler(svc)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	defer stopListener()
	for {
		nc, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		wg.Go(func() { serveConn(ctx, nc, handler) })
	}
}

func serveConn(ctx context.Context, nc net.Conn, handler jsonrpc2.Handler) {
	conn := jsonrpc2.NewConn(ctx, jsonrpc2.NewPlainObjectStream(nc), handler)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	<-conn.DisconnectNotify()
}

func newHandler(svc *core.Service) jsonrpc2.Handler {
	methods := methodTable(svc)
	return jsonrpc2.HandlerWithError(func(ctx context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		fn, ok := methods[req.Method]
		if !ok {
			return nil, &jsonrpc2.Error{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
		}
		result, err := fn(ctx, req.Params)
		if err != nil {
			return nil, toRPCError(err)
		}
		return result, nil
	})
}

func methodTable(svc *core.Service) map[string]handlerFunc {
	return map[string]handlerFunc{
		"core.ping": method(func(context.Context, empty) (pingResult, error) {
			return pingResult{Version: Version}, nil
		}),
		"project.create": method(func(ctx context.Context, p createProjectParams) (project.Project, error) {
			return svc.CreateProject(ctx, p.Name, p.Repos)
		}),
		"project.list": method(func(ctx context.Context, _ empty) ([]project.Project, error) {
			return svc.ListProjects(ctx)
		}),
		"project.get": method(func(ctx context.Context, p idParams) (project.Project, error) {
			return svc.GetProject(ctx, p.ID)
		}),
		"project.addRepo": method(func(ctx context.Context, p addRepoParams) (project.Project, error) {
			return svc.AddRepo(ctx, p.ID, p.Path)
		}),
		"project.delete": method(func(ctx context.Context, p idParams) (empty, error) {
			return empty{}, svc.DeleteProject(ctx, p.ID)
		}),
		"session.create": method(func(ctx context.Context, p session.CreateRequest) (session.Session, error) {
			return svc.CreateSession(ctx, p)
		}),
		"session.list": method(func(ctx context.Context, p listSessionsParams) ([]session.Session, error) {
			return svc.ListSessions(ctx, p.ProjectID)
		}),
		"session.get": method(func(ctx context.Context, p idParams) (session.Session, error) {
			return svc.GetSession(ctx, p.ID)
		}),
		"session.kill": method(func(ctx context.Context, p idParams) (empty, error) {
			return empty{}, svc.KillSession(ctx, p.ID)
		}),
		"process.tree": method(svc.ProcessTree),
		"process.kill": method(func(ctx context.Context, p core.KillRequest) (killResult, error) {
			killed, err := svc.KillProcesses(ctx, p)
			return killResult{Killed: killed}, err
		}),
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
