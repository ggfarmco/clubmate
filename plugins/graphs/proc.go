package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	outputCap   = 1 << 20
	outputTail  = 4000
	cutNotice   = "\n… cut at 1 MB\n"
	exitUnknown = -1
	exitMissing = 127
)

const (
	reasonTimedOut = "timed out"
	reasonNotFound = "not found"
)

type stepSpec struct {
	Dir     string
	Shell   string
	Env     []string
	Timeout time.Duration
	OutPath string
	Main    string
}

type stepResult struct {
	ExitCode int
	Reason   string
	Tail     string
	Duration time.Duration
}

type procRunner interface {
	Run(ctx context.Context, s stepSpec, started func(pid int)) stepResult
}

type shellRunner struct {
	grace time.Duration
}

type stepOutput struct {
	mu      sync.Mutex
	file    *os.File
	written int
	cut     bool
	tail    []byte
}

func (r shellRunner) Run(ctx context.Context, s stepSpec, started func(pid int)) stepResult {
	begin := time.Now()
	out, err := openOutput(s.OutPath)
	if err != nil {
		return stepResult{ExitCode: exitUnknown, Reason: "could not start: " + err.Error()}
	}
	defer out.close()
	cmd := exec.Command("/bin/sh", "-c", s.Shell)
	cmd.Dir, cmd.Env = s.Dir, s.Env
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = r.grace
	if err := cmd.Start(); err != nil {
		return stepResult{ExitCode: exitUnknown, Reason: "could not start: " + err.Error(), Duration: time.Since(begin)}
	}
	started(cmd.Process.Pid)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(s.Timeout)
	defer timer.Stop()
	var reason string
	var werr error
	select {
	case werr = <-done:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	case <-timer.C:
		reason, werr = reasonTimedOut, r.kill(cmd.Process.Pid, done)
	case <-ctx.Done():
		reason, werr = reasonStopped, r.kill(cmd.Process.Pid, done)
	}
	res := stepResult{ExitCode: exitCode(cmd, werr), Reason: reason, Tail: out.tailText(), Duration: time.Since(begin)}
	if res.Reason == "" && res.ExitCode == exitMissing && strings.Contains(res.Tail, "not found") {
		res.Reason = reasonNotFound
	}
	return res
}

func (r shellRunner) kill(pid int, done <-chan error) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case err := <-done:
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return err
	case <-time.After(r.grace):
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return <-done
}

func exitCode(cmd *exec.Cmd, err error) int {
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		return exit.ExitCode()
	}
	return exitUnknown
}

func openOutput(path string) (*stepOutput, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("output: %w", err)
	}
	return &stepOutput{file: f}, nil
}

func (o *stepOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if room := outputCap - o.written; room > 0 {
		n := min(room, len(p))
		w, _ := o.file.Write(p[:n])
		o.written += w
	}
	if !o.cut && o.written >= outputCap && len(p) > 0 {
		_, _ = o.file.WriteString(cutNotice)
		o.cut = true
	}
	o.tail = append(o.tail, p...)
	if extra := len(o.tail) - 4*outputTail; extra > 0 {
		o.tail = o.tail[extra:]
	}
	return len(p), nil
}

func (o *stepOutput) tailText() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	text := strings.TrimSpace(strings.ToValidUTF8(string(o.tail), ""))
	if n := utf8.RuneCountInString(text); n > outputTail {
		text = string([]rune(text)[n-outputTail:])
	}
	return text
}

func (o *stepOutput) close() {
	_ = o.file.Close()
}
