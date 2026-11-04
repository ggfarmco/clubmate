package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/report"
)

const (
	maxUploads    = 4
	maxMacUploads = 2
	maxListed     = 100
	maxMetaJSON   = 64 << 10
	maxTracked    = 100_000
	replayWindow  = 10 * time.Minute
	idleBucket    = 24 * time.Hour
	minTransfer   = 10 * time.Second
	maxTransfer   = 2 * time.Minute
	minRate       = 64 << 10
	sweepInterval = time.Hour
	anonBucket    = "anon"
)

var (
	errReplayed  = errors.New("signature already used")
	errTooMany   = errors.New("too many reports")
	errBigMeta   = errors.New("report.json over 64 KiB")
	errNoLength  = errors.New("content length is required")
	errTooBig    = errors.New("report too big")
	errBanned    = errors.New("banned")
	errBadBody   = errors.New("bad request body")
	errNotStored = errors.New("could not store the report")
)

type reportLimits struct {
	keyHour *limiter
	keyDay  *limiter
	ip      *limiter
	anon    *limiter
	mac     *limiter
}

type replayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

type pacedBody struct {
	body     io.ReadCloser
	rc       *http.ResponseController
	deadline time.Time
	limit    time.Time
}

type inflight struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func newReportLimits(now func() time.Time) *reportLimits {
	return &reportLimits{
		keyHour: newLimiter(3.0/3600, 3, now),
		keyDay:  newLimiter(10.0/86400, 10, now),
		ip:      newLimiter(10.0/3600, 10, now),
		anon:    newLimiter(200.0/86400, 200, now),
		mac:     newLimiter(30.0/3600, 30, now),
	}
}

func newReplayCache() *replayCache {
	return &replayCache{seen: map[string]time.Time{}}
}

func newInflight() *inflight {
	return &inflight{keys: map[string]struct{}{}}
}

