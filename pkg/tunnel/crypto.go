package tunnel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
)

const (
	Version     = 1
	KeySize     = 32
	CounterSize = 8
	TagSize     = 16
	MaxRecords  = 1 << 32
	WindowSize  = 1 << 16

	protocolName = "clubmate tunnel v1"
	infoK1       = "clubmate tunnel v1 k1"
	infoKeys     = "clubmate tunnel v1 keys"
	nonceSize    = 12
)

var (
	ErrHandshake = errors.New("tunnel: handshake failed")
	ErrOpen      = errors.New("tunnel: record does not open")
	ErrReplay    = errors.New("tunnel: record repeated or below the window")
	ErrExhausted = errors.New("tunnel: channel used all its records")
)

type Static struct {
	key *ecdh.PrivateKey
}

func NewStatic() (*Static, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Static{key: k}, nil
}

func LoadStatic(b []byte) (*Static, error) {
	if len(b) != KeySize {
		return nil, fmt.Errorf("tunnel: static key is %d bytes, want %d", len(b), KeySize)
	}
	k, err := ecdh.X25519().NewPrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("tunnel: static key: %w", err)
	}
	return &Static{key: k}, nil
}

func (s *Static) Bytes() []byte { return s.key.Bytes() }

func (s *Static) Public() []byte { return s.key.PublicKey().Bytes() }

func (s *Static) KeyID() string { return KeyID(s.Public()) }

func KeyID(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

func EncodeKey(pub []byte) string { return base64.RawURLEncoding.EncodeToString(pub) }

func DecodeKey(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("tunnel: key: %w", err)
	}
	if len(b) != KeySize {
		return nil, fmt.Errorf("tunnel: key is %d bytes, want %d", len(b), KeySize)
	}
	return b, nil
}

type hello struct {
	Dicts []int  `json:"dicts"`
	KID   string `json:"kid"`
	TH    string `json:"th,omitempty"`
}

type accept struct {
	Dict int `json:"dict"`
}

type Initiator struct {
	macPub []byte
	eph    *ecdh.PrivateKey
	dicts  []int
	es     []byte
	h0     []byte
	h1     []byte
	k1     []byte
}

func Initiate(macPub []byte, eph *ecdh.PrivateKey, dicts []int, ticketHash string) (*Initiator, []byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(macPub)
	if err != nil {
		return nil, nil, fmt.Errorf("tunnel: mac key: %w", err)
	}
	if eph == nil {
		if eph, err = ecdh.X25519().GenerateKey(rand.Reader); err != nil {
			return nil, nil, err
		}
	}
	es, err := eph.ECDH(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	i := &Initiator{macPub: slices.Clone(macPub), eph: eph, dicts: slices.Clone(dicts), es: es}
	i.h0 = hash(protocolName, macPub)
	i.h1 = hash(i.h0, eph.PublicKey().Bytes())
	if i.k1, err = hkdf.Key(sha256.New, es, i.h0, infoK1, KeySize); err != nil {
		return nil, nil, err
	}
	payload, err := json.Marshal(hello{Dicts: nonNil(dicts), KID: KeyID(macPub), TH: ticketHash})
	if err != nil {
		return nil, nil, err
	}
	msg1 := append([]byte{Version}, eph.PublicKey().Bytes()...)
	msg1, err = sealOnce(msg1, i.k1, i.h1, payload)
	if err != nil {
		return nil, nil, err
	}
	return i, msg1, nil
}

func (i *Initiator) Finish(ch string, msg2 []byte) (*Channel, error) {
	if len(msg2) < KeySize+TagSize {
		return nil, ErrHandshake
	}
	mPub, err := ecdh.X25519().NewPublicKey(msg2[:KeySize])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	ee, err := i.eph.ECDH(mPub)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	ks, err := deriveKeys(i.es, ee, i.h1, msg2[:KeySize])
	if err != nil {
		return nil, err
	}
	plain, err := openOnce(ks.k2, ks.h2, msg2[KeySize:])
	if err != nil {
		return nil, ErrHandshake
	}
	var a accept
	if err := json.Unmarshal(plain, &a); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	if a.Dict != DictNone && !slices.Contains(i.dicts, a.Dict) {
		return nil, fmt.Errorf("%w: dictionary %d was not offered", ErrHandshake, a.Dict)
	}
	return newChannel(ch, a.Dict, KeyID(i.macPub), ks, ks.kdm, ks.kmd)
}

func Respond(ch string, msg1 []byte, keys []*Static, eph *ecdh.PrivateKey) (*Channel, []byte, error) {
	if len(msg1) < 1+KeySize+TagSize || msg1[0] != Version {
		return nil, nil, ErrHandshake
	}
	dPubBytes := msg1[1 : 1+KeySize]
	dPub, err := ecdh.X25519().NewPublicKey(dPubBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	for _, s := range keys {
		es, err := s.key.ECDH(dPub)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrHandshake, err)
		}
		h0 := hash(protocolName, s.Public())
		h1 := hash(h0, dPubBytes)
		k1, err := hkdf.Key(sha256.New, es, h0, infoK1, KeySize)
		if err != nil {
			return nil, nil, err
		}
		plain, err := openOnce(k1, h1, msg1[1+KeySize:])
		if err != nil {
			continue
		}
		var h hello
		if err := json.Unmarshal(plain, &h); err != nil || h.KID != s.KeyID() {
			return nil, nil, ErrHandshake
		}
		return respond(ch, s, es, h1, dPub, h.Dicts, h.TH, eph)
	}
	return nil, nil, ErrHandshake
}

