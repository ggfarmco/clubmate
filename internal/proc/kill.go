package proc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const killPollInterval = 50 * time.Millisecond

var ErrUnsafePID = errors.New("refusing to signal pid")

type Target struct {
	PID   int
	Start time.Time
}

func StartTime(pid int) (time.Time, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, err
	}
	if int(k.Proc.P_pid) != pid {
		return time.Time{}, unix.ESRCH
	}
	return time.Unix(int64(k.Proc.P_starttime.Sec), int64(k.Proc.P_starttime.Usec)*1000), nil
}

func KillTree(ctx context.Context, targets []Target, grace time.Duration) ([]int, error) {
	self := os.Getpid()
	for _, t := range targets {
		if t.PID <= 1 || t.PID == self {
			return nil, fmt.Errorf("%w %d", ErrUnsafePID, t.PID)
		}
	}

	signalled, errs := signalAll(targets, unix.SIGTERM)
	survivors := awaitExit(ctx, targets, grace)
	_, killErrs := signalAll(survivors, unix.SIGKILL)
	errs = append(errs, killErrs...)
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}
	return signalled, errors.Join(errs...)
}

func awaitExit(ctx context.Context, targets []Target, grace time.Duration) []Target {
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(killPollInterval)
	defer tick.Stop()
	for {
		survivors := alive(targets)
		if len(survivors) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return survivors
		case <-deadline.C:
			return alive(survivors)
		case <-tick.C:
		}
	}
}

func signalAll(targets []Target, sig unix.Signal) ([]int, []error) {
	var sent []int
	var errs []error
	for _, t := range targets {
		if !matches(t) {
			continue
		}
		err := unix.Kill(t.PID, sig)
		switch {
		case err == nil:
			sent = append(sent, t.PID)
		case !errors.Is(err, unix.ESRCH):
			errs = append(errs, fmt.Errorf("signal %d with %s: %w", t.PID, sig, err))
		}
	}
	return sent, errs
}

func alive(targets []Target) []Target {
	var out []Target
	for _, t := range targets {
		if matches(t) {
			out = append(out, t)
		}
	}
	return out
}

func matches(t Target) bool {
	start, err := StartTime(t.PID)
	return err == nil && start.Equal(t.Start)
}
