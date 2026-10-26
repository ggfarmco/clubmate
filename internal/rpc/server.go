package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/core"
	"clubmate/internal/id"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
)

const (
	Version = "0.1.0"

	projectPrefix = "prj"

	acceptBackoffMin = 5 * time.Millisecond
	acceptBackoffMax = time.Second
)

var ownerPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

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
	Owner     string `json:"owner"`
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
		wg.Go(func() { serveConn(ctx, nc, handler) })
	}
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
			return encodeProject(svc.CreateProject(ctx, p.Name, p.Repos))
		}),
		"project.list": method(func(ctx context.Context, _ empty) ([]project.Project, error) {
			list, err := svc.ListProjects(ctx)
			for i := range list {
				list[i].Repos = nonNil(list[i].Repos)
			}
			return nonNil(list), err
		}),
		"project.get": method(func(ctx context.Context, p idParams) (project.Project, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return project.Project{}, err
			}
			return encodeProject(svc.GetProject(ctx, p.ID))
		}),
		"project.addRepo": method(func(ctx context.Context, p addRepoParams) (project.Project, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return project.Project{}, err
			}
			return encodeProject(svc.AddRepo(ctx, p.ID, p.Path))
		}),
		"project.delete": method(func(ctx context.Context, p idParams) (empty, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, svc.DeleteProject(ctx, p.ID)
		}),
		"session.create": method(func(ctx context.Context, p session.CreateRequest) (session.Session, error) {
			if err := requireID(projectPrefix, p.ProjectID); err != nil {
				return session.Session{}, err
			}
			p.Owner = ""
			return svc.CreateSession(ctx, p)
		}),
		"session.list": method(func(ctx context.Context, p listSessionsParams) ([]session.Session, error) {
			if err := optionalID(projectPrefix, p.ProjectID); err != nil {
				return nil, err
			}
			if p.Owner != "" && !ownerPattern.MatchString(p.Owner) {
				return nil, &jsonrpc2.Error{Code: codeInvalidParams, Message: fmt.Sprintf("invalid owner: %q", p.Owner)}
			}
			list, err := svc.ListSessions(ctx, session.ListFilter{ProjectID: p.ProjectID, Owner: p.Owner})
			return nonNil(list), err
		}),
		"session.get": method(func(ctx context.Context, p idParams) (session.Session, error) {
			if err := requireID(session.IDPrefix, p.ID); err != nil {
				return session.Session{}, err
			}
			return svc.GetSession(ctx, p.ID)
		}),
		"session.kill": method(func(ctx context.Context, p idParams) (empty, error) {
			if err := requireID(session.IDPrefix, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, svc.KillSession(ctx, p.ID)
		}),
		"session.remove": method(func(ctx context.Context, p idParams) (empty, error) {
			if err := requireID(session.IDPrefix, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, svc.RemoveSession(ctx, p.ID)
		}),
		"process.tree": method(func(ctx context.Context, f core.TreeFilter) ([]proc.Node, error) {
			if err := optionalID(projectPrefix, f.ProjectID); err != nil {
				return nil, err
			}
			if err := optionalID(session.IDPrefix, f.SessionID); err != nil {
				return nil, err
			}
			nodes, err := svc.ProcessTree(ctx, f)
			return nonNil(nodes), err
		}),
		"process.kill": method(func(ctx context.Context, p core.KillRequest) (killResult, error) {
			if err := optionalID(session.IDPrefix, p.SessionID); err != nil {
				return killResult{}, err
			}
			killed, err := svc.KillProcesses(ctx, p)
			return killResult{Killed: nonNil(killed)}, err
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

func requireID(prefix, s string) error {
	if id.Valid(prefix, s) {
		return nil
	}
	return &jsonrpc2.Error{Code: codeInvalidParams, Message: fmt.Sprintf("invalid %s id: %q", prefix, s)}
}

func optionalID(prefix, s string) error {
	if s == "" {
		return nil
	}
	return requireID(prefix, s)
}

func encodeProject(p project.Project, err error) (project.Project, error) {
	p.Repos = nonNil(p.Repos)
	return p, err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
