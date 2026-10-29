package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/internal/wire"
)

const (
	ReadDefault = 16 << 10
	ReadMax     = 256 << 10

	enterDelay      = 50 * time.Millisecond
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
	if !enter {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(enterDelay):
	}
	return writeInput(conn, []byte("\r"))
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

func Plain(b []byte) string {
	out := make([]byte, 0, len(b))
	lineStart := 0
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			i = skipEscape(b, i)
			continue
		case c == '\r' && i+1 < len(b) && b[i+1] == '\n':
		case c == '\r':
			out = out[:lineStart]
		case c == '\b':
			if len(out) > lineStart {
				_, size := utf8.DecodeLastRune(out[lineStart:])
				out = out[:len(out)-size]
			}
		case c == '\n':
			out = append(out, c)
			lineStart = len(out)
		case c == '\t' || c >= 0x20 && c != 0x7f:
			out = append(out, c)
		}
		i++
	}
	return strings.ToValidUTF8(string(out), "")
}

func skipEscape(b []byte, i int) int {
	if i+1 >= len(b) {
		return len(b)
	}
	if b[i+1] < 0x20 {
		return i + 1
	}
	switch b[i+1] {
	case '[':
		j := i + 2
		for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
			j++
		}
		return min(j+1, len(b))
	case ']', 'P', 'X', '^', '_':
		for j := i + 2; j < len(b); j++ {
			if b[j] == 0x07 {
				return j + 1
			}
			if b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\' {
				return j + 2
			}
		}
		return len(b)
	}
	j := i + 1
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f {
		j++
	}
	return min(j+1, len(b))
}
