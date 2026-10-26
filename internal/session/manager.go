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
	"sort"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"clubmate/internal/id"
	"clubmate/internal/paths"
	"clubmate/internal/sessionhost"
)

const (
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
	ProjectID string
	Title     string
	Command   []string
	Cwd       string
	Env       []string
}

type Manager struct {
	dirs    paths.Dirs
	hostBin string
}

func NewManager(dirs paths.Dirs, hostBin string) *Manager {
	return &Manager{dirs: dirs, hostBin: hostBin}
}

func (m *Manager) Create(ctx context.Context, req CreateRequest) (Session, error) {
	sid := id.New("ses")
	cfg := sessionhost.Config{
		ID:        sid,
		ProjectID: req.ProjectID,
		Title:     req.Title,
		Command:   req.Command,
		Cwd:       req.Cwd,
		Env:       req.Env,
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

func (m *Manager) List(_ context.Context, projectID string) ([]Session, error) {
	entries, err := os.ReadDir(m.dirs.SessionsDir())
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		meta, err := sessionhost.ReadMeta(filepath.Join(m.dirs.SessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		if projectID != "" && meta.ProjectID != projectID {
			continue
		}
		sessions = append(sessions, fromMeta(meta))
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartedAt.Before(sessions[j].StartedAt) })
	return sessions, nil
}

func (m *Manager) Get(_ context.Context, sid string) (Session, error) {
	meta, err := sessionhost.ReadMeta(m.dirs.SessionMeta(sid))
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, fmt.Errorf("session %s: %w", sid, ErrNotFound)
	}
	if err != nil {
		return Session{}, err
	}
	return fromMeta(meta), nil
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

func (m *Manager) awaitReady(ctx context.Context, sid string, pid int) error {
	deadline := time.NewTimer(startTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		hostGone := errors.Is(unix.Kill(pid, 0), unix.ESRCH)
		if exists(m.dirs.SessionMeta(sid)) && (hostGone || exists(m.dirs.SessionSocket(sid))) {
			return nil
		}
		if hostGone {
			return m.startError(sid)
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

func (m *Manager) startError(sid string) error {
	return fmt.Errorf("%w: see %s", ErrHostStartTimeout, m.dirs.SessionLog(sid))
}

func fromMeta(meta sessionhost.Meta) Session {
	return Session{
		ID:        meta.ID,
		ProjectID: meta.ProjectID,
		Title:     meta.Title,
		Command:   meta.Command,
		Cwd:       meta.Cwd,
		HostPID:   meta.HostPID,
		ChildPID:  meta.ChildPID,
		PGID:      meta.PGID,
		StartedAt: meta.StartedAt,
		State:     deriveState(meta),
		ExitCode:  meta.ExitCode,
	}
}

func deriveState(meta sessionhost.Meta) State {
	if meta.ExitCode != nil {
		return StateExited
	}
	if errors.Is(unix.Kill(meta.HostPID, 0), unix.ESRCH) {
		return StateLost
	}
	return StateRunning
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
