package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	codeBytes        = 16
	codeTTL          = 5 * time.Minute
	maxFailures      = 5
	maxTotalFailures = 50
	linkVersion      = "1"
)

var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type pairing struct {
	now func() time.Time

	mu       sync.Mutex
	active   bool
	gen      uint64
	hash     [sha256.Size]byte
	expires  time.Time
	failures map[string]int
	total    int
}

type pairResult struct {
	Link      string `json:"link"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
}

func newPairing(now func() time.Time) *pairing {
	return &pairing{now: now}
}

func (p *pairing) issue() (string, time.Time) {
	raw := make([]byte, codeBytes)
	_, _ = rand.Read(raw)
	code := codeEncoding.EncodeToString(raw)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	p.gen++
	p.hash = sha256.Sum256([]byte(code))
	p.expires = p.now().Add(codeTTL)
	p.failures = map[string]int{}
	p.total = 0
	return code, p.expires
}

func (p *pairing) cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.gen++
}

func (p *pairing) redeem(code, source string) (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active || p.failures[source] >= maxFailures {
		return 0, false
	}
	if !p.now().Before(p.expires) {
		p.active = false
		return 0, false
	}
	got := sha256.Sum256([]byte(strings.ToUpper(code)))
	if subtle.ConstantTimeCompare(got[:], p.hash[:]) != 1 {
		p.failures[source]++
		p.total++
		if p.total >= maxTotalFailures {
			p.active = false
		}
		return 0, false
	}
	p.active = false
	return p.gen, true
}

func (p *pairing) reopen(gen uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gen == gen && p.now().Before(p.expires) {
		p.active = true
	}
}

func pairLink(code string, hosts []string, port int, c computer, expires time.Time) string {
	var b strings.Builder
	b.WriteString("clubmate://pair?v=" + linkVersion + "&c=" + code)
	for _, h := range hosts {
		b.WriteString("&h=" + queryEscape(h))
	}
	b.WriteString("&p=" + strconv.Itoa(port) + "&id=" + queryEscape(c.ID) + "&n=" + queryEscape(c.Name) + "&e=" + strconv.FormatInt(expires.Unix(), 10))
	return b.String()
}

func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
