package sessionhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAltScreenFeed(t *testing.T) {
	var a altScreen
	a.feed([]byte("hi\x1b[?10"))
	assert.False(t, a.on)
	a.feed([]byte("49hframe\x1b[?25l"))
	assert.True(t, a.on)
	a.feed([]byte("\x1b[?1047l\x1b[?47h\x1b"))
	assert.True(t, a.on)
	a.feed([]byte("[?1049l"))
	assert.False(t, a.on)
	a.feed([]byte("\x1b[?10\x1b[?1049h"))
	assert.True(t, a.on)
	a.feed([]byte("\x1bc"))
	assert.False(t, a.on)
}
