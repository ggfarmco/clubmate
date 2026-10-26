package wire_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/wire"
)

func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		frame wire.Frame
	}{
		{"data", wire.Frame{Type: wire.FrameData, Payload: []byte("hi")}},
		{"resize", wire.Frame{Type: wire.FrameResize, Payload: wire.ResizePayload(120, 40)}},
		{"exit", wire.Frame{Type: wire.FrameExit, Payload: wire.ExitPayload(-1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, wire.WriteFrame(&buf, tt.frame))
			got, err := wire.ReadFrame(&buf)
			require.NoError(t, err)
			assert.Equal(t, tt.frame, got)
		})
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	hdr := []byte{byte(wire.FrameData), 0x00, 0x20, 0x00, 0x00}
	_, err := wire.ReadFrame(bytes.NewReader(hdr))
	assert.ErrorIs(t, err, wire.ErrFrameTooLarge)
}

func TestParseHelpers(t *testing.T) {
	cols, rows, err := wire.ParseResize(wire.ResizePayload(120, 40))
	require.NoError(t, err)
	assert.Equal(t, [2]uint16{120, 40}, [2]uint16{cols, rows})
	code, err := wire.ParseExit(wire.ExitPayload(7))
	require.NoError(t, err)
	assert.Equal(t, 7, code)
}
