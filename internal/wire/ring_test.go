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
