package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"clubmate/internal/bus"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/session"
	"clubmate/pkg/contract"
)

const (
	DefaultKillGrace = 3 * time.Second
	startTimeSlack   = time.Second
	eventSource      = "core"
	stopPoll         = 20 * time.Millisecond
	stopSettle       = 2 * time.Second
)

var (
	ErrNotTracked         = errors.New("process is not tracked by any session")
	ErrEmptyKillRequest   = errors.New("kill request selects no processes")
	ErrOrphansNeedSession = errors.New("orphansOnly requires a session or project id")
	ErrNoFolders          = errors.New("no folders to open")
)

type TreeFilter struct {
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId"`
}

type KillRequest = contract.KillRequest

type Option func(*Service)

type Service struct {
	projects  *project.Store
	sessions  *session.Manager
	tracker   *proc.Tracker
	snapshot  func() ([]proc.Process, error)
	killGrace time.Duration
	bus       *bus.Bus
	logger    *slog.Logger
	exclude   func() []int

	workspaceMu sync.Mutex

	transitionsMu sync.Mutex
	baselined     bool
	reported      map[string]bool

	startTime func(pid int) (time.Time, error)
	rootsMu   sync.Mutex
	roots     []*rootRecord
	dropped   map[RootOwner]bool
}

func WithBus(b *bus.Bus) Option {
	return func(s *Service) { s.bus = b }
}

func WithLogger(l *slog.Logger) Option {
	return func(s *Service) { s.logger = l }
}

func WithExcludePIDs(fn func() []int) Option {
	return func(s *Service) { s.exclude = fn }
}

func WithStartTime(fn func(pid int) (time.Time, error)) Option {
	return func(s *Service) { s.startTime = fn }
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
		logger:    slog.New(slog.DiscardHandler),
		reported:  map[string]bool{},
		startTime: proc.StartTime,
		dropped:   map[RootOwner]bool{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) CreateProject(ctx context.Context, name string, repos []string) (project.Project, error) {
	p, err := s.projects.Create(ctx, name, repos)
	if err != nil {
		return project.Project{}, err
	}
	s.publish(ctx, contract.TopicProjectCreated, contract.ProjectChanged{Project: p})
	return p, nil
}

func (s *Service) OpenFolders(ctx context.Context, folders []string) (project.Project, bool, error) {
	cleaned := project.CanonicalPaths(folders)
	if len(cleaned) == 0 {
		return project.Project{}, false, ErrNoFolders
	}
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	p, found, err := s.projects.FindByRepos(ctx, cleaned)
	if err != nil || found {
		return p, false, err
	}
	p, err = s.CreateProject(ctx, filepath.Base(filepath.Clean(folders[0])), cleaned)
	return p, err == nil, err
}

func (s *Service) HomeWorkspace(ctx context.Context) (project.Project, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return project.Project{}, fmt.Errorf("resolve home dir: %w", err)
	}
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	p, created, err := s.projects.Home(ctx, home)
	if err != nil {
		return project.Project{}, err
	}
	if created {
		s.publish(ctx, contract.TopicProjectCreated, contract.ProjectChanged{Project: p})
	}
	return p, nil
}

func (s *Service) RenameProject(ctx context.Context, id, name string) (project.Project, error) {
	if err := s.projects.Rename(ctx, id, name); err != nil {
		return project.Project{}, err
	}
	p, err := s.projects.Get(ctx, id)
	if err != nil {
		return project.Project{}, err
	}
	s.publish(ctx, contract.TopicProjectUpdated, contract.ProjectChanged{Project: p})
	return p, nil
}

func (s *Service) TouchProject(ctx context.Context, id string) error {
	return s.projects.Touch(ctx, id, time.Now())
}

func (s *Service) ListProjects(ctx context.Context) ([]project.Project, error) {
	return s.projects.List(ctx)
}

func (s *Service) GetProject(ctx context.Context, id string) (project.Project, error) {
	return s.projects.Get(ctx, id)
}

func (s *Service) AddRepo(ctx context.Context, id, path string) (project.Project, error) {
	p, err := s.projects.AddRepo(ctx, id, path)
	if err != nil {
		return project.Project{}, err
	}
	s.publish(ctx, contract.TopicProjectUpdated, contract.ProjectChanged{Project: p})
	return p, nil
}

func (s *Service) SetWorktree(ctx context.Context, id, repo, path string) (project.Project, error) {
	p, err := s.projects.SetWorktree(ctx, id, repo, path)
	if err != nil {
		return project.Project{}, err
	}
	s.publish(ctx, contract.TopicProjectUpdated, contract.ProjectChanged{Project: p})
	return p, nil
}

