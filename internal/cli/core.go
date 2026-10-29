package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"clubmate/internal/paths"
)

const (
	coreBinary    = "clubmate-core"
	exitNotUp     = 3
	pollInterval  = 50 * time.Millisecond
	pingTimeout   = time.Second
	coreStartWait = 15 * time.Second
)

var errCoreNotRunning = errors.New("clubmate-core is not running")

type pingInfo struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

func (a *app) coreCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "core", Short: "Manage the clubmate core"}
	cmd.AddCommand(a.coreStatusCmd(), a.coreStartCmd(), a.coreStopCmd())
	return cmd
}

func (a *app) coreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the core is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := a.ping(cmd.Context())
			if err != nil {
				return a.notRunning(err)
			}
			fmt.Fprintf(a.out, "running pid=%d version=%s\n", info.PID, info.Version)
			return nil
		},
	}
}

func (a *app) coreStartCmd() *cobra.Command {
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the core in the background",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := a.ping(cmd.Context())
			if err == nil {
				fmt.Fprintln(a.out, "already running")
				return nil
			}
			if !errors.Is(err, errCoreNotRunning) {
				return err
			}
			dirs, err := paths.Resolve()
			if err != nil {
				return err
			}
			if err := spawnCore(dirs); err != nil {
				return err
			}
			info, ok := a.waitUp(cmd.Context(), wait)
			if !ok {
				return fmt.Errorf("core did not start; see %s", dirs.CoreLog())
			}
			fmt.Fprintf(a.out, "started pid=%d\n", info.PID)
			return nil
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", coreStartWait, "how long to wait for the core")
	return cmd
}

func (a *app) ensureCore(ctx context.Context) error {
	_, err := a.ping(ctx)
	if !errors.Is(err, errCoreNotRunning) {
		return err
	}
	dirs, err := paths.Resolve()
	if err != nil {
		return err
	}
	if err := spawnCore(dirs); err != nil {
		return err
	}
	if _, ok := a.waitUp(ctx, coreStartWait); !ok {
		return fmt.Errorf("core did not start; see %s", dirs.CoreLog())
	}
	return nil
}

func (a *app) coreStopCmd() *cobra.Command {
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the core",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := a.ping(cmd.Context())
			if err != nil {
				return a.notRunning(err)
			}
			if err := a.call(cmd.Context(), "core.shutdown", struct{}{}, nil); err != nil {
				return err
			}
			if !waitExit(cmd.Context(), wait, info.PID) {
				return errors.New("core did not stop in time")
			}
			fmt.Fprintln(a.out, "stopped")
			return nil
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", 20*time.Second, "how long to wait for the core to exit")
	return cmd
}

func (a *app) notRunning(err error) error {
	if !errors.Is(err, errCoreNotRunning) {
		return err
	}
	fmt.Fprintln(a.out, "not running")
	return &ExitError{Code: exitNotUp}
}

func (a *app) ping(ctx context.Context) (pingInfo, error) {
	var info pingInfo
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	err := a.call(ctx, "core.ping", struct{}{}, &info)
	return info, err
}

func (a *app) waitUp(ctx context.Context, wait time.Duration) (pingInfo, bool) {
	var info pingInfo
	ok := poll(ctx, wait, func(ctx context.Context) bool {
		var err error
		info, err = a.ping(ctx)
		return err == nil
	})
	return info, ok
}

func waitExit(ctx context.Context, wait time.Duration, pid int) bool {
	return poll(ctx, wait, func(context.Context) bool {
		return errors.Is(unix.Kill(pid, 0), unix.ESRCH)
	})
}

func poll(ctx context.Context, wait time.Duration, done func(context.Context) bool) bool {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if done(ctx) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func spawnCore(dirs paths.Dirs) error {
	bin, err := coreBinaryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirs.State, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	logFile, err := os.OpenFile(dirs.CoreLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open core log: %w", err)
	}
	defer logFile.Close()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()
	cmd := exec.Command(bin)
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", coreBinary, err)
	}
	return cmd.Process.Release()
}

func coreBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), coreBinary), nil
}
