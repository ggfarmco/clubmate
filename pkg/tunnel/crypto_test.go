package tunnel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func pair(t *testing.T, ch string, dicts []int, mac *Static, used *Static) (dev, srv *Channel) {
	t.Helper()
	keys := []*Static{mac}
	if used == nil {
		used = mac
	} else if used != mac {
		keys = append(keys, used)
	}
	ini, msg1, err := Initiate(used.Public(), nil, dicts, "")
	require.NoError(t, err)
	srv, msg2, err := Respond(ch, msg1, keys, nil)
	require.NoError(t, err)
	dev, err = ini.Finish(ch, msg2)
	require.NoError(t, err)
	return dev, srv
}

func newStatic(t *testing.T) *Static {
	t.Helper()
	s, err := NewStatic()
	require.NoError(t, err)
	return s
}

func TestHandshakeAgrees(t *testing.T) {
	cur, old := newStatic(t), newStatic(t)
	for _, tc := range []struct {
		name  string
		dicts []int
		used  *Static
		dict  int
	}{
		{"dict offered", []int{DictV1}, cur, DictV1},
		{"no dict", nil, cur, DictNone},
		{"rotated key", []int{DictV1}, old, DictV1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev, mac := pair(t, "ch_a", tc.dicts, cur, tc.used)
			require.Equal(t, tc.dict, dev.Dict)
			require.Equal(t, tc.dict, mac.Dict)
			require.Equal(t, tc.used.KeyID(), mac.KeyID)
			require.Equal(t, dev.keys, mac.keys)

			rec, err := dev.Seal(Header{Type: TypeRequest, Flags: FlagLast, Msg: 1}, []byte("ping"))
			require.NoError(t, err)
			h, p, err := mac.Open(rec)
			require.NoError(t, err)
			require.Equal(t, Header{Type: TypeRequest, Flags: FlagLast, Msg: 1}, h)
			require.Equal(t, "ping", string(p))

			rec, err = mac.Seal(Header{Type: TypeReply, Flags: FlagLast, Msg: 1}, []byte("pong"))
			require.NoError(t, err)
			_, p, err = dev.Open(rec)
			require.NoError(t, err)
			require.Equal(t, "pong", string(p))
		})
	}
}

func TestHandshakeWrongStaticFails(t *testing.T) {
	real, other := newStatic(t), newStatic(t)
	_, msg1, err := Initiate(real.Public(), nil, []int{DictV1}, "")
	require.NoError(t, err)
	_, _, err = Respond("ch_a", msg1, []*Static{other}, nil)
	require.ErrorIs(t, err, ErrHandshake)
}

func TestOpenRefusesTamper(t *testing.T) {
	mac := newStatic(t)
	for _, tc := range []struct {
		name   string
		macCh  string
		tamper func([]byte)
	}{
		{"ciphertext", "ch_a", func(b []byte) { b[len(b)-20] ^= 1 }},
		{"tag", "ch_a", func(b []byte) { b[len(b)-1] ^= 1 }},
		{"counter", "ch_a", func(b []byte) { b[CounterSize-1] = 2 }},
		{"channel in aad", "ch_b", func([]byte) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ini, msg1, err := Initiate(mac.Public(), nil, nil, "")
			require.NoError(t, err)
			srv, msg2, err := Respond(tc.macCh, msg1, []*Static{mac}, nil)
			require.NoError(t, err)
			dev, err := ini.Finish("ch_a", msg2)
			require.NoError(t, err)
			rec, err := dev.Seal(Header{Type: TypeRequest, Flags: FlagLast, Msg: 1}, []byte("payload bytes"))
			require.NoError(t, err)
			tc.tamper(rec)
			_, _, err = srv.Open(rec)
			require.ErrorIs(t, err, ErrOpen)
		})
	}
}

func TestReplayWindow(t *testing.T) {
	dev, mac := pair(t, "ch_a", nil, newStatic(t), nil)
	recs := make([][]byte, WindowSize+10)
	for i := range recs {
		var err error
		recs[i], err = dev.Seal(Header{Type: TypeRequest, Flags: FlagLast, Msg: uint32(i + 1)}, nil)
		require.NoError(t, err)
	}
	open := func(counter int) error {
		_, _, err := mac.Open(recs[counter-1])
		return err
	}
	require.NoError(t, open(2))
	require.NoError(t, open(1))
	require.ErrorIs(t, open(1), ErrReplay)
	require.ErrorIs(t, open(2), ErrReplay)
	require.NoError(t, open(WindowSize+10))
	require.ErrorIs(t, open(10), ErrReplay)
	require.NoError(t, open(11))
	require.NoError(t, open(WindowSize+9))
	require.ErrorIs(t, open(WindowSize+10), ErrReplay)
}

func TestHandshakeCarriesTheTicketHash(t *testing.T) {
	mac := newStatic(t)
	_, msg1, err := Initiate(mac.Public(), nil, nil, "abc123")
	require.NoError(t, err)

	ch, _, err := Respond("ch_a", msg1, []*Static{mac}, nil)

	require.NoError(t, err)
	require.Equal(t, "abc123", ch.TicketHash)
}