func (s *Service) CheckWorktrees(ctx context.Context, id string) (project.Project, error) {
	p, err := s.projects.Get(ctx, id)
	if err != nil {
		return project.Project{}, err
	}
	if len(p.Missing) > 0 {
		s.publish(ctx, contract.TopicProjectUpdated, contract.ProjectChanged{Project: p})
	}
	return p, nil
}

func (s *Service) DeleteProject(ctx context.Context, id string) error {
	if _, err := s.projects.Get(ctx, id); err != nil {
		return err
	}
	nodes, err := s.ProcessTree(ctx, TreeFilter{ProjectID: id})
	if err != nil {
		return err
	}
	if _, err := proc.KillTree(ctx, nodeTargets(s.withoutLiveRoots(id, nodes)), s.killGrace); err != nil {
		return fmt.Errorf("kill project %s: %w", id, err)
	}
	sessions, err := s.sessions.List(ctx, session.ListFilter{ProjectID: id})
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if err := s.awaitStopped(ctx, sess.ID); err != nil {
			return err
		}
		if err := s.RemoveSession(ctx, sess.ID); err != nil {
			return err
		}
	}
	if err := s.projects.Delete(ctx, id); err != nil {
		return err
	}
	s.publish(ctx, contract.TopicProjectDeleted, contract.ProjectDeleted{ID: id})
	return nil
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
	sess, err := s.sessions.Create(ctx, req)
	if err != nil {
		return session.Session{}, err
	}
	s.publish(ctx, contract.TopicSessionCreated, contract.SessionCreated{Session: sess})
	return sess, nil
}

func (s *Service) ListSessions(ctx context.Context, f session.ListFilter) ([]session.Session, error) {
	return s.sessions.List(ctx, f)
}

func (s *Service) GetSession(ctx context.Context, id string) (session.Session, error) {
	return s.sessions.Get(ctx, id)
}

func (s *Service) SendSession(ctx context.Context, id, text string, enter bool) error {
	return s.sessions.Send(ctx, id, text, enter)
}

func (s *Service) ReadSession(ctx context.Context, id string, n int, raw bool) (session.Snapshot, error) {
	return s.sessions.Read(ctx, id, n, raw)
}

func (s *Service) RemoveSession(ctx context.Context, id string) error {
	if err := s.sessions.Remove(ctx, id); err != nil {
		return err
	}
	s.publish(ctx, contract.TopicSessionRemoved, contract.SessionGone{ID: id})
	return nil
}

func (s *Service) KillSession(ctx context.Context, id string) error {
	if _, err := s.sessions.Get(ctx, id); err != nil {
		return err
	}
	nodes, err := s.ProcessTree(ctx, TreeFilter{SessionID: id})
	if err != nil {
		return err
	}
	if _, err := proc.KillTree(ctx, nodeTargets(nodes), s.killGrace); err != nil {
		return fmt.Errorf("kill session %s: %w", id, err)
	}
	if err := s.awaitStopped(ctx, id); err != nil {
		return err
	}
	s.publish(ctx, contract.TopicSessionKilled, contract.SessionGone{ID: id})
	return nil
}

func (s *Service) CheckTransitions(ctx context.Context) error {
	s.transitionsMu.Lock()
	defer s.transitionsMu.Unlock()
	sessions, err := s.sessions.List(ctx, session.ListFilter{})
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		present[sess.ID] = true
		if sess.State == session.StateRunning || s.reported[sess.ID] {
			continue
		}
		s.reported[sess.ID] = true
		if !s.baselined {
			continue
		}
		s.publish(ctx, contract.TopicSessionExited, contract.SessionExited{
			ID:        sess.ID,
			ProjectID: sess.ProjectID,
			Owner:     sess.Owner,
			State:     sess.State,
			ExitCode:  sess.ExitCode,
		})
	}
	for id := range s.reported {
		if !present[id] {
			delete(s.reported, id)
		}
	}
	s.baselined = true
	return nil
}

