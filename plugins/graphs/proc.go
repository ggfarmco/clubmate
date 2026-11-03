package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
	escapeReach = 1024
)

const reasonTimedOut = "timed out"

var escapeRe = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?|\x1b[ -~]?`)

type stepSpec struct {
	Dir     string
	Start   string
	Shell   string
	Env     []string
	Timeout time.Duration
	OutPath string
	Main    string
}

type stepResult struct {
	ExitCode   int
	Reason     string
	Tail       string
	Duration   time.Duration
	CheckFiles []string
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
	if reason == "" && cmd.ProcessState != nil {
		reason = signalReason(cmd.ProcessState)
	}
	return stepResult{ExitCode: exitCode(cmd, werr), Reason: reason, Tail: out.tailText(), Duration: time.Since(begin)}
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

func signalReason(ps *os.ProcessState) string {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return "killed by signal " + strconv.Itoa(int(ws.Signal()))
	}
	return ""
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
	n := min(max(outputCap-o.written, 0), len(p))
	if n > 0 {
		w, _ := o.file.Write(p[:n])
		o.written += w
	}
	if !o.cut && n < len(p) {
		_, _ = o.file.WriteString(cutNotice)
		o.cut = true
	}
	o.tail = append(o.tail, p...)
	if extra := len(o.tail) - 4*outputTail; extra > 0 {
		o.tail = o.tail[escapeEnd(o.tail, extra):]
	}
	return len(p), nil
}

func escapeEnd(b []byte, at int) int {
	from := max(at-escapeReach, 0)
	i := bytes.LastIndexByte(b[from:at], 0x1b)
	if i < 0 {
		return at
	}
	if m := escapeRe.FindIndex(b[from+i:]); m != nil && m[0] == 0 && from+i+m[1] > at {
		return from + i + m[1]
	}
	return at
}

func (o *stepOutput) tailText() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	text := strings.TrimSpace(stripControl(strings.ToValidUTF8(string(o.tail), "")))
	if n := utf8.RuneCountInString(text); n > outputTail {
		text = string([]rune(text)[n-outputTail:])
	}
	return text
}

func (o *stepOutput) close() {
	_ = o.file.Close()
}

func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 0x20 && r < 0x7f) || r > 0x9f {
			return r
		}
		return -1
	}, escapeRe.ReplaceAllString(s, ""))
}
