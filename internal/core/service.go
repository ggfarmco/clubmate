package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
)

const DefaultKillGrace = 3 * time.Second

var (
	ErrNotTracked       = errors.New("process is not tracked by any session")
	ErrEmptyKillRequest = errors.New("kill request selects no processes")
)

type TreeFilter struct {
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId"`
}

type KillRequest struct {
	PIDs        []int  `json:"pids"`
	SessionID   string `json:"sessionId"`
	OrphansOnly bool   `json:"orphansOnly"`
}

type Option func(*Service)

type Service struct {
	projects  *project.Store
	sessions  *session.Manager
	tracker   *proc.Tracker
	snapshot  func() ([]proc.Process, error)
	killGrace time.Duration
}

func WithKillGrace(d time.Duration) Option {
	return func(s *Service) { s.killGrace = d }
}

func NewService(projects *project.Store, sessions *session.Manager, tracker *proc.Tracker, snapshot func() ([]proc.Process, error), opts ...Option) *Service {
	s := &Service{
		projects:  projects,
		sessions:  sessions,
		tracker:   tracker,
		snapshot:  snapshot,
		killGrace: DefaultKillGrace,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) CreateProject(ctx context.Context, name string, repos []string) (project.Project, error) {
	return s.projects.Create(ctx, name, repos)
}

func (s *Service) ListProjects(ctx context.Context) ([]project.Project, error) {
	return s.projects.List(ctx)
}

func (s *Service) GetProject(ctx context.Context, id string) (project.Project, error) {
	return s.projects.Get(ctx, id)
}

func (s *Service) AddRepo(ctx context.Context, id, path string) (project.Project, error) {
	return s.projects.AddRepo(ctx, id, path)
}

func (s *Service) DeleteProject(ctx context.Context, id string) error {
	if _, err := s.projects.Get(ctx, id); err != nil {
		return err
	}
	nodes, err := s.ProcessTree(ctx, TreeFilter{ProjectID: id})
	if err != nil {
		return err
	}
	if err := proc.KillTree(ctx, nodePIDs(nodes), s.killGrace); err != nil {
		return fmt.Errorf("kill project %s: %w", id, err)
	}
	return s.projects.Delete(ctx, id)
}

func (s *Service) CreateSession(ctx context.Context, req session.CreateRequest) (session.Session, error) {
	p, err := s.projects.Get(ctx, req.ProjectID)
	if err != nil {
		return session.Session{}, err
	}
	if req.Cwd == "" {
		if req.Cwd, err = defaultCwd(p); err != nil {
			return session.Session{}, err
		}
	}
	return s.sessions.Create(ctx, req)
}

func (s *Service) ListSessions(ctx context.Context, projectID string) ([]session.Session, error) {
	return s.sessions.List(ctx, projectID)
}

func (s *Service) GetSession(ctx context.Context, id string) (session.Session, error) {
	return s.sessions.Get(ctx, id)
}

func (s *Service) KillSession(ctx context.Context, id string) error {
	if _, err := s.sessions.Get(ctx, id); err != nil {
		return err
	}
	nodes, err := s.ProcessTree(ctx, TreeFilter{SessionID: id})
	if err != nil {
		return err
	}
	if err := proc.KillTree(ctx, nodePIDs(nodes), s.killGrace); err != nil {
		return fmt.Errorf("kill session %s: %w", id, err)
	}
	return nil
}

func (s *Service) ProcessTree(ctx context.Context, f TreeFilter) ([]proc.Node, error) {
	sessions, err := s.sessions.List(ctx, "")
	if err != nil {
		return nil, err
	}
	procs, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	projectOf := make(map[string]string, len(sessions))
	roots := make([]proc.SessionRoot, 0, len(sessions))
	for _, sess := range sessions {
		projectOf[sess.ID] = sess.ProjectID
		roots = append(roots, rootOf(sess))
	}
	var out []proc.Node
	for _, n := range s.tracker.Attribute(procs, roots) {
		if f.SessionID != "" && n.SessionID != f.SessionID {
			continue
		}
		if f.ProjectID != "" && projectOf[n.SessionID] != f.ProjectID {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Service) KillProcesses(ctx context.Context, r KillRequest) ([]int, error) {
	if len(r.PIDs) == 0 && !r.OrphansOnly {
		return nil, ErrEmptyKillRequest
	}
	nodes, err := s.ProcessTree(ctx, TreeFilter{SessionID: r.SessionID})
	if err != nil {
		return nil, err
	}
	candidates := make(map[int]bool, len(nodes))
	for _, n := range nodes {
		if !r.OrphansOnly || n.Orphan {
			candidates[n.Process.PID] = true
		}
	}
	targets, err := selectTargets(r.PIDs, candidates)
	if err != nil {
		return nil, err
	}
	if err := proc.KillTree(ctx, targets, s.killGrace); err != nil {
		return nil, fmt.Errorf("kill processes: %w", err)
	}
	return targets, nil
}

func (s *Service) RefreshLoop(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_, _ = s.ProcessTree(ctx, TreeFilter{})
		}
	}
}

func selectTargets(requested []int, candidates map[int]bool) ([]int, error) {
	if len(requested) == 0 {
		var all []int
		for pid := range candidates {
			all = append(all, pid)
		}
		slices.Sort(all)
		return all, nil
	}
	for _, pid := range requested {
		if !candidates[pid] {
			return nil, fmt.Errorf("pid %d: %w", pid, ErrNotTracked)
		}
	}
	targets := slices.Clone(requested)
	slices.Sort(targets)
	return slices.Compact(targets), nil
}

func rootOf(sess session.Session) proc.SessionRoot {
	if sess.State != session.StateRunning {
		return proc.SessionRoot{SessionID: sess.ID}
	}
	return proc.SessionRoot{SessionID: sess.ID, RootPID: sess.ChildPID, PGID: sess.PGID}
}

func defaultCwd(p project.Project) (string, error) {
	if len(p.Repos) > 0 {
		return p.Repos[0], nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return home, nil
}

func nodePIDs(nodes []proc.Node) []int {
	var pids []int
	for _, n := range nodes {
		pids = append(pids, n.Process.PID)
	}
	return pids
}
