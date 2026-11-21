package tunnelrelay

import (
	"errors"
	"io"
	"net/http"
	"time"

	"clubmate/pkg/tunnel"
)

const pipeChunk = 32 << 10

func (r *Relay) deviceTicket(w http.ResponseWriter, req *http.Request) (string, string, bool) {
	mac, hash, ok := r.tickets.lookup(req.Header.Get(HeaderTicket))
	if !ok {
		writeError(w, http.StatusUnauthorized, "unknown ticket")
	}
	return mac, hash, ok
}

func (r *Relay) deviceChannel(w http.ResponseWriter, req *http.Request) (*channel, bool) {
	mac, hash, ok := r.deviceTicket(w, req)
	if !ok {
		return nil, false
	}
	c := r.route.channel(req.Header.Get(HeaderChannel), mac, hash)
	if c == nil {
		writeError(w, http.StatusGone, "unknown channel")
		return nil, false
	}
	if r.route.mac(mac) == nil {
		writeError(w, http.StatusServiceUnavailable, "Mac not connected")
		return nil, false
	}
	return c, true
}

func (r *Relay) deviceOpen(w http.ResponseWriter, req *http.Request) {
	mac, hash, ok := r.deviceTicket(w, req)
	if !ok {
		return
	}
	if q := r.quota.enter(mac); q.what != "" {
		r.refuseQuota(w, mac, q)
		return
	}
	defer r.quota.leave(mac)
	if q := r.quota.allowOpen(mac, hash); q.what != "" {
		r.refuseQuota(w, mac, q)
		return
	}
	transfer(w)
	msg1, ok := readBody(w, req, maxMsg1)
	if !ok {
		return
	}
	o, d := r.route.startOpen(mac, hash, msg1, r.now())
	if o == nil {
		r.deny(w, mac, d)
		return
	}
	r.quota.charge(mac, int64(len(msg1)))
	defer r.route.endOpen(o)
	t := time.NewTimer(r.openWait)
	defer t.Stop()
	select {
	case msg2 := <-o.msg2:
		if !r.route.addChannel(o, r.now()) {
			writeError(w, http.StatusUnauthorized, "unknown ticket")
			return
		}
		r.quota.charge(mac, int64(len(msg2)))
		r.logf("open %s ch=%s", mac, short(o.ch))
		w.Header().Set(HeaderChannel, o.ch)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(msg2)
	case <-o.cut:
		if o.revoked {
			writeError(w, http.StatusUnauthorized, "unknown ticket")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "Mac not connected")
	case <-t.C:
		writeError(w, http.StatusGatewayTimeout, "Mac did not answer")
	case <-req.Context().Done():
	}
}

func (r *Relay) deviceCall(w http.ResponseWriter, req *http.Request) {
	c, ok := r.deviceChannel(w, req)
	if !ok {
		return
	}
	if q := r.quota.enter(c.mac); q.what != "" {
		r.refuseQuota(w, c.mac, q)
		return
	}
	defer r.quota.leave(c.mac)
	if q := r.quota.allowCall(c.mac); q.what != "" {
		r.refuseQuota(w, c.mac, q)
		return
	}
	transfer(w)
	body, held, ok := r.readHeld(w, req, c.mac, MaxBody)
	if !ok {
		return
	}
	r.quota.charge(c.mac, int64(len(body)))
	p, d := r.route.startReq(c, KindCall, body, held, r.now())
	if p == nil {
		r.deny(w, c.mac, d)
		return
	}
	start := r.now()
	status, out := http.StatusBadGateway, int64(0)
	defer func() {
		r.route.endReq(p, callEnd(status), r.now())
		r.quota.charge(c.mac, out)
		r.logf("call %s ch=%s in=%d out=%d ms=%d status=%d", c.mac, short(c.id), len(body), out, r.now().Sub(start).Milliseconds(), status)
	}()
	longLived(w)
	started := false
	begin := func() {
		if !started {
			started = true
			startStream(w, "application/octet-stream")
		}
	}
	head := time.NewTimer(r.headWait)
	defer head.Stop()
	t := time.NewTimer(r.callWait)
	defer t.Stop()
	for {
		select {
		case rp := <-p.reply:
			begin()
			res := r.pipeCall(w, p, rp)
			rp.result <- res
			out = res.n
			switch {
			case res.cut:
			case res.writeErr != nil:
				status = 499
			case res.readErr != nil:
			default:
				status = http.StatusOK
				return
			}
			panic(http.ErrAbortHandler)
		case <-head.C:
			begin()
		case <-p.cut:
			if !started && p.unknown {
				status = http.StatusGone
				writeError(w, status, "unknown channel")
				return
			}
			begin()
			panic(http.ErrAbortHandler)
		case <-t.C:
			status = http.StatusGatewayTimeout
			begin()
			panic(http.ErrAbortHandler)
		case <-req.Context().Done():
			status = 499
			return
		}
	}
}

