package sessionhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"clubmate/internal/proc"
	"clubmate/internal/wire"
)

const (
	defaultScrollback = 256 << 10
	maxPendingBytes   = 4 << 20
	maxFramePayload   = 1 << 20
	readBufSize       = 32 << 10
	initialCols       = 80
	initialRows       = 24
	exitFlushTimeout  = 2 * time.Second
	repaintTimeout    = 500 * time.Millisecond
	defaultTerm       = "TERM=xterm-256color"
	sessionMarker     = "CLUBMATE_SESSION_ID="
	projectMarker     = "CLUBMATE_PROJECT_ID="
)

var ErrEmptyCommand = errors.New("sessionhost: empty command")

var altScreenReset = []byte("\x1b[?1049h\x1b[2J\x1b[H")

type host struct {
	ptmx       *os.File
	pgid       int
	ring       *wire.Ring
	alt        altScreen
	status     statusProbe
	queries    chan struct{}
	output     chan struct{}
	maxPending int
	mu         sync.Mutex
	clients    map[*client]struct{}
	exit       *wire.Frame
	wg         sync.WaitGroup
}

type client struct {
	conn     net.Conn
	wake     chan struct{}
	pending  []byte
	repaint  bool
	snapshot *wire.Frame
}

func Run(ctx context.Context, cfg Config) error {
	if len(cfg.Command) == 0 {
		return ErrEmptyCommand
	}
	if cfg.Scrollback <= 0 {
		cfg.Scrollback = defaultScrollback
	}
	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = childEnv(cfg)
	if err := raiseNoFile(); err != nil {
		fmt.Fprintln(os.Stderr, "clubmate-session: raise open-file limit:", err)
	}
	ptmx, err := pty.StartWithAttrs(cmd, &pty.Winsize{Cols: initialCols, Rows: initialRows}, &syscall.SysProcAttr{Setsid: true, Setctty: true})
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer ptmx.Close()
	hostStart, err := proc.StartTime(os.Getpid())
	if err != nil {
		fmt.Fprintln(os.Stderr, "clubmate-session: host start time unavailable:", err)
	}
	meta := Meta{
		ID:        cfg.ID,
		ProjectID: cfg.ProjectID,
		Title:     cfg.Title,
		Owner:     cfg.Owner,
		Command:   slices.Clone(cfg.Command),
		Cwd:       cfg.Cwd,
		HostPID:   os.Getpid(),
		ChildPID:  cmd.Process.Pid,
		PGID:      cmd.Process.Pid,
		HostStart: hostStart,
		StartedAt: time.Now(),
		Proto:     Protocol,
	}
	if err := WriteMeta(cfg.MetaPath, meta); err != nil {
		return abort(cmd, err)
	}
	ln, err := listen(cfg.Socket)
	if err != nil {
		os.Remove(cfg.MetaPath)
		return abort(cmd, err)
	}
	h := &host{
		ptmx:       ptmx,
		pgid:       meta.PGID,
		ring:       wire.NewRing(cfg.Scrollback),
		maxPending: max(maxPendingBytes, 2*cfg.Scrollback),
		clients:    map[*client]struct{}{},
		output:     make(chan struct{}, 1),
		queries:    make(chan struct{}, 1),
	}
	code := h.serve(ctx, cmd, ln)
	exitedAt := time.Now()
	meta.ExitCode, meta.ExitedAt = &code, &exitedAt
	metaErr := WriteMeta(cfg.MetaPath, meta)
	h.shutdown(code)
	ln.Close()
	os.Remove(cfg.Socket)
	ptmx.Close()
	h.wg.Wait()
	return metaErr
}

func StripMarkers(env []string) []string {
	return slices.DeleteFunc(env, isMarker)
}

func (h *host) serve(ctx context.Context, cmd *exec.Cmd, ln net.Listener) int {
	exited := make(chan struct{})
	defer close(exited)
	h.wg.Go(func() { h.acceptLoop(ln) })
	h.wg.Go(func() { hangupOnCancel(ctx, cmd.Process.Pid, exited) })
	go answerStatus(h.ptmx, h.queries, exited)
	h.pump()
	return waitExit(cmd)
}

func (h *host) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		h.add(conn)
	}
}

func (h *host) add(conn net.Conn) {
	c := &client{conn: conn, wake: make(chan struct{}, 1)}
	h.mu.Lock()
	if h.alt.on && h.exit == nil {
		c.pending = slices.Clone(altScreenReset)
		c.repaint = true
	} else {
		c.pending = h.ring.Bytes()
	}
	c.wake <- struct{}{}
	if h.exit != nil {
		h.finishLocked(c)
	} else {
		h.clients[c] = struct{}{}
	}
	h.mu.Unlock()
	h.wg.Go(func() { h.writeLoop(c) })
	h.wg.Go(func() { h.readLoop(c) })
}

