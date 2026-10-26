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

func KillTree(ctx context.Context, pids []int, grace time.Duration) error {
	self := os.Getpid()
	for _, pid := range pids {
		if pid <= 1 || pid == self {
			return fmt.Errorf("%w %d", ErrUnsafePID, pid)
		}
	}

	errs := signalAll(pids, unix.SIGTERM)
	survivors := awaitExit(ctx, pids, grace)
	errs = append(errs, signalAll(survivors, unix.SIGKILL)...)
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func awaitExit(ctx context.Context, pids []int, grace time.Duration) []int {
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	tick := time.NewTicker(killPollInterval)
	defer tick.Stop()
	for {
		survivors := alive(pids)
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

func signalAll(pids []int, sig unix.Signal) []error {
	var errs []error
	for _, pid := range pids {
		if err := unix.Kill(pid, sig); err != nil && !errors.Is(err, unix.ESRCH) {
			errs = append(errs, fmt.Errorf("signal %d with %s: %w", pid, sig, err))
		}
	}
	return errs
}

func alive(pids []int) []int {
	var out []int
	for _, pid := range pids {
		if unix.Kill(pid, 0) == nil {
			out = append(out, pid)
		}
	}
	return out
}
