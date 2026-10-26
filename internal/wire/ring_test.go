package wire_test

import (
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
