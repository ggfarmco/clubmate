package rpc

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/internal/buildinfo"
	"clubmate/internal/core"
	"clubmate/internal/id"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
)

const (
	projectPrefix      = "prj"
	shutdownReplyGrace = 100 * time.Millisecond

	notifyCoreStopping = "core.stopping"
	notifyProjectOpen  = "project.open"

	handleProjectOpen = "project.open"
	handleUIConfirm   = "ui.confirm"
)

var ownerPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

type idParams struct {
	ID string `json:"id"`
}

type createProjectParams struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

type renameParams struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workspaceOpenParams struct {
	Folders []string `json:"folders"`
}

type workspaceOpenResult struct {
	ID      string `json:"id"`
	Created bool   `json:"created"`
	Clients int    `json:"clients"`
}

type workspaceHomeResult struct {
	ID      string `json:"id"`
	Clients int    `json:"clients"`
}

type addRepoParams struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type worktreeSetParams struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
	Path string `json:"path"`
}

type listSessionsParams struct {
	ProjectID string `json:"projectId"`
	Owner     string `json:"owner"`
}

type sessionSendParams struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Enter bool   `json:"enter"`
}

type sessionReadParams struct {
	ID    string `json:"id"`
	Bytes int    `json:"bytes"`
	Raw   bool   `json:"raw"`
}

type pingResult struct {
	Version         string `json:"version"`
	PID             int    `json:"pid"`
	SessionProtocol int    `json:"sessionProtocol"`
}

type openResult struct {
	Clients int `json:"clients"`
}

type killResult struct {
	Killed []int `json:"killed"`
}

func (s *Server) coreMethods() map[string]handlerFunc {
	svc := s.svc
	return map[string]handlerFunc{
		"core.ping": method(func(context.Context, empty) (pingResult, error) {
			return pingResult{Version: buildinfo.String(), PID: os.Getpid(), SessionProtocol: sessionhost.Protocol}, nil
		}),
		"core.shutdown": method(s.shutdownCore),
		"project.create": method(func(ctx context.Context, p createProjectParams) (project.Project, error) {
			return encodeProject(svc.CreateProject(ctx, p.Name, p.Repos))
		}),
		"project.list": method(func(ctx context.Context, _ empty) ([]project.Project, error) {
			list, err := svc.ListProjects(ctx)
			for i := range list {
				list[i].Repos = nonNil(list[i].Repos)
				list[i].Roots = nonNil(list[i].Roots)
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
			if u, err := s.uiState(); err == nil {
				u.store.DropProject(p.ID)
			}
			return empty{}, s.deleteUIState(ctx, p.ID)
		}),
		"project.rename": method(func(ctx context.Context, p renameParams) (project.Project, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return project.Project{}, err
			}
			return encodeProject(svc.RenameProject(ctx, p.ID, p.Name))
		}),
		"project.open":   method(s.projectOpen),
		"workspace.open": method(s.workspaceOpen),
		"workspace.home": method(s.workspaceHome),
		"workspace.touch": method(func(ctx context.Context, p idParams) (empty, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, svc.TouchProject(ctx, p.ID)
		}),
		"workspace.worktree.set": method(func(ctx context.Context, p worktreeSetParams) (project.Project, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return project.Project{}, err
			}
			return encodeProject(svc.SetWorktree(ctx, p.ID, p.Repo, p.Path))
		}),
		"workspace.worktree.check": method(func(ctx context.Context, p idParams) (project.Project, error) {
			if err := requireID(projectPrefix, p.ID); err != nil {
				return project.Project{}, err
			}
			return encodeProject(svc.CheckWorktrees(ctx, p.ID))
		}),
		"client.register": method(s.clientRegister),
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
		"session.send": method(func(ctx context.Context, p sessionSendParams) (empty, error) {
			if err := requireID(session.IDPrefix, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, svc.SendSession(ctx, p.ID, p.Text, p.Enter)
		}),
		"session.read": method(func(ctx context.Context, p sessionReadParams) (session.Snapshot, error) {
			if err := requireID(session.IDPrefix, p.ID); err != nil {
				return session.Snapshot{}, err
			}
			if p.Bytes < 0 || p.Bytes > session.ReadMax {
				return session.Snapshot{}, invalidParams(fmt.Sprintf("bytes must be 0..%d", session.ReadMax))
			}
			return svc.ReadSession(ctx, p.ID, p.Bytes, p.Raw)
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
			for i := range nodes {
				nodes[i].Process.Args = nonNil(proc.ReadArgs(nodes[i].Process.PID))
			}
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
	p.Roots = nonNil(p.Roots)
	return p, err
}

func (s *Server) shutdownCore(ctx context.Context, _ empty) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("core.shutdown is not available to plugins")
	}
	if fn := s.shutdown.Load(); fn != nil {
		time.AfterFunc(shutdownReplyGrace, func() {
			s.notifyClients(notifyCoreStopping, nil, nil, "")
			(*fn)()
		})
	}
	return empty{}, nil
}

func (s *Server) projectOpen(ctx context.Context, p idParams) (openResult, error) {
	if err := s.requireProject(ctx, p.ID); err != nil {
		return openResult{}, err
	}
	return openResult{Clients: s.notifyClients(notifyProjectOpen, p, connFrom(ctx), handleProjectOpen)}, nil
}

func (s *Server) workspaceOpen(ctx context.Context, p workspaceOpenParams) (workspaceOpenResult, error) {
	prj, created, err := s.svc.OpenFolders(ctx, p.Folders)
	if err != nil {
		return workspaceOpenResult{}, err
	}
	clients := s.notifyClients(notifyProjectOpen, idParams{ID: prj.ID}, connFrom(ctx), handleProjectOpen)
	return workspaceOpenResult{ID: prj.ID, Created: created, Clients: clients}, nil
}

func (s *Server) workspaceHome(ctx context.Context, _ empty) (workspaceHomeResult, error) {
	prj, err := s.svc.HomeWorkspace(ctx)
	if err != nil {
		return workspaceHomeResult{}, err
	}
	clients := s.notifyClients(notifyProjectOpen, idParams{ID: prj.ID}, connFrom(ctx), handleProjectOpen)
	return workspaceHomeResult{ID: prj.ID, Clients: clients}, nil
}

type registerParams struct {
	Handles []string `json:"handles"`
}

func (s *Server) clientRegister(ctx context.Context, p registerParams) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("client.register is not available to plugins")
	}
	for _, h := range p.Handles {
		if h != handleProjectOpen && h != handleUIConfirm {
			return empty{}, invalidParams(fmt.Sprintf("unknown handle: %q", h))
		}
	}
	for _, h := range p.Handles {
		s.registerClient(connFrom(ctx), h)
	}
	return empty{}, nil
}
