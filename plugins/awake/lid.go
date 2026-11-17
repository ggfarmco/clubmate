package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	guardArg = "lid-guard"
	lockName = "clubmate-awake-lid.lock"

	guardLockWait = 2 * time.Second
	lockRetry     = 20 * time.Millisecond
)

var errLidBusy = errors.New("another keep awake holds the lid")

type lidControl interface {
	Hold() error
	Free() error
}

type clamshell struct {
	mu       sync.Mutex
	write    func(disable bool) error
	lockPath string
	lock     *os.File
	guardIn  io.WriteCloser
}

func newClamshell() *clamshell {
	return &clamshell{write: setClamshellIOKit, lockPath: filepath.Join(os.TempDir(), lockName)}
}

func (c *clamshell) Hold() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.startGuard(); err != nil {
		return fmt.Errorf("start lid guard: %w", err)
	}
	if c.lock == nil {
		f, err := tryLock(c.lockPath)
		if err != nil {
			return err
		}
		c.lock = f
	}
	return c.write(true)
}

func (c *clamshell) Free() error {
	return c.free(0)
}

func (c *clamshell) free(wait time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lock == nil {
		f, err := lockWithin(c.lockPath, wait)
		if errors.Is(err, errLidBusy) {
			return nil
		}
		if err != nil {
			return err
		}
		c.lock = f
	}
	err := c.write(false)
	c.lock.Close()
	c.lock = nil
	return err
}

func (c *clamshell) startGuard() error {
	if c.guardIn != nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, guardArg)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	c.guardIn = in
	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		if c.guardIn == in {
			c.guardIn = nil
		}
		c.mu.Unlock()
	}()
	return nil
}

func tryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLidBusy
		}
		return nil, err
	}
	return f, nil
}

func lockWithin(path string, wait time.Duration) (*os.File, error) {
	deadline := time.Now().Add(wait)
	for {
		f, err := tryLock(path)
		if !errors.Is(err, errLidBusy) || !time.Now().Before(deadline) {
			return f, err
		}
		time.Sleep(lockRetry)
	}
}

func runGuard(c *clamshell) {
	_, _ = io.Copy(io.Discard, os.Stdin)
	logError("lid guard", c.free(guardLockWait))
}