func (s *Service) ProcessTree(ctx context.Context, f TreeFilter) ([]proc.Node, error) {
	sessions, err := s.sessions.List(ctx, session.ListFilter{})
	if err != nil {
		return nil, err
	}
	snapStart := time.Now()
	procs, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	byPID := make(map[int]proc.Process, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	projectOf := make(map[string]string, len(sessions))
	excluded := make(map[int]bool, len(sessions))
	roots := make([]proc.SessionRoot, 0, len(sessions))
	for _, sess := range sessions {
		projectOf[sess.ID] = sess.ProjectID
		excluded[sess.HostPID] = true
		roots = append(roots, rootOf(sess, byPID))
	}
	roots = append(roots, s.registeredRoots(byPID, snapStart, projectOf)...)
	if s.exclude != nil {
		for _, pid := range s.exclude() {
			excluded[pid] = true
		}
	}
	nodes := s.tracker.Attribute(procs, roots)
	s.sweepRoots(ctx, nodes)
	var out []proc.Node
	for _, n := range nodes {
		if excluded[n.Process.PID] {
			continue
		}
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
	if r.OrphansOnly && r.SessionID == "" && r.ProjectID == "" {
		return nil, ErrOrphansNeedSession
	}
	if len(r.PIDs) == 0 && !r.OrphansOnly {
		return nil, ErrEmptyKillRequest
	}
	nodes, err := s.ProcessTree(ctx, TreeFilter{SessionID: r.SessionID, ProjectID: r.ProjectID})
	if err != nil {
		return nil, err
	}
	candidates := make(map[int]proc.Node, len(nodes))
	for _, n := range nodes {
		if !r.OrphansOnly || n.Orphan {
			candidates[n.Process.PID] = n
		}
	}
	pids, err := selectPIDs(r.PIDs, candidates)
	if err != nil {
		return nil, err
	}
	targets := make([]proc.Target, 0, len(pids))
	for _, pid := range pids {
		targets = append(targets, targetOf(candidates[pid]))
	}
	killed, err := proc.KillTree(ctx, targets, s.killGrace)
	if err != nil {
		return nil, fmt.Errorf("kill processes: %w", err)
	}
	return killed, nil
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
			if err := s.CheckTransitions(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("check session transitions", "err", err)
			}
		}
	}
}

func (s *Service) publish(ctx context.Context, topic string, payload any) {
	if s.bus == nil {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("marshal event", "topic", topic, "err", err)
		return
	}
	if _, err := s.bus.Publish(context.WithoutCancel(ctx), topic, eventSource, raw); err != nil {
		s.logger.Error("publish event", "topic", topic, "err", err)
	}
}

func (s *Service) awaitStopped(ctx context.Context, id string) error {
	deadline := time.NewTimer(s.killGrace + stopSettle)
	defer deadline.Stop()
	tick := time.NewTicker(stopPoll)
	defer tick.Stop()
	for {
		sess, err := s.sessions.Get(ctx, id)
		if err != nil {
			return err
		}
		if sess.State != session.StateRunning {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("kill session %s: %w", id, session.ErrRunning)
		case <-tick.C:
		}
	}
}

func selectPIDs(requested []int, candidates map[int]proc.Node) ([]int, error) {
	if len(requested) == 0 {
		var all []int
		for pid := range candidates {
			all = append(all, pid)
		}
		slices.Sort(all)
		return all, nil
	}
	for _, pid := range requested {
		if _, ok := candidates[pid]; !ok {
			return nil, fmt.Errorf("pid %d: %w", pid, ErrNotTracked)
		}
	}
	targets := slices.Clone(requested)
	slices.Sort(targets)
	return slices.Compact(targets), nil
}

func rootOf(sess session.Session, byPID map[int]proc.Process) proc.SessionRoot {
	root := proc.SessionRoot{SessionID: sess.ID}
	if sess.State != session.StateRunning {
		return root
	}
	host, hostOK := byPID[sess.HostPID]
	child, childOK := byPID[sess.ChildPID]
	if !hostOK || !childOK || child.PGID != sess.PGID {
		return root
	}
	if startedAfter(host, sess.StartedAt) || startedAfter(child, sess.StartedAt) {
		return root
	}
	root.RootPID, root.PGID = sess.ChildPID, sess.PGID
	return root
}

func startedAfter(p proc.Process, startedAt time.Time) bool {
	return p.StartTime.After(startedAt.Add(startTimeSlack))
}

func defaultCwd(p project.Project) (string, error) {
	if len(p.Roots) > 0 {
		return p.Roots[0], nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return home, nil
}

func nodeTargets(nodes []proc.Node) []proc.Target {
	targets := make([]proc.Target, 0, len(nodes))
	for _, n := range nodes {
		targets = append(targets, targetOf(n))
	}
	return targets
}

func targetOf(n proc.Node) proc.Target {
	return proc.Target{PID: n.Process.PID, Start: n.Process.StartTime}
}
