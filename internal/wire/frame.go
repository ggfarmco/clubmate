package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type FrameType byte

const (
	FrameData FrameType = iota + 1
	FrameResize
	FrameExit
)

const (
	headerSize    = 5
	maxPayloadLen = 1 << 20
)

var (
	ErrFrameTooLarge = errors.New("wire: frame payload too large")
	ErrBadPayload    = errors.New("wire: malformed payload")
)

type Frame struct {
	Type    FrameType
	Payload []byte
}

func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > maxPayloadLen {
		return ErrFrameTooLarge
	}
	buf := make([]byte, headerSize+len(f.Payload))
	buf[0] = byte(f.Type)
	binary.BigEndian.PutUint32(buf[1:headerSize], uint32(len(f.Payload)))
	copy(buf[headerSize:], f.Payload)
	_, err := w.Write(buf)
	return err
}

func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [headerSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, fmt.Errorf("read frame header: %w", err)
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > maxPayloadLen {
		return Frame{}, ErrFrameTooLarge
	}
	var payload []byte
	if n > 0 {
		payload = make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return Frame{}, fmt.Errorf("read frame payload: %w", err)
		}
	}
	return Frame{Type: FrameType(hdr[0]), Payload: payload}, nil
}

func ResizePayload(cols, rows uint16) []byte {
	p := binary.BigEndian.AppendUint16(nil, cols)
	return binary.BigEndian.AppendUint16(p, rows)
}

func ParseResize(p []byte) (cols, rows uint16, err error) {
	if len(p) != 4 {
		return 0, 0, ErrBadPayload
	}
	return binary.BigEndian.Uint16(p[0:2]), binary.BigEndian.Uint16(p[2:4]), nil
}

func ExitPayload(code int) []byte {
	return binary.BigEndian.AppendUint32(nil, uint32(int32(code)))
}

func ParseExit(p []byte) (int, error) {
	if len(p) != 4 {
		return 0, ErrBadPayload
	}
	return int(int32(binary.BigEndian.Uint32(p))), nil
}
