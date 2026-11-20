package tunnel

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "write testdata/vectors.json and the CompanionKit copies")

const (
	vectorsPath = "testdata/vectors.json"
	dictPath    = "dict-v1.bin"
	kitFixtures = "../../client/CompanionKit/Tests/CompanionKitTests/Fixtures"
	kitDict     = "../../client/CompanionKit/Sources/CompanionKit/Resources/dict-v1.bin"
)

var vectorTicketHash = hex.EncodeToString(hash("cmk_vector", nil))

var layout = []string{
	"Keys are X25519, hashes SHA-256, KDF HKDF-SHA256, AEAD AES-256-GCM with a 16-byte tag. '|' joins bytes. Hex fields are lowercase hex; macKey is base64url without padding; SSE data and JSON d fields are standard base64 with padding.",
	"kid = hex(SHA256(macStaticPub)[0:8]).",
	"h0 = SHA256(UTF-8 'clubmate tunnel v1' | macStaticPub); es = X25519(deviceEphPriv, macStaticPub); h1 = SHA256(h0 | deviceEphPub); k1 = HKDF(ikm es, salt h0, info 'clubmate tunnel v1 k1', 32).",
	"msg1 = 0x01 | deviceEphPub | AES-GCM(k1, nonce 12 zero bytes, aad h1, hello JSON {\"dicts\":[...],\"kid\":\"...\",\"th\":\"...\"}); th = hex(SHA256(UTF-8 relay ticket)) and is left out when the device has no ticket.",
	"ee = X25519(macEphPriv, deviceEphPub); h2 = SHA256(h1 | macEphPub); kDM | kMD | k2 = HKDF(ikm es | ee, salt h1, info UTF-8 'clubmate tunnel v1 keys' | h2, 96).",
	"msg2 = macEphPub | AES-GCM(k2, nonce 12 zero bytes, aad h2, accept JSON {\"dict\":n}); n = 1 when the device offered 1, else 0.",
	"record on the wire = len (u32 BE, bytes after it) | counter (u64 BE) | AES-GCM(key, nonce = 4 zero bytes | counter u64 BE, aad = UTF-8 channel id, plaintext = type (1) | flags (1) | msg (u32 BE) | part (u16 BE) | payload). key = kDM device to Mac, kMD Mac to device. Record plaintext is at most 65536 bytes, so payload is at most 65528.",
	"counter and msg start at 1 per direction. The receiver keeps a window of 65536 counters: counter 0, a repeated counter or a counter <= highest - 65536 is refused.",
	"type: 1 request, 2 reply, 3 stream chunk, 4 stream end. flags: bit 0 last part, bit 1 deflate, bits 2-3 dictionary id. Every record of one message has the same type, msg and flags; only the last part has bit 0. part counts from 0 in order.",
	"The records of one message carry, in order, re (u32 BE) | seq (u32 BE) | message plaintext for types 2, 3 and 4, and only the message plaintext for type 1. re = msg of the request this answers. seq = 0 for the reply of a call; for a stream, 0 for the reply head, then 1, 2, ... for each chunk and the end, with no gap. The phone refuses an answer with another re, a gap or a repeat. In the records below, payload is the message plaintext.",
	"message plaintext = realLen (u32 BE) | content | zero padding. content = head JSON | 0x0A | body for types 1 and 2, body for types 3 and 4, deflated when flag bit 1 is set. realLen = length of content as sent (after deflate).",
	"deflate: raw deflate (RFC 1951), level 6, fresh per message, preset dictionary dict-v1.bin when bits 2-3 = 1. Skipped when head | 0x0A | body is under 128 bytes, when the request head has \"z\":0, or when the deflated bytes are not shorter.",
	"padding: the whole message plaintext (realLen included) rounds up to a multiple of 64, at least 64, up to 1024; above 1024 to the first b >= size of b = 1024, b = b + floor(b / 20).",
	"push batch = repeated kind (1 data, 2 end) | id (16 bytes) | len (u32 BE) | bytes. SSE frame = 'data: ' | base64(bytes) | '\\n\\n'.",
}

type vectors struct {
	Layout        []string       `json:"layout"`
	MacStaticPriv string         `json:"macStaticPriv"`
	MacStaticPub  string         `json:"macStaticPub"`
	MacKey        string         `json:"macKey"`
	KID           string         `json:"kid"`
	DeviceEphPriv string         `json:"deviceEphPriv"`
	DeviceEphPub  string         `json:"deviceEphPub"`
	MacEphPriv    string         `json:"macEphPriv"`
	MacEphPub     string         `json:"macEphPub"`
	Channel       string         `json:"channel"`
	Dicts         []int          `json:"dicts"`
	TicketHash    string         `json:"ticketHash"`
	Dict          int            `json:"dict"`
	H0            string         `json:"h0"`
	H1            string         `json:"h1"`
	H2            string         `json:"h2"`
	ES            string         `json:"es"`
	EE            string         `json:"ee"`
	K1            string         `json:"k1"`
	K2            string         `json:"k2"`
	KDM           string         `json:"kDM"`
	KMD           string         `json:"kMD"`
	Msg1          string         `json:"msg1"`
	Msg2          string         `json:"msg2"`
	DictV1SHA256  string         `json:"dictV1SHA256"`
	Buckets       []vectorBucket `json:"buckets"`
	Packs         []vectorPack   `json:"packs"`
	Records       []vectorRecord `json:"records"`
}

