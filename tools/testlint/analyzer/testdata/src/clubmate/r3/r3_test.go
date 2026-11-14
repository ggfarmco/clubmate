package r3

import (
	"context"

	"clubmate/internal/bus"
)

func helper(b *bus.Bus) {
	b.Read(context.Background(), "t", 0, 10) // want "read-once"
	_ = func() { b.Read(context.Background(), "t", 0, 10) }
}
