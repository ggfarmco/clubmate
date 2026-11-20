package tunnel

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPushBatchRoundTrip(t *testing.T) {
	items := []PushItem{
		{Kind: PushData, ID: [IDSize]byte{1}, Data: []byte("sealed records")},
		{Kind: PushEnd, ID: [IDSize]byte{2}, Data: []byte{}},
	}
	var batch []byte
	for _, it := range items {
		batch = AppendPush(batch, it)
	}
	pr := NewPushReader(bytes.NewReader(batch))
	for _, want := range items {
		got, err := pr.Next()
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := pr.Next()
	require.ErrorIs(t, err, io.EOF)

	_, err = NewPushReader(bytes.NewReader(batch[:10])).Next()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestRecordReaderRefusesBadLengths(t *testing.T) {
	for _, size := range []int{minRecordWire - 1, MaxRecordWire + 1} {
		_, err := NewRecordReader(bytes.NewReader(AppendRecord(nil, make([]byte, size)))).Next()
		require.ErrorIs(t, err, ErrFrame)
	}
}

func TestSSEDataRoundTrip(t *testing.T) {
	frame := []byte{0, 1, 2, 0xff, '\n'}
	line := SSEData(frame)
	require.Equal(t, "data: AAEC/wo=\n\n", line)
	got, err := ParseSSEData(line)
	require.NoError(t, err)
	require.Equal(t, frame, got)
}
