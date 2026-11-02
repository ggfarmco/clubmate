package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Topic = "com.clubmate.companion"

	EnvDevelopment = "development"
	EnvProduction  = "production"

	TypeAlert      = "alert"
	TypeBackground = "background"

	MaxDeviceToken = 200
	MaxPayload     = 4096
	MaxCollapseID  = 64

	tokenTTL = 50 * time.Minute
	timeout  = 15 * time.Second
	maxReply = 64 << 10
)

var (
	ErrBadKey             = errors.New("not a PKCS8 P-256 private key (.p8)")
	ErrUnknownEnvironment = errors.New("unknown push environment")
)

type Client struct {
	Hosts map[string]string
	HTTP  *http.Client
	Now   func() time.Time

	key    *ecdsa.PrivateKey
	keyID  string
	teamID string

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

type Push struct {
	Type       string
	Priority   int
	CollapseID string
	Payload    []byte
}

type Reply struct {
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

type errorBody struct {
	Reason string `json:"reason"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type jwtClaims struct {
	Iss string `json:"iss"`
	Iat int64  `json:"iat"`
}

func NewClient(key *ecdsa.PrivateKey, keyID, teamID string) *Client {
	return &Client{
		Hosts:  DefaultHosts(),
		HTTP:   &http.Client{Timeout: timeout, Transport: &http.Transport{ForceAttemptHTTP2: true}},
		Now:    time.Now,
		key:    key,
		keyID:  keyID,
		teamID: teamID,
	}
}

func DefaultHosts() map[string]string {
	return map[string]string{
		EnvDevelopment: "https://api.sandbox.push.apple.com",
		EnvProduction:  "https://api.push.apple.com",
	}
}

func ParseKey(raw []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, ErrBadKey
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadKey, err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, ErrBadKey
	}
	return key, nil
}

func KeyIDFromPath(path string) string {
	name, ok := strings.CutPrefix(filepath.Base(path), "AuthKey_")
	if !ok {
		return ""
	}
	id, ok := strings.CutSuffix(name, ".p8")
	if !ok {
		return ""
	}
	return id
}

func ValidDeviceToken(token string) bool {
	if len(token) > MaxDeviceToken {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func (c *Client) Send(ctx context.Context, env, deviceToken string, p Push) (Reply, error) {
	host, ok := c.Hosts[env]
	if !ok {
		return Reply{}, fmt.Errorf("%w: %q", ErrUnknownEnvironment, env)
	}
	bearer, err := c.bearer()
	if err != nil {
		return Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+deviceToken, bytes.NewReader(p.Payload))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", Topic)
	req.Header.Set("apns-push-type", p.Type)
	req.Header.Set("apns-priority", strconv.Itoa(p.Priority))
	if p.CollapseID != "" {
		req.Header.Set("apns-collapse-id", p.CollapseID)
	}
	req.Header.Set("content-type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer res.Body.Close()
	var body errorBody
	if res.StatusCode != http.StatusOK {
		_ = json.NewDecoder(io.LimitReader(res.Body, maxReply)).Decode(&body)
	}
	return Reply{Status: res.StatusCode, Reason: body.Reason}, nil
}

func (r Reply) GoneToken() bool {
	return r.Status == http.StatusGone || r.Status == http.StatusBadRequest && (r.Reason == "BadDeviceToken" || r.Reason == "Unregistered")
}

func (c *Client) bearer() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	if c.token != "" && now.Sub(c.tokenAt) < tokenTTL {
		return c.token, nil
	}
	token, err := signJWT(c.key, c.keyID, c.teamID, now)
	if err != nil {
		return "", err
	}
	c.token, c.tokenAt = token, now
	return token, nil
}

func signJWT(key *ecdsa.PrivateKey, keyID, teamID string, now time.Time) (string, error) {
	header, err := json.Marshal(jwtHeader{Alg: "ES256", Kid: keyID})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(jwtClaims{Iss: teamID, Iat: now.Unix()})
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + enc.EncodeToString(sig), nil
}
