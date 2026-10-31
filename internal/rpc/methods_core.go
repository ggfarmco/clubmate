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
	"clubmate/internal/proc"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
	"clubmate/internal/workspace"
	"clubmate/pkg/id"
)

const (
	workspacePrefix    = "prj"
	shutdownReplyGrace = 100 * time.Millisecond

	notifyCoreStopping  = "core.stopping"
	notifyWorkspaceShow = "workspace.show"

	handleWorkspaceShow = "workspace.show"
	handleUIConfirm     = "ui.confirm"
)

var ownerPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

type idParams struct {
	ID string `json:"id"`
}

type createWorkspaceParams struct {
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
	WorkspaceID string `json:"workspaceId"`
	Owner       string `json:"owner"`
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
	Flavor          string `json:"flavor"`
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
			return pingResult{Version: buildinfo.String(), PID: os.Getpid(), SessionProtocol: sessionhost.Protocol, Flavor: buildinfo.Flavor}, nil
		}),
		"core.shutdown": method(s.shutdownCore),
		"workspace.create": method(func(ctx context.Context, p createWorkspaceParams) (workspace.Workspace, error) {
			return svc.CreateWorkspace(ctx, p.Name, p.Repos)
		}),
		"workspace.list": method(func(ctx context.Context, _ empty) ([]workspace.Workspace, error) {
			list, err := svc.ListWorkspaces(ctx)
			return nonNil(list), err
		}),
		"workspace.get": method(func(ctx context.Context, p idParams) (workspace.Workspace, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return workspace.Workspace{}, err
			}
			return svc.GetWorkspace(ctx, p.ID)
		}),
		"workspace.folder.add": method(func(ctx context.Context, p addRepoParams) (workspace.Workspace, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return workspace.Workspace{}, err
			}
			return svc.AddRepo(ctx, p.ID, p.Path)
		}),
		"workspace.delete": method(func(ctx context.Context, p idParams) (empty, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return empty{}, err
			}
			if err := svc.DeleteWorkspace(ctx, p.ID); err != nil {
				return empty{}, err
			}
			if u, err := s.uiState(); err == nil {
				u.store.DropWorkspace(p.ID)
			}
			return empty{}, nil
		}),
		"workspace.rename": method(func(ctx context.Context, p renameParams) (workspace.Workspace, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return workspace.Workspace{}, err
			}
			return svc.RenameWorkspace(ctx, p.ID, p.Name)
		}),
		"workspace.show": method(s.workspaceShow),
		"workspace.open": method(s.workspaceOpen),
		"workspace.home": method(s.workspaceHome),
		"workspace.touch": method(func(ctx context.Context, p idParams) (empty, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return empty{}, err
			}
			return empty{}, svc.TouchWorkspace(ctx, p.ID)
		}),
		"workspace.worktree.set": method(func(ctx context.Context, p worktreeSetParams) (workspace.Workspace, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return workspace.Workspace{}, err
			}
			return svc.SetWorktree(ctx, p.ID, p.Repo, p.Path)
		}),
		"workspace.worktree.check": method(func(ctx context.Context, p idParams) (workspace.Workspace, error) {
			if err := requireID(workspacePrefix, p.ID); err != nil {
				return workspace.Workspace{}, err
			}
			return svc.CheckWorktrees(ctx, p.ID)
		}),
		"client.register": method(s.clientRegister),
		"session.create": method(func(ctx context.Context, p session.CreateRequest) (session.Session, error) {
			if err := requireID(workspacePrefix, p.WorkspaceID); err != nil {
				return session.Session{}, err
			}
			p.Owner = CallerFrom(ctx).PluginID
			return svc.CreateSession(ctx, p)
		}),
		"session.list": method(func(ctx context.Context, p listSessionsParams) ([]session.Session, error) {
			if err := optionalID(workspacePrefix, p.WorkspaceID); err != nil {
				return nil, err
			}
			if p.Owner != "" && !ownerPattern.MatchString(p.Owner) {
				return nil, &jsonrpc2.Error{Code: codeInvalidParams, Message: fmt.Sprintf("invalid owner: %q", p.Owner)}
			}
			list, err := svc.ListSessions(ctx, session.ListFilter{WorkspaceID: p.WorkspaceID, Owner: p.Owner})
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
			if err := optionalID(workspacePrefix, f.WorkspaceID); err != nil {
				return nil, err
			}
			if err := optionalGroupID(f.SessionID); err != nil {
				return nil, err
			}
			nodes, err := svc.ProcessTree(ctx, f)
			for i := range nodes {
				nodes[i].Process.Args = nonNil(svc.ProcessArgs(nodes[i].Process))
			}
			return nonNil(nodes), err
		}),
		"process.kill": method(func(ctx context.Context, p core.KillRequest) (killResult, error) {
			if err := optionalGroupID(p.SessionID); err != nil {
				return killResult{}, err
			}
			if err := optionalID(workspacePrefix, p.WorkspaceID); err != nil {
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

func (s *Server) workspaceShow(ctx context.Context, p idParams) (openResult, error) {
	if err := s.requireWorkspace(ctx, p.ID); err != nil {
		return openResult{}, err
	}
	return openResult{Clients: s.notifyClients(notifyWorkspaceShow, p, connFrom(ctx), handleWorkspaceShow)}, nil
}

func (s *Server) workspaceOpen(ctx context.Context, p workspaceOpenParams) (workspaceOpenResult, error) {
	ws, created, err := s.svc.OpenFolders(ctx, p.Folders)
	if err != nil {
		return workspaceOpenResult{}, err
	}
	clients := s.notifyClients(notifyWorkspaceShow, idParams{ID: ws.ID}, connFrom(ctx), handleWorkspaceShow)
	return workspaceOpenResult{ID: ws.ID, Created: created, Clients: clients}, nil
}

func (s *Server) workspaceHome(ctx context.Context, _ empty) (workspaceHomeResult, error) {
	ws, err := s.svc.HomeWorkspace(ctx)
	if err != nil {
		return workspaceHomeResult{}, err
	}
	clients := s.notifyClients(notifyWorkspaceShow, idParams{ID: ws.ID}, connFrom(ctx), handleWorkspaceShow)
	return workspaceHomeResult{ID: ws.ID, Clients: clients}, nil
}

type registerParams struct {
	Handles []string `json:"handles"`
}

func (s *Server) clientRegister(ctx context.Context, p registerParams) (empty, error) {
	if CallerFrom(ctx).IsPlugin() {
		return empty{}, invalidParams("client.register is not available to plugins")
	}
	for _, h := range p.Handles {
		if h != handleWorkspaceShow && h != handleUIConfirm && h != handleUISnapshot && h != handleUIPresent && h != handleUIKey && h != handleBrowserCall {
			return empty{}, invalidParams(fmt.Sprintf("unknown handle: %q", h))
		}
	}
	for _, h := range p.Handles {
		s.registerClient(connFrom(ctx), h)
	}
	return empty{}, nil
}
