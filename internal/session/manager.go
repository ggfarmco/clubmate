package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"clubmate/internal/id"
	"clubmate/internal/paths"
	"clubmate/internal/proc"
	"clubmate/internal/sessionhost"
)

const (
	IDPrefix     = "ses"
	startTimeout = 3 * time.Second
	pollInterval = 20 * time.Millisecond
)

var (
	ErrHostStartTimeout = errors.New("session host did not start")
	ErrNotFound         = errors.New("session not found")
	ErrRunning          = errors.New("session is running")
)

type State string

const (
	StateRunning State = "running"
	StateExited  State = "exited"
	StateLost    State = "lost"
)

type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	Owner     string    `json:"owner"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd"`
	HostPID   int       `json:"hostPid"`
	ChildPID  int       `json:"childPid"`
	PGID      int       `json:"pgid"`
	StartedAt time.Time `json:"startedAt"`
	State     State     `json:"state"`
	ExitCode  *int      `json:"exitCode"`
}

type CreateRequest struct {
	ProjectID string   `json:"projectId"`
	Title     string   `json:"title"`
	Owner     string   `json:"-"`
	Command   []string `json:"command"`
	Cwd       string   `json:"cwd"`
	Env       []string `json:"env"`
	UnsetEnv  []string `json:"unsetEnv"`
}

type ListFilter struct {
	ProjectID string
	Owner     string
}

type Manager struct {
	dirs            paths.Dirs
	hostBin         string
	pathDir         string
	snapshotTimeout time.Duration
}

type Option func(*Manager)

func WithPathDir(dir string) Option {
	return func(m *Manager) {
		info, err := os.Stat(filepath.Join(dir, "clubmate"))
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			m.pathDir = dir
		}
	}
}

func NewManager(dirs paths.Dirs, hostBin string, opts ...Option) *Manager {
	m := &Manager{dirs: dirs, hostBin: hostBin, snapshotTimeout: snapshotTimeout}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Manager) Create(ctx context.Context, req CreateRequest) (Session, error) {
	sid := id.New(IDPrefix)
	cfg := sessionhost.Config{
		ID:        sid,
		ProjectID: req.ProjectID,
		Title:     req.Title,
		Owner:     req.Owner,
		Command:   req.Command,
		Cwd:       req.Cwd,
		Env:       req.Env,
		UnsetEnv:  req.UnsetEnv,
		Socket:    m.dirs.SessionSocket(sid),
		MetaPath:  m.dirs.SessionMeta(sid),
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return Session{}, fmt.Errorf("encode session config: %w", err)
	}
	pid, err := m.spawn(sid, payload)
	if err != nil {
		return Session{}, err
	}
	if err := m.awaitReady(ctx, sid, pid); err != nil {
		return Session{}, err
	}
	return m.Get(ctx, sid)
}

func (m *Manager) List(_ context.Context, f ListFilter) ([]Session, error) {
	entries, err := os.ReadDir(m.dirs.SessionsDir())
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		s, err := readSession(filepath.Join(m.dirs.SessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		if f.ProjectID != "" && s.ProjectID != f.ProjectID {
			continue
		}
		if f.Owner != "" && s.Owner != f.Owner {
			continue
		}
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartedAt.Before(sessions[j].StartedAt) })
	return sessions, nil
}

func (m *Manager) Get(_ context.Context, sid string) (Session, error) {
	if !id.Valid(IDPrefix, sid) {
		return Session{}, fmt.Errorf("session %q: %w", sid, ErrNotFound)
	}
	s, err := readSession(m.dirs.SessionMeta(sid))
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, fmt.Errorf("session %s: %w", sid, ErrNotFound)
	}
	return s, err
}

func (m *Manager) Remove(ctx context.Context, sid string) error {
	s, err := m.Get(ctx, sid)
	if err != nil {
		return err
	}
	if s.State == StateRunning {
		return fmt.Errorf("session %s: %w", sid, ErrRunning)
	}
	for _, p := range []string{m.dirs.SessionMeta(sid), m.dirs.SessionLog(sid)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", p, err)
		}
	}
	return nil
}

func (m *Manager) spawn(sid string, payload []byte) (int, error) {
	logFile, err := os.OpenFile(m.dirs.SessionLog(sid), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open session log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(m.hostBin)
	cmd.Env = prependPath(hostEnv(), m.pathDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start session host: %w", err)
	}
	go func() {
		_ = cmd.Wait()
	}()
	return cmd.Process.Pid, nil
}

func hostEnv() []string {
	env := sessionhost.StripMarkers(os.Environ())
	defaults, err := paths.Defaults()
	if err != nil {
		return env
	}
	return slices.DeleteFunc(env, func(kv string) bool {
		return kv == "CLUBMATE_STATE_DIR="+defaults.State || kv == "CLUBMATE_RUNTIME_DIR="+defaults.Runtime
	})
}

func prependPath(env []string, dir string) []string {
	if dir == "" {
		return env
	}
	i := slices.IndexFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })
	if i < 0 {
		return append(env, "PATH="+dir)
	}
	current := strings.TrimPrefix(env[i], "PATH=")
	if first, _, _ := strings.Cut(current, ":"); first == dir {
		return env
	}
	if current == "" {
		env[i] = "PATH=" + dir
	} else {
		env[i] = "PATH=" + dir + ":" + current
	}
	return env
}

func (m *Manager) awaitReady(ctx context.Context, sid string, pid int) error {
	deadline := time.NewTimer(startTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		if exists(m.dirs.SessionMeta(sid)) && exists(m.dirs.SessionSocket(sid)) {
			return nil
		}
		if errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
			return m.exitedEarly(sid)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return m.startError(sid)
		case <-tick.C:
		}
	}
}

func (m *Manager) exitedEarly(sid string) error {
	meta, err := sessionhost.ReadMeta(m.dirs.SessionMeta(sid))
	if err != nil || meta.ExitCode == nil {
		return m.startError(sid)
	}
	return nil
}

func (m *Manager) startError(sid string) error {
	return fmt.Errorf("%w: see %s", ErrHostStartTimeout, m.dirs.SessionLog(sid))
}

func readSession(path string) (Session, error) {
	meta, err := sessionhost.ReadMeta(path)
	if err != nil {
		return Session{}, err
	}
	if meta.ExitCode != nil {
		return fromMeta(meta, StateExited), nil
	}
	if hostAlive(meta) {
		return fromMeta(meta, StateRunning), nil
	}
	again, err := sessionhost.ReadMeta(path)
	if err != nil || again.ExitCode == nil {
		return fromMeta(meta, StateLost), nil
	}
	return fromMeta(again, StateExited), nil
}

func fromMeta(meta sessionhost.Meta, state State) Session {
	return Session{
		ID:        meta.ID,
		ProjectID: meta.ProjectID,
		Title:     meta.Title,
		Owner:     meta.Owner,
		Command:   meta.Command,
		Cwd:       meta.Cwd,
		HostPID:   meta.HostPID,
		ChildPID:  meta.ChildPID,
		PGID:      meta.PGID,
		StartedAt: meta.StartedAt,
		State:     state,
		ExitCode:  meta.ExitCode,
	}
}

func hostAlive(meta sessionhost.Meta) bool {
	if meta.HostPID <= 1 || unix.Kill(meta.HostPID, 0) != nil {
		return false
	}
	if meta.HostStart.IsZero() {
		return true
	}
	start, err := proc.StartTime(meta.HostPID)
	return err == nil && start.Equal(meta.HostStart)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
