package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"clubmate/internal/paths"
	"clubmate/internal/wire"
)

const stdinChunk = 32 * 1024

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
			code, err := attach(cmd.Context(), args[0], os.Stdin, a.out)
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

func attach(ctx context.Context, id string, stdin *os.File, stdout io.Writer) (int, error) {
	dirs, err := paths.Resolve()
	if err != nil {
		return 1, err
	}
	if err := dirs.CheckRuntime(); err != nil {
		return 1, fmt.Errorf("attach %s: %w", id, err)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", dirs.SessionSocket(id))
	if err != nil {
		return 1, fmt.Errorf("attach %s: %w", id, err)
	}
	defer conn.Close()
	if err := verifyPeer(conn); err != nil {
		return 1, fmt.Errorf("attach %s: %w", id, err)
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
