package tunnelrelay

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	PingEvery     = 25 * time.Second
	Liveness      = 2*PingEvery + 5*time.Second
	InlineMax     = 64 << 10
	MaxBody       = 17 << 20
	MaxInFlight   = 90
	BackoffFirst  = time.Second
	BackoffMax    = 30 * time.Second
	BackoffJitter = 0.2
	BackoffReset  = 60 * time.Second

	OpenWait = 10 * time.Second
	CallWait = 2*time.Minute + 10*time.Second
	HeadWait = 3 * time.Second

	HeaderRelay   = "X-Clubmate-Relay"
	HeaderTicket  = "X-Clubmate-Ticket"
	HeaderChannel = "X-Clubmate-Channel"
	HeaderStream  = "X-Clubmate-Stream"

	NotCurrentStream = "not this Mac's current stream"

	channelPrefix = "ch_"
	maxMsg1       = 4 << 10
	maxMsg2       = 4 << 10
	maxTicketsPut = 16 << 10
	transferTime  = 5 * time.Minute
)

type Config struct {
	Auth        func(bearer string) (mac string, ok bool)
	TicketsPath string
	Log         *log.Logger
	Now         func() time.Time
	Limits      Limits
}

type Relay struct {
	auth    func(string) (string, bool)
	log     *log.Logger
	now     func() time.Time
	tickets *tickets
	route   *router
	quota   *quota
	buf     *budget
	lim     Limits
	active  atomic.Int64

	ping     time.Duration
	openWait time.Duration
	callWait time.Duration
	headWait time.Duration
}

func New(cfg Config) (*Relay, error) {
	if cfg.Auth == nil {
		return nil, errors.New("tunnelrelay: Auth is required")
	}
	if cfg.Log == nil {
		cfg.Log = log.New(io.Discard, "", 0)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	t, err := loadTickets(cfg.TicketsPath, cfg.Now)
	if err != nil {
		return nil, err
	}
	lim := cfg.Limits.withDefaults()
	buf := &budget{max: lim.Buffered}
	r := &Relay{
		auth:     cfg.Auth,
		log:      cfg.Log,
		now:      cfg.Now,
		tickets:  t,
		route:    newRouter(t, lim, buf),
		buf:      buf,
		lim:      lim,
		ping:     PingEvery,
		openWait: OpenWait,
		callWait: CallWait,
		headWait: HeadWait,
	}
	r.quota = newQuota(lim, cfg.Now, r.logf)
	return r, nil
}

func (r *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/mac/stream", r.macStream)
	mux.HandleFunc("POST /v1/mac/open/{ch}", r.macOpenReply)
	mux.HandleFunc("GET /v1/mac/req/{id}/body", r.macPullBody)
	mux.HandleFunc("POST /v1/mac/reply/{id}", r.macReply)
	mux.HandleFunc("POST /v1/mac/push", r.macPush)
	mux.HandleFunc("POST /v1/mac/close/{ch}", r.macClose)
	mux.HandleFunc("PUT /v1/mac/tickets", r.putTickets)
	mux.HandleFunc("POST /v1/d/open", r.deviceOpen)
	mux.HandleFunc("POST /v1/d/call", r.deviceCall)
	mux.HandleFunc("POST /v1/d/stream", r.deviceStream)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rw := &relayWriter{ResponseWriter: w}
		rw.Header().Set(HeaderRelay, "1")
		defer r.recover(rw, req)
		if r.active.Add(1) > int64(r.lim.Handlers) {
			r.active.Add(-1)
			r.refuseQuota(rw, "", refusal{what: QuotaHandlers, retry: busyRetry})
			return
		}
		defer r.active.Add(-1)
		mux.ServeHTTP(rw, req)
	})
}

func (r *Relay) Shutdown() {
	r.route.mu.Lock()
	macs := make([]*macConn, 0, len(r.route.macs))
	for _, m := range r.route.macs {
		macs = append(macs, m)
	}
	r.route.mu.Unlock()
	for _, m := range macs {
		m.end(endDropped)
	}
}

func (r *Relay) recover(w *relayWriter, req *http.Request) {
	v := recover()
	if v == nil {
		return
	}
	if v == http.ErrAbortHandler {
		panic(v)
	}
	r.log.Printf("tunnel panic %s %s: %v", req.Method, req.URL.Path, v)
	if w.wrote {
		panic(http.ErrAbortHandler)
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (r *Relay) macAuth(w http.ResponseWriter, req *http.Request) (string, string, bool) {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", "", false
	}
	mac, ok := r.auth(token)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", "", false
	}
	return mac, token, true
}

func (r *Relay) macCurrent(w http.ResponseWriter, req *http.Request) (string, bool) {
	mac, _, ok := r.macAuth(w, req)
	if !ok {
		return "", false
	}
	if !r.route.current(mac, req.Header.Get(HeaderStream)) {
		writeError(w, http.StatusConflict, NotCurrentStream)
		return "", false
	}
	return mac, true
}

func (r *Relay) logf(format string, args ...any) { r.log.Printf(format, args...) }

type relayWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *relayWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *relayWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

func (w *relayWriter) Flush() { _ = w.FlushError() }

func (w *relayWriter) FlushError() error {
	w.wrote = true
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *relayWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func longLived(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
}

func transfer(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	until := time.Now().Add(transferTime)
	_ = rc.SetReadDeadline(until)
	_ = rc.SetWriteDeadline(until)
}

func startStream(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	http.NewResponseController(w).Flush()
}

func (r *Relay) readHeld(w http.ResponseWriter, req *http.Request, limit int64) ([]byte, int64, bool) {
	br := &budgetReader{r: http.MaxBytesReader(w, req.Body, limit), b: r.buf}
	b, err := io.ReadAll(br)
	if br.busy {
		r.buf.give(br.held)
		r.refuseQuota(w, "", refusal{what: QuotaBuffered, retry: busyRetry})
		return nil, 0, false
	}
	if !bodyOK(w, err) {
		r.buf.give(br.held)
		return nil, 0, false
	}
	return b, br.held, true
}

func readBody(w http.ResponseWriter, req *http.Request, limit int64) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, req.Body, limit))
	return b, bodyOK(w, err)
}

func bodyOK(w http.ResponseWriter, err error) bool {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		return false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad body")
		return false
	}
	return true
}
