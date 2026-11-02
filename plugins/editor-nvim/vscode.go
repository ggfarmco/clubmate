package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	vscodePortKey   = "vscode.port"
	vscodeAppCode   = "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
	vscodeReadyMark = "Web UI available at "
	vscodeHost      = "127.0.0.1"
	startTimeout    = 60 * time.Second
	downloadTimeout = 10 * time.Minute
	downloadPoll    = 250 * time.Millisecond
	stopGrace       = 5 * time.Second
	procTailSize    = 4096
	orphanPoll      = 50 * time.Millisecond
	pidFileName     = "server.pid"
	stagingSuffix   = ".staging"
)

var (
	errCodeMissing      = errors.New("VS Code not found — install it or switch the editor to Neovim in Settings → Code")
	errServerMissing    = errors.New("vscode: server not downloaded")
	errServerExited     = errors.New("vscode: server exited")
	errStartTimeout     = errors.New("vscode: server did not start in time")
	errDownloadNotFound = errors.New("vscode: download finished without a server")
)

type portStore interface {
	Port(ctx context.Context) (int, error)
	SetPort(ctx context.Context, port int) error
}

type kvPorts struct{ h *pluginsdk.Host }

func (k kvPorts) Port(ctx context.Context) (int, error) {
	var p int
	_, err := k.h.StorageGet(ctx, vscodePortKey, &p)
	return p, err
}

func (k kvPorts) SetPort(ctx context.Context, port int) error {
	return k.h.StorageSet(ctx, vscodePortKey, port)
}

func findCode(lookPath func(string) (string, error), exists func(string) bool) (string, error) {
	if p, err := lookPath("code"); err == nil {
		return p, nil
	}
	if exists(vscodeAppCode) {
		return vscodeAppCode, nil
	}
	return "", errCodeMissing
}

type vscodeServer struct {
	dir    string
	code   string
	kv     portStore
	mu     sync.Mutex
	run    *serverRun
	exited chan error
}

type serverRun struct {
	proc  *proc
	port  int
	token string
}

func newVSCodeServer(stateDir, code string, kv portStore) *vscodeServer {
	return &vscodeServer{
		dir:    filepath.Join(stateDir, "vscode"),
		code:   code,
		kv:     kv,
		exited: make(chan error, 1),
	}
}

func (s *vscodeServer) cliDir() string { return filepath.Join(s.dir, "cli") }

func (s *vscodeServer) serverDir() string { return filepath.Join(s.dir, "server") }

func (s *vscodeServer) Download(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	if err := s.removeStaging(); err != nil {
		return err
	}
	_, tokenFile, err := s.writeToken()
	if err != nil {
		return err
	}
	p, err := startProc(s.code, "serve-web",
		"--host", vscodeHost,
		"--port", "0",
		"--connection-token-file", tokenFile,
		"--accept-server-license-terms",
		"--cli-data-dir", s.cliDir(),
		"--server-data-dir", s.serverDir())
	if err != nil {
		return fmt.Errorf("vscode download: %w", err)
	}
	defer p.stop()
	select {
	case <-p.ready:
	case <-p.done:
		return fmt.Errorf("vscode download: %w", p.err)
	case <-ctx.Done():
		return fmt.Errorf("vscode download: %w", ctx.Err())
	}
	if err := fetch(ctx, p.url); err != nil && !s.Downloaded() {
		return fmt.Errorf("vscode download: %w", err)
	}
	tick := time.NewTicker(downloadPoll)
	defer tick.Stop()
	for !s.Downloaded() {
		select {
		case <-tick.C:
		case <-p.done:
			if s.Downloaded() {
				return nil
			}
			return fmt.Errorf("vscode download: %w: %w", errDownloadNotFound, p.err)
		case <-ctx.Done():
			return fmt.Errorf("vscode download: %w", ctx.Err())
		}
	}
	return nil
}

