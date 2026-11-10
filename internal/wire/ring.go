package wire

import "sync"

type Ring struct {
	mu   sync.Mutex
	size int
	buf  []byte
	head int
}

func NewRing(size int) *Ring {
	return &Ring{size: max(size, 0)}
}

func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if r.size == 0 {
		return n, nil
	}
	if r.buf == nil {
		r.buf = make([]byte, 0, r.size)
	}
	if len(p) >= r.size {
		r.buf = append(r.buf[:0], p[len(p)-r.size:]...)
		r.head = 0
		return n, nil
	}
	if fill := min(r.size-len(r.buf), len(p)); fill > 0 {
		r.buf = append(r.buf, p[:fill]...)
		p = p[fill:]
	}
	if len(p) == 0 {
		return n, nil
	}
	copied := copy(r.buf[r.head:], p)
	copy(r.buf, p[copied:])
	r.head = (r.head + len(p)) % r.size
	return n, nil
}

func (r *Ring) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.head:]...)
	return append(out, r.buf[:r.head]...)
}