type vectorBucket struct {
	Size   int `json:"size"`
	Bucket int `json:"bucket"`
}

type vectorPack struct {
	Name    string `json:"name"`
	Type    int    `json:"type"`
	Head    string `json:"head"`
	Body    string `json:"body"`
	Dict    int    `json:"dict"`
	Deflate bool   `json:"deflate"`
	Flags   int    `json:"flags"`
	Plain   string `json:"plain"`
}

type vectorRecord struct {
	Pack    string `json:"pack"`
	Dir     string `json:"dir"`
	Counter int    `json:"counter"`
	Type    int    `json:"type"`
	Flags   int    `json:"flags"`
	Msg     int    `json:"msg"`
	Re      *int   `json:"re,omitempty"`
	Seq     *int   `json:"seq,omitempty"`
	Part    int    `json:"part"`
	Payload string `json:"payload"`
	Wire    string `json:"wire"`
}

func testKey(t *testing.T, name string) *ecdh.PrivateKey {
	t.Helper()
	seed := sha256.Sum256([]byte(name))
	k, err := ecdh.X25519().NewPrivateKey(seed[:])
	require.NoError(t, err)
	return k
}

func makeVectors(t *testing.T) []byte {
	t.Helper()
	macStatic := testKey(t, "mac static")
	devEph, macEph := testKey(t, "device ephemeral"), testKey(t, "mac ephemeral")
	s, err := LoadStatic(macStatic.Bytes())
	require.NoError(t, err)
	const ch = "ch_0123456789abcdef"
	dicts := []int{DictV1}
	ini, msg1, err := Initiate(s.Public(), devEph, dicts, vectorTicketHash)
	require.NoError(t, err)
	mac, msg2, err := Respond(ch, msg1, []*Static{s}, macEph)
	require.NoError(t, err)
	dev, err := ini.Finish(ch, msg2)
	require.NoError(t, err)

	v := vectors{
		Layout:        layout,
		MacStaticPriv: hex.EncodeToString(s.Bytes()),
		MacStaticPub:  hex.EncodeToString(s.Public()),
		MacKey:        EncodeKey(s.Public()),
		KID:           s.KeyID(),
		DeviceEphPriv: hex.EncodeToString(devEph.Bytes()),
		DeviceEphPub:  hex.EncodeToString(devEph.PublicKey().Bytes()),
		MacEphPriv:    hex.EncodeToString(macEph.Bytes()),
		MacEphPub:     hex.EncodeToString(macEph.PublicKey().Bytes()),
		Channel:       ch,
		Dicts:         dicts,
		TicketHash:    vectorTicketHash,
		Dict:          dev.Dict,
		H0:            hex.EncodeToString(ini.h0),
		H1:            hex.EncodeToString(ini.h1),
		H2:            hex.EncodeToString(dev.keys.h2),
		ES:            hex.EncodeToString(ini.es),
		EE:            hex.EncodeToString(dev.keys.ee),
		K1:            hex.EncodeToString(ini.k1),
		K2:            hex.EncodeToString(dev.keys.k2),
		KDM:           hex.EncodeToString(dev.keys.kdm),
		KMD:           hex.EncodeToString(dev.keys.kmd),
		Msg1:          hex.EncodeToString(msg1),
		Msg2:          hex.EncodeToString(msg2),
		DictV1SHA256:  DictV1SHA256,
	}
	for _, n := range []int{0, 1, 64, 65, 1024, 1025, 1076, 65536, 1 << 20} {
		v.Buckets = append(v.Buckets, vectorBucket{Size: n, Bucket: Bucket(n)})
	}

	reply := []byte(`[{"id":"agt_0000000000000001","workspace":"prj_00000000000000f1","agent":"claude","status":"waiting","mode":"default","needsYou":true,"group":"needsYou","active":true},{"id":"agt_0000000000000002","workspace":"prj_00000000000000f1","agent":"claude","status":"done","mode":"default","needsYou":false,"group":"done","active":false}]`)
	noise := make([]byte, 0, 1500)
	for seed := sha256.Sum256([]byte("noise")); len(noise) < 1500; seed = sha256.Sum256(seed[:]) {
		noise = append(noise, seed[:]...)
	}
	noise = noise[:1500]
	cases := []struct {
		name    string
		typ     Type
		head    string
		body    []byte
		dict    int
		deflate bool
	}{
		{"request-small", TypeRequest, `{"m":"POST","p":"/v1/rpc","h":{"Authorization":"Bearer cmt_test"}}`, []byte(`{"method":"hello"}`), DictV1, true},
		{"request-z0", TypeRequest, `{"m":"POST","p":"/v1/rpc","h":{"Authorization":"Bearer cmt_test"},"z":0}`, reply, DictV1, false},
		{"reply-deflate", TypeReply, `{"s":200,"h":{"Content-Type":"application/json"}}`, reply, DictNone, true},
		{"reply-deflate-dict", TypeReply, `{"s":200,"h":{"Content-Type":"application/json"}}`, reply, DictV1, true},
		{"chunk-padded", TypeChunk, "", noise, DictV1, true},
		{"end", TypeEnd, "", []byte{}, DictV1, true},
	}
	packs := map[string][]byte{}
	flagsOf := map[string]uint8{}
	for _, c := range cases {
		var head []byte
		if c.head != "" {
			head = []byte(c.head)
		}
		plain, flags, err := Pack(head, c.body, c.dict, c.deflate)
		require.NoError(t, err)
		h, b, err := Unpack(Message{Type: c.typ, Flags: flags, Plain: plain}, c.dict, MaxMessage)
		require.NoError(t, err)
		require.Equal(t, c.head, string(h))
		require.Equal(t, c.body, b)
		packs[c.name], flagsOf[c.name] = plain, flags
		v.Packs = append(v.Packs, vectorPack{Name: c.name, Type: int(c.typ), Head: c.head, Body: hex.EncodeToString(c.body), Dict: c.dict, Deflate: c.deflate, Flags: int(flags), Plain: hex.EncodeToString(plain)})
	}

	var re uint32
	for _, r := range []struct {
		pack string
		typ  Type
		seq  int
	}{
		{"request-small", TypeRequest, 0},
		{"reply-deflate-dict", TypeReply, 0},
		{"request-z0", TypeRequest, 0},
		{"chunk-padded", TypeChunk, 1},
		{"end", TypeEnd, 2},
	} {
		var wire []byte
		var err error
		rec := vectorRecord{Pack: r.pack, Dir: "dm", Type: int(r.typ), Flags: int(flagsOf[r.pack] | FlagLast), Payload: hex.EncodeToString(packs[r.pack])}
		from, to := dev, mac
		if r.typ == TypeRequest {
			wire, re, err = dev.SealRequest(flagsOf[r.pack], packs[r.pack])
		} else {
			from, to, rec.Dir = mac, dev, "md"
			wire, err = mac.SealAnswer(r.typ, flagsOf[r.pack], re, uint32(r.seq), packs[r.pack])
			rec.Re, rec.Seq = new(int(re)), new(r.seq)
		}
		require.NoError(t, err)
		m, err := to.ReadMessage(bytes.NewReader(wire))
		require.NoError(t, err)
		require.Equal(t, packs[r.pack], m.Plain)
		rec.Counter, rec.Msg, rec.Wire = int(from.counter), int(m.Msg), hex.EncodeToString(wire)
		v.Records = append(v.Records, rec)
	}

	b, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return append(b, '\n')
}

