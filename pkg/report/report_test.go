package report

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"hash/crc32"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	body := []byte("bundle")
	req, err := http.NewRequest(http.MethodPost, "https://relay.example/v1/reports", nil)
	require.NoError(t, err)

	Sign(req, priv, body, now)

	got, err := Verify(req, body, now)
	require.NoError(t, err)
	assert.Equal(t, pub, got)
	_, err = Verify(req, []byte("bundlf"), now)
	assert.ErrorIs(t, err, ErrBadSignature)
	sig := req.Header.Get(HeaderSignature)
	last := strings.IndexByte(alphabet, sig[len(sig)-1])
	req.Header.Set(HeaderSignature, sig[:len(sig)-1]+string(alphabet[last^1]))
	_, err = Verify(req, body, now)
	assert.ErrorIs(t, err, ErrBadSignature)
	req.Header.Set(HeaderSignature, sig)
	_, err = Verify(req, body, now.Add(6*time.Minute))
	assert.ErrorIs(t, err, ErrSkew)
	bare, err := http.NewRequest(http.MethodPost, "https://relay.example/v1/reports", nil)
	require.NoError(t, err)
	_, err = Verify(bare, body, now)
	assert.ErrorIs(t, err, ErrUnsigned)
}

func TestReadMeta(t *testing.T) {
	want := Meta{Text: "graphs keeps timing out", Summary: "run hung", Files: []string{"shot.png"}}
	got, err := ReadMeta(bundle(t, want))
	require.NoError(t, err)
	assert.Equal(t, want, got)

	refused := []struct {
		name string
		meta Meta
	}{
		{name: "text too long", meta: Meta{Text: strings.Repeat("a", MaxText+1)}},
		{name: "blank text", meta: Meta{Text: " \n\t"}},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadMeta(bundle(t, tt.meta))
			assert.ErrorIs(t, err, ErrBadMeta)
		})
	}
}

func TestCheckEntries(t *testing.T) {
	many := make([]rawEntry, MaxEntries+1)
	for i := range many {
		many[i] = rawEntry{name: strconv.Itoa(i)}
	}
	cases := map[string][]rawEntry{
		"parent":   {{name: "../x"}},
		"absolute": {{name: "/etc/x"}},
		"reserved": {{name: ".HANDLED"}},
		"stored":   {{name: "Stored.json/x"}},
		"symlink":  {{name: "link", mode: fs.ModeSymlink | 0o777}},
		"entry":    {{name: "big", size: MaxEntry + 1}},
		"total":    {{name: "a", size: MaxEntry}, {name: "b", size: MaxEntry}, {name: "c", size: MaxEntry}, {name: "d", size: MaxEntry}, {name: "e", size: 1}},
		"entries":  many,
		"nul":      {{name: "a\x00b"}},
		"utf8":     {{name: "\xff\xfe"}},
		"part":     {{name: "d/" + strings.Repeat("a", MaxNamePart+1)}},
		"long":     {{name: strings.Repeat("d/", MaxName/2) + "a"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			raw := rawZip(t, entries)
			zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			require.NoError(t, err)

			assert.ErrorIs(t, CheckEntries(zr), ErrBadBundle)
		})
	}
}

func TestNewIDOrder(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	first, second := NewID(now), NewID(now.Add(time.Second))
	assert.True(t, strings.HasPrefix(first, "rpt_20270115T080000Z_"), first)
	assert.Less(t, first, second)
}

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func bundle(t *testing.T, m Meta) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(MetaName)
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(w).Encode(m))
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

type rawEntry struct {
	name string
	mode fs.FileMode
	size uint64
}

func rawZip(t *testing.T, entries []rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	content := []byte("small")
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(content), CompressedSize64: uint64(len(content)), UncompressedSize64: max(e.size, uint64(len(content)))}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateRaw(h)
		require.NoError(t, err)
		_, err = w.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
