package tunnelrelay

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"clubmate/pkg/tunnel"
)

var (
	ping     = []byte(": ping\n\n")
	replaced = []byte("event: replaced\ndata: {}\n\n")
)

type ticketsRequest struct {
	Tickets []Ticket `json:"tickets"`
}

type pushReply struct {
	Gone []string `json:"gone"`
}

func (r *Relay) macStream(w http.ResponseWriter, req *http.Request) {
	mac, token, ok := r.macAuth(w, req)
	if !ok {
		return
	}
	m := r.route.attach(mac)
	if m == nil {
		r.refuseQuota(w, "", refusal{what: QuotaMacs, retry: macsRetry})
		return
	}
	longLived(w)
	w.Header().Set(HeaderStream, m.id)
	startStream(w, "text/event-stream")
	r.logf("mac %s stream open", mac)
	defer func() {
		r.route.detach(m)
		if m.why == endReplaced {
			r.logf("mac %s stream replaced", mac)
		} else {
			r.logf("mac %s stream close", mac)
		}
	}()
	t := time.NewTicker(r.ping)
	defer t.Stop()
	rc := http.NewResponseController(w)
	write := func(b []byte) bool {
		if _, err := w.Write(b); err != nil {
			m.end(endDropped)
			return false
		}
		if err := rc.Flush(); err != nil {
			m.end(endDropped)
			return false
		}
		return true
	}
	if !write(event("limits", r.quota.limits(mac))) {
		return
	}
	for {
		select {
		case f := <-m.frames:
			m.buf.give(int64(len(f)))
			if !write(f) {
				return
			}
		case <-t.C:
			if who, ok := r.auth(token); !ok || who != mac {
				m.end(endRevoked)
				continue
			}
			now := r.now()
			r.route.sweep(now)
			r.quota.sweep(now)
			if !write(ping) {
				return
			}
		case <-m.done:
			if m.why == endReplaced {
				write(replaced)
			}
			return
		case <-req.Context().Done():
			m.end(endDropped)
			return
		}
	}
}

func (r *Relay) macOpenReply(w http.ResponseWriter, req *http.Request) {
	mac, _, ok := r.macAuth(w, req)
	if !ok {
		return
	}
	transfer(w)
	msg2, ok := readBody(w, req, maxMsg2)
	if !ok {
		return
	}
	o := r.route.takeOpen(mac, req.PathValue("ch"))
	if o == nil {
		writeError(w, http.StatusGone, "device left")
		return
	}
	select {
	case o.msg2 <- msg2:
		w.WriteHeader(http.StatusNoContent)
	case <-o.left:
		writeError(w, http.StatusGone, "device left")
	}
}

func (r *Relay) macPullBody(w http.ResponseWriter, req *http.Request) {
	mac, _, ok := r.macAuth(w, req)
	if !ok {
		return
	}
	body, held, ok := r.route.takeBody(req.PathValue("id"), mac)
	if !ok {
		writeError(w, http.StatusGone, "device left")
		return
	}
	defer r.buf.give(held)
	transfer(w)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (r *Relay) macReply(w http.ResponseWriter, req *http.Request) {
	mac, _, ok := r.macAuth(w, req)
	if !ok {
		return
	}
	p := r.route.takeCall(req.PathValue("id"), mac)
	if p == nil {
		writeError(w, http.StatusGone, "device left")
		return
	}
	transfer(w)
	rp := &replyPipe{
		body:   http.MaxBytesReader(w, req.Body, MaxBody),
		abort:  func() { _ = http.NewResponseController(w).SetReadDeadline(time.Now()) },
		result: make(chan pipeResult, 1),
	}
	select {
	case p.reply <- rp:
	case <-p.left:
		writeError(w, http.StatusGone, "device left")
		return
	}
	res := <-rp.result
	var tooBig *http.MaxBytesError
	switch {
	case res.cut:
		writeError(w, http.StatusGone, "channel closed")
	case errors.As(res.readErr, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, "body too large")
	case res.readErr != nil:
		writeError(w, http.StatusBadRequest, "bad body")
	case res.writeErr != nil:
		writeError(w, http.StatusGone, "device left")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (r *Relay) macPush(w http.ResponseWriter, req *http.Request) {
	mac, ok := r.macCurrent(w, req)
	if !ok {
		return
	}
	if q := r.quota.allowBytes(mac); q.what != "" {
		r.refuseQuota(w, mac, q)
		return
	}
	transfer(w)
	pr := tunnel.NewPushReader(http.MaxBytesReader(w, req.Body, MaxBody))
	for {
		it, err := pr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad push batch")
			return
		}
		if r.route.deliver(mac, hex.EncodeToString(it.ID[:]), item{data: it.Data, end: it.Kind == tunnel.PushEnd}) {
			r.quota.charge(mac, int64(len(it.Data)))
		}
	}
	gone := r.route.takeGone(mac)
	if gone == nil {
		gone = []string{}
	}
	writeJSON(w, pushReply{Gone: gone})
}

func (r *Relay) macClose(w http.ResponseWriter, req *http.Request) {
	mac, ok := r.macCurrent(w, req)
	if !ok {
		return
	}
	ch := req.PathValue("ch")
	if r.route.closeChannel(mac, ch) {
		r.logf("close %s ch=%s why=mac", mac, short(ch))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Relay) putTickets(w http.ResponseWriter, req *http.Request) {
	mac, _, ok := r.macAuth(w, req)
	if !ok {
		return
	}
	stream := req.Header.Get(HeaderStream)
	var body ticketsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxTicketsPut)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body")
		return
	}
	stored, err := r.tickets.set(mac, body.Tickets, func() bool { return r.route.current(mac, stream) })
	if err != nil {
		switch {
		case errors.Is(err, errNotCurrent):
			writeError(w, http.StatusConflict, NotCurrentStream)
		case errors.Is(err, errTicketTaken):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, errBadTicket), errors.Is(err, errTooMany):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			r.logf("tickets %s: %v", mac, err)
			writeError(w, http.StatusInternalServerError, "could not store the tickets")
		}
		return
	}
	closed := r.route.closeChannels(mac, func(h string) bool { return r.tickets.has(mac, h) })
	r.logf("tickets %s n=%d closed=%d", mac, stored, len(closed))
	w.WriteHeader(http.StatusNoContent)
}
