package tunnelrelay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"
)

const (
	KindCall   = "call"
	KindStream = "stream"

	WhyDeviceGone    = "device-gone"
	WhyTicketRevoked = "ticket-revoked"
	WhyIdle          = "idle"
	WhyTimeout       = "timeout"
	WhyCut           = "cut"

	endReplaced = "replaced"
	endDropped  = "dropped"
	endRevoked  = "revoked"
	endSlow     = "slow"

	macQueue    = 4096
	streamQueue = 1024
	maxGone     = 4096
)

type router struct {
	mu      sync.Mutex
	lim     Limits
	buf     *budget
	macs    map[string]*macConn
	chans   map[string]*channel
	opens   map[string]*opening
	reqs    map[string]*pending
	gone    map[string][]string
	load    map[string]*macLoad
	ips     map[string]int
	swept   time.Time
	tickets *tickets

	curMu sync.Mutex
	cur   map[string]string
}

type macLoad struct {
	chans   map[string]*channel
	calls   int
	streams int
}

type macConn struct {
	id     string
	name   string
	ip     string
	frames chan []byte
	done   chan struct{}
	once   sync.Once
	why    string
	buf    *budget
	wake   func()
	mu     sync.Mutex
	closed bool
}

type channel struct {
	id     string
	mac    string
	ticket string
	opened time.Time
	used   time.Time
	reqs   map[string]*pending
}

type opening struct {
	ch      string
	mac     string
	conn    *macConn
	ticket  string
	msg2    chan []byte
	cut     chan struct{}
	left    chan struct{}
	once    sync.Once
	revoked bool
}

type pending struct {
	id      string
	ch      *channel
	mac     string
	conn    *macConn
	kind    string
	body    []byte
	held    int64
	reply   chan *replyPipe
	items   chan item
	cut     chan struct{}
	left    chan struct{}
	once    sync.Once
	ended   bool
	unknown bool
}

type item struct {
	data []byte
	end  bool
}

type replyPipe struct {
	body   io.Reader
	abort  func()
	result chan pipeResult
}

type pipeResult struct {
	n        int64
	readErr  error
	writeErr error
	cut      bool
}

type denial struct {
	code  int
	quota refusal
}

const (
	sent = iota
	sendGone
	sendFull
	sendBusy
)

func newRouter(t *tickets, lim Limits, buf *budget) *router {
	return &router{
		lim:     lim,
		buf:     buf,
		macs:    map[string]*macConn{},
		chans:   map[string]*channel{},
		opens:   map[string]*opening{},
		reqs:    map[string]*pending{},
		gone:    map[string][]string{},
		cur:     map[string]string{},
		load:    map[string]*macLoad{},
		ips:     map[string]int{},
		tickets: t,
	}
}

func newID(prefix string, n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func short(ch string) string {
	if len(ch) > len(channelPrefix)+8 {
		return ch[len(channelPrefix) : len(channelPrefix)+8]
	}
	return ch
}

func quotaDenial(what string, retry time.Duration) denial {
	return denial{code: http.StatusTooManyRequests, quota: refusal{what: what, retry: retry}}
}

func busyDenial(res int) denial {
	f := overMac
	if res == sendBusy {
		f = overAll
	}
	return denial{code: http.StatusTooManyRequests, quota: f.refusal()}
}

func (m *macConn) end(why string) {
	m.once.Do(func() {
		m.why = why
		close(m.done)
	})
}

func (m *macConn) halt(why string) {
	m.end(why)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.wake()
	}
}

func (m *macConn) send(frame []byte, force bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return sendGone
	}
	select {
	case <-m.done:
		return sendGone
	default:
	}
	n := int64(len(frame))
	if force {
		m.buf.force(m.name, n)
	} else {
		switch m.buf.take(m.name, n) {
		case overMac:
			return sendFull
		case overAll:
			return sendBusy
		}
	}
	select {
	case m.frames <- frame:
		return sent
	default:
		m.buf.give(m.name, n)
		m.end(endSlow)
		m.wake()
		return sendGone
	}
}

func (m *macConn) drain() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for {
		select {
		case f := <-m.frames:
			m.buf.give(m.name, int64(len(f)))
		default:
			return
		}
	}
}

func (o *opening) stop() { o.once.Do(func() { close(o.cut) }) }

func (p *pending) stop() { p.once.Do(func() { close(p.cut) }) }

func (p *pending) stopUnknown() {
	p.once.Do(func() {
		p.unknown = true
		close(p.cut)
	})
}

func event(name string, data any) []byte {
	raw, _ := json.Marshal(data)
	return []byte("event: " + name + "\ndata: " + string(raw) + "\n\n")
}

