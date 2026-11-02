package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	chunkMS      = "320"
	maxEventLine = 1 << 20
	idleUnload   = 10 * time.Minute
)

var (
	errUVMissing = errors.New("voice needs uv (https://docs.astral.sh/uv/)")
	errCancelled = errors.New("utterance cancelled")
)

type recognizer interface {
	Start(id, lang, hint string) error
	Feed(id string, pcm []byte) (string, error)
	Finish(ctx context.Context, id string) (string, error)
	Cancel(id string)
	Ready() bool
}

type procRecognizer struct {
	command func() (*exec.Cmd, error)
	lost    func(id string)
	idle    time.Duration
	mu      sync.Mutex
	proc    *process
	streams map[string]*stream
	unload  *time.Timer
	last    time.Time
	closed  bool
}

type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	loaded atomic.Bool
	wmu    sync.Mutex
	ready  bool
	queue  [][]byte
}

type stream struct {
	partial string
	err     error
	final   chan finalResult
}

type finalResult struct {
	text string
	err  error
}

type startRequest struct {
	Op   string `json:"op"`
	ID   string `json:"id"`
	Lang string `json:"lang"`
	Hint string `json:"hint"`
}

type audioRequest struct {
	Op  string `json:"op"`
	ID  string `json:"id"`
	PCM []byte `json:"pcm"`
}

type idRequest struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

type recognizerEvent struct {
	Ev      string `json:"ev"`
	ID      string `json:"id"`
	Text    string `json:"text"`
	Message string `json:"message"`
}

func newProcRecognizer(command func() (*exec.Cmd, error), lost func(id string)) *procRecognizer {
	return &procRecognizer{command: command, lost: lost, idle: idleUnload, streams: map[string]*stream{}}
}

func (r *procRecognizer) Start(id, lang, hint string) error {
	r.mu.Lock()
	p, err := r.ensure()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.streams[id] = &stream{}
	r.touch()
	r.mu.Unlock()
	return r.send(p, id, startRequest{Op: "start", ID: id, Lang: lang, Hint: hint})
}

func (r *procRecognizer) Feed(id string, pcm []byte) (string, error) {
	p, s := r.lookup(id)
	if s == nil {
		return "", errRecognizerStopped
	}
	if err := r.streamErr(s); err != nil {
		return "", err
	}
	if err := r.send(p, id, audioRequest{Op: "audio", ID: id, PCM: pcm}); err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return s.partial, s.err
}

func (r *procRecognizer) Finish(ctx context.Context, id string) (string, error) {
	r.mu.Lock()
	p, s := r.proc, r.streams[id]
	if s == nil {
		r.mu.Unlock()
		return "", errRecognizerStopped
	}
	if s.err != nil {
		r.mu.Unlock()
		r.Cancel(id)
		return "", s.err
	}
	s.final = make(chan finalResult, 1)
	r.mu.Unlock()
	if err := r.send(p, id, idRequest{Op: "end", ID: id}); err != nil {
		return "", err
	}
	select {
	case res := <-s.final:
		return res.text, res.err
	case <-ctx.Done():
		r.Cancel(id)
		return "", ctx.Err()
	}
}

func (r *procRecognizer) Cancel(id string) {
	r.mu.Lock()
	p, s := r.proc, r.streams[id]
	delete(r.streams, id)
	r.touch()
	r.mu.Unlock()
	if s == nil {
		return
	}
	s.resolve(finalResult{err: errCancelled})
	logError("cancel "+id, p.send(idRequest{Op: "cancel", ID: id}))
}

func (r *procRecognizer) Ready() bool {
	r.mu.Lock()
	p := r.proc
	r.mu.Unlock()
	return p != nil && p.loaded.Load()
}

func (r *procRecognizer) warm() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.ensure()
	return err
}

func (r *procRecognizer) stop() {
	r.mu.Lock()
	r.closed = true
	p := r.proc
	if r.unload != nil {
		r.unload.Stop()
	}
	r.mu.Unlock()
	p.kill()
}

func (r *procRecognizer) touch() {
	r.last = time.Now()
	if r.proc == nil || r.closed {
		return
	}
	if r.unload == nil {
		r.unload = time.AfterFunc(r.idle, r.unloadIdle)
		return
	}
	r.unload.Reset(r.idle)
}

func (r *procRecognizer) unloadIdle() {
	r.mu.Lock()
	if r.proc == nil || r.closed || time.Since(r.last) < r.idle {
		r.mu.Unlock()
		return
	}
	p, streams := r.proc, r.streams
	r.proc, r.streams = nil, map[string]*stream{}
	r.mu.Unlock()
	p.kill()
	for id, s := range streams {
		s.resolve(finalResult{err: errRecognizerStopped})
		r.lost(id)
	}
}

