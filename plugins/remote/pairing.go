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

	"clubmate/internal/tunnelrelay"
)

const (
	codeBytes        = 16
	codeTTL          = 5 * time.Minute
	maxFailures      = 5
	maxTotalFailures = 50
	linkVersion      = "1"
	linkVersionRelay = "2"

	pairOK      = "ok"
	pairBadCode = "bad-code"
	pairExpired = "expired"
	pairLimit   = "limit"
)

var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type pairing struct {
	now func() time.Time

	mu       sync.Mutex
	active   bool
	gen      uint64
	hash     [sha256.Size]byte
	ticket   string
	expires  time.Time
	failures map[string]int
	total    int
}

type pairResult struct {
	Link      string `json:"link"`
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
	Relay     bool   `json:"relay"`
}

func newPairing(now func() time.Time) *pairing {
	return &pairing{now: now}
}

func (p *pairing) issue() (string, time.Time) {
	code, _, expires := p.issueWithTicket()
	return code, expires
}

func (p *pairing) issueWithTicket() (string, string, time.Time) {
	raw := make([]byte, codeBytes)
	_, _ = rand.Read(raw)
	code := codeEncoding.EncodeToString(raw)
	ticket := newSecret(ticketPrefix)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	p.gen++
	p.hash = sha256.Sum256([]byte(code))
	p.ticket = hashToken(ticket)
	p.expires = p.now().Add(codeTTL)
	p.failures = map[string]int{}
	p.total = 0
	return code, ticket, p.expires
}

func (p *pairing) ticketHash() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ticket
}

func (p *pairing) expiry() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.expires
}

func (p *pairing) liveTicket() (tunnelrelay.Ticket, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active || p.ticket == "" || !p.now().Before(p.expires) {
		return tunnelrelay.Ticket{}, false
	}
	return tunnelrelay.Ticket{Hash: p.ticket, Exp: p.expires.Unix()}, true
}

func (p *pairing) cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	p.gen++
}

func (p *pairing) redeem(code, source string) (uint64, bool) {
	gen, result := p.try(code, source)
	return gen, result == pairOK
}

func (p *pairing) try(code, source string) (uint64, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures[source] >= maxFailures {
		return 0, pairLimit
	}
	if !p.active {
		return 0, pairExpired
	}
	if !p.now().Before(p.expires) {
		p.active = false
		return 0, pairExpired
	}
	got := sha256.Sum256([]byte(strings.ToUpper(code)))
	if subtle.ConstantTimeCompare(got[:], p.hash[:]) != 1 {
		p.failures[source]++
		p.total++
		if p.total >= maxTotalFailures {
			p.active = false
		}
		return 0, pairBadCode
	}
	p.active = false
	return p.gen, pairOK
}

func (p *pairing) reopen(gen uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gen == gen && p.now().Before(p.expires) {
		p.active = true
	}
}

type linkRelay struct {
	URL    string
	Key    string
	KID    string
	Ticket string
}

func pairLink(code string, hosts []string, port int, c computer, expires time.Time, relay *linkRelay) string {
	version := linkVersion
	if relay != nil {
		version = linkVersionRelay
	}
	var b strings.Builder
	b.WriteString("clubmate://pair?v=" + version + "&c=" + code)
	for _, h := range hosts {
		b.WriteString("&h=" + queryEscape(h))
	}
	b.WriteString("&p=" + strconv.Itoa(port) + "&id=" + queryEscape(c.ID) + "&n=" + queryEscape(c.Name) + "&e=" + strconv.FormatInt(expires.Unix(), 10))
	if relay != nil {
		b.WriteString("&r=" + queryEscape(relay.URL) + "&k=" + queryEscape(relay.Key) + "&kid=" + queryEscape(relay.KID) + "&pt=" + queryEscape(relay.Ticket))
	}
	return b.String()
}

func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