func (r *router) attach(name, ip string, wake func()) (*macConn, string) {
	m := &macConn{id: newID("st_", 16), name: name, ip: ip, frames: make(chan []byte, macQueue), done: make(chan struct{}), buf: r.buf, wake: wake}
	r.mu.Lock()
	old := r.macs[name]
	if old == nil && len(r.macs) >= r.lim.MacStreams {
		r.mu.Unlock()
		return nil, QuotaMacs
	}
	if (old == nil || old.ip != ip) && r.ips[ip] >= r.lim.MacStreamsPerIP {
		r.mu.Unlock()
		return nil, QuotaMacsPerIP
	}
	if old != nil {
		r.leaveIPLocked(old.ip)
	}
	r.ips[ip]++
	r.macs[name] = m
	r.curMu.Lock()
	r.cur[name] = m.id
	r.curMu.Unlock()
	r.mu.Unlock()
	if old != nil {
		old.end(endReplaced)
	}
	return m, ""
}

func (r *router) leaveIPLocked(ip string) {
	if r.ips[ip] <= 1 {
		delete(r.ips, ip)
		return
	}
	r.ips[ip]--
}

func (r *router) detach(m *macConn) {
	m.drain()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.macs[m.name] == m {
		delete(r.macs, m.name)
		r.leaveIPLocked(m.ip)
		r.curMu.Lock()
		delete(r.cur, m.name)
		r.curMu.Unlock()
	}
	for id, o := range r.opens {
		if o.conn == m {
			delete(r.opens, id)
			o.stop()
		}
	}
	for id, p := range r.reqs {
		if p.conn == m {
			r.dropLocked(id, p)
			p.stop()
		}
	}
	if m.why == endRevoked {
		for _, c := range r.chans {
			if c.mac == m.name {
				r.dropChanLocked(c)
			}
		}
	}
}

func (r *router) current(name, stream string) bool {
	r.curMu.Lock()
	defer r.curMu.Unlock()
	return stream != "" && r.cur[name] == stream
}

func (r *router) mac(name string) *macConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.macs[name]
}

func (r *router) loadLocked(mac string) *macLoad {
	l := r.load[mac]
	if l == nil {
		l = &macLoad{chans: map[string]*channel{}}
		r.load[mac] = l
	}
	return l
}

func (r *router) tidyLocked(mac string) {
	if l := r.load[mac]; l != nil && len(l.chans) == 0 && l.calls == 0 && l.streams == 0 {
		delete(r.load, mac)
	}
}

func (r *router) dropChanLocked(c *channel) {
	if r.chans[c.id] == c {
		delete(r.chans, c.id)
	}
	if l := r.load[c.mac]; l != nil {
		delete(l.chans, c.id)
	}
	r.tidyLocked(c.mac)
}

func (r *router) sweepLocked(now time.Time, force bool) []*channel {
	if !force && now.Sub(r.swept) < sweepEvery {
		return nil
	}
	r.swept = now
	var idle []*channel
	for _, c := range r.chans {
		if len(c.reqs) == 0 && now.Sub(c.used) >= r.lim.Idle {
			r.dropChanLocked(c)
			idle = append(idle, c)
		}
	}
	return idle
}

func (r *router) sweep(now time.Time) {
	r.mu.Lock()
	idle := r.sweepLocked(now, false)
	conns := r.connsLocked(idle)
	r.mu.Unlock()
	r.closeIdle(idle, conns)
}

func (r *router) connsLocked(chans []*channel) []*macConn {
	conns := make([]*macConn, len(chans))
	for i, c := range chans {
		conns[i] = r.macs[c.mac]
	}
	return conns
}

func (r *router) closeIdle(chans []*channel, conns []*macConn) {
	for i, c := range chans {
		if conns[i] != nil {
			conns[i].send(event("close", closeFrame{Ch: c.id, Why: WhyIdle}), true)
		}
	}
}

func (r *router) opensLocked(mac string) int {
	n := 0
	for _, o := range r.opens {
		if o.mac == mac {
			n++
		}
	}
	return n
}

func (l *macLoad) unused() *channel {
	var old *channel
	for _, c := range l.chans {
		if len(c.reqs) == 0 && (old == nil || c.used.Before(old.used)) {
			old = c
		}
	}
	return old
}

