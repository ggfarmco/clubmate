package bus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/bus"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, topic string
		want           bool
	}{
		{"session.created", "session.created", true},
		{"session.*", "session.created", true},
		{"session.*", "session.x.y", true},
		{"session.*", "sessions.created", false},
		{"*", "anything.here", true},
		{"session", "session.created", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, bus.Match(tt.pattern, tt.topic), tt.pattern+" "+tt.topic)
	}
}

func TestValidPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"*", true},
		{"a.*", true},
		{"a.b", true},
		{"*.a", false},
		{"a.*.b", false},
		{"A", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, bus.ValidPattern(tt.pattern), tt.pattern)
	}
}
