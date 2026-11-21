package tunnel

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	MaxRecordWire = CounterSize + MaxRecordPlain + TagSize
	minRecordWire = CounterSize + HeaderSize + TagSize

	PushData    byte = 1
	PushEnd     byte = 2
	IDSize           = 16
	MaxPushItem      = 17 << 20

	ssePrefix = "data: "
)

var ErrFrame = errors.New("tunnel: bad frame")

func AppendRecord(dst, rec []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(rec)))
	return append(dst, rec...)
}

type RecordReader struct {
	r io.Reader
}

func NewRecordReader(r io.Reader) *RecordReader { return &RecordReader{r: r} }

func (rr *RecordReader) Next() ([]byte, error) {
	var n [4]byte
	if _, err := io.ReadFull(rr.r, n[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size < minRecordWire || size > MaxRecordWire {
		return nil, fmt.Errorf("%w: record of %d bytes", ErrFrame, size)
	}
	rec := make([]byte, size)
	if _, err := io.ReadFull(rr.r, rec); err != nil {
		return nil, unexpected(err)
	}
	return rec, nil
}

type PushItem struct {
	Kind byte
	ID   [IDSize]byte
	Data []byte
}

func AppendPush(dst []byte, it PushItem) []byte {
	dst = append(dst, it.Kind)
	dst = append(dst, it.ID[:]...)
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(it.Data)))
	return append(dst, it.Data...)
}

type PushReader struct {
	r io.Reader
}

func NewPushReader(r io.Reader) *PushReader { return &PushReader{r: r} }

func (pr *PushReader) Next() (PushItem, error) {
	var head [1 + IDSize + 4]byte
	if _, err := io.ReadFull(pr.r, head[:1]); err != nil {
		return PushItem{}, err
	}
	if _, err := io.ReadFull(pr.r, head[1:]); err != nil {
		return PushItem{}, unexpected(err)
	}
	it := PushItem{Kind: head[0]}
	if it.Kind != PushData && it.Kind != PushEnd {
		return PushItem{}, fmt.Errorf("%w: push kind %d", ErrFrame, it.Kind)
	}
	copy(it.ID[:], head[1:1+IDSize])
	n := binary.BigEndian.Uint32(head[1+IDSize:])
	if n > MaxPushItem {
		return PushItem{}, fmt.Errorf("%w: push item of %d bytes", ErrFrame, n)
	}
	data, err := io.ReadAll(io.LimitReader(pr.r, int64(n)))
	if err != nil {
		return PushItem{}, err
	}
	if len(data) < int(n) {
		return PushItem{}, io.ErrUnexpectedEOF
	}
	it.Data = data
	return it, nil
}

func SSEData(frame []byte) string {
	return ssePrefix + base64.StdEncoding.EncodeToString(frame) + "\n\n"
}

func ParseSSEData(line string) ([]byte, error) {
	v, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), ssePrefix)
	if !ok {
		return nil, fmt.Errorf("%w: not an SSE data line", ErrFrame)
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFrame, err)
	}
	return b, nil
}

func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
