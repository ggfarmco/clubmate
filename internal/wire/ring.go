package wire

import (
	"slices"
	"sync"
)

type Ring struct {
	mu   sync.Mutex
	size int
	buf  []byte
}

func NewRing(size int) *Ring {
	return &Ring{size: size}
}

func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= r.size {
		r.buf = append(r.buf[:0], p[len(p)-r.size:]...)
		return len(p), nil
	}
	r.buf = append(r.buf, p...)
	if over := len(r.buf) - r.size; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	return len(p), nil
}

func (r *Ring) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.buf)
}
