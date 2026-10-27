package rpc

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/core"
	"clubmate/internal/id"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
)

const (
	projectPrefix      = "prj"
	shutdownReplyGrace = 100 * time.Millisecond

	notifyCoreStopping = "core.stopping"
)

var ownerPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

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
	PID     int    `json:"pid"`
}

type killResult struct {
	Killed []int `json:"killed"`
}

func (s *Server) coreMethods() map[string]handlerFunc {
	svc := s.svc
	return map[string]handlerFunc{
		"core.ping": method(func(context.Context, empty) (pingResult, error) {
			return pingResult{Version: Version, PID: os.Getpid()}, nil
		}),
		"core.shutdown": method(s.shutdownCore),
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
			if err := svc.DeleteProject(ctx, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, s.deleteUIState(ctx, p.ID)
		}),
		"session.create": method(func(ctx context.Context, p session.CreateRequest) (session.Session, error) {
			if err := requireID(projectPrefix, p.ProjectID); err != nil {
				return session.Session{}, err
			}
			p.Owner = CallerFrom(ctx).PluginID
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
			if err := optionalID(projectPrefix, p.ProjectID); err != nil {
				return killResult{}, err
			}
			killed, err := svc.KillProcesses(ctx, p)
			return killResult{Killed: nonNil(killed)}, err
		}),
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

func (s *Server) shutdownCore(ctx context.Context, _ empty) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("core.shutdown is not available to plugins")
	}
	if fn := s.shutdown.Load(); fn != nil {
		time.AfterFunc(shutdownReplyGrace, func() {
			s.notifyClients(notifyCoreStopping)
			(*fn)()
		})
	}
	return empty{}, nil
}
