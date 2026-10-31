package sessionhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTermStateTracksAltScreen(t *testing.T) {
	var s termState
	s.feed([]byte("hi\x1b[?10"), false)
	assert.False(t, s.alt)
	s.feed([]byte("49hframe\x1b[?25l"), false)
	assert.True(t, s.alt)
	s.feed([]byte("\x1b[?1047l\x1b[?47h\x1b"), false)
	assert.True(t, s.alt)
	s.feed([]byte("[?1049l"), false)
	assert.False(t, s.alt)
	s.feed([]byte("\x1b[?10\x1b[?1049h"), false)
	assert.True(t, s.alt)
	s.feed([]byte("\x1bc"), false)
	assert.False(t, s.alt)
}

func TestTermStatePreludeRestoresModes(t *testing.T) {
	var s termState
	s.feed([]byte("\x1b[?1049h\x1b[?1;2004h\x1b[?1002h\x1b[?1006h\x1b[?2048h\x1b[?1002l\x1b[>1u\x1b[>3u\x1b[=4;2u\x1b[>5u\x1b[<u"), false)
	assert.Equal(t, "\x1b[?1049h\x1b[2J\x1b[H\x1b[?1h\x1b[?1006h\x1b[?2004h\x1b[>7u", string(s.prelude()))
	assert.True(t, s.resizeEvents())
	s.feed([]byte("\x1bc"), false)
	assert.Equal(t, string(altScreenReset), string(s.prelude()))
	assert.False(t, s.resizeEvents())
}

func TestTermStateKeepsQueriesSentWhileDetachedInAltScreen(t *testing.T) {
	var s termState
	s.feed([]byte("\x1b[c"), true)
	s.feed([]byte("\x1b[?1049h\x1b[?2026$p\x1b[?u"), true)
	s.feed([]byte("\x1b[?2048$p"), false)
	s.feed([]byte("\x1b[c\x1b[>c\x1b[5n"), true)
	assert.Equal(t, "\x1b[?2026$p\x1b[?u\x1b[c", string(s.takeQueries()))
	assert.Empty(t, s.takeQueries())
	s.feed([]byte("\x1b[?u\x1b[?1049l"), true)
	assert.Empty(t, s.takeQueries())
}
