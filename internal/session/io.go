package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"time"

	"clubmate/internal/wire"
)

const (
	ReadDefault = 16 << 10
	ReadMax     = 256 << 10

	enterDelay      = 50 * time.Millisecond
	drainTimeout    = 2 * time.Second
	queuedDrain     = time.Minute
	sendTimeout     = 10 * time.Second
	snapshotTimeout = 2 * time.Second
)

var (
	ErrNotRunning  = errors.New("session is not running")
	ErrNoSnapshot  = errors.New("session host cannot be read; restart the session")
	ErrSessionBusy = errors.New("session busy: its program is not reading input; part of the text may have been typed")
)

type SendResult struct {
	Queued bool `json:"queued,omitempty"`
}

type Snapshot struct {
	Text      string `json:"text"`
	AltScreen bool   `json:"altScreen"`
}

func (m *Manager) Send(ctx context.Context, sid, text string, enter bool) (SendResult, error) {
	conn, err := m.dial(ctx, sid)
	if err != nil {
		return SendResult{}, err
	}
	if err := m.writeAll(ctx, conn, []byte(text), enter); err != nil {
		conn.Close()
		return SendResult{}, fmt.Errorf("session %s: %w", sid, err)
	}
	return m.finishInput(conn)
}

func (m *Manager) writeAll(ctx context.Context, conn net.Conn, text []byte, enter bool) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(m.sendTimeout)
	}
	conn.SetWriteDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.SetWriteDeadline(time.Now()) })
	defer stop()
	if err := writeInput(conn, text); err != nil {
		return sendFailed(ctx, err)
	}
	if !enter {
		return nil
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w; the text was typed without Enter", ctx.Err())
	case <-time.After(enterDelay):
	}
	if err := writeInput(conn, []byte("\r")); err != nil {
		return sendFailed(ctx, err)
	}
	return nil
}

func sendFailed(ctx context.Context, err error) error {
	if ne, ok := errors.AsType[net.Error](err); !ok || !ne.Timeout() {
		return err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w; part of the text may have been typed", ctx.Err())
	}
	return ErrSessionBusy
}

func (m *Manager) finishInput(conn net.Conn) (SendResult, error) {
	if uc, ok := conn.(*net.UnixConn); ok {
		if err := uc.CloseWrite(); err != nil {
			conn.Close()
			return SendResult{}, fmt.Errorf("send input: %w", err)
		}
	}
	conn.SetReadDeadline(time.Now().Add(m.drainTimeout))
	_, err := io.Copy(io.Discard, conn)
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		go func() {
			defer conn.Close()
			conn.SetReadDeadline(time.Now().Add(queuedDrain))
			io.Copy(io.Discard, conn)
		}()
		return SendResult{Queued: true}, nil
	}
	conn.Close()
	return SendResult{}, err
}

func (m *Manager) Read(ctx context.Context, sid string, n int, raw bool) (Snapshot, error) {
	if n <= 0 {
		n = ReadDefault
	}
	n = min(n, ReadMax)
	conn, err := m.dial(ctx, sid)
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(m.snapshotTimeout))
	if err := wire.WriteFrame(conn, wire.Frame{Type: wire.FrameSnapshot}); err != nil {
		return Snapshot{}, fmt.Errorf("session %s: %w", sid, err)
	}
	for {
		f, err := wire.ReadFrame(conn)
		var ne net.Error
		switch {
		case errors.As(err, &ne) && ne.Timeout():
			return Snapshot{}, fmt.Errorf("session %s: %w", sid, ErrNoSnapshot)
		case err != nil:
			return Snapshot{}, fmt.Errorf("session %s: %w", sid, ErrNotRunning)
		case f.Type == wire.FrameExit:
			return Snapshot{}, fmt.Errorf("session %s: %w", sid, ErrNotRunning)
		case f.Type != wire.FrameSnapshot:
			continue
		case len(f.Payload) == 0 || f.Payload[0] == 1:
			return Snapshot{AltScreen: true}, nil
		}
		data := f.Payload[1:]
		data = data[max(0, len(data)-n):]
		if raw {
			return Snapshot{Text: string(data)}, nil
		}
		return Snapshot{Text: Plain(data, n)}, nil
	}
}

func (m *Manager) dial(ctx context.Context, sid string) (net.Conn, error) {
	s, err := m.Get(ctx, sid)
	if err != nil {
		return nil, err
	}
	if s.State != StateRunning {
		return nil, fmt.Errorf("session %s: %w", sid, ErrNotRunning)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", m.dirs.SessionSocket(sid))
	if err != nil {
		return nil, fmt.Errorf("session %s: %w: %v", sid, ErrNotRunning, err)
	}
	return conn, nil
}

func writeInput(conn net.Conn, data []byte) error {
	for chunk := range slices.Chunk(data, wire.MaxPayload) {
		if err := wire.WriteFrame(conn, wire.Frame{Type: wire.FrameData, Payload: chunk}); err != nil {
			return fmt.Errorf("send input: %w", err)
		}
	}
	return nil
}
