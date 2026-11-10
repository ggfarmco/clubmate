package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/apns"
)

const (
	pushRelayURLKey   = "pushRelayURL"
	pushRelayTokenKey = "pushRelayToken"
	pushRelayPath     = "/v1/push"
	relayDevicesPath  = "/v1/devices"
	relaySyncTimeout  = 5 * time.Second
	relayTimeout      = 30 * time.Second
	relayMaxReply     = 64 << 10
)

var (
	legacyAPNSKeys = []string{"apnsKey", "apnsKeyID", "apnsTeamID"}

	errRelayNotHTTPS = errors.New("push relay url must be https://<host>[:port]")
	errNoRelayToken  = errors.New("push relay token is required")
	errRelayUnset    = errors.New("no push relay configured")
	errRelayUnbound  = errors.New("push relay: device not registered for this Mac")
)

type relayClient struct {
	kv     kvStore
	client *http.Client

	mu         sync.Mutex
	url        string
	token      string
	reportURL  string
	registered string
}

type relayPush struct {
	pushType     string
	collapseID   string
	notification string
	payload      json.RawMessage
}

type relayDevices struct {
	Tokens []string `json:"tokens"`
}

type relayRequest struct {
	Token       string          `json:"token"`
	Environment string          `json:"environment"`
	Type        string          `json:"type"`
	CollapseID  string          `json:"collapseID,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

func newRelayClient(kv kvStore, client *http.Client) *relayClient {
	return &relayClient{kv: kv, client: noRedirects(client), reportURL: defaultReportRelay}
}

func noRedirects(client *http.Client) *http.Client {
	strict := *client
	strict.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &strict
}

func (c *relayClient) load(ctx context.Context) error {
	var u, token string
	if _, err := c.kv.StorageGet(ctx, pushRelayURLKey, &u); err != nil {
		return err
	}
	if _, err := c.kv.StorageGet(ctx, pushRelayTokenKey, &token); err != nil {
		return err
	}
	c.mu.Lock()
	c.url, c.token, c.registered = u, token, ""
	c.mu.Unlock()
	for _, k := range legacyAPNSKeys {
		if err := c.kv.StorageDelete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (c *relayClient) configure(ctx context.Context, rawURL, token string) error {
	if rawURL == "" {
		token = ""
	} else {
		base, ok := httpsBase(rawURL)
		if !ok {
			return errRelayNotHTTPS
		}
		if token == "" {
			return errNoRelayToken
		}
		rawURL = base
	}
	if err := c.kv.StorageSet(ctx, pushRelayURLKey, rawURL); err != nil {
		return err
	}
	if err := c.kv.StorageSet(ctx, pushRelayTokenKey, token); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.url, c.token, c.registered = rawURL, token, ""
	return nil
}

func (c *relayClient) register(ctx context.Context, deviceTokens []string) error {
	c.mu.Lock()
	base, token, done := c.url, c.token, c.registered
	c.mu.Unlock()
	if base == "" {
		return errRelayUnset
	}
	body, err := json.Marshal(relayDevices{Tokens: deviceTokens})
	if err != nil {
		return err
	}
	key := base + "\n" + string(body)
	if key == done {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, base+relayDevicesPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, relayMaxReply))
	switch res.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound, http.StatusMethodNotAllowed:
	default:
		return fmt.Errorf("push relay devices: %d %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.url == base && c.token == token {
		c.registered = key
	}
	return nil
}

func (c *relayClient) forgetRegistration() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registered = ""
}

func (c *relayClient) configured() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.url != ""
}

func (c *relayClient) reportTarget() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.url != "" {
		return c.url, c.token
	}
	return c.reportURL, ""
}

func (c *relayClient) reportHost() string {
	base, _ := c.reportTarget()
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *relayClient) send(ctx context.Context, env, deviceToken string, p relayPush) (apns.Reply, error) {
	c.mu.Lock()
	base, token := c.url, c.token
	c.mu.Unlock()
	if base == "" {
		return apns.Reply{}, errRelayUnset
	}
	body, err := json.Marshal(relayRequest{Token: deviceToken, Environment: env, Type: p.pushType, CollapseID: p.collapseID, Payload: p.payload})
	if err != nil {
		return apns.Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+pushRelayPath, bytes.NewReader(body))
	if err != nil {
		return apns.Reply{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return apns.Reply{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, relayMaxReply))
	if err != nil {
		return apns.Reply{}, err
	}
	if res.StatusCode == http.StatusForbidden {
		return apns.Reply{}, fmt.Errorf("%w: %s", errRelayUnbound, strings.TrimSpace(string(raw)))
	}
	if res.StatusCode != http.StatusOK {
		return apns.Reply{}, fmt.Errorf("push relay: %d %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var reply apns.Reply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return apns.Reply{}, fmt.Errorf("push relay reply: %w", err)
	}
	return reply, nil
}

func httpsBase(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || !(u.Scheme == "https" || u.Scheme == "http" && loopbackTestRelay(u.Hostname())) || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimSuffix(u.String(), "/"), true
}

func loopbackTestRelay(host string) bool {
	ip, err := netip.ParseAddr(host)
	return os.Getenv(testModeEnv) == "1" && err == nil && ip.IsLoopback()
}