func (r *relay) postReport(w http.ResponseWriter, req *http.Request) {
	r.refresh()
	ip := clientIP(req)
	ipKey := bucketKey(ip)
	if r.bans.ip(ip) {
		http.Error(w, errBanned.Error(), http.StatusForbidden)
		return
	}
	mac, trusted := r.tokens.lookup(bearer(req))
	if !trusted && !r.limits.ip.allow(ipKey) {
		http.Error(w, errTooMany.Error(), http.StatusTooManyRequests)
		return
	}
	if req.ContentLength < 0 {
		http.Error(w, errNoLength.Error(), http.StatusLengthRequired)
		return
	}
	if req.ContentLength > report.MaxBundle {
		http.Error(w, errTooBig.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	slots := r.macUploads
	if !trusted {
		if !r.active.start(ipKey) {
			http.Error(w, errTooMany.Error(), http.StatusTooManyRequests)
			return
		}
		defer r.active.done(ipKey)
		slots = r.uploads
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		http.Error(w, errTooMany.Error(), http.StatusTooManyRequests)
		return
	}
	rc, start := http.NewResponseController(w), time.Now()
	paced := &pacedBody{body: req.Body, rc: rc, deadline: start.Add(r.grace), limit: start.Add(maxTransfer)}
	_ = rc.SetReadDeadline(paced.deadline)
	_ = rc.SetWriteDeadline(paced.limit)
	body, err := io.ReadAll(http.MaxBytesReader(w, paced, report.MaxBundle))
	_ = rc.SetWriteDeadline(time.Now().Add(minTransfer))
	if err != nil {
		w.Header().Set("Connection", "close")
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, errTooBig.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, errBadBody.Error(), http.StatusBadRequest)
		return
	}
	_ = rc.SetReadDeadline(time.Time{})
	now := r.now()
	pub, err := report.Verify(req, body, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	sig, _ := base64.RawURLEncoding.DecodeString(req.Header.Get(report.HeaderSignature))
	if r.seen.used(string(sig), now) {
		http.Error(w, errReplayed.Error(), http.StatusUnauthorized)
		return
	}
	hexKey := hex.EncodeToString(pub)
	if r.bans.key(hexKey) {
		http.Error(w, errBanned.Error(), http.StatusForbidden)
		return
	}
	if !r.limits.allow(hexKey, mac, trusted) {
		http.Error(w, errTooMany.Error(), http.StatusTooManyRequests)
		return
	}
	if err := r.seen.add(string(sig), now); err != nil {
		code := http.StatusTooManyRequests
		if errors.Is(err, errReplayed) {
			code = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), code)
		return
	}
	meta, err := report.ReadMeta(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if raw, err := encodeJSON(meta); err != nil || len(raw) > maxMetaJSON {
		http.Error(w, errBigMeta.Error(), http.StatusBadRequest)
		return
	}
	if err := r.reports.sweep(now); err != nil {
		r.log.Printf("sweep reports: %v", err)
	}
	st, err := r.reports.add(report.Stored{
		Received: now,
		Trusted:  trusted,
		Mac:      mac,
		Key:      report.KeyID(pub),
		IP:       ip.String(),
		Report:   meta,
	}, body)
	if errors.Is(err, errQuota) {
		http.Error(w, err.Error(), http.StatusInsufficientStorage)
		return
	}
	if err != nil {
		r.log.Printf("store report: %v", err)
		http.Error(w, errNotStored.Error(), http.StatusInternalServerError)
		return
	}
	from := mac
	if !trusted {
		from = "key " + st.Key
	}
	r.log.Printf("report %s %s %s %d", st.ID, from, st.IP, st.Bytes)
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": st.ID})
}

func (r *relay) listReports(w http.ResponseWriter, req *http.Request) {
	items, err := r.reports.list(req.URL.Query().Get("after"), maxListed)
	if err != nil {
		r.log.Printf("list reports: %v", err)
		http.Error(w, "could not list the reports", http.StatusInternalServerError)
		return
	}
	w.Header().Set("content-type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(items)
}

func (r *relay) reportBundle(w http.ResponseWriter, req *http.Request) {
	path, ok := r.reports.bundle(req.PathValue("id"))
	if !ok {
		http.NotFound(w, req)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, req)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "could not read the report", http.StatusInternalServerError)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(transferTime(info.Size())))
	w.Header().Set("content-type", "application/zip")
	w.Header().Set("content-length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, f)
}

func (r *relay) deleteReport(w http.ResponseWriter, req *http.Request) {
	if err := r.reports.remove(req.PathValue("id")); err != nil {
		r.log.Printf("delete report: %v", err)
		http.Error(w, "could not delete the report", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *relay) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if r.admins == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := r.admins.refresh(); err != nil {
			r.log.Printf("reload admins: %v", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, ok := r.admins.lookup(bearer(req)); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, req)
	}
}

func (r *relay) sweepEvery(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.housekeep(r.now())
		}
	}
}

func (r *relay) housekeep(now time.Time) {
	if err := r.reports.sweep(now); err != nil {
		r.log.Printf("sweep reports: %v", err)
	}
	for _, l := range []*limiter{r.limiter, r.limits.keyHour, r.limits.keyDay, r.limits.ip, r.limits.anon, r.limits.mac} {
		l.prune(now)
	}
	r.seen.prune(now)
}

func (r *relay) refresh() {
	if err := r.tokens.refresh(); err != nil {
		r.log.Printf("reload tokens: %v", err)
	}
	if err := r.bans.refresh(); err != nil {
		r.log.Printf("reload bans: %v", err)
	}
}

func (l *reportLimits) allow(hexKey, mac string, trusted bool) bool {
	if trusted {
		return l.mac.allow(mac)
	}
	if !l.keyHour.ready(hexKey) || !l.keyDay.ready(hexKey) || !l.anon.ready(anonBucket) {
		return false
	}
	return l.keyHour.allow(hexKey) && l.keyDay.allow(hexKey) && l.anon.allow(anonBucket)
}

func (c *replayCache) used(sig string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.seen[sig]
	return ok && !now.After(until)
}

func (c *replayCache) add(sig string, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.seen[sig]
	if ok && !now.After(until) {
		return errReplayed
	}
	if !ok && len(c.seen) >= maxTracked {
		return errTooMany
	}
	c.seen[sig] = now.Add(replayWindow)
	return nil
}

func (c *replayCache) prune(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for sig, until := range c.seen {
		if now.After(until) {
			delete(c.seen, sig)
		}
	}
}

func (p *pacedBody) Read(b []byte) (int, error) {
	n, err := p.body.Read(b)
	if n > 0 && err == nil {
		p.deadline = p.deadline.Add(time.Duration(n) * time.Second / minRate)
		if p.deadline.After(p.limit) {
			p.deadline = p.limit
		}
		_ = p.rc.SetReadDeadline(p.deadline)
	}
	return n, err
}

func (p *pacedBody) Close() error {
	return p.body.Close()
}

func (f *inflight) start(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.keys[key]; ok {
		return false
	}
	f.keys[key] = struct{}{}
	return true
}

func (f *inflight) done(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, key)
}

func transferTime(n int64) time.Duration {
	return max(minTransfer, time.Duration(n)*time.Second/minRate)
}

func bucketKey(addr netip.Addr) string {
	if addr.Is6() {
		return netip.PrefixFrom(addr, 64).Masked().String()
	}
	return addr.String()
}

func clientIP(req *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(req.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr := ap.Addr().Unmap()
	if !addr.IsLoopback() && !addr.IsPrivate() {
		return addr
	}
	hops := req.Header.Values("X-Forwarded-For")
	if len(hops) == 0 {
		return addr
	}
	last := hops[len(hops)-1]
	if i := strings.LastIndexByte(last, ','); i >= 0 {
		last = last[i+1:]
	}
	fwd, err := netip.ParseAddr(strings.TrimSpace(last))
	if err != nil {
		return addr
	}
	return fwd.Unmap()
}
