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
	snapshotTimeout = 2 * time.Second
)

var (
	ErrNotRunning = errors.New("session is not running")
	ErrNoSnapshot = errors.New("session host cannot be read; restart the session")
)

type Snapshot struct {
	Text      string `json:"text"`
	AltScreen bool   `json:"altScreen"`
}

func (m *Manager) Send(ctx context.Context, sid, text string, enter bool) error {
	conn, err := m.dial(ctx, sid)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := writeInput(conn, []byte(text)); err != nil {
		return err
	}
	if enter {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(enterDelay):
		}
		if err := writeInput(conn, []byte("\r")); err != nil {
			return err
		}
	}
	return finishInput(conn)
}

func finishInput(conn net.Conn) error {
	if uc, ok := conn.(*net.UnixConn); ok {
		if err := uc.CloseWrite(); err != nil {
			return fmt.Errorf("send input: %w", err)
		}
	}
	conn.SetReadDeadline(time.Now().Add(drainTimeout))
	_, err := io.Copy(io.Discard, conn)
	return err
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
		return Snapshot{Text: Plain(data)}, nil
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
