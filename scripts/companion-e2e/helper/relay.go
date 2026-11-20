package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"clubmate/internal/tunnelrelay"
)

const (
	deviceStreamPath = "/v1/d/stream"
	heldStatus       = http.StatusServiceUnavailable
)

type relayCall struct {
	At     time.Time `json:"at"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Status int       `json:"status"`
}

type relayState struct {
	mu      sync.Mutex
	calls   []*relayCall
	streams map[int]context.CancelFunc
	next    int
	hold    bool
}

func newTunnelRelay(token string) (*tunnelrelay.Relay, error) {
	return tunnelrelay.New(tunnelrelay.Config{
		Auth: func(bearer string) (string, bool) { return "e2e", bearer == token },
	})
}

func (h *helper) tunnel(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := &relayCall{At: time.Now(), Method: r.Method, Path: r.URL.Path}
		h.tun.mu.Lock()
		h.tun.calls = append(h.tun.calls, call)
		held := h.tun.hold && r.URL.Path == deviceStreamPath
		h.tun.mu.Unlock()
		sw := &statusWriter{ResponseWriter: w, record: func(code int) {
			h.tun.mu.Lock()
			call.Status = code
			h.tun.mu.Unlock()
		}}
		if held {
			sw.WriteHeader(heldStatus)
			return
		}
		if r.URL.Path == deviceStreamPath {
			ctx, cancel := context.WithCancel(r.Context())
			id := h.tun.track(cancel)
			defer h.tun.untrack(id)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(sw, r)
	})
}

func (s *relayState) track(cancel context.CancelFunc) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streams == nil {
		s.streams = map[int]context.CancelFunc{}
	}
	s.next++
	s.streams[s.next] = cancel
	return s.next
}

func (s *relayState) untrack(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.streams[id]; cancel != nil {
		cancel()
	}
	delete(s.streams, id)
}

func (h *helper) relayLog(w http.ResponseWriter, _ *http.Request) {
	h.tun.mu.Lock()
	out, err := json.Marshal(func() []relayCall {
		calls := make([]relayCall, 0, len(h.tun.calls))
		for _, c := range h.tun.calls {
			calls = append(calls, *c)
		}
		return calls
	}())
	h.tun.mu.Unlock()
	reply(w, out, err)
}

func (h *helper) clearRelayLog(w http.ResponseWriter, _ *http.Request) {
	h.tun.mu.Lock()
	h.tun.calls = nil
	h.tun.mu.Unlock()
	reply(w, json.RawMessage(`{}`), nil)
}

func (h *helper) relayStreams(w http.ResponseWriter, _ *http.Request) {
	h.tun.mu.Lock()
	out, err := json.Marshal(map[string]any{"open": len(h.tun.streams), "held": h.tun.hold})
	h.tun.mu.Unlock()
	reply(w, out, err)
}

func (h *helper) dropRelayStreams(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hold bool `json:"hold"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.tun.mu.Lock()
	h.tun.hold = req.Hold
	cancels := make([]context.CancelFunc, 0, len(h.tun.streams))
	for _, cancel := range h.tun.streams {
		cancels = append(cancels, cancel)
	}
	h.tun.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	reply(w, json.RawMessage(`{}`), nil)
}

func (h *helper) releaseRelayStreams(w http.ResponseWriter, _ *http.Request) {
	h.tun.mu.Lock()
	h.tun.hold = false
	h.tun.mu.Unlock()
	reply(w, json.RawMessage(`{}`), nil)
}

type statusWriter struct {
	http.ResponseWriter
	record func(int)
	sent   bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.sent {
		w.sent = true
		w.record(code)
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.sent {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if !w.sent {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
