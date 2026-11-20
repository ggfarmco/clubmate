package tunnel

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBucket(t *testing.T) {
	for n, want := range map[int]int{0: 64, 1: 64, 64: 64, 65: 128, 1000: 1024, 1024: 1024, 1025: 1075, 1075: 1075, 1076: 1128} {
		require.Equal(t, want, Bucket(n), "n=%d", n)
	}
}

func TestPackDeflateRules(t *testing.T) {
	zero := 0
	big := bytes.Repeat([]byte(`{"id":"agt_0000000000000001","status":"waiting","title":"Refactor the cart"},`), 20)
	for _, tc := range []struct {
		name  string
		head  RequestHead
		body  []byte
		dict  int
		flags uint8
	}{
		{"under 128 bytes", RequestHead{Method: "POST", Path: "/v1/rpc"}, []byte(`{"method":"hello"}`), DictNone, 0},
		{"z 0", RequestHead{Method: "POST", Path: "/v1/rpc", Z: &zero}, big, DictNone, 0},
		{"deflate", RequestHead{Method: "POST", Path: "/v1/rpc"}, big, DictNone, FlagDeflate},
		{"deflate with dict-v1", RequestHead{Method: "POST", Path: "/v1/rpc"}, big, DictV1, FlagDeflate | DictV1<<dictShift},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain, flags, err := PackRequest(tc.head, tc.body, tc.dict)
			require.NoError(t, err)
			require.Equal(t, tc.flags, flags)
			require.Len(t, plain, Bucket(len(plain)))
			head, body, err := Unpack(Message{Type: TypeRequest, Flags: flags, Plain: plain}, tc.dict, MaxMessage)
			require.NoError(t, err)
			require.Equal(t, tc.body, body)
			require.Contains(t, string(head), `"p":"/v1/rpc"`)
		})
	}
}

func TestDictV1ShrinksContractReplies(t *testing.T) {
	body := []byte(`[{"id":"agt_0000000000000001","workspace":"prj_00000000000000f1","agent":"claude","status":"waiting","mode":"default","needsYou":true,"group":"needsYou","active":true}]`)
	plain, _, err := PackReply(ReplyHead{Status: 200}, body, DictNone, true)
	require.NoError(t, err)
	withDict, flags, err := PackReply(ReplyHead{Status: 200}, body, DictV1, true)
	require.NoError(t, err)
	require.Less(t, binary.BigEndian.Uint32(withDict), binary.BigEndian.Uint32(plain))
	_, got, err := Unpack(Message{Type: TypeReply, Flags: flags, Plain: withDict}, DictV1, MaxMessage)
	require.NoError(t, err)
	require.Equal(t, body, got)
}

func TestUnpackRefusesDictionaryNotAgreed(t *testing.T) {
	body := bytes.Repeat([]byte(`{"status":"waiting"},`), 20)
	plain, flags, err := PackReply(ReplyHead{Status: 200}, body, DictV1, true)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		flags  uint8
		agreed int
	}{
		{"not agreed", flags, DictNone},
		{"unknown id", flags | 2<<dictShift, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Unpack(Message{Type: TypeReply, Flags: tc.flags, Plain: plain}, tc.agreed, MaxMessage)
			require.ErrorIs(t, err, ErrPack)
		})
	}
}

func TestUnpackStopsInflateAtTheLimit(t *testing.T) {
	plain, flags, err := PackRequest(RequestHead{Method: "POST", Path: "/v1/rpc"}, make([]byte, 1<<20), DictNone)
	require.NoError(t, err)
	m := Message{Type: TypeRequest, Flags: flags, Plain: plain}

	_, _, err = Unpack(m, DictNone, 1<<20)
	require.ErrorIs(t, err, ErrTooLarge)
	_, body, err := Unpack(m, DictNone, 2<<20)
	require.NoError(t, err)
	require.Len(t, body, 1<<20)
}
