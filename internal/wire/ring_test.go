package wire_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/wire"
)

func TestRingKeepsTail(t *testing.T) {
	r := wire.NewRing(8)
	_, _ = r.Write([]byte("hello"))
	_, _ = r.Write([]byte("world!"))
	assert.Equal(t, "loworld!", string(r.Bytes()))
}

func TestRingWrapsLikeATail(t *testing.T) {
	r := wire.NewRing(7)
	var all []byte
	for i := range 40 {
		chunk := bytes.Repeat([]byte{byte('a' + i%26)}, i%9+1)
		_, _ = r.Write(chunk)
		all = append(all, chunk...)
		assert.Equal(t, string(all[max(0, len(all)-7):]), string(r.Bytes()), i)
	}
}

func TestRingReplayStartsAtALineOnceOverflowed(t *testing.T) {
	for _, tc := range []struct {
		size     int
		in, want string
	}{
		{16, "one\ntwo\nthree", "one\ntwo\nthree"},
		{8, "one\ntwo\nthree", "three"},
		{4, "\x1b_Gbase64", ""},
	} {
		r := wire.NewRing(tc.size)
		_, _ = r.Write([]byte(tc.in))
		assert.Equal(t, tc.want, string(r.Replay()), tc.in)
	}
}
