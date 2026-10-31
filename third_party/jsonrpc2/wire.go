package jsonrpc2

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
)

const (
	readBufferSize = 32 << 10
	keepBufferSize = 1 << 20
)

var errEmptyRaw = errors.New("jsonrpc2: empty raw JSON value")

type plainObjectStream struct {
	conn    io.ReadWriteCloser
	r       *bufio.Reader
	encoder *json.Encoder
	wbuf    []byte
	rbuf    []byte
}

func NewPlainObjectStream(conn io.ReadWriteCloser) ObjectStream {
	return &plainObjectStream{
		conn:    conn,
		r:       bufio.NewReaderSize(conn, readBufferSize),
		encoder: json.NewEncoder(conn),
	}
}

func (s *plainObjectStream) ReadObject(v interface{}) error {
	raw, err := readValue(s.r, s.rbuf[:0])
	s.rbuf = keep(raw)
	if err != nil {
		return err
	}
	if m, ok := v.(*anyMessage); ok {
		return m.UnmarshalJSON(raw)
	}
	return json.Unmarshal(raw, v)
}

func (s *plainObjectStream) WriteObject(v interface{}) error {
	m, ok := v.(*anyMessage)
	if !ok {
		return s.encoder.Encode(v)
	}
	b, err := m.appendJSON(s.wbuf[:0])
	if err != nil {
		return err
	}
	b = append(b, '\n')
	s.wbuf = keep(b)
	_, err = s.conn.Write(b)
	return err
}

func (s *plainObjectStream) Close() error {
	return s.conn.Close()
}

func keep(b []byte) []byte {
	if cap(b) > keepBufferSize {
		return nil
	}
	return b
}

func readValue(r *bufio.Reader, dst []byte) ([]byte, error) {
	if err := skipSpace(r); err != nil {
		return dst, err
	}
	first, _ := r.Peek(1)
	if first[0] != '{' && first[0] != '[' {
		return dst, fmt.Errorf("jsonrpc2: invalid character %q looking for beginning of value", first[0])
	}
	var sc scanner
	for {
		if r.Buffered() == 0 {
			if _, err := r.Peek(1); err != nil {
				if errors.Is(err, io.EOF) {
					err = io.ErrUnexpectedEOF
				}
				return dst, err
			}
		}
		chunk, _ := r.Peek(r.Buffered())
		n, done := sc.scan(chunk)
		dst = append(dst, chunk[:n]...)
		_, _ = r.Discard(n)
		if done {
			return dst, nil
		}
	}
}

func skipSpace(r *bufio.Reader) error {
	for {
		c, err := r.ReadByte()
		if err != nil {
			return err
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return r.UnreadByte()
	}
}

type scanner struct {
	depth    int
	inString bool
	escaped  bool
}

func (s *scanner) scan(chunk []byte) (int, bool) {
	for i, c := range chunk {
		if s.inString {
			switch {
			case s.escaped:
				s.escaped = false
			case c == '\\':
				s.escaped = true
			case c == '"':
				s.inString = false
			}
			continue
		}
		switch c {
		case '"':
			s.inString = true
		case '{', '[':
			s.depth++
		case '}', ']':
			s.depth--
			if s.depth == 0 {
				return i + 1, true
			}
		}
	}
	return len(chunk), false
}

func marshalValue(v interface{}) ([]byte, error) {
	switch v := v.(type) {
	case json.RawMessage:
		if len(v) == 0 {
			return slices.Clone(jsonNull), nil
		}
		return v, nil
	case *json.RawMessage:
		if v == nil || len(*v) == 0 {
			return slices.Clone(jsonNull), nil
		}
		return *v, nil
	}
	return json.Marshal(v)
}

func appendRaw(dst, raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return dst, errEmptyRaw
	}
	if bytes.IndexByte(raw, '\n') < 0 {
		return append(dst, raw...), nil
	}
	buf := bytes.NewBuffer(dst)
	if err := json.Compact(buf, raw); err != nil {
		return dst, err
	}
	return buf.Bytes(), nil
}

func appendField(dst []byte, name string, raw *json.RawMessage) ([]byte, error) {
	if raw == nil {
		return dst, nil
	}
	dst = append(dst, ',', '"')
	dst = append(dst, name...)
	dst = append(dst, '"', ':')
	return appendRaw(dst, *raw)
}

func (id ID) appendJSON(dst []byte) []byte {
	if id.IsString {
		dst, _ = jsontext.AppendQuote(dst, id.Str)
		return dst
	}
	return strconv.AppendUint(dst, id.Num, 10)
}

func rawLen(raw *json.RawMessage) int {
	if raw == nil {
		return 0
	}
	return len(*raw)
}

func eachField(data []byte, fn func(name string, value jsontext.Value) error) error {
	dec := jsontext.NewDecoder(bytes.NewBuffer(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '{' {
		return fmt.Errorf("jsonrpc2: expected object, got %s", tok.Kind())
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		name := tok.String()
		value, err := dec.ReadValue()
		if err != nil {
			return err
		}
		if err := fn(name, value); err != nil {
			return err
		}
	}
	_, err = dec.ReadToken()
	return err
}

func cloneRaw(v jsontext.Value) *json.RawMessage {
	if v.Kind() == 'n' {
		return &jsonNull
	}
	raw := json.RawMessage(slices.Clone(v))
	return &raw
}

func unquote(v jsontext.Value) (string, error) {
	var s string
	err := json.Unmarshal(v, &s)
	return s, err
}
