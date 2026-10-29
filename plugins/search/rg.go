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

func (e rgEngine) run(ctx context.Context, opts grepOptions, emit serialEmit) error {
	if len(e.roots) == 0 {
		return nil
	}
	globs, err := compileGlobs(opts.Glob)
	if err != nil {
		return err
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
	killed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		kill()
		close(killed)
	})
	events, capped := e.read(stdout, globs, emit)
	if capped {
		kill()
	}
	_, _ = io.Copy(io.Discard, stdout)
	if !stop() {
		<-killed
	}
	err = cmd.Wait()
	if capped || ctx.Err() != nil {
		return nil
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if ee.ExitCode() == 1 || events > 0 {
			return nil
		}
		if msg := strings.TrimSpace(stderr.buf.String()); msg != "" {
			return errors.New(msg)
		}
	}
	return err
}

func (e rgEngine) args(opts grepOptions) []string {
	args := []string{"--json", "--no-config", "--hidden", "--glob", "!.git", "--max-count", "1000"}
	if !opts.Regex {
		args = append(args, "--fixed-strings")
	}
	if opts.Case {
		args = append(args, "--case-sensitive")
	} else {
		args = append(args, "--ignore-case")
	}
	if opts.Word {
		args = append(args, "--word-regexp")
	}
	args = append(args, "--", opts.Query)
	for _, r := range e.roots {
		args = append(args, r.resolved)
	}
	return args
}

func (e rgEngine) read(stdout io.Reader, globs globFilter, emit serialEmit) (int, bool) {
	r := bufio.NewReaderSize(stdout, 64<<10)
	events := 0
	for {
		line, err := readEvent(r)
		if len(line) > 0 && line[0] == '{' {
			events++
			if m, ok := e.match(line, globs); ok && !emit([]grepMatch{m}) {
				return events, true
			}
		}
		if err != nil {
			return events, false
		}
	}
}

func (e rgEngine) match(line []byte, globs globFilter) (grepMatch, bool) {
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
		if !globs.match(rel) {
			return grepMatch{}, false
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
