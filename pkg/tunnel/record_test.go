package tunnel

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMessageRoundTrip(t *testing.T) {
	dev, mac := pair(t, "ch_a", nil, newStatic(t), nil)
	for i, size := range []int{0, 2*MaxPayload + 10} {
		plain := bytes.Repeat([]byte{byte(i + 1)}, size)
		wire, msg, err := dev.SealRequest(FlagDeflate, plain)
		require.NoError(t, err)
		m, err := mac.ReadMessage(bytes.NewReader(wire))
		require.NoError(t, err)
		require.Equal(t, Message{Type: TypeRequest, Flags: FlagDeflate, Msg: msg, Plain: plain}, m)
	}
}

func TestAnswerIsBoundToItsRequest(t *testing.T) {
	dev, mac := pair(t, "ch_a", nil, newStatic(t), nil)
	_, re, err := dev.SealRequest(0, []byte("request"))
	require.NoError(t, err)
	wire, err := mac.SealAnswer(TypeChunk, 0, re, 2, []byte("chunk"))
	require.NoError(t, err)

	m, err := dev.ReadMessage(bytes.NewReader(wire))
	require.NoError(t, err)

	require.Equal(t, []byte("chunk"), m.Plain)
	require.NoError(t, m.Answers(re, 2))
	require.ErrorIs(t, m.Answers(re+1, 2), ErrBind)
	require.ErrorIs(t, m.Answers(re, 1), ErrBind)
	_, err = mac.SealAnswer(TypeRequest, 0, re, 0, nil)
	require.ErrorIs(t, err, ErrBind)
}

func TestAnswerWithoutBindingIsRefused(t *testing.T) {
	dev, mac := pair(t, "ch_a", nil, newStatic(t), nil)
	rec, err := mac.Seal(Header{Type: TypeReply, Flags: FlagLast, Msg: 1}, []byte("short"))
	require.NoError(t, err)

	_, err = dev.ReadMessage(bytes.NewReader(AppendRecord(nil, rec)))

	require.ErrorIs(t, err, ErrBind)
}

func TestReadMessageRefusesBrokenMessages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(a, b [][]byte) [][]byte
		error error
	}{
		{"part order", func(a, _ [][]byte) [][]byte { return [][]byte{a[1], a[0], a[2]} }, ErrOrder},
		{"missing last part", func(a, _ [][]byte) [][]byte { return a[:2] }, ErrIncomplete},
		{"part of another message", func(a, b [][]byte) [][]byte { return [][]byte{a[0], b[1], a[2]} }, ErrOrder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev, mac := pair(t, "ch_a", nil, newStatic(t), nil)
			a := sealedRecords(t, dev)
			b := sealedRecords(t, dev)
			var wire []byte
			for _, r := range tc.edit(a, b) {
				wire = AppendRecord(wire, r)
			}
			_, err := mac.ReadMessage(bytes.NewReader(wire))
			require.ErrorIs(t, err, tc.error)
		})
	}
}

func TestReadMessageCleanEndIsEOF(t *testing.T) {
	_, mac := pair(t, "ch_a", nil, newStatic(t), nil)
	_, err := mac.ReadMessage(bytes.NewReader(nil))
	require.ErrorIs(t, err, io.EOF)
}

func TestAssemblerRefusesPartsPastMax(t *testing.T) {
	var a Assembler
	for i := range MaxParts {
		_, err := a.Add(Record{Header: Header{Type: TypeChunk, Part: uint16(i)}})
		require.NoError(t, err)
	}
	_, err := a.Add(Record{Header: Header{Type: TypeChunk, Part: 0}})
	require.ErrorIs(t, err, ErrTooLarge)
}

func sealedRecords(t *testing.T, c *Channel) [][]byte {
	t.Helper()
	wire, _, err := c.SealRequest(0, make([]byte, 2*MaxPayload+1))
	require.NoError(t, err)
	rr := NewRecordReader(bytes.NewReader(wire))
	var recs [][]byte
	for range 3 {
		r, err := rr.Next()
		require.NoError(t, err)
		recs = append(recs, r)
	}
	return recs
}