func fetch(ctx context.Context, u string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, procTailSize))
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *vscodeServer) removeStaging() error {
	dirs, err := filepath.Glob(filepath.Join(s.cliDir(), "serve-web", "*"+stagingSuffix))
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	return nil
}

func (s *vscodeServer) Downloaded() bool {
	return s.serverBin() != ""
}

func (s *vscodeServer) serverBin() string {
	root := filepath.Join(s.cliDir(), "serve-web")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var newest string
	var newestTime time.Time
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), stagingSuffix) {
			continue
		}
		bin := filepath.Join(root, e.Name(), "bin", "code-server")
		info, err := os.Stat(bin)
		if err != nil || info.IsDir() {
			continue
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest, newestTime = bin, info.ModTime()
		}
	}
	return newest
}

func (s *vscodeServer) pidFile() string { return filepath.Join(s.dir, pidFileName) }

func (s *vscodeServer) Start(ctx context.Context, dark bool) error {
	s.Stop()
	s.killOrphan()
	bin := s.serverBin()
	if bin == "" {
		return errServerMissing
	}
	if err := writeMachineSettings(s.serverDir(), dark); err != nil {
		return err
	}
	token, tokenFile, err := s.writeToken()
	if err != nil {
		return err
	}
	port, err := s.pickPort(ctx)
	if err != nil {
		return err
	}
	p, err := startProc(bin,
		"--host", vscodeHost,
		"--port", strconv.Itoa(port),
		"--connection-token-file", tokenFile,
		"--accept-server-license-terms",
		"--disable-workspace-trust",
		"--server-data-dir", s.serverDir())
	if err != nil {
		return fmt.Errorf("vscode server: %w", err)
	}
	if err := os.WriteFile(s.pidFile(), []byte(strconv.Itoa(p.cmd.Process.Pid)), 0o600); err != nil {
		p.stop()
		return err
	}
	r := &serverRun{proc: p, port: port, token: token}
	s.mu.Lock()
	s.run = r
	select {
	case <-s.exited:
	default:
	}
	s.mu.Unlock()
	go s.watch(r)
	timer := time.NewTimer(startTimeout)
	defer timer.Stop()
	select {
	case <-p.ready:
		return nil
	case <-p.done:
		select {
		case <-p.ready:
			return nil
		default:
		}
		return fmt.Errorf("vscode server: %w", p.err)
	case <-timer.C:
		s.stopRun(r)
		return errStartTimeout
	case <-ctx.Done():
		s.stopRun(r)
		return ctx.Err()
	}
}

func (s *vscodeServer) watch(r *serverRun) {
	<-r.proc.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != r {
		return
	}
	s.run = nil
	select {
	case <-r.proc.ready:
	default:
		return
	}
	select {
	case s.exited <- r.proc.err:
	default:
	}
}

func (s *vscodeServer) stopRun(r *serverRun) {
	s.mu.Lock()
	if s.run == r {
		s.run = nil
	}
	s.mu.Unlock()
	r.proc.stop()
}

func (s *vscodeServer) Stop() {
	s.mu.Lock()
	r := s.run
	s.run = nil
	s.mu.Unlock()
	if r != nil {
		r.proc.stop()
		_ = os.Remove(s.pidFile())
	}
}

func (s *vscodeServer) killOrphan() {
	raw, err := os.ReadFile(s.pidFile())
	if err != nil {
		return
	}
	defer os.Remove(s.pidFile())
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.Contains(string(out), s.serverDir()) {
		return
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		_ = syscall.Kill(-pid, sig)
		deadline := time.Now().Add(stopGrace)
		for time.Now().Before(deadline) {
			if syscall.Kill(pid, 0) != nil {
				return
			}
			time.Sleep(orphanPoll)
		}
	}
}

func (s *vscodeServer) Base() (port int, token string, ok bool) {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	if r == nil {
		return 0, "", false
	}
	select {
	case <-r.proc.ready:
		return r.port, r.token, true
	default:
		return 0, "", false
	}
}

