package apns

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type seen struct {
	path   string
	header http.Header
	body   string
}

func TestSendAddressesAndSignsThePush(t *testing.T) {
	key, raw := newKey(t)
	parsed, err := ParseKey(raw)
	require.NoError(t, err)
	var got seen
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = seen{path: r.URL.Path, header: r.Header.Clone(), body: string(body)}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorBody{Reason: "BadDeviceToken"})
	}))
	t.Cleanup(ts.Close)
	c := NewClient(parsed, "ABC123", "TEAM")
	c.Hosts = map[string]string{EnvDevelopment: ts.URL + "/dev", EnvProduction: ts.URL + "/prod"}
	c.Now = func() time.Time { return time.Unix(1_800_000_000, 0) }

	reply, err := c.Send(t.Context(), EnvDevelopment, "aa01", Push{Type: TypeAlert, Priority: 10, CollapseID: "ntf_1", Payload: []byte(`{"aps":{}}`)})

	require.NoError(t, err)
	assert.Equal(t, Reply{Status: http.StatusBadRequest, Reason: "BadDeviceToken"}, reply)
	assert.True(t, reply.GoneToken())
	assert.Equal(t, "/dev/3/device/aa01", got.path)
	assert.Equal(t, Topic, got.header.Get("apns-topic"))
	assert.Equal(t, "alert", got.header.Get("apns-push-type"))
	assert.Equal(t, "10", got.header.Get("apns-priority"))
	assert.Equal(t, "ntf_1", got.header.Get("apns-collapse-id"))
	assert.Equal(t, `{"aps":{}}`, got.body)
	verifyJWT(t, &key.PublicKey, got.header.Get("authorization"))
}

func TestBearerIsReusedForFiftyMinutes(t *testing.T) {
	key, _ := newKey(t)
	now := time.Unix(1_800_000_000, 0)
	c := NewClient(key, "ABC123", "TEAM")
	c.Now = func() time.Time { return now }

	first, err := c.bearer()
	require.NoError(t, err)
	now = now.Add(49 * time.Minute)
	second, err := c.bearer()
	require.NoError(t, err)
	now = now.Add(2 * time.Minute)
	third, err := c.bearer()
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.NotEqual(t, second, third)
}

func TestGoneToken(t *testing.T) {
	tests := []struct {
		reply Reply
		gone  bool
	}{
		{Reply{Status: http.StatusGone, Reason: "Unregistered"}, true},
		{Reply{Status: http.StatusBadRequest, Reason: "Unregistered"}, true},
		{Reply{Status: http.StatusBadRequest, Reason: "PayloadTooLarge"}, false},
		{Reply{Status: http.StatusTooManyRequests, Reason: "TooManyRequests"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.reply.Reason, func(t *testing.T) {
			assert.Equal(t, tt.gone, tt.reply.GoneToken())
		})
	}
}

func TestParseKeyRejectsNonP8(t *testing.T) {
	_, err := ParseKey([]byte("nope"))

	assert.ErrorIs(t, err, ErrBadKey)
}

func TestKeyIDFromPath(t *testing.T) {
	assert.Equal(t, "ABC123", KeyIDFromPath("/tmp/AuthKey_ABC123.p8"))
	assert.Empty(t, KeyIDFromPath("/tmp/key.p8"))
}

func newKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func verifyJWT(t *testing.T, pub *ecdsa.PublicKey, auth string) {
	t.Helper()
	token, ok := strings.CutPrefix(auth, "bearer ")
	require.True(t, ok)
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"alg":"ES256","kid":"ABC123"}`, string(header))
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	assert.JSONEq(t, `{"iss":"TEAM","iat":1800000000}`, string(claims))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	require.Len(t, sig, 64)
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	assert.True(t, ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])))
}
