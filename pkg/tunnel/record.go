package tunnel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type Type uint8

const (
	TypeRequest Type = 1
	TypeReply   Type = 2
	TypeChunk   Type = 3
	TypeEnd     Type = 4
)

const (
	FlagLast    uint8 = 1
	FlagDeflate uint8 = 2

	dictShift = 2
	dictMask  = 3 << dictShift

	HeaderSize     = 8
	MaxRecordPlain = 64 << 10
	MaxPayload     = MaxRecordPlain - HeaderSize
	MaxParts       = 1 << 16
	MaxMessage     = 64 << 20
	BindSize       = 8
)

var (
	ErrRecord     = errors.New("tunnel: bad record header")
	ErrOrder      = errors.New("tunnel: record out of order or from another message")
	ErrIncomplete = errors.New("tunnel: message has no last part")
	ErrTooLarge   = errors.New("tunnel: message too large")
	ErrBind       = errors.New("tunnel: answer not bound to this request")
)

type Header struct {
	Type  Type
	Flags uint8
	Msg   uint32
	Part  uint16
}

func (h Header) Last() bool { return h.Flags&FlagLast != 0 }

func (h Header) append(b []byte) []byte {
	b = append(b, byte(h.Type), h.Flags)
	b = binary.BigEndian.AppendUint32(b, h.Msg)
	return binary.BigEndian.AppendUint16(b, h.Part)
}

func parseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrRecord
	}
	h := Header{Type: Type(b[0]), Flags: b[1], Msg: binary.BigEndian.Uint32(b[2:]), Part: binary.BigEndian.Uint16(b[6:])}
	if h.Type < TypeRequest || h.Type > TypeEnd || h.Flags&^(FlagLast|FlagDeflate|dictMask) != 0 {
		return Header{}, ErrRecord
	}
	return h, nil
}

type Record struct {
	Header
	Payload []byte
}

type Message struct {
	Type  Type
	Flags uint8
	Msg   uint32
	Re    uint32
	Seq   uint32
	Plain []byte
}

func (m Message) Answers(re, seq uint32) error {
	if m.Type == TypeRequest || m.Re != re || m.Seq != seq {
		return ErrBind
	}
	return nil
}

func SplitMessage(t Type, flags uint8, msg uint32, plain []byte) ([]Record, error) {
	if len(plain) > MaxMessage+BindSize {
		return nil, ErrTooLarge
	}
	n := max(1, (len(plain)+MaxPayload-1)/MaxPayload)
	if n > MaxParts {
		return nil, ErrTooLarge
	}
	flags &^= FlagLast
	recs := make([]Record, n)
	for i := range n {
		end := min(len(plain), (i+1)*MaxPayload)
		recs[i] = Record{Header: Header{Type: t, Flags: flags, Msg: msg, Part: uint16(i)}, Payload: plain[i*MaxPayload : end]}
	}
	recs[n-1].Flags |= FlagLast
	return recs, nil
}

type Assembler struct {
	first Header
	next  int
	buf   []byte
	done  bool
}

func (a *Assembler) Add(r Record) (bool, error) {
	if a.done {
		return false, ErrOrder
	}
	if a.next >= MaxParts {
		return false, ErrTooLarge
	}
	if a.next == 0 {
		if r.Part != 0 {
			return false, ErrOrder
		}
		a.first = r.Header
		a.buf = make([]byte, 0, len(r.Payload))
	} else if r.Part != uint16(a.next) || r.Type != a.first.Type || r.Msg != a.first.Msg || (r.Flags^a.first.Flags)&^FlagLast != 0 {
		return false, ErrOrder
	}
	if len(a.buf)+len(r.Payload) > MaxMessage+BindSize {
		return false, ErrTooLarge
	}
	a.buf = append(a.buf, r.Payload...)
	a.next++
	a.done = r.Last()
	return a.done, nil
}

func (a *Assembler) Message() (Message, error) {
	if !a.done {
		return Message{}, ErrIncomplete
	}
	m := Message{Type: a.first.Type, Flags: a.first.Flags &^ FlagLast, Msg: a.first.Msg, Plain: a.buf}
	if m.Type == TypeRequest {
		return m, nil
	}
	if len(a.buf) < BindSize {
		return Message{}, ErrBind
	}
	m.Re, m.Seq, m.Plain = binary.BigEndian.Uint32(a.buf), binary.BigEndian.Uint32(a.buf[4:]), a.buf[BindSize:]
	return m, nil
}

func (c *Channel) SealRequest(flags uint8, plain []byte) ([]byte, uint32, error) {
	var b bytes.Buffer
	msg := c.NextMsg()
	if err := c.writeMessage(&b, TypeRequest, flags, msg, plain); err != nil {
		return nil, 0, err
	}
	return b.Bytes(), msg, nil
}

func (c *Channel) SealAnswer(t Type, flags uint8, re, seq uint32, plain []byte) ([]byte, error) {
	if t == TypeRequest {
		return nil, ErrBind
	}
	bound := make([]byte, BindSize, BindSize+len(plain))
	binary.BigEndian.PutUint32(bound, re)
	binary.BigEndian.PutUint32(bound[4:], seq)
	var b bytes.Buffer
	if err := c.writeMessage(&b, t, flags, c.NextMsg(), append(bound, plain...)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (c *Channel) writeMessage(w io.Writer, t Type, flags uint8, msg uint32, plain []byte) error {
	recs, err := SplitMessage(t, flags, msg, plain)
	if err != nil {
		return err
	}
	var buf []byte
	for _, r := range recs {
		sealed, err := c.Seal(r.Header, r.Payload)
		if err != nil {
			return err
		}
		buf = AppendRecord(buf[:0], sealed)
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	return nil
}

func (c *Channel) ReadMessage(r io.Reader) (Message, error) {
	rr := NewRecordReader(r)
	var a Assembler
	for read := false; ; read = true {
		rec, err := rr.Next()
		if errors.Is(err, io.EOF) {
			if !read {
				return Message{}, io.EOF
			}
			return Message{}, ErrIncomplete
		}
		if err != nil {
			return Message{}, err
		}
		h, payload, err := c.Open(rec)
		if err != nil {
			return Message{}, err
		}
		done, err := a.Add(Record{Header: h, Payload: payload})
		if err != nil {
			return Message{}, err
		}
		if done {
			return a.Message()
		}
	}
}
