package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"clubmate/internal/core"
	"clubmate/internal/paths"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
)

const refreshEvery = 2 * time.Second

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := run(logger)
	if errors.Is(err, paths.ErrCoreRunning) {
		fmt.Fprintln(os.Stderr, "clubmate-core already running")
		os.Exit(1)
	}
	if err != nil {
		logger.Error("clubmate-core failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dirs, err := paths.Resolve()
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	if err := dirs.Ensure(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}
	unlock, err := paths.LockCore(dirs)
	if err != nil {
		return err
	}
	defer unlock()

	hostBin, err := hostBinary()
	if err != nil {
		return err
	}
	store, err := project.Open(dirs.DB())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	listener, err := listen(dirs.CoreSocket())
	if err != nil {
		return err
	}
	defer os.Remove(dirs.CoreSocket())

	tracker := proc.NewTracker(proc.ReadEnv, os.Getpid())
	svc := core.NewService(store, session.NewManager(dirs, hostBin), tracker, proc.Snapshot)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		svc.RefreshLoop(ctx, refreshEvery)
	}()

	logger.Info("clubmate-core listening", "socket", dirs.CoreSocket(), "version", rpc.Version)
	serveErr := rpc.Serve(ctx, listener, svc)
	stop()
	<-loopDone
	logger.Info("clubmate-core stopped")
	return serveErr
}

func listen(socket string) (net.Listener, error) {
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", socket, err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return l, nil
}

func hostBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	bin := filepath.Join(filepath.Dir(exe), "clubmate-session")
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("clubmate-session not found next to clubmate-core: %s", bin)
	}
	return bin, nil
}
