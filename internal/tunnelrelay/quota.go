package tunnelrelay

import (
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	QuotaBytes     = "bytes"
	QuotaChannels  = "channels"
	QuotaCalls     = "calls"
	QuotaStreams   = "streams"
	QuotaRate      = "rate"
	QuotaOpenRate  = "open-rate"
	QuotaMacs      = "mac-streams"
	QuotaAllChans  = "all-channels"
	QuotaHandlers  = "handlers"
	QuotaBuffered  = "buffered"
	QuotaMacCalls  = "mac-handlers"
	QuotaSelfBytes = "self-bytes"
	QuotaMacsPerIP = "ip-mac-streams"
	QuotaTickets   = "tickets"
	QuotaPush      = "push"
	busyRetry      = time.Second
	macsRetry      = time.Minute
	sweepEvery     = time.Minute
	readChunk      = 32 << 10
	day            = 24 * time.Hour
	bucketIdleTime = 10 * time.Minute
)

var errBusy = errors.New("relay busy")

type Limits struct {
	BytesPerDay int64
	Channels    int
	Calls       int
	Streams     int
	Rate        float64
	Burst       int
	OpenRate    float64
	OpenBurst   int
	Idle        time.Duration
	MacStreams  int
	AllChannels int
	Handlers    int
	Buffered    int64

	MacBuffered        int64
	MacHandlers        int
	SelfBytesPerDay    int64
	SelfBytesPerDayAll int64
	MacStreamsPerIP    int
	TicketsRate        float64
	TicketsBurst       int
}