func TestVectorsStable(t *testing.T) {
	got := makeVectors(t)
	if *update {
		require.NoError(t, os.WriteFile(vectorsPath, got, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(kitFixtures, "tunnel-vectors.json"), got, 0o644))
		dict, err := os.ReadFile(dictPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(kitFixtures, "dict-v1.bin"), dict, 0o644))
		require.NoError(t, os.WriteFile(kitDict, dict, 0o644))
	}
	want, err := os.ReadFile(vectorsPath)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "run go test ./pkg/tunnel -run TestVectorsStable -update")
}

func TestDictV1MatchesItsHash(t *testing.T) {
	sum := sha256.Sum256(dictV1)
	require.Equal(t, DictV1SHA256, hex.EncodeToString(sum[:]))
	require.Len(t, dictV1, 32<<10)
}

func TestCompanionKitCopiesMatch(t *testing.T) {
	for _, c := range []struct{ src, dst string }{
		{vectorsPath, filepath.Join(kitFixtures, "tunnel-vectors.json")},
		{dictPath, filepath.Join(kitFixtures, "dict-v1.bin")},
		{dictPath, kitDict},
	} {
		want, err := os.ReadFile(c.src)
		require.NoError(t, err)
		got, err := os.ReadFile(c.dst)
		require.NoError(t, err)
		require.True(t, bytes.Equal(want, got), "%s differs from %s; run go test ./pkg/tunnel -run TestVectorsStable -update", c.dst, c.src)
	}
}