func (h *host) pump() {
	buf := make([]byte, readBufSize)
	for {
		n, err := h.ptmx.Read(buf)
		if n > 0 {
			h.broadcast(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (h *host) broadcast(p []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ring.Write(p)
	h.alt.feed(p)
	if h.status.feed(p) && len(h.clients) == 0 {
		select {
		case h.queries <- struct{}{}:
		default:
		}
	}
	select {
	case h.output <- struct{}{}:
	default:
	}
	for c := range h.clients {
		if len(c.pending)+len(p) > h.maxPending {
			h.dropLocked(c)
			continue
		}
		c.pending = append(c.pending, p...)
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

func (h *host) shutdown(code int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.exit = &wire.Frame{Type: wire.FrameExit, Payload: wire.ExitPayload(code)}
	for c := range h.clients {
		h.finishLocked(c)
	}
}

func (h *host) finishLocked(c *client) {
	delete(h.clients, c)
	close(c.wake)
	c.conn.SetWriteDeadline(time.Now().Add(exitFlushTimeout))
}

func (h *host) writeLoop(c *client) {
	defer c.conn.Close()
	for {
		_, open := <-c.wake
		h.mu.Lock()
		data, exit, snapshot := c.pending, h.exit, c.snapshot
		c.pending = nil
		h.mu.Unlock()
		if err := writeData(c.conn, data); err != nil {
			h.drop(c)
			return
		}
		if snapshot != nil {
			wire.WriteFrame(c.conn, *snapshot)
			return
		}
		if open {
			continue
		}
		if exit != nil {
			wire.WriteFrame(c.conn, *exit)
		}
		return
	}
}

func (h *host) readLoop(c *client) {
	defer h.drop(c)
	repaint := c.repaint
	for {
		f, err := wire.ReadFrame(c.conn)
		if err != nil {
			return
		}
		switch f.Type {
		case wire.FrameData:
			h.ptmx.Write(f.Payload)
		case wire.FrameResize:
			h.resize(f.Payload, repaint)
			repaint = false
		case wire.FrameSnapshot:
			h.snapshot(c)
		}
	}
}

func (h *host) snapshot(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	c.pending = nil
	payload := []byte{0}
	if h.alt.on {
		payload[0] = 1
	} else {
		ring := h.ring.Bytes()
		payload = append(payload, ring[max(0, len(ring)-(wire.MaxPayload-1)):]...)
	}
	c.snapshot = &wire.Frame{Type: wire.FrameSnapshot, Payload: payload}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (h *host) resize(payload []byte, repaint bool) {
	cols, rows, err := wire.ParseResize(payload)
	if err != nil || cols == 0 || rows == 0 {
		return
	}
	if repaint && rows > 1 && h.sizeIs(cols, rows) {
		h.repaint(cols, rows)
		return
	}
	h.setSize(cols, rows)
}

func (h *host) repaint(cols, rows uint16) {
	select {
	case <-h.output:
	default:
	}
	if !h.setSize(cols, rows-1) {
		return
	}
	select {
	case <-h.output:
	case <-time.After(repaintTimeout):
	}
	if h.sizeIs(cols, rows-1) {
		h.setSize(cols, rows)
	}
}

func (h *host) sizeIs(cols, rows uint16) bool {
	ws, err := pty.GetsizeFull(h.ptmx)
	return err == nil && ws.Cols == cols && ws.Rows == rows
}

func (h *host) setSize(cols, rows uint16) bool {
	if err := pty.Setsize(h.ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return false
	}
	unix.Kill(-h.foregroundPGID(), unix.SIGWINCH)
	return true
}

func (h *host) foregroundPGID() int {
	pgid, err := unix.IoctlGetInt(int(h.ptmx.Fd()), unix.TIOCGPGRP)
	if err != nil || pgid <= 1 {
		return h.pgid
	}
	return pgid
}

func (h *host) drop(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(c)
}

func (h *host) dropLocked(c *client) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	close(c.wake)
	c.conn.Close()
}

func writeData(w io.Writer, data []byte) error {
	for chunk := range slices.Chunk(data, maxFramePayload) {
		if err := wire.WriteFrame(w, wire.Frame{Type: wire.FrameData, Payload: chunk}); err != nil {
			return err
		}
	}
	return nil
}

func childEnv(cfg Config) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return slices.Contains(cfg.UnsetEnv, key)
	})
	env = StripMarkers(append(env, cfg.Env...))
	env = append(env, sessionMarker+cfg.ID, projectMarker+cfg.ProjectID)
	if !slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "TERM=") }) {
		env = append(env, defaultTerm)
	}
	return appendPath(env, cfg.PathAppend)
}

func appendPath(env, extra []string) []string {
	if len(extra) == 0 {
		return env
	}
	i := slices.IndexFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })
	var parts []string
	if i >= 0 {
		if v := strings.TrimPrefix(env[i], "PATH="); v != "" {
			parts = strings.Split(v, ":")
		}
	}
	for _, e := range extra {
		if !slices.Contains(parts, e) {
			parts = append(parts, e)
		}
	}
	joined := "PATH=" + strings.Join(parts, ":")
	if i < 0 {
		return append(env, joined)
	}
	env[i] = joined
	return env
}

func isMarker(kv string) bool {
	return strings.HasPrefix(kv, sessionMarker) || strings.HasPrefix(kv, projectMarker)
}

func listen(sock string) (net.Listener, error) {
	tmp := sock + ".tmp"
	os.Remove(tmp)
	ln, err := net.Listen("unix", tmp)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, sock); err != nil {
		ln.Close()
		return nil, fmt.Errorf("publish socket: %w", err)
	}
	return ln, nil
}

func abort(cmd *exec.Cmd, err error) error {
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Wait()
	return err
}

func hangupOnCancel(ctx context.Context, pgid int, exited <-chan struct{}) {
	select {
	case <-ctx.Done():
		syscall.Kill(-pgid, syscall.SIGHUP)
	case <-exited:
	}
}

func waitExit(cmd *exec.Cmd) int {
	cmd.Wait()
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}
