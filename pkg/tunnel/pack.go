package tunnel

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	DictNone = 0
	DictV1   = 1

	DictV1SHA256 = "c6d0cd4f01096508ea5cc43fd4d712a96b8410fcc5e0f1121e97eeff4c60d4a0"

	MinDeflate   = 128
	deflateLevel = 6
	lengthSize   = 4
	smallBucket  = 64
	smallLimit   = 1024
)

var ErrPack = errors.New("tunnel: bad message plaintext")

//go:embed dict-v1.bin
var dictV1 []byte

func Dictionary(id int) ([]byte, error) {
	switch id {
	case DictNone:
		return nil, nil
	case DictV1:
		return dictV1, nil
	}
	return nil, fmt.Errorf("tunnel: unknown dictionary %d", id)
}

type RequestHead struct {
	Method string            `json:"m"`
	Path   string            `json:"p"`
	Header map[string]string `json:"h,omitempty"`
	Z      *int              `json:"z,omitempty"`
}

func (h RequestHead) Deflate() bool { return h.Z == nil || *h.Z != 0 }

type ReplyHead struct {
	Status int               `json:"s"`
	Header map[string]string `json:"h,omitempty"`
}

func Bucket(n int) int {
	if n <= smallLimit {
		return max(smallBucket, (n+smallBucket-1)/smallBucket*smallBucket)
	}
	b := smallLimit
	for b < n {
		b += b / 20
	}
	return b
}

func Pack(head, body []byte, dict int, deflate bool) ([]byte, uint8, error) {
	content := body
	if head != nil {
		if bytes.IndexByte(head, '\n') >= 0 {
			return nil, 0, fmt.Errorf("%w: head has a newline", ErrPack)
		}
		content = make([]byte, 0, len(head)+1+len(body))
		content = append(append(append(content, head...), '\n'), body...)
	}
	var flags uint8
	if deflate && len(content) >= MinDeflate {
		z, err := compress(content, dict)
		if err != nil {
			return nil, 0, err
		}
		if len(z) < len(content) {
			content = z
			flags = FlagDeflate | uint8(dict)<<dictShift
		}
	}
	size := Bucket(lengthSize + len(content))
	if size > MaxMessage {
		return nil, 0, ErrTooLarge
	}
	out := make([]byte, size)
	binary.BigEndian.PutUint32(out, uint32(len(content)))
	copy(out[lengthSize:], content)
	return out, flags, nil
}

func PackRequest(h RequestHead, body []byte, dict int) ([]byte, uint8, error) {
	head, err := json.Marshal(h)
	if err != nil {
		return nil, 0, err
	}
	return Pack(head, body, dict, h.Deflate())
}

func PackReply(h ReplyHead, body []byte, dict int, deflate bool) ([]byte, uint8, error) {
	head, err := json.Marshal(h)
	if err != nil {
		return nil, 0, err
	}
	return Pack(head, body, dict, deflate)
}

func Unpack(m Message, agreed, limit int) (head, body []byte, err error) {
	if len(m.Plain) < lengthSize {
		return nil, nil, ErrPack
	}
	n := binary.BigEndian.Uint32(m.Plain)
	if uint64(n) > uint64(len(m.Plain)-lengthSize) {
		return nil, nil, ErrPack
	}
	content := m.Plain[lengthSize : lengthSize+int(n)]
	if m.Flags&FlagDeflate != 0 {
		dict := int(m.Flags&dictMask) >> dictShift
		if dict != DictNone && dict != agreed {
			return nil, nil, fmt.Errorf("%w: dictionary %d was not agreed", ErrPack, dict)
		}
		if content, err = inflate(content, dict, min(limit, MaxMessage)); err != nil {
			return nil, nil, err
		}
	} else if m.Flags&dictMask != 0 {
		return nil, nil, fmt.Errorf("%w: dictionary without deflate", ErrPack)
	}
	if m.Type != TypeRequest && m.Type != TypeReply {
		return nil, content, nil
	}
	i := bytes.IndexByte(content, '\n')
	if i < 0 {
		return nil, nil, fmt.Errorf("%w: no head", ErrPack)
	}
	return content[:i], content[i+1:], nil
}

func compress(b []byte, dict int) ([]byte, error) {
	d, err := Dictionary(dict)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w, err := flate.NewWriterDict(&buf, deflateLevel, d)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func inflate(b []byte, dict, limit int) ([]byte, error) {
	d, err := Dictionary(dict)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPack, err)
	}
	r := flate.NewReaderDict(bytes.NewReader(b), d)
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPack, err)
	}
	if len(out) > limit {
		return nil, ErrTooLarge
	}
	return out, nil
}
