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

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"clubmate/internal/id"
	"clubmate/internal/paths"
	"clubmate/internal/rpc"
	"clubmate/internal/session"
	"clubmate/internal/wire"
)

const stdinChunk = 32 * 1024

var ErrInvalidSessionID = errors.New("invalid session id")

type frameWriter struct {
	mu   sync.Mutex
	conn net.Conn
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
	s, err := a.lookupSession(ctx, sid)
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
	return stream(ctx, sid, stdin, a.out)
}

func (a *app) lookupSession(ctx context.Context, sid string) (session.Session, error) {
	var s session.Session
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
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", dirs.SessionSocket(sid))
	if err != nil {
		return 1, fmt.Errorf("attach %s: %w", sid, err)
	}
	defer conn.Close()
	if err := verifyPeer(conn); err != nil {
		return 1, fmt.Errorf("attach %s: %w", sid, err)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	closeOnCancel := context.AfterFunc(ctx, func() { conn.Close() })
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

	fw := &frameWriter{conn: conn}
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

	return readLoop(conn, stdout)
}

func verifyPeer(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return paths.ErrUntrustedPeer
	}
	return paths.VerifyPeer(uc)
}

func readLoop(conn net.Conn, stdout io.Writer) (int, error) {
	for {
		f, err := wire.ReadFrame(conn)
		if err != nil {
			return 1, fmt.Errorf("connection lost: %w", err)
		}
		switch f.Type {
		case wire.FrameData:
			if _, err := stdout.Write(f.Payload); err != nil {
				return 1, err
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

func (w *frameWriter) write(f wire.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return wire.WriteFrame(w.conn, f)
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
			if werr := w.write(wire.Frame{Type: wire.FrameData, Payload: append([]byte(nil), buf[:n]...)}); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
