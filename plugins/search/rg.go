package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	rgEventLimit = 16 << 20
	rgStderrMax  = 4 << 10
	rgWaitDelay  = 2 * time.Second
)

type rgEngine struct {
	bin   string
	roots []root
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

type rgText struct {
	Text  *string `json:"text"`
	Bytes string  `json:"bytes"`
}

type cappedBuffer struct {
	buf bytes.Buffer
}

func (e rgEngine) run(ctx context.Context, opts grepOptions, emit func([]grepMatch) bool) error {
	if len(e.roots) == 0 {
		return nil
	}
	var stderr cappedBuffer
	cmd := exec.Command(e.bin, e.args(opts)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = &stderr
	cmd.WaitDelay = rgWaitDelay
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start rg: %w", err)
	}
	kill := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	stop := context.AfterFunc(ctx, kill)
	defer stop()
	emitted, capped := e.read(stdout, emit)
	if capped {
		kill()
	}
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	if capped || ctx.Err() != nil {
		return nil
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if ee.ExitCode() == 1 || emitted > 0 {
			return nil
		}
		if msg := strings.TrimSpace(stderr.buf.String()); msg != "" {
			return errors.New(msg)
		}
	}
	return err
}

func (e rgEngine) args(opts grepOptions) []string {
	args := []string{"--json", "--max-count", "1000"}
	if !opts.Regex {
		args = append(args, "--fixed-strings")
	}
	if !opts.Case {
		args = append(args, "--ignore-case")
	}
	if opts.Word {
		args = append(args, "--word-regexp")
	}
	for _, g := range splitGlobs(opts.Glob) {
		args = append(args, "--glob", g)
	}
	args = append(args, "--", opts.Query)
	for _, r := range e.roots {
		args = append(args, r.resolved)
	}
	return args
}

func (e rgEngine) read(stdout io.Reader, emit func([]grepMatch) bool) (int, bool) {
	r := bufio.NewReaderSize(stdout, 64<<10)
	emitted := 0
	for {
		line, err := readEvent(r)
		if len(line) > 0 {
			if m, ok := e.match(line); ok {
				if !emit([]grepMatch{m}) {
					return emitted, true
				}
				emitted++
			}
		}
		if err != nil {
			return emitted, false
		}
	}
}

func (e rgEngine) match(line []byte) (grepMatch, bool) {
	if !bytes.HasPrefix(line, []byte(`{"type":"match"`)) {
		return grepMatch{}, false
	}
	var ev rgEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return grepMatch{}, false
	}
	path := string(ev.Data.Path.value())
	for _, r := range e.roots {
		rel, ok := strings.CutPrefix(path, r.resolved+"/")
		if !ok {
			continue
		}
		spans := make([][2]int, 0, len(ev.Data.Submatches))
		for _, s := range ev.Data.Submatches {
			spans = append(spans, [2]int{s.Start, s.End})
		}
		return newMatch(join(r.label, rel), filepath.Join(r.folder, rel), ev.Data.LineNumber, ev.Data.Lines.value(), spans), true
	}
	return grepMatch{}, false
}

func (t rgText) value() []byte {
	if t.Text != nil {
		return []byte(*t.Text)
	}
	raw, err := base64.StdEncoding.DecodeString(t.Bytes)
	if err != nil {
		return nil
	}
	return raw
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	if room := rgStderrMax - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func readEvent(r *bufio.Reader) ([]byte, error) {
	var line []byte
	over := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !over && len(line)+len(chunk) > rgEventLimit {
			over, line = true, nil
		}
		if !over {
			line = append(line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}