func (r *procRecognizer) ensure() (*process, error) {
	if r.closed {
		return nil, errRecognizerStopped
	}
	if r.proc != nil {
		return r.proc, nil
	}
	cmd, err := r.command()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start recognizer: %w", err)
	}
	p := &process{cmd: cmd, stdin: stdin}
	r.proc = p
	r.touch()
	go r.read(p, stdout)
	return p, nil
}

func (r *procRecognizer) read(p *process, stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), maxEventLine)
	for sc.Scan() {
		var ev recognizerEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			logError("recognizer event", err)
			continue
		}
		r.handle(p, ev)
	}
	logError("recognizer exited", cmp.Or(sc.Err(), p.cmd.Wait()))
	r.died(p)
}

func (r *procRecognizer) handle(p *process, ev recognizerEvent) {
	switch ev.Ev {
	case "ready":
		p.loaded.Store(true)
		go p.flush()
		return
	case "error":
		if ev.ID == "" {
			logError("recognizer", errors.New(ev.Message))
			return
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.streams[ev.ID]
	if s == nil {
		return
	}
	switch ev.Ev {
	case "partial":
		s.partial = ev.Text
	case "final":
		delete(r.streams, ev.ID)
		r.touch()
		s.resolve(finalResult{text: ev.Text})
	case "error":
		s.err = errors.New(ev.Message)
		if s.final != nil {
			delete(r.streams, ev.ID)
			r.touch()
			s.resolve(finalResult{err: s.err})
		}
	}
}

func (r *procRecognizer) died(p *process) {
	r.mu.Lock()
	if r.proc != p {
		r.mu.Unlock()
		return
	}
	r.proc = nil
	streams := r.streams
	r.streams = map[string]*stream{}
	respawn := p.loaded.Load() && !r.closed
	r.mu.Unlock()
	for id, s := range streams {
		s.resolve(finalResult{err: errRecognizerStopped})
		r.lost(id)
	}
	if respawn {
		logError("respawn recognizer", r.warm())
	}
}

func (r *procRecognizer) lookup(id string) (*process, *stream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.touch()
	return r.proc, r.streams[id]
}

func (r *procRecognizer) streamErr(s *stream) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return s.err
}

func (r *procRecognizer) send(p *process, id string, msg any) error {
	if p == nil {
		return errRecognizerStopped
	}
	if err := p.send(msg); err != nil {
		return fmt.Errorf("%w: %s: %v", errRecognizerStopped, id, err)
	}
	return nil
}

func (p *process) kill() {
	if p == nil {
		return
	}
	_ = p.stdin.Close()
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
}

func (p *process) send(msg any) error {
	if p == nil {
		return nil
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	p.wmu.Lock()
	defer p.wmu.Unlock()
	if !p.ready {
		p.queue = append(p.queue, line)
		return nil
	}
	_, err = p.stdin.Write(line)
	return err
}

func (p *process) flush() {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	for _, line := range p.queue {
		if _, err := p.stdin.Write(line); err != nil {
			logError("recognizer write", err)
			break
		}
	}
	p.queue = nil
	p.ready = true
}

func (s *stream) resolve(res finalResult) {
	if s.final == nil {
		return
	}
	select {
	case s.final <- res:
	default:
	}
}

func asrCmd(m model) (*exec.Cmd, error) {
	uv, err := findUV()
	if err != nil {
		return nil, err
	}
	dir, err := asrDir()
	if err != nil {
		return nil, err
	}
	args := []string{"run", "--script", filepath.Join(dir, "server.py"), "--engine", m.engine, "--model", m.repo, "--revision", m.revision, "--sha256", m.sha256, "--chunk-ms", chunkMS}
	if engine := os.Getenv("CLUBMATE_VOICE_ENGINE"); engine != "" {
		args = []string{"run", "--script", filepath.Join(dir, "server.py"), "--engine", engine, "--chunk-ms", chunkMS}
	}
	return exec.Command(uv, args...), nil
}

func asrDir() (string, error) {
	if dir := os.Getenv("CLUBMATE_VOICE_ASR_DIR"); dir != "" {
		return dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "asr"), nil
}

func findUV() (string, error) {
	if path, err := exec.LookPath("uv"); err == nil {
		return path, nil
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{filepath.Join(home, ".local", "bin", "uv"), "/opt/homebrew/bin/uv"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
	}
	return "", errUVMissing
}
