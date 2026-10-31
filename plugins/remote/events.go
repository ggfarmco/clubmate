package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	streamBuffer    = 256
	pingInterval    = 15 * time.Second
	sseWriteTimeout = 30 * time.Second
	lastEventHeader = "Last-Event-ID"
	afterQuery      = "after"
)

var (
	forwardedTopics = []string{"agents.changed", "notify.posted", "plugin.changed"}

	errBadCursor = errors.New("invalid Last-Event-ID or after")
)

type hub struct {
	changed func()

	mu      sync.Mutex
	streams map[*stream]struct{}
}

type stream struct {
	device string
	events chan pluginsdk.Event
	done   chan struct{}
	once   sync.Once
}

type wireEvent struct {
	Seq       int64           `json:"seq"`
	Topic     string          `json:"topic"`
	Workspace string          `json:"workspace"`
	Payload   json.RawMessage `json:"payload"`
}

type workspacePayload struct {
	Workspace string `json:"workspace"`
}

func newHub(changed func()) *hub {
	return &hub{changed: changed, streams: map[*stream]struct{}{}}
}

func (h *hub) open(device string, valid func() bool) (*stream, bool) {
	st := &stream{device: device, events: make(chan pluginsdk.Event, streamBuffer), done: make(chan struct{})}
	h.mu.Lock()
	if !valid() {
		h.mu.Unlock()
		return nil, false
	}
	first := !h.connectedLocked(device)
	h.streams[st] = struct{}{}
	h.mu.Unlock()
	if first {
		h.changed()
	}
	return st, true
}

func (h *hub) close(st *stream) {
	st.stop()
	h.mu.Lock()
	delete(h.streams, st)
	last := !h.connectedLocked(st.device)
	h.mu.Unlock()
	if last {
		h.changed()
	}
}

func (h *hub) publish(ev pluginsdk.Event) {
	if !slices.Contains(forwardedTopics, ev.Topic) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for st := range h.streams {
		select {
		case st.events <- ev:
		default:
			st.stop()
		}
	}
}

func (h *hub) revoke(device string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for st := range h.streams {
		if st.device == device {
			st.stop()
		}
	}
}

func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for st := range h.streams {
		st.stop()
	}
}

func (h *hub) connected(device string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connectedLocked(device)
}

func (h *hub) connectedLocked(device string) bool {
	for st := range h.streams {
		if st.device == device {
			return true
		}
	}
	return false
}

func (st *stream) stop() {
	st.once.Do(func() { close(st.done) })
}

func (s *server) events(w http.ResponseWriter, r *http.Request, d device) {
	after, resume, err := resumePoint(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParams, err.Error())
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	valid := func() bool { return s.devices.valid(d) }
	st, ok := s.hub.open(d.ID, valid)
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "unknown or revoked device token")
		return
	}
	defer s.hub.close(st)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if !s.send(rc, w, ": connected\n\n") {
		return
	}
	var seen int64
	if resume {
		head, err := s.backend.Head(r.Context())
		if err != nil {
			return
		}
		if after <= head {
			if seen, err = s.backfill(r.Context(), rc, w, after); err != nil {
				return
			}
		}
	}
	ping := time.NewTicker(s.ping)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-st.done:
			return
		case ev := <-st.events:
			if ev.Seq <= seen {
				continue
			}
			if !s.send(rc, w, sseFrame(ev)) {
				return
			}
		case <-ping.C:
			if !valid() || !s.send(rc, w, ": ping\n\n") {
				return
			}
		}
	}
}

func (s *server) backfill(ctx context.Context, rc *http.ResponseController, w io.Writer, after int64) (int64, error) {
	for {
		page, err := s.backend.Events(ctx, after)
		if err != nil {
			return after, err
		}
		for _, ev := range page {
			after = ev.Seq
			if !slices.Contains(forwardedTopics, ev.Topic) {
				continue
			}
			if !s.send(rc, w, sseFrame(ev)) {
				return after, errStreamClosed
			}
		}
		if len(page) < busPageSize {
			return after, nil
		}
	}
}

func (s *server) send(rc *http.ResponseController, w io.Writer, frame string) bool {
	_ = rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	if _, err := io.WriteString(w, frame); err != nil {
		return false
	}
	return rc.Flush() == nil
}

func resumePoint(r *http.Request) (int64, bool, error) {
	raw := r.Header.Get(lastEventHeader)
	if raw == "" {
		raw = r.URL.Query().Get(afterQuery)
	}
	if raw == "" {
		return 0, false, nil
	}
	after, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || after < 0 {
		return 0, false, errBadCursor
	}
	return after, true, nil
}

func sseFrame(ev pluginsdk.Event) string {
	var p workspacePayload
	_ = json.Unmarshal(ev.Payload, &p)
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	data, _ := json.Marshal(wireEvent{Seq: ev.Seq, Topic: ev.Topic, Workspace: p.Workspace, Payload: payload})
	seq := strconv.FormatInt(ev.Seq, 10)
	return "id: " + seq + "\nevent: bus\ndata: " + string(data) + "\n\n"
}