func (r *router) startOpen(mac, ticket string, msg1 []byte, now time.Time) (*opening, denial) {
	o := &opening{ch: newID(channelPrefix, 12), mac: mac, ticket: ticket, msg2: make(chan []byte), cut: make(chan struct{}), left: make(chan struct{})}
	r.mu.Lock()
	m := r.macs[mac]
	if m == nil {
		r.mu.Unlock()
		return nil, denial{code: http.StatusServiceUnavailable}
	}
	idle := r.sweepLocked(now, false)
	if len(r.chans)+len(r.opens) >= r.lim.AllChannels {
		idle = append(idle, r.sweepLocked(now, true)...)
	}
	if len(r.chans)+len(r.opens) >= r.lim.AllChannels {
		conns := r.connsLocked(idle)
		r.mu.Unlock()
		r.closeIdle(idle, conns)
		return nil, denial{code: http.StatusTooManyRequests, quota: refusal{what: QuotaAllChans, retry: busyRetry, relay: true}}
	}
	l := r.loadLocked(mac)
	if len(l.chans)+r.opensLocked(mac) >= r.lim.Channels {
		c := l.unused()
		if c == nil {
			conns := r.connsLocked(idle)
			r.tidyLocked(mac)
			r.mu.Unlock()
			r.closeIdle(idle, conns)
			return nil, quotaDenial(QuotaChannels, busyRetry)
		}
		r.dropChanLocked(c)
		idle = append(idle, c)
	}
	o.conn = m
	r.opens[o.ch] = o
	conns := r.connsLocked(idle)
	r.mu.Unlock()
	r.closeIdle(idle, conns)
	switch res := m.send(event("open", openFrame{Ch: o.ch, D: msg1}), false); res {
	case sendFull, sendBusy:
		r.endOpen(o)
		return nil, busyDenial(res)
	case sendGone:
		r.endOpen(o)
		return nil, denial{code: http.StatusServiceUnavailable}
	}
	return o, denial{}
}

func (r *router) endOpen(o *opening) {
	close(o.left)
	r.mu.Lock()
	if r.opens[o.ch] == o {
		delete(r.opens, o.ch)
	}
	r.mu.Unlock()
}

func (r *router) takeOpen(mac, ch string) *opening {
	r.mu.Lock()
	defer r.mu.Unlock()
	o := r.opens[ch]
	if o == nil || o.mac != mac {
		return nil
	}
	delete(r.opens, ch)
	return o
}

func (r *router) addChannel(o *opening, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.tickets.has(o.mac, o.ticket) {
		return false
	}
	c := &channel{id: o.ch, mac: o.mac, ticket: o.ticket, opened: now, used: now, reqs: map[string]*pending{}}
	r.chans[o.ch] = c
	r.loadLocked(o.mac).chans[o.ch] = c
	return true
}

func (r *router) channel(ch, mac, ticket string) *channel {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.chans[ch]
	if c == nil || c.mac != mac || c.ticket != ticket {
		return nil
	}
	return c
}

func (r *router) startReq(c *channel, kind string, body []byte, held int64, now time.Time) (*pending, denial) {
	p := &pending{id: newID("", 16), ch: c, mac: c.mac, kind: kind, cut: make(chan struct{}), left: make(chan struct{})}
	f := reqFrame{ID: p.id, Ch: c.id, Kind: kind, N: len(body)}
	if len(body) <= InlineMax {
		f.D = body
	}
	if kind == KindCall {
		p.reply = make(chan *replyPipe)
	} else {
		p.items = make(chan item, streamQueue)
	}
	r.mu.Lock()
	if r.chans[c.id] != c {
		r.mu.Unlock()
		r.buf.give(c.mac, held)
		return nil, denial{code: http.StatusGone}
	}
	m := r.macs[c.mac]
	if m == nil {
		r.mu.Unlock()
		r.buf.give(c.mac, held)
		return nil, denial{code: http.StatusServiceUnavailable}
	}
	l := r.loadLocked(c.mac)
	if kind == KindCall && l.calls >= r.lim.Calls {
		r.mu.Unlock()
		r.buf.give(c.mac, held)
		return nil, quotaDenial(QuotaCalls, busyRetry)
	}
	if kind == KindStream && l.streams >= r.lim.Streams {
		r.mu.Unlock()
		r.buf.give(c.mac, held)
		return nil, quotaDenial(QuotaStreams, busyRetry)
	}
	if kind == KindCall {
		l.calls++
	} else {
		l.streams++
	}
	if len(body) > InlineMax {
		p.body, p.held = body, held
	} else {
		r.buf.give(c.mac, held)
	}
	p.conn = m
	r.reqs[p.id] = p
	c.reqs[p.id] = p
	c.used = now
	r.mu.Unlock()
	switch res := m.send(event("req", f), false); res {
	case sendFull, sendBusy:
		r.endReq(p, "", now)
		return nil, busyDenial(res)
	case sendGone:
		r.endReq(p, "", now)
		return nil, denial{code: http.StatusServiceUnavailable}
	}
	return p, denial{}
}