func DefaultLimits() Limits {
	return Limits{
		BytesPerDay: 2 << 30,
		Channels:    32,
		Calls:       64,
		Streams:     16,
		Rate:        20,
		Burst:       100,
		OpenRate:    1,
		OpenBurst:   10,
		Idle:        10 * time.Minute,
		MacStreams:  25_000,
		AllChannels: 100_000,
		Handlers:    10_000,
		Buffered:    256 << 20,

		MacBuffered:        2 * MaxBody,
		MacHandlers:        256,
		SelfBytesPerDay:    512 << 20,
		SelfBytesPerDayAll: 200 << 30,
		MacStreamsPerIP:    32,
		TicketsRate:        0.1,
		TicketsBurst:       10,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.BytesPerDay <= 0 {
		l.BytesPerDay = d.BytesPerDay
	}
	if l.Channels <= 0 {
		l.Channels = d.Channels
	}
	if l.Calls <= 0 {
		l.Calls = d.Calls
	}
	if l.Streams <= 0 {
		l.Streams = d.Streams
	}
	if l.Rate <= 0 {
		l.Rate = d.Rate
	}
	if l.Burst <= 0 {
		l.Burst = d.Burst
	}
	if l.OpenRate <= 0 {
		l.OpenRate = d.OpenRate
	}
	if l.OpenBurst <= 0 {
		l.OpenBurst = d.OpenBurst
	}
	if l.Idle <= 0 {
		l.Idle = d.Idle
	}
	if l.MacStreams <= 0 {
		l.MacStreams = d.MacStreams
	}
	if l.AllChannels <= 0 {
		l.AllChannels = d.AllChannels
	}
	if l.Handlers <= 0 {
		l.Handlers = d.Handlers
	}
	if l.Buffered <= 0 {
		l.Buffered = d.Buffered
	}
	if l.MacBuffered <= 0 {
		l.MacBuffered = d.MacBuffered
	}
	if l.MacHandlers <= 0 {
		l.MacHandlers = d.MacHandlers
	}
	if l.SelfBytesPerDay <= 0 {
		l.SelfBytesPerDay = d.SelfBytesPerDay
	}
	if l.SelfBytesPerDayAll <= 0 {
		l.SelfBytesPerDayAll = d.SelfBytesPerDayAll
	}
	if l.MacStreamsPerIP <= 0 {
		l.MacStreamsPerIP = d.MacStreamsPerIP
	}
	if l.TicketsRate <= 0 {
		l.TicketsRate = d.TicketsRate
	}
	if l.TicketsBurst <= 0 {
		l.TicketsBurst = d.TicketsBurst
	}
	return l
}

type limitsFrame struct {
	BytesPerDay int64   `json:"bytesPerDay"`
	BytesToday  int64   `json:"bytesToday"`
	Channels    int     `json:"channels"`
	Calls       int     `json:"calls"`
	Streams     int     `json:"streams"`
	Body        int     `json:"body"`
	Rate        float64 `json:"rate"`
	Burst       int     `json:"burst"`
	OpenRate    float64 `json:"openRate"`
	OpenBurst   int     `json:"openBurst"`
	IdleSeconds int     `json:"idleSeconds"`
}

type refusal struct {
	what  string
	retry time.Duration
	relay bool
}

type quota struct {
	lim Limits
	now func() time.Time
	log func(format string, args ...any)

	mu      sync.Mutex
	macs    map[string]*macQuota
	tickets map[string]*bucket
	global  map[string]time.Time
	swept   time.Time

	selfDay   int64
	selfBytes int64
}

type macQuota struct {
	day      int64
	bytes    int64
	self     bool
	handlers int
	pushing  bool
	rate     bucket
	puts     bucket
	logged   map[string]int64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newQuota(lim Limits, now func() time.Time, logf func(string, ...any)) *quota {
	return &quota{lim: lim, now: now, log: logf, macs: map[string]*macQuota{}, tickets: map[string]*bucket{}, global: map[string]time.Time{}}
}

func dayOf(t time.Time) int64 { return t.Unix() / int64(day/time.Second) }

func untilMidnight(t time.Time) time.Duration {
	u := t.UTC()
	next := time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(u)
}

func (q *quota) macLocked(mac string, now time.Time) *macQuota {
	m := q.macs[mac]
	if m == nil {
		m = &macQuota{
			day:    dayOf(now),
			rate:   bucket{tokens: float64(q.lim.Burst), last: now},
			puts:   bucket{tokens: float64(q.lim.TicketsBurst), last: now},
			logged: map[string]int64{},
		}
		q.macs[mac] = m
	}
	if d := dayOf(now); d != m.day {
		m.day, m.bytes = d, 0
	}
	return m
}

func (q *quota) selfLocked(now time.Time) int64 {
	if d := dayOf(now); d != q.selfDay {
		q.selfDay, q.selfBytes = d, 0
	}
	return q.selfBytes
}

func (q *quota) mark(mac string, self bool) {
	now := q.now()
	q.mu.Lock()
	q.macLocked(mac, now).self = self
	q.mu.Unlock()
}

func (q *quota) charge(mac string, n int64) {
	if n <= 0 {
		return
	}
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.macLocked(mac, now)
	m.bytes += n
	if m.self {
		q.selfLocked(now)
		q.selfBytes += n
	}
}

func (q *quota) used(mac string) int64 {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.macLocked(mac, now).bytes
}

func (q *quota) dailyLocked(m *macQuota) int64 {
	if m.self {
		return q.lim.SelfBytesPerDay
	}
	return q.lim.BytesPerDay
}

func (q *quota) bytesLocked(m *macQuota, now time.Time) refusal {
	if m.self && q.selfLocked(now) >= q.lim.SelfBytesPerDayAll {
		return refusal{what: QuotaSelfBytes, retry: untilMidnight(now), relay: true}
	}
	if m.bytes >= q.dailyLocked(m) {
		return refusal{what: QuotaBytes, retry: untilMidnight(now)}
	}
	return refusal{}
}

func (q *quota) enter(mac string) refusal {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.macLocked(mac, now)
	if m.handlers >= q.lim.MacHandlers {
		return refusal{what: QuotaMacCalls, retry: busyRetry}
	}
	m.handlers++
	return refusal{}
}

func (q *quota) leave(mac string) {
	now := q.now()
	q.mu.Lock()
	q.macLocked(mac, now).handlers--
	q.mu.Unlock()
}

func (q *quota) allowPush(mac string) refusal {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.macLocked(mac, now)
	if r := q.bytesLocked(m, now); r.what != "" {
		return r
	}
	if m.pushing {
		return refusal{what: QuotaPush, retry: busyRetry}
	}
	m.pushing = true
	return refusal{}
}

func (q *quota) pushed(mac string) {
	now := q.now()
	q.mu.Lock()
	q.macLocked(mac, now).pushing = false
	q.mu.Unlock()
}

func (q *quota) allowTickets(mac string) refusal {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	if wait, ok := q.macLocked(mac, now).puts.take(now, q.lim.TicketsRate, q.lim.TicketsBurst); !ok {
		return refusal{what: QuotaTickets, retry: wait}
	}
	return refusal{}
}

func (q *quota) allowCall(mac string) refusal {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.macLocked(mac, now)
	if r := q.bytesLocked(m, now); r.what != "" {
		return r
	}
	if wait, ok := m.rate.take(now, q.lim.Rate, q.lim.Burst); !ok {
		return refusal{what: QuotaRate, retry: wait}
	}
	return refusal{}
}

func (q *quota) allowOpen(mac, ticket string) refusal {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.macLocked(mac, now)
	if r := q.bytesLocked(m, now); r.what != "" {
		return r
	}
	tb := q.tickets[ticket]
	if tb == nil {
		tb = &bucket{tokens: float64(q.lim.OpenBurst), last: now}
		q.tickets[ticket] = tb
	}
	tb.refill(now, q.lim.OpenRate, q.lim.OpenBurst)
	if tb.tokens < 1 {
		return refusal{what: QuotaOpenRate, retry: tb.wait(q.lim.OpenRate)}
	}
	if wait, ok := m.rate.take(now, q.lim.Rate, q.lim.Burst); !ok {
		return refusal{what: QuotaRate, retry: wait}
	}
	tb.tokens--
	return refusal{}
}

func (q *quota) report(mac string, r refusal) {
	now := q.now()
	q.mu.Lock()
	m := q.macLocked(mac, now)
	first := m.logged[r.what] != m.day+1
	m.logged[r.what] = m.day + 1
	q.mu.Unlock()
	if first {
		q.log("quota %s %s", mac, r.what)
	}
}

func (q *quota) reportGlobal(what string) {
	now := q.now()
	q.mu.Lock()
	last, seen := q.global[what]
	first := !seen || now.Sub(last) >= sweepEvery
	if first {
		q.global[what] = now
	}
	q.mu.Unlock()
	if first {
		q.log("quota relay %s", what)
	}
}

func (q *quota) sweep(now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if now.Sub(q.swept) < sweepEvery {
		return
	}
	q.swept = now
	for h, b := range q.tickets {
		if now.Sub(b.last) >= bucketIdleTime {
			delete(q.tickets, h)
		}
	}
}

func (q *quota) limits(mac string) limitsFrame {
	now := q.now()
	q.mu.Lock()
	m := q.macLocked(mac, now)
	daily, used := q.dailyLocked(m), m.bytes
	q.mu.Unlock()
	return limitsFrame{
		BytesPerDay: daily,
		BytesToday:  used,
		Channels:    q.lim.Channels,
		Calls:       q.lim.Calls,
		Streams:     q.lim.Streams,
		Body:        MaxBody,
		Rate:        q.lim.Rate,
		Burst:       q.lim.Burst,
		OpenRate:    q.lim.OpenRate,
		OpenBurst:   q.lim.OpenBurst,
		IdleSeconds: int(q.lim.Idle / time.Second),
	}
}

func (b *bucket) refill(now time.Time, rate float64, burst int) {
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = math.Min(float64(burst), b.tokens+el*rate)
	}
	b.last = now
}

func (b *bucket) wait(rate float64) time.Duration {
	return time.Duration((1 - b.tokens) / rate * float64(time.Second))
}

func (b *bucket) take(now time.Time, rate float64, burst int) (time.Duration, bool) {
	b.refill(now, rate, burst)
	if b.tokens < 1 {
		return b.wait(rate), false
	}
	b.tokens--
	return 0, true
}

type fit int

const (
	fits fit = iota
	overMac
	overAll
)

type budget struct {
	max    int64
	macMax int64

	mu    sync.Mutex
	used  int64
	byMac map[string]int64
}

func newBudget(all, mac int64) *budget {
	return &budget{max: all, macMax: mac, byMac: map[string]int64{}}
}

func (b *budget) take(mac string, n int64) fit {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.byMac[mac]+n > b.macMax {
		return overMac
	}
	if b.used+n > b.max {
		return overAll
	}
	b.used += n
	b.byMac[mac] += n
	return fits
}

func (b *budget) force(mac string, n int64) {
	b.mu.Lock()
	b.used += n
	b.byMac[mac] += n
	b.mu.Unlock()
}

func (b *budget) give(mac string, n int64) {
	if n <= 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= n
	if left := b.byMac[mac] - n; left > 0 {
		b.byMac[mac] = left
	} else {
		delete(b.byMac, mac)
	}
}

func (b *budget) total() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

func (f fit) refusal() refusal {
	return refusal{what: QuotaBuffered, retry: busyRetry, relay: f == overAll}
}

type budgetReader struct {
	r    io.Reader
	b    *budget
	mac  string
	held int64
	busy fit
}

func (br *budgetReader) Read(p []byte) (int, error) {
	if len(p) > readChunk {
		p = p[:readChunk]
	}
	if f := br.b.take(br.mac, int64(len(p))); f != fits {
		br.busy = f
		return 0, errBusy
	}
	n, err := br.r.Read(p)
	br.b.give(br.mac, int64(len(p)-n))
	br.held += int64(n)
	return n, err
}

func retryAfter(d time.Duration) string {
	s := int64(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return strconv.FormatInt(s, 10)
}

func (r *Relay) refuseQuota(w http.ResponseWriter, mac string, q refusal) {
	if mac == "" || q.relay {
		r.quota.reportGlobal(q.what)
	} else {
		r.quota.report(mac, q)
	}
	w.Header().Set("Retry-After", retryAfter(q.retry))
	writeError(w, http.StatusTooManyRequests, "quota: "+q.what)
}
