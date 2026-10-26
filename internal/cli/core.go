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

	"clubmate/internal/paths"
)

const (
	coreBinary   = "clubmate-core"
	exitNotUp    = 3
	pollInterval = 50 * time.Millisecond
	pingTimeout  = time.Second
)

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
			info, ok := a.ping(cmd.Context())
			if !ok {
				fmt.Fprintln(a.out, "not running")
				return &ExitError{Code: exitNotUp}
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
			if _, ok := a.ping(cmd.Context()); ok {
				fmt.Fprintln(a.out, "already running")
				return nil
			}
			dirs, err := paths.Resolve()
			if err != nil {
				return err
			}
			if err := spawnCore(dirs); err != nil {
				return err
			}
			info, ok := a.waitFor(cmd.Context(), wait, true)
			if !ok {
				return fmt.Errorf("core did not start; see %s", coreLogPath(dirs))
			}
			fmt.Fprintf(a.out, "started pid=%d\n", info.PID)
			return nil
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", 5*time.Second, "how long to wait for the core")
	return cmd
}

func (a *app) coreStopCmd() *cobra.Command {
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the core",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.call(cmd.Context(), "core.shutdown", struct{}{}, nil); err != nil {
				return err
			}
			if _, ok := a.waitFor(cmd.Context(), wait, false); !ok {
				return errors.New("core did not stop in time")
			}
			fmt.Fprintln(a.out, "stopped")
			return nil
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", 5*time.Second, "how long to wait for the core to stop")
	return cmd
}

func (a *app) ping(ctx context.Context) (pingInfo, bool) {
	var info pingInfo
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	err := a.call(ctx, "core.ping", struct{}{}, &info)
	return info, err == nil
}

func (a *app) waitFor(ctx context.Context, wait time.Duration, wantUp bool) (pingInfo, bool) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		info, up := a.ping(ctx)
		if ctx.Err() != nil {
			return info, false
		}
		if up == wantUp {
			return info, true
		}
		select {
		case <-ctx.Done():
			return info, false
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
	logFile, err := os.OpenFile(coreLogPath(dirs), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
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

func coreLogPath(dirs paths.Dirs) string {
	return filepath.Join(dirs.State, "core.log")
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