func (r *router) endReq(p *pending, why string, now time.Time) {
	close(p.left)
	r.mu.Lock()
	_, open := r.reqs[p.id]
	r.dropLocked(p.id, p)
	if why == WhyDeviceGone && p.kind == KindStream && !p.ended {
		r.addGoneLocked(p.mac, p.id)
	}
	r.buf.give(p.mac, p.held)
	p.held, p.body = 0, nil
	if p.items != nil {
		for drained := false; !drained; {
			select {
			case it := <-p.items:
				r.buf.give(p.mac, int64(len(it.data)))
			default:
				drained = true
			}
		}
	}
	if now.After(p.ch.used) {
		p.ch.used = now
	}
	m := r.macs[p.mac]
	r.mu.Unlock()
	if why != "" && open && m != nil {
		m.send(event("close", closeFrame{Ch: p.ch.id, ID: p.id, Why: why}), true)
	}
}

func (r *router) dropLocked(id string, p *pending) {
	if r.reqs[id] == p {
		delete(r.reqs, id)
	}
	if _, ok := p.ch.reqs[id]; !ok {
		return
	}
	delete(p.ch.reqs, id)
	if l := r.load[p.mac]; l != nil {
		if p.kind == KindCall {
			l.calls--
		} else {
			l.streams--
		}
	}
	r.tidyLocked(p.mac)
}

func (r *router) addGoneLocked(mac, id string) {
	g := r.gone[mac]
	if slices.Contains(g, id) {
		return
	}
	if len(g) >= maxGone {
		g = g[1:]
	}
	r.gone[mac] = append(g, id)
}

func (r *router) takeCall(id, mac string) *pending {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.reqs[id]
	if p == nil || p.mac != mac || p.kind != KindCall {
		return nil
	}
	delete(r.reqs, id)
	return p
}

func (r *router) takeBody(id, mac string) ([]byte, int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.reqs[id]
	if p == nil || p.mac != mac || p.body == nil {
		return nil, 0, false
	}
	b, held := p.body, p.held
	p.body, p.held = nil, 0
	return b, held, true
}

func (r *router) deliver(mac, id string, it item) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.reqs[id]
	if p == nil || p.mac != mac || p.kind != KindStream {
		r.addGoneLocked(mac, id)
		return false
	}
	n := int64(len(it.data))
	if r.buf.take(mac, n) == fits {
		select {
		case p.items <- it:
			if it.end {
				p.ended = true
				r.dropLocked(id, p)
			}
			return true
		default:
			r.buf.give(mac, n)
		}
	}
	r.dropLocked(id, p)
	r.addGoneLocked(mac, id)
	p.stop()
	return false
}

func (r *router) takeGone(mac string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.gone[mac]
	delete(r.gone, mac)
	return g
}

func (r *router) closeChannel(mac, ch string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.chans[ch]
	if c == nil || c.mac != mac {
		return false
	}
	r.dropChanLocked(c)
	for id, p := range c.reqs {
		r.dropLocked(id, p)
		p.stopUnknown()
	}
	return true
}

func (r *router) closeChannels(mac string, keep func(ticket string) bool) []string {
	r.mu.Lock()
	var closed []*channel
	if l := r.load[mac]; l != nil {
		for _, c := range l.chans {
			if !keep(c.ticket) {
				r.dropChanLocked(c)
				closed = append(closed, c)
			}
		}
	}
	for id, o := range r.opens {
		if o.mac == mac && !keep(o.ticket) {
			delete(r.opens, id)
			o.revoked = true
			o.stop()
		}
	}
	for _, c := range closed {
		for id, p := range c.reqs {
			r.dropLocked(id, p)
			p.stop()
		}
	}
	m := r.macs[mac]
	r.mu.Unlock()
	ids := make([]string, 0, len(closed))
	for _, c := range closed {
		ids = append(ids, c.id)
		if m != nil {
			m.send(event("close", closeFrame{Ch: c.id, Why: WhyTicketRevoked}), true)
		}
	}
	return ids
}

type openFrame struct {
	Ch string `json:"ch"`
	D  []byte `json:"d"`
}

type reqFrame struct {
	ID   string `json:"id"`
	Ch   string `json:"ch"`
	Kind string `json:"kind"`
	N    int    `json:"n"`
	D    []byte `json:"d,omitempty"`
}

type closeFrame struct {
	Ch  string `json:"ch"`
	ID  string `json:"id,omitempty"`
	Why string `json:"why"`
}
