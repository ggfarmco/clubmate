package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	apnsKeyKey    = "apnsKey"
	apnsKeyIDKey  = "apnsKeyID"
	apnsTeamIDKey = "apnsTeamID"
	apnsTopic     = "com.clubmate.companion"
	apnsTokenTTL  = 50 * time.Minute
	apnsTimeout   = 15 * time.Second
	apnsMaxBody   = 64 << 10

	envDevelopment = "development"
	envProduction  = "production"
)

var (
	apnsHosts = map[string]string{
		envDevelopment: "https://api.sandbox.push.apple.com",
		envProduction:  "https://api.push.apple.com",
	}

	errBadAPNSKey     = errors.New("not a PKCS8 P-256 private key (.p8)")
	errNoAPNSKeyID    = errors.New("keyID is required unless the file is named AuthKey_<KEYID>.p8")
	errNoAPNSTeamID   = errors.New("teamID is required")
	errAPNSUnset      = errors.New("no APNs key configured")
	errUnknownPushEnv = errors.New("unknown push environment")
)

type apnsClient struct {
	kv     kvStore
	client *http.Client
	hosts  map[string]string
	now    func() time.Time

	mu      sync.Mutex
	key     *ecdsa.PrivateKey
	keyID   string
	teamID  string
	token   string
	tokenAt time.Time
}

type apnsPush struct {
	pushType   string
	priority   string
	collapseID string
	body       []byte
}

type apnsReply struct {
	status int
	reason string
}

type apnsErrorBody struct {
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

func newAPNSClient(kv kvStore, now func() time.Time, hosts map[string]string) *apnsClient {
	return &apnsClient{
		kv:     kv,
		client: &http.Client{Timeout: apnsTimeout, Transport: &http.Transport{ForceAttemptHTTP2: true}},
		hosts:  hosts,
		now:    now,
	}
}

func (c *apnsClient) load(ctx context.Context) error {
	var raw, keyID, teamID string
	for k, out := range map[string]*string{apnsKeyKey: &raw, apnsKeyIDKey: &keyID, apnsTeamIDKey: &teamID} {
		if _, err := c.kv.StorageGet(ctx, k, out); err != nil {
			return err
		}
	}
	if raw == "" {
		return nil
	}
	key, err := parseAPNSKey([]byte(raw))
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.keyID, c.teamID, c.token = key, keyID, teamID, ""
	return nil
}

func (c *apnsClient) configure(ctx context.Context, path, keyID, teamID string) error {
	if path == "" {
		return c.store(ctx, nil, "", "", "")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	key, err := parseAPNSKey(raw)
	if err != nil {
		return err
	}
	if keyID == "" {
		keyID = keyIDFromPath(path)
	}
	if keyID == "" {
		return errNoAPNSKeyID
	}
	if teamID == "" {
		return errNoAPNSTeamID
	}
	return c.store(ctx, key, string(raw), keyID, teamID)
}

func (c *apnsClient) configured() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.key != nil
}

func (c *apnsClient) send(ctx context.Context, env, deviceToken string, p apnsPush) (apnsReply, error) {
	host, ok := c.hosts[env]
	if !ok {
		return apnsReply{}, fmt.Errorf("%w: %q", errUnknownPushEnv, env)
	}
	bearer, err := c.bearer()
	if err != nil {
		return apnsReply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+deviceToken, bytes.NewReader(p.body))
	if err != nil {
		return apnsReply{}, err
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", apnsTopic)
	req.Header.Set("apns-push-type", p.pushType)
	req.Header.Set("apns-priority", p.priority)
	if p.collapseID != "" {
		req.Header.Set("apns-collapse-id", p.collapseID)
	}
	req.Header.Set("content-type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return apnsReply{}, err
	}
	defer res.Body.Close()
	var body apnsErrorBody
	if res.StatusCode != http.StatusOK {
		_ = json.NewDecoder(io.LimitReader(res.Body, apnsMaxBody)).Decode(&body)
	}
	return apnsReply{status: res.StatusCode, reason: body.Reason}, nil
}

func (c *apnsClient) store(ctx context.Context, key *ecdsa.PrivateKey, raw, keyID, teamID string) error {
	for k, v := range map[string]string{apnsKeyKey: raw, apnsKeyIDKey: keyID, apnsTeamIDKey: teamID} {
		if err := c.kv.StorageSet(ctx, k, v); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.keyID, c.teamID, c.token = key, keyID, teamID, ""
	return nil
}

func (c *apnsClient) bearer() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == nil {
		return "", errAPNSUnset
	}
	now := c.now()
	if c.token != "" && now.Sub(c.tokenAt) < apnsTokenTTL {
		return c.token, nil
	}
	token, err := signJWT(c.key, c.keyID, c.teamID, now)
	if err != nil {
		return "", err
	}
	c.token, c.tokenAt = token, now
	return token, nil
}

func (r apnsReply) goneToken() bool {
	return r.status == http.StatusGone || r.status == http.StatusBadRequest && (r.reason == "BadDeviceToken" || r.reason == "Unregistered")
}

func parseAPNSKey(raw []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errBadAPNSKey
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBadAPNSKey, err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errBadAPNSKey
	}
	return key, nil
}

func keyIDFromPath(path string) string {
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
