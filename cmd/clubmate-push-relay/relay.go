package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/apns"
)

const maxRequest = 16 << 10

var (
	priorities = map[string]int{
		apns.TypeAlert:      10,
		apns.TypeBackground: 5,
	}

	errBadDeviceToken = errors.New("token must be hex, 1-200 characters")
	errBadEnvironment = errors.New("environment must be development or production")
	errBadType        = errors.New("type must be alert or background")
	errBadCollapseID  = errors.New("collapseID must be at most 64 bytes")
	errBadPayload     = errors.New("payload must be a JSON object of at most 4096 bytes")
)

type relay struct {
	tokens  *tokens
	apns    sender
	limiter *limiter
	log     *log.Logger
}

type sender interface {
	Send(ctx context.Context, env, deviceToken string, p apns.Push) (apns.Reply, error)
}

type pushRequest struct {
	Token       string          `json:"token"`
	Environment string          `json:"environment"`
	Type        string          `json:"type"`
	CollapseID  string          `json:"collapseID"`
	Payload     json.RawMessage `json:"payload"`
}

type limiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(rate, burst float64, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: burst, now: now, buckets: map[string]*bucket{}}
}

func (r *relay) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/push", r.push)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func (r *relay) push(w http.ResponseWriter, req *http.Request) {
	if err := r.tokens.refresh(); err != nil {
		r.log.Printf("reload tokens: %v", err)
	}
	mac, ok := r.tokens.lookup(bearer(req))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !r.limiter.allow(mac) {
		http.Error(w, "too many pushes", http.StatusTooManyRequests)
		return
	}
	var p pushRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxRequest)).Decode(&p); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	if err := p.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reply, err := r.apns.Send(req.Context(), p.Environment, p.Token, apns.Push{
		Type:       p.Type,
		Priority:   priorities[p.Type],
		CollapseID: p.CollapseID,
		Payload:    p.Payload,
	})
	if err != nil {
		r.log.Printf("push %s %s: %v", mac, p.Type, err)
		http.Error(w, "APNs unreachable", http.StatusBadGateway)
		return
	}
	r.log.Printf("push %s %s %d %s", mac, p.Type, reply.Status, reply.Reason)
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

func (p pushRequest) validate() error {
	if p.Token == "" || !apns.ValidDeviceToken(p.Token) {
		return errBadDeviceToken
	}
	if p.Environment != apns.EnvDevelopment && p.Environment != apns.EnvProduction {
		return errBadEnvironment
	}
	if _, ok := priorities[p.Type]; !ok {
		return errBadType
	}
	if len(p.CollapseID) > apns.MaxCollapseID {
		return errBadCollapseID
	}
	if len(p.Payload) > apns.MaxPayload || !bytes.HasPrefix(bytes.TrimSpace(p.Payload), []byte("{")) {
		return errBadPayload
	}
	return nil
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func bearer(req *http.Request) string {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return token
}
