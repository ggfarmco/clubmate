package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"clubmate/internal/paths"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/wire"
	"clubmate/pkg/id"
)

const (
	stdinChunk     = 32 * 1024
	lookupTimeout  = 2 * time.Second
	reconnectDelay = 50 * time.Millisecond
)

var (
	ErrInvalidSessionID = errors.New("invalid session id")
	errConnectionLost   = errors.New("connection lost")
)

type cursor struct {
	seen  uint64
	pos   uint64
	known bool
}

type frameWriter struct {
	writeMu sync.Mutex
	mu      sync.Mutex
	conn    net.Conn
	closed  bool
}

func (a *app) attachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <session-id>",
		Short: "Attach this terminal to a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := a.attach(cmd.Context(), args[0], os.Stdin, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}
}

func (a *app) attach(ctx context.Context, sid string, stdin *os.File, stderr io.Writer) (int, error) {
	if !id.Valid(session.IDPrefix, sid) {
		return 1, fmt.Errorf("attach %q: %w", sid, ErrInvalidSessionID)
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	s, err := a.lookupSession(ctx, sid)
	if ctx.Err() != nil {
		return 0, nil
	}
	if err != nil {
		return 1, err
	}
	switch s.State {
	case session.StateExited:
		code := 1
		if s.ExitCode != nil {
			code = *s.ExitCode
		}
		fmt.Fprintf(stderr, "session %s exited (code %d)\n", sid, code)
		return code, nil
	case session.StateLost:
		fmt.Fprintf(stderr, "session %s lost\n", sid)
		return 1, nil
	}
	code, err := stream(ctx, sid, stdin, a.out)
	if ctx.Err() != nil {
		return 0, nil
	}
	return code, err
}

func (a *app) lookupSession(ctx context.Context, sid string) (session.Session, error) {
	var s session.Session
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	err := a.call(ctx, "session.get", map[string]string{"id": sid}, &s)
	var remote *rpc.RemoteError
	if errors.As(err, &remote) {
		return session.Session{}, fmt.Errorf("attach %s: %w", sid, err)
	}
	return s, nil
}

func stream(ctx context.Context, sid string, stdin *os.File, stdout io.Writer) (int, error) {
	dirs, err := paths.Resolve()
	if err != nil {
		return 1, err
	}
	if err := dirs.CheckRuntime(); err != nil {
		return 1, fmt.Errorf("attach %s: %w", sid, err)
	}
	socket := dirs.SessionSocket(sid)
	conn, err := dialSession(ctx, socket)
	if err != nil {
		return 1, fmt.Errorf("attach %s: %w", sid, err)
	}
	fw := &frameWriter{conn: conn}
	defer fw.close()
	closeOnCancel := context.AfterFunc(ctx, fw.close)
	defer closeOnCancel()

	fd := int(stdin.Fd())
	isTTY := term.IsTerminal(fd)
	if isTTY {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return 1, fmt.Errorf("raw mode: %w", err)
		}
		defer term.Restore(fd, state)
	}

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	done := make(chan struct{})
	var wg sync.WaitGroup
	defer wg.Wait()
	defer close(done)

	if isTTY {
		fw.sendSize(fd)
		wg.Go(func() {
			for {
				select {
				case <-winch:
					fw.sendSize(fd)
				case <-done:
					return
				}
			}
		})
	}
	go fw.forwardStdin(stdin)

	var cur cursor
	for {
		code, err := readLoop(conn, stdout, &cur)
		if !errors.Is(err, errConnectionLost) {
			return code, err
		}
		next, derr := redial(ctx, socket)
		if derr != nil || !fw.swap(next) {
			return code, err
		}
		conn = next
		if isTTY {
			fw.sendSize(fd)
		}
	}
}

func dialSession(ctx context.Context, socket string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	if err := verifyPeer(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func redial(ctx context.Context, socket string) (net.Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(reconnectDelay):
	}
	return dialSession(ctx, socket)
}

func verifyPeer(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return paths.ErrUntrustedPeer
	}
	return paths.VerifyPeer(uc)
}

func readLoop(conn net.Conn, stdout io.Writer, cur *cursor) (int, error) {
	cur.known = false
	for {
		f, err := wire.ReadFrame(conn)
		if err != nil {
			return 1, fmt.Errorf("%w: %w", errConnectionLost, err)
		}
		switch f.Type {
		case wire.FrameData:
			if _, err := stdout.Write(cur.fresh(f.Payload)); err != nil {
				return 1, err
			}
		case wire.FrameReplay, wire.FrameMark:
			off, err := wire.ParseOffset(f.Payload)
			if err != nil {
				return 1, err
			}
			cur.pos, cur.known = off, true
			if f.Type == wire.FrameMark {
				cur.seen = off
			}
		case wire.FrameExit:
			code, err := wire.ParseExit(f.Payload)
			if err != nil {
				return 1, err
			}
			return code, nil
		}
	}
}

func (c *cursor) fresh(p []byte) []byte {
	if !c.known {
		return p
	}
	skip := 0
	if c.seen > c.pos {
		skip = int(min(c.seen-c.pos, uint64(len(p))))
	}
	c.pos += uint64(len(p))
	c.seen = max(c.seen, c.pos)
	return p[skip:]
}

func (w *frameWriter) write(f wire.Frame) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	return wire.WriteFrame(conn, f)
}

func (w *frameWriter) swap(conn net.Conn) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.Close()
	if w.closed {
		conn.Close()
		return false
	}
	w.conn = conn
	return true
}

func (w *frameWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	w.conn.Close()
}

func (w *frameWriter) sendSize(fd int) {
	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return
	}
	_ = w.write(wire.Frame{Type: wire.FrameResize, Payload: wire.ResizePayload(uint16(cols), uint16(rows))})
}

func (w *frameWriter) forwardStdin(stdin io.Reader) {
	buf := make([]byte, stdinChunk)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			_ = w.write(wire.Frame{Type: wire.FrameData, Payload: append([]byte(nil), buf[:n]...)})
		}
		if err != nil {
			return
		}
	}
}
