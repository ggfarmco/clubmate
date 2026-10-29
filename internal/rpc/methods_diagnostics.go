package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/sys/unix"

	"clubmate/internal/buildinfo"
	"clubmate/internal/diagnostics"
	"clubmate/internal/session"
	"clubmate/internal/sessionhost"
)

const diagnosticsExtraMax = 10 << 20

var (
	errNoDiagnostics  = errors.New("diagnostics not configured")
	diagnosticsExtras = map[string]bool{"settings.json": true, diagnostics.WindowName: true}
)

type diagnosticsParams struct {
	ProjectID  string            `json:"projectId"`
	AppVersion string            `json:"appVersion"`
	Extra      map[string][]byte `json:"extra"`
}

type diagnosticsInfo struct {
	Version           string    `json:"version"`
	Commit            string    `json:"commit"`
	Flavor            string    `json:"flavor"`
	CoreVersion       string    `json:"coreVersion"`
	SessionProtocol   int       `json:"sessionProtocol"`
	MacOS             string    `json:"macOS"`
	Arch              string    `json:"arch"`
	CoreUptimeSeconds int64     `json:"coreUptimeSeconds"`
	CollectedAt       time.Time `json:"collectedAt"`
	AppVersion        string    `json:"appVersion,omitempty"`
}

type diagnosticsWorkspace struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Repos   []string `json:"repos"`
	Roots   []string `json:"roots"`
	Missing []string `json:"missing"`
}

type diagnosticsSession struct {
	ID        string        `json:"id"`
	Workspace string        `json:"workspace"`
	Title     string        `json:"title"`
	Owner     string        `json:"owner"`
	Cwd       string        `json:"cwd"`
	State     session.State `json:"state"`
	ExitCode  *int          `json:"exitCode"`
	Created   time.Time     `json:"created"`
}

func (s *Server) diagnosticsMethods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"diagnostics.collect": method(s.collectDiagnostics),
	}
}

func (s *Server) collectDiagnostics(ctx context.Context, p diagnosticsParams) (diagnostics.Result, error) {
	if CallerFrom(ctx).IsPlugin() {
		return diagnostics.Result{}, invalidParams("diagnostics.collect is not available to plugins")
	}
	for name, data := range p.Extra {
		if !diagnosticsExtras[name] {
			return diagnostics.Result{}, invalidParams(fmt.Sprintf("extra: unknown file %q", name))
		}
		if len(data) > diagnosticsExtraMax {
			return diagnostics.Result{}, invalidParams(fmt.Sprintf("extra: %s is over %d bytes", name, diagnosticsExtraMax))
		}
	}
	if err := optionalID(projectPrefix, p.ProjectID); err != nil {
		return diagnostics.Result{}, err
	}
	if s.dirs == nil {
		return diagnostics.Result{}, errNoDiagnostics
	}
	now := time.Now()
	entries, err := s.diagnosticsEntries(ctx, p, now)
	if err != nil {
		return diagnostics.Result{}, err
	}
	for _, name := range []string{"settings.json", diagnostics.WindowName} {
		if data, ok := p.Extra[name]; ok {
			entries = append(entries, diagnostics.Entry{Name: name, Data: data})
		}
	}
	return diagnostics.Write(s.dirs.DiagnosticsDir(), now, entries)
}

func (s *Server) diagnosticsEntries(ctx context.Context, p diagnosticsParams, now time.Time) ([]diagnostics.Entry, error) {
	var entries []diagnostics.Entry
	add := func(name string, v any) error {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", name, err)
		}
		entries = append(entries, diagnostics.Entry{Name: name, Data: append(data, '\n')})
		return nil
	}
	if err := add("info.json", s.diagnosticsInfo(p.AppVersion, now)); err != nil {
		return nil, err
	}
	logs, err := s.diagnosticsLogs()
	if err != nil {
		return nil, err
	}
	entries = append(entries, logs...)
	var plugins any = []struct{}{}
	if fn := s.pluginList.Load(); fn != nil {
		plugins = (*fn)()
	}
	workspaces, err := s.diagnosticsWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := s.diagnosticsSessions(ctx)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(add("plugins.json", plugins), add("workspaces.json", workspaces), add("sessions.json", sessions)); err != nil {
		return nil, err
	}
	u, err := s.uiState()
	if err != nil {
		return entries, nil
	}
	projects := s.presence.projects()
	if p.ProjectID != "" {
		projects = []string{p.ProjectID}
	}
	for _, id := range projects {
		if err := add("ui/"+id+".json", nonNil(u.store.Get(id))); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (s *Server) diagnosticsInfo(appVersion string, now time.Time) diagnosticsInfo {
	macOS, _ := unix.Sysctl("kern.osproductversion")
	return diagnosticsInfo{
		Version:           buildinfo.Version,
		Commit:            buildinfo.Commit,
		Flavor:            buildinfo.Flavor,
		CoreVersion:       buildinfo.String(),
		SessionProtocol:   sessionhost.Protocol,
		MacOS:             macOS,
		Arch:              runtime.GOARCH,
		CoreUptimeSeconds: int64(now.Sub(s.started).Seconds()),
		CollectedAt:       now.UTC(),
		AppVersion:        appVersion,
	}
}

func (s *Server) diagnosticsLogs() ([]diagnostics.Entry, error) {
	plugins, err := filepath.Glob(filepath.Join(s.dirs.PluginsDir(), "*.log"))
	if err != nil {
		return nil, err
	}
	sources := [][2]string{{"logs/core.log", s.dirs.CoreLog()}}
	for _, path := range plugins {
		sources = append(sources, [2]string{"logs/plugins/" + filepath.Base(path), path})
	}
	var entries []diagnostics.Entry
	for _, src := range sources {
		name := src[0]
		data, err := diagnostics.LogTail(src[1], diagnostics.LogTailMax)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		entries = append(entries, diagnostics.Entry{Name: name, Data: data})
	}
	return entries, nil
}

func (s *Server) diagnosticsWorkspaces(ctx context.Context) ([]diagnosticsWorkspace, error) {
	list, err := s.svc.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]diagnosticsWorkspace, 0, len(list))
	for _, p := range list {
		out = append(out, diagnosticsWorkspace{ID: p.ID, Name: p.Name, Repos: nonNil(p.Repos), Roots: nonNil(p.Roots), Missing: nonNil(p.Missing)})
	}
	return out, nil
}

func (s *Server) diagnosticsSessions(ctx context.Context) ([]diagnosticsSession, error) {
	list, err := s.svc.ListSessions(ctx, session.ListFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]diagnosticsSession, 0, len(list))
	for _, x := range list {
		out = append(out, diagnosticsSession{ID: x.ID, Workspace: x.ProjectID, Title: x.Title, Owner: x.Owner, Cwd: x.Cwd, State: x.State, ExitCode: x.ExitCode, Created: x.StartedAt})
	}
	return out, nil
}