func (r *Relay) pipeCall(w http.ResponseWriter, p *pending, rp *replyPipe) pipeResult {
	done := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-p.cut:
			rp.abort()
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now())
		case <-done:
		}
	}()
	res := pipe(w, rp.body, r.liveness)
	close(done)
	<-watched
	if res.readErr != nil || res.writeErr != nil {
		select {
		case <-p.cut:
			res.cut = true
		default:
		}
	}
	return res
}

func (r *Relay) deviceStream(w http.ResponseWriter, req *http.Request) {
	c, ok := r.deviceChannel(w, req)
	if !ok {
		return
	}
	if q := r.quota.enter(c.mac); q.what != "" {
		r.refuseQuota(w, c.mac, q)
		return
	}
	defer r.quota.leave(c.mac)
	if q := r.quota.allowCall(c.mac); q.what != "" {
		r.refuseQuota(w, c.mac, q)
		return
	}
	transfer(w)
	body, held, ok := r.readHeld(w, req, c.mac, MaxBody)
	if !ok {
		return
	}
	r.quota.charge(c.mac, int64(len(body)))
	p, d := r.route.startReq(c, KindStream, body, held, r.now())
	if p == nil {
		r.deny(w, c.mac, d)
		return
	}
	opened := r.now()
	gone, cut := false, false
	defer func() {
		r.route.endReq(p, streamEnd(gone, cut), r.now())
		r.logf("stream %s ch=%s lived=%ds", c.mac, short(c.id), int(r.now().Sub(opened).Seconds()))
		if cut {
			panic(http.ErrAbortHandler)
		}
	}()
	longLived(w)
	started := false
	begin := func() {
		if !started {
			started = true
			startStream(w, "text/event-stream")
		}
	}
	rc := http.NewResponseController(w)
	write := func(b []byte) bool {
		begin()
		r.writeBy(rc)
		_, err := w.Write(b)
		if err == nil {
			err = rc.Flush()
		}
		gone = err != nil
		return !gone
	}
	head := time.NewTimer(r.headWait)
	defer head.Stop()
	t := time.NewTicker(r.ping)
	defer t.Stop()
	for {
		select {
		case it := <-p.items:
			r.buf.give(c.mac, int64(len(it.data)))
			if len(it.data) > 0 && !write([]byte(tunnel.SSEData(it.data))) {
				return
			}
			if it.end {
				begin()
				return
			}
		case <-head.C:
			begin()
		case <-t.C:
			if !write(ping) {
				return
			}
		case <-p.cut:
			if !started && p.unknown {
				writeError(w, http.StatusGone, "unknown channel")
				return
			}
			begin()
			cut = true
			return
		case <-req.Context().Done():
			gone = true
			return
		}
	}
}

func callEnd(status int) string {
	switch status {
	case http.StatusOK, http.StatusGone:
		return ""
	case 499:
		return WhyDeviceGone
	case http.StatusGatewayTimeout:
		return WhyTimeout
	}
	return WhyCut
}

func streamEnd(gone, cut bool) string {
	switch {
	case gone:
		return WhyDeviceGone
	case cut:
		return WhyCut
	}
	return ""
}

func (r *Relay) deny(w http.ResponseWriter, mac string, d denial) {
	switch d.code {
	case http.StatusTooManyRequests:
		r.refuseQuota(w, mac, d.quota)
	case http.StatusGone:
		writeError(w, d.code, "unknown channel")
	default:
		writeError(w, http.StatusServiceUnavailable, "Mac not connected")
	}
}

func pipe(w http.ResponseWriter, body io.Reader, liveness time.Duration) pipeResult {
	rc := http.NewResponseController(w)
	buf := make([]byte, pipeChunk)
	var res pipeResult
	for {
		n, err := body.Read(buf)
		if n > 0 {
			_ = rc.SetWriteDeadline(time.Now().Add(liveness))
			if _, werr := w.Write(buf[:n]); werr != nil {
				res.writeErr = werr
				return res
			}
			if werr := rc.Flush(); werr != nil {
				res.writeErr = werr
				return res
			}
			res.n += int64(n)
		}
		if errors.Is(err, io.EOF) {
			return res
		}
		if err != nil {
			res.readErr = err
			return res
		}
	}
}