func (s *vscodeServer) Exited() <-chan error {
	return s.exited
}

func (s *vscodeServer) writeToken() (token, file string, err error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", "", err
	}
	token = rand.Text()
	file = filepath.Join(s.dir, "token")
	if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", "", err
	}
	if err := os.WriteFile(file, []byte(token), 0o600); err != nil {
		return "", "", err
	}
	return token, file, nil
}

func (s *vscodeServer) pickPort(ctx context.Context) (int, error) {
	stored, err := s.kv.Port(ctx)
	if err != nil {
		return 0, err
	}
	if stored > 0 && portFree(stored) {
		return stored, nil
	}
	l, err := net.Listen("tcp", net.JoinHostPort(vscodeHost, "0"))
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return 0, err
	}
	return port, s.kv.SetPort(ctx, port)
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort(vscodeHost, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func workbenchURL(port int, token, folder string, open *openArgs) string {
	u := fmt.Sprintf("http://%s:%d/?tkn=%s&folder=%s", vscodeHost, port, url.QueryEscape(token), url.QueryEscape(folder))
	if open == nil {
		return u
	}
	loc := open.Path
	if open.Line > 0 {
		loc += ":" + strconv.Itoa(open.Line)
		if open.Col > 0 {
			loc += ":" + strconv.Itoa(open.Col)
		}
	}
	file := url.URL{Scheme: "vscode-remote", Host: net.JoinHostPort(vscodeHost, strconv.Itoa(port)), Path: loc}
	payload, _ := json.Marshal([][]string{{"openFile", file.String()}, {"gotoLineMode", "true"}})
	return u + "&payload=" + url.QueryEscape(string(payload))
}

func writeMachineSettings(serverDir string, dark bool) error {
	file := filepath.Join(serverDir, "data", "Machine", "settings.json")
	settings := map[string]any{}
	raw, err := os.ReadFile(file)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("vscode settings %s: %w", file, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	settings["workbench.colorTheme"] = "Default Light Modern"
	if dark {
		settings["workbench.colorTheme"] = "Default Dark Modern"
	}
	settings["chat.disableAIFeatures"] = true
	settings["workbench.secondarySideBar.defaultVisibility"] = "hidden"
	settings["task.allowAutomaticTasks"] = "off"
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, out, 0o600)
}

type proc struct {
	cmd   *exec.Cmd
	tail  *tailBuffer
	ready chan struct{}
	done  chan struct{}
	url   string
	err   error
}

func startProc(name string, args ...string) (*proc, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = stopGrace
	p := &proc{cmd: cmd, tail: &tailBuffer{}, ready: make(chan struct{}), done: make(chan struct{})}
	cmd.Stderr = p.tail
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go p.watch(stdout)
	return p, nil
}

func (p *proc) watch(stdout io.Reader) {
	ready := false
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		_, _ = p.tail.Write([]byte(line + "\n"))
		if _, u, ok := strings.Cut(line, vscodeReadyMark); ok && !ready {
			ready = true
			p.url = strings.TrimSpace(u)
			close(p.ready)
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	err := p.cmd.Wait()
	if err == nil {
		err = errServerExited
	}
	if msg := p.tail.String(); msg != "" {
		err = fmt.Errorf("%w: %s", err, msg)
	}
	p.err = err
	close(p.done)
}

func (p *proc) stop() {
	p.signal(syscall.SIGTERM)
	select {
	case <-p.done:
		return
	case <-time.After(stopGrace):
	}
	p.signal(syscall.SIGKILL)
	<-p.done
}

func (p *proc) signal(sig syscall.Signal) {
	_ = syscall.Kill(-p.cmd.Process.Pid, sig)
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > procTailSize {
		t.buf = t.buf[len(t.buf)-procTailSize:]
	}
	return len(b), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
