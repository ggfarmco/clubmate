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

	"clubmate/internal/bus"
	"clubmate/internal/core"
	"clubmate/internal/kv"
	"clubmate/internal/paths"
	"clubmate/internal/plugin"
	"clubmate/internal/proc"
	"clubmate/internal/project"
	"clubmate/internal/rpc"
	"clubmate/internal/sdui"
	"clubmate/internal/session"
)

const (
	refreshEvery      = 2 * time.Second
	pluginStopTimeout = 15 * time.Second
)

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

	exe, err := coreExecutable()
	if err != nil {
		return err
	}
	hostBin, err := hostBinary(exe)
	if err != nil {
		return err
	}
	store, err := project.Open(dirs.DB())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	eventBus, err := bus.Open(dirs.BusDB())
	if err != nil {
		return fmt.Errorf("open bus: %w", err)
	}
	defer eventBus.Close()

	pluginKV, err := kv.Open(dirs.PluginsDB())
	if err != nil {
		return fmt.Errorf("open plugin storage: %w", err)
	}
	defer pluginKV.Close()

	listener, err := listen(dirs.CoreSocket())
	if err != nil {
		return err
	}
	defer os.Remove(dirs.CoreSocket())

	manifests, discoverErrs := plugin.Discover(plugin.SearchPath(exe, dirs))
	for _, err := range discoverErrs {
		logger.Error("plugin discovery", "err", err)
	}

	var sup *plugin.Supervisor
	tracker := proc.NewTracker(proc.ReadEnv, os.Getpid())
	svc := core.NewService(store, session.NewManager(dirs, hostBin), tracker, proc.Snapshot,
		core.WithBus(eventBus), core.WithLogger(logger), core.WithExcludePIDs(func() []int { return sup.PIDs() }))
	srv := rpc.NewServer(svc, eventBus, rpc.WithKV(pluginKV))
	uiStore := sdui.NewStore()
	sup = plugin.NewSupervisor(srv, dirs, logger, plugin.WithOnStop(uiStore.ClearOwner))
	srv.SetPluginList(func() any { return sup.Statuses() })
	srv.SetUI(uiStore, sdui.NewSections(plugin.SectionDefs(manifests)), plugin.NewRouter(sup))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv.SetShutdown(stop)

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		svc.RefreshLoop(ctx, refreshEvery)
	}()

	sup.Start(ctx, manifests)
	logger.Info("clubmate-core listening", "socket", dirs.CoreSocket(), "version", rpc.Version, "plugins", len(manifests))
	serveErr := srv.Serve(ctx, listener)
	stop()
	stopPlugins(sup)
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

func stopPlugins(sup *plugin.Supervisor) {
	ctx, cancel := context.WithTimeout(context.Background(), pluginStopTimeout)
	defer cancel()
	sup.Stop(ctx)
}

func coreExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	return exe, nil
}

func hostBinary(exe string) (string, error) {
	bin := filepath.Join(filepath.Dir(exe), "clubmate-session")
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("clubmate-session not found next to clubmate-core: %s", bin)
	}
	return bin, nil
}