func respond(ch string, s *Static, es, h1 []byte, dPub *ecdh.PublicKey, offered []int, ticketHash string, eph *ecdh.PrivateKey) (*Channel, []byte, error) {
	var err error
	if eph == nil {
		if eph, err = ecdh.X25519().GenerateKey(rand.Reader); err != nil {
			return nil, nil, err
		}
	}
	ee, err := eph.ECDH(dPub)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	mPub := eph.PublicKey().Bytes()
	ks, err := deriveKeys(es, ee, h1, mPub)
	if err != nil {
		return nil, nil, err
	}
	a := accept{Dict: DictNone}
	if slices.Contains(offered, DictV1) {
		a.Dict = DictV1
	}
	payload, err := json.Marshal(a)
	if err != nil {
		return nil, nil, err
	}
	msg2, err := sealOnce(slices.Clone(mPub), ks.k2, ks.h2, payload)
	if err != nil {
		return nil, nil, err
	}
	c, err := newChannel(ch, a.Dict, s.KeyID(), ks, ks.kmd, ks.kdm)
	if err != nil {
		return nil, nil, err
	}
	c.TicketHash = ticketHash
	return c, msg2, nil
}

type keys struct {
	ee  []byte
	h2  []byte
	kdm []byte
	kmd []byte
	k2  []byte
}

func deriveKeys(es, ee, h1, mPub []byte) (keys, error) {
	h2 := hash(h1, mPub)
	okm, err := hkdf.Key(sha256.New, slices.Concat(es, ee), h1, infoKeys+string(h2), 3*KeySize)
	if err != nil {
		return keys{}, err
	}
	return keys{ee: ee, h2: h2, kdm: okm[:KeySize], kmd: okm[KeySize : 2*KeySize], k2: okm[2*KeySize:]}, nil
}

func hash[T string | []byte](a T, b []byte) []byte {
	h := sha256.New()
	h.Write([]byte(a))
	h.Write(b)
	return h.Sum(nil)
}

func nonNil(d []int) []int {
	if d == nil {
		return []int{}
	}
	return d
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func sealOnce(dst, key, aad, plain []byte) ([]byte, error) {
	a, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return a.Seal(dst, make([]byte, nonceSize), plain, aad), nil
}

func openOnce(key, aad, ct []byte) ([]byte, error) {
	a, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return a.Open(nil, make([]byte, nonceSize), ct, aad)
}

type Channel struct {
	ID         string
	Dict       int
	KeyID      string
	TicketHash string

	keys keys
	aad  []byte
	send cipher.AEAD
	recv cipher.AEAD

	sendMu  sync.Mutex
	counter uint64
	msg     uint32

	recvMu sync.Mutex
	window window
}

func newChannel(ch string, dict int, kid string, ks keys, sendKey, recvKey []byte) (*Channel, error) {
	send, err := newAEAD(sendKey)
	if err != nil {
		return nil, err
	}
	recv, err := newAEAD(recvKey)
	if err != nil {
		return nil, err
	}
	return &Channel{ID: ch, Dict: dict, KeyID: kid, keys: ks, aad: []byte(ch), send: send, recv: recv}, nil
}

func (c *Channel) NextMsg() uint32 {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.msg++
	return c.msg
}

func (c *Channel) Seal(h Header, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, ErrTooLarge
	}
	c.sendMu.Lock()
	if c.counter >= MaxRecords {
		c.sendMu.Unlock()
		return nil, ErrExhausted
	}
	c.counter++
	n := c.counter
	c.sendMu.Unlock()
	out := make([]byte, CounterSize, CounterSize+HeaderSize+len(payload)+TagSize)
	binary.BigEndian.PutUint64(out, n)
	plain := h.append(make([]byte, 0, HeaderSize+len(payload)))
	plain = append(plain, payload...)
	return c.send.Seal(out, nonce(n), plain, c.aad), nil
}

func (c *Channel) Open(rec []byte) (Header, []byte, error) {
	if len(rec) < CounterSize+HeaderSize+TagSize {
		return Header{}, nil, ErrOpen
	}
	n := binary.BigEndian.Uint64(rec)
	c.recvMu.Lock()
	seen := c.window.seen(n)
	c.recvMu.Unlock()
	if seen {
		return Header{}, nil, ErrReplay
	}
	plain, err := c.recv.Open(nil, nonce(n), rec[CounterSize:], c.aad)
	if err != nil {
		return Header{}, nil, ErrOpen
	}
	c.recvMu.Lock()
	fresh := c.window.mark(n)
	c.recvMu.Unlock()
	if !fresh {
		return Header{}, nil, ErrReplay
	}
	h, err := parseHeader(plain)
	if err != nil {
		return Header{}, nil, err
	}
	return h, plain[HeaderSize:], nil
}

func nonce(n uint64) []byte {
	b := make([]byte, nonceSize)
	binary.BigEndian.PutUint64(b[4:], n)
	return b
}

type window struct {
	top  uint64
	bits [WindowSize / 64]uint64
}

func (w *window) seen(n uint64) bool {
	if n == 0 || n > MaxRecords {
		return true
	}
	if n > w.top {
		return false
	}
	if w.top-n >= WindowSize {
		return true
	}
	return w.bits[(n/64)%uint64(len(w.bits))]&(1<<(n%64)) != 0
}

func (w *window) mark(n uint64) bool {
	if w.seen(n) {
		return false
	}
	if n > w.top {
		if n-w.top >= WindowSize {
			w.bits = [WindowSize / 64]uint64{}
		} else {
			for i := w.top + 1; i < n; i++ {
				w.bits[(i/64)%uint64(len(w.bits))] &^= 1 << (i % 64)
			}
		}
		w.top = n
		w.bits[(n/64)%uint64(len(w.bits))] &^= 1 << (n % 64)
	}
	w.bits[(n/64)%uint64(len(w.bits))] |= 1 << (n % 64)
	return true
}
