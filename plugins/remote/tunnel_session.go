package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/internal/tunnelrelay"
	"clubmate/pkg/tunnel"
)

const (
	macOpenPath     = "/v1/mac/open/"
	macReplyPath    = "/v1/mac/reply/"
	macPushPath     = "/v1/mac/push"
	macClosePath    = "/v1/mac/close/"
	maxQueued       = 32 << 20
	pushItemHead    = 1 + tunnel.IDSize + 4
	macBodyPath     = "/v1/mac/req/%s/body"
	relayAddrPrefix = "relay:"
	sessionIdle     = 10 * time.Minute
	maxSessions     = 256
	tunnelTransfer  = 5 * time.Minute
	kindCall        = "call"
	kindStream      = "stream"
	maxTunnelHead   = 64 << 10
	maxServing      = 80
	eventsPath      = "/v1/events"
)

var replyTooLargeBody, _ = json.Marshal(errorBody{Error: apiError{Code: codeTooLarge, Message: "This reply is too large for the relay path."}})

var (
	errBadRequest  = errors.New("bad tunnel request")
	errStreamGone  = errors.New("tunnel stream gone")
	errQueueFull   = errors.New("tunnel push queue full")
	errPushRefused = errors.New("relay refused the push")
	errReplyLarge  = errors.New("tunnel reply too large")
)

type sessions struct {
	handler  http.Handler
	client   *http.Client
	now      func() time.Time
	callWait time.Duration

	mu     sync.Mutex
	byCh   map[string]*tunnelSession
	pushes *pushQueue
	slots  chan struct{}

	classify func(ticketHash string) ticketKind
}

type pushQueue struct {
	s *sessions

	max int

	mu    sync.Mutex
	ctx   context.Context
	mc    macConn
	items []tunnel.PushItem
	size  int
	busy  bool
}

type pushReply struct {
	Gone []string `json:"gone"`
}

type tunnelSession struct {
	ch      *tunnel.Channel
	last    time.Time
	reqs    map[string]context.CancelFunc
	pairing bool
	spent   bool
	streams map[string]context.CancelFunc
}

type ticketKind int

const (
	ticketUnknown ticketKind = iota
	ticketDevice
	ticketPairing
)

type openFrame struct {
	Ch string `json:"ch"`
	D  []byte `json:"d"`
}

type pairingKey struct{}

const (
	pairingOpen = iota + 1
	pairingSpent
)

type afterReplyKey struct{}

type afterReply struct {
	mu  sync.Mutex
	fns []func()
}

type reqFrame struct {
	ID   string `json:"id"`
	Ch   string `json:"ch"`
	Kind string `json:"kind"`
	N    int    `json:"n"`
	D    []byte `json:"d"`
}

type closeFrame struct {
	Ch  string `json:"ch"`
	ID  string `json:"id"`
	Why string `json:"why"`
}

type tunnelWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
	max    int
	over   bool
}

type streamWriter struct {
	tunnelWriter
	s       *sessions
	run     context.Context
	ctx     context.Context
	mc      macConn
	ch      *tunnel.Channel
	id      [tunnel.IDSize]byte
	re      uint32
	seq     uint32
	started bool
	err     error
}

func newSessions(handler http.Handler, client *http.Client, now func() time.Time) *sessions {
	s := &sessions{handler: handler, client: client, now: now, byCh: map[string]*tunnelSession{}, slots: make(chan struct{}, maxServing), callWait: tunnelrelay.CallWait}
	s.pushes = &pushQueue{s: s, max: tunnelrelay.MaxBody}
	return s
}

func (s *sessions) respond(ctx context.Context, mc macConn, keys []*tunnel.Static, f openFrame) bool {
	ch, msg2, err := tunnel.Respond(f.Ch, f.D, keys, nil)
	if err != nil {
		logEvent("tunnel open", "ch", logChannel(f.Ch), "err", "handshake")
		return false
	}
	kind := ticketDevice
	if s.classify != nil {
		kind = s.classify(ch.TicketHash)
	}
	if kind == ticketUnknown {
		logEvent("tunnel open", "ch", logChannel(f.Ch), "err", "ticket")
		return false
	}
	s.add(f.Ch, ch, kind == ticketPairing)
	status, err := s.post(ctx, mc, macOpenPath+f.Ch, msg2, nil)
	if err != nil || status != http.StatusNoContent {
		s.drop(f.Ch)
		logEvent("tunnel open", "ch", logChannel(f.Ch), "err", postErr(status, err))
		return false
	}
	logEvent("tunnel open", "ch", logChannel(f.Ch))
	return true
}

func (s *sessions) add(id string, ch *tunnel.Channel, pairing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.byCh) >= maxSessions {
		s.dropOldestLocked()
	}
	s.byCh[id] = &tunnelSession{ch: ch, last: s.now(), reqs: map[string]context.CancelFunc{}, streams: map[string]context.CancelFunc{}, pairing: pairing}
}

func (s *sessions) dropOldestLocked() {
	var oldest string
	var at time.Time
	for id, t := range s.byCh {
		if oldest == "" || t.last.Before(at) {
			oldest, at = id, t.last
		}
	}
	s.dropLocked(oldest)
}

func (s *sessions) drop(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropLocked(id)
}

func (s *sessions) dropLocked(id string) bool {
	t, ok := s.byCh[id]
	if !ok {
		return false
	}
	for _, cancel := range t.reqs {
		cancel()
	}
	delete(s.byCh, id)
	return true
}

func (s *sessions) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.byCh {
		s.dropLocked(id)
	}
}

func (s *sessions) cancel(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.byCh {
		for _, id := range ids {
			if cancel, ok := t.reqs[id]; ok {
				cancel()
			}
		}
	}
}

func (s *sessions) cancelStreams() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.byCh {
		for _, cancel := range t.streams {
			cancel()
		}
	}
}

func (s *sessions) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byCh)
}

func (s *sessions) forgetIdle() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, t := range s.byCh {
		if len(t.reqs) == 0 && s.now().Sub(t.last) >= sessionIdle {
			s.dropLocked(id)
			logEvent("tunnel forget", "ch", logChannel(id))
			n++
		}
	}
	return n
}

func (s *sessions) closed(f closeFrame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.ID == "" {
		if s.dropLocked(f.Ch) {
			logEvent("tunnel close", "ch", logChannel(f.Ch), "why", f.Why)
			return true
		}
		return false
	}
	if t, ok := s.byCh[f.Ch]; ok {
		if cancel, ok := t.reqs[f.ID]; ok {
			cancel()
		}
	}
	return false
}

func (s *sessions) begin(ctx context.Context, f reqFrame) (*tunnel.Channel, context.Context, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byCh[f.Ch]
	if !ok {
		return nil, nil, nil
	}
	t.last = s.now()
	ctx, cancel := context.WithCancel(ctx)
	if t.pairing {
		mode := pairingOpen
		if t.spent {
			mode = pairingSpent
		}
		t.spent = true
		ctx = context.WithValue(ctx, pairingKey{}, mode)
	}
	t.reqs[f.ID] = cancel
	if f.Kind == kindStream {
		t.streams[f.ID] = cancel
	}
	return t.ch, ctx, func() {
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(t.reqs, f.ID)
		delete(t.streams, f.ID)
		t.last = s.now()
	}
}

func (s *sessions) serve(ctx context.Context, mc macConn, f reqFrame) {
	raw, err := hex.DecodeString(f.ID)
	if err != nil || len(raw) != tunnel.IDSize || (f.Kind != kindCall && f.Kind != kindStream) {
		logEvent("tunnel req", "ch", logChannel(f.Ch), "err", "bad frame")
		return
	}
	id := [tunnel.IDSize]byte(raw)
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		logEvent("tunnel req", "ch", logChannel(f.Ch), "err", "busy")
		s.cut(ctx, mc, f, id)
		return
	}
	ch, rctx, done := s.begin(ctx, f)
	if ch == nil {
		logEvent("tunnel req", "ch", logChannel(f.Ch), "err", "unknown channel")
		s.forget(ctx, mc, f.Ch)
		return
	}
	defer done()
	if f.Kind == kindCall {
		var cancel context.CancelFunc
		rctx, cancel = context.WithTimeout(rctx, s.callWait)
		defer cancel()
	}
	req, re, err := s.request(rctx, mc, ch, f)
	if err != nil {
		logEvent("tunnel req", "ch", logChannel(f.Ch), "err", err.Error())
		s.cut(ctx, mc, f, id)
		return
	}
	if f.Kind == kindStream {
		w := &streamWriter{tunnelWriter: tunnelWriter{header: http.Header{}, max: maxBodyBytes}, s: s, run: ctx, ctx: rctx, mc: mc, ch: ch, id: id, re: re}
		s.handler.ServeHTTP(w, req)
		w.finish()
		return
	}
	w := &tunnelWriter{header: http.Header{}, max: tunnel.MaxMessage}
	hooks := &afterReply{}
	defer hooks.run()
	s.handler.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), afterReplyKey{}, hooks)))
	if rctx.Err() != nil {
		logEvent("tunnel reply", "ch", logChannel(f.Ch), "err", rctx.Err().Error())
		s.cut(ctx, mc, f, id)
		return
	}
	var sealed []byte
	if !w.over {
		sealed, err = s.seal(ch, tunnel.TypeReply, re, 0, tunnel.ReplyHead{Status: w.code(), Header: w.head()}, w.body.Bytes())
	}
	if w.over || errors.Is(err, tunnel.ErrTooLarge) || len(sealed) > tunnelrelay.MaxBody {
		logEvent("tunnel reply", "ch", logChannel(f.Ch), "err", errReplyLarge.Error())
		sealed, err = s.seal(ch, tunnel.TypeReply, re, 0, tunnel.ReplyHead{Status: http.StatusRequestEntityTooLarge, Header: map[string]string{"Content-Type": jsonContentType}}, replyTooLargeBody)
	}
	if err != nil {
		logEvent("tunnel reply", "ch", logChannel(f.Ch), "err", err.Error())
		s.cut(ctx, mc, f, id)
		return
	}
	if status, err := s.post(rctx, mc, macReplyPath+f.ID, sealed, nil); err != nil || status != http.StatusNoContent {
		logEvent("tunnel reply", "ch", logChannel(f.Ch), "err", postErr(status, err))
	}
}

func (s *sessions) request(ctx context.Context, mc macConn, ch *tunnel.Channel, f reqFrame) (*http.Request, uint32, error) {
	records := f.D
	if records == nil && f.N > 0 {
		var err error
		if records, err = s.pull(ctx, mc, f.ID); err != nil {
			return nil, 0, err
		}
	}
	m, err := ch.ReadMessage(bytes.NewReader(records))
	if err != nil {
		return nil, 0, err
	}
	if m.Type != tunnel.TypeRequest {
		return nil, 0, fmt.Errorf("%w: message type %d", errBadRequest, m.Type)
	}
	head, body, err := tunnel.Unpack(m, ch.Dict, maxBodyBytes+maxTunnelHead)
	if err != nil {
		return nil, 0, err
	}
	var rh tunnel.RequestHead
	if err := json.Unmarshal(head, &rh); err != nil {
		return nil, 0, fmt.Errorf("%w: head", errBadRequest)
	}
	if !strings.HasPrefix(rh.Path, "/") || strings.HasPrefix(rh.Path, "//") {
		return nil, 0, fmt.Errorf("%w: path", errBadRequest)
	}
	req, err := http.NewRequestWithContext(ctx, rh.Method, "http://relay"+rh.Path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", errBadRequest, err)
	}
	if f.Kind == kindCall && req.URL.Path == eventsPath {
		return nil, 0, fmt.Errorf("%w: %s as a call", errBadRequest, eventsPath)
	}
	for k, v := range rh.Header {
		req.Header.Set(k, v)
	}
	req.RemoteAddr = relayAddrPrefix + ch.TicketHash
	return req, m.Msg, nil
}

func (s *sessions) pull(ctx context.Context, mc macConn, id string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, tunnelTransfer)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mc.base+fmt.Sprintf(macBodyPath, id), nil)
	if err != nil {
		return nil, err
	}
	mc.auth(req)
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.Header.Get(tunnelrelay.HeaderRelay) != "1" || res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull body: %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, tunnelrelay.MaxBody))
}

func (s *sessions) seal(ch *tunnel.Channel, t tunnel.Type, re, seq uint32, h tunnel.ReplyHead, body []byte) ([]byte, error) {
	plain, flags, err := tunnel.PackReply(h, body, ch.Dict, true)
	if err != nil {
		return nil, err
	}
	return ch.SealAnswer(t, flags, re, seq, plain)
}

func sealPlain(ch *tunnel.Channel, t tunnel.Type, re, seq uint32, body []byte) ([]byte, error) {
	plain, flags, err := tunnel.Pack(nil, body, ch.Dict, t == tunnel.TypeChunk)
	if err != nil {
		return nil, err
	}
	return ch.SealAnswer(t, flags, re, seq, plain)
}

func (s *sessions) cut(ctx context.Context, mc macConn, f reqFrame, id [tunnel.IDSize]byte) {
	if f.Kind == kindStream {
		s.pushes.add(ctx, mc, true, tunnel.PushItem{Kind: tunnel.PushEnd, ID: id})
		return
	}
	_, _ = s.post(ctx, mc, macReplyPath+f.ID, nil, nil)
}

func (s *sessions) forget(ctx context.Context, mc macConn, ch string) {
	if status, err := s.post(ctx, mc, macClosePath+ch, nil, nil); err != nil || status != http.StatusNoContent {
		logEvent("tunnel close", "ch", logChannel(ch), "err", postErr(status, err))
	}
}

func (s *sessions) post(ctx context.Context, mc macConn, path string, body []byte, out any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, tunnelTransfer)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mc.base+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	mc.auth(req)
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.Header.Get(tunnelrelay.HeaderRelay) != "1" {
		return res.StatusCode, errRelayUnreachable
	}
	if out != nil && res.StatusCode == http.StatusOK {
		return res.StatusCode, json.NewDecoder(io.LimitReader(res.Body, relayMaxReply)).Decode(out)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, relayMaxReply))
	return res.StatusCode, nil
}

func onReplySent(ctx context.Context, fn func()) {
	if a, ok := ctx.Value(afterReplyKey{}).(*afterReply); ok {
		a.mu.Lock()
		a.fns = append(a.fns, fn)
		a.mu.Unlock()
		return
	}
	fn()
}

func (a *afterReply) run() {
	a.mu.Lock()
	fns := a.fns
	a.fns = nil
	a.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func pairingMode(ctx context.Context) int {
	mode, _ := ctx.Value(pairingKey{}).(int)
	return mode
}

func (q *pushQueue) add(ctx context.Context, mc macConn, force bool, items ...tunnel.PushItem) error {
	n := 0
	for _, it := range items {
		n += len(it.Data)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.mc != mc {
		q.items, q.size, q.mc = nil, 0, mc
	}
	if !force && q.size > 0 && q.size+n > maxQueued {
		return errQueueFull
	}
	q.items = append(q.items, items...)
	q.size += n
	q.ctx = ctx
	if !q.busy {
		q.busy = true
		go q.run()
	}
	return nil
}

func (q *pushQueue) run() {
	for {
		q.mu.Lock()
		if len(q.items) == 0 {
			q.busy = false
			q.mu.Unlock()
			return
		}
		batch, rest, taken := nextBatch(q.items, q.max)
		q.items, q.size = rest, q.size-taken
		ctx, mc := q.ctx, q.mc
		q.mu.Unlock()
		q.s.cancel(q.send(ctx, mc, batch)...)
	}
}

func nextBatch(items []tunnel.PushItem, limit int) (batch, rest []tunnel.PushItem, taken int) {
	open := map[[tunnel.IDSize]byte]int{}
	owned := map[int]bool{}
	size, i := 0, 0
	for ; i < len(items); i++ {
		it := items[i]
		j, merge := open[it.ID]
		merge = merge && it.Kind == tunnel.PushData
		cost := len(it.Data)
		if !merge {
			cost += pushItemHead
		}
		if i > 0 && size+cost > limit {
			break
		}
		size += cost
		taken += len(it.Data)
		switch {
		case merge:
			if !owned[j] {
				batch[j].Data, owned[j] = slices.Clip(batch[j].Data), true
			}
			batch[j].Data = append(batch[j].Data, it.Data...)
		case it.Kind == tunnel.PushData:
			open[it.ID] = len(batch)
			batch = append(batch, it)
		default:
			delete(open, it.ID)
			batch = append(batch, it)
		}
	}
	if i == len(items) {
		return batch, nil, taken
	}
	return batch, items[i:], taken
}

func (q *pushQueue) send(ctx context.Context, mc macConn, batch []tunnel.PushItem) []string {
	var body []byte
	for _, it := range batch {
		body = tunnel.AppendPush(body, it)
	}
	var reply pushReply
	status, err := q.s.post(ctx, mc, macPushPath, body, &reply)
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("%w: %d", errPushRefused, status)
	}
	if err == nil {
		return reply.Gone
	}
	logEvent("tunnel push", "items", strconv.Itoa(len(batch)), "err", err.Error())
	ids := make([]string, 0, len(batch))
	for _, it := range batch {
		ids = append(ids, hex.EncodeToString(it.ID[:]))
	}
	return ids
}

func (w *streamWriter) Flush() { _ = w.FlushError() }

func (w *streamWriter) FlushError() error {
	if w.err != nil {
		return w.err
	}
	if w.ctx.Err() != nil {
		w.err = errStreamGone
		return w.err
	}
	if w.over {
		w.err = errReplyLarge
		return w.err
	}
	var items []tunnel.PushItem
	if !w.started {
		w.started = true
		head, err := w.s.seal(w.ch, tunnel.TypeReply, w.re, w.next(), tunnel.ReplyHead{Status: w.code(), Header: w.head()}, nil)
		if err != nil {
			w.err = err
			return err
		}
		items = append(items, w.item(head))
	}
	if w.body.Len() > 0 {
		chunk, err := sealPlain(w.ch, tunnel.TypeChunk, w.re, w.next(), w.body.Bytes())
		w.body.Reset()
		if err != nil {
			w.err = err
			return err
		}
		items = append(items, w.item(chunk))
	}
	if len(items) == 0 {
		return nil
	}
	w.err = w.s.pushes.add(w.run, w.mc, false, items...)
	return w.err
}

func (w *streamWriter) finish() {
	if w.ctx.Err() != nil {
		return
	}
	end := tunnel.PushItem{Kind: tunnel.PushEnd, ID: w.id}
	if w.err != nil || w.over {
		w.s.pushes.add(w.run, w.mc, true, end)
		return
	}
	var items []tunnel.PushItem
	var last []byte
	var err error
	if !w.started {
		last, err = w.s.seal(w.ch, tunnel.TypeReply, w.re, w.next(), tunnel.ReplyHead{Status: w.code(), Header: w.head()}, w.body.Bytes())
	} else if w.body.Len() > 0 {
		last, err = sealPlain(w.ch, tunnel.TypeChunk, w.re, w.next(), w.body.Bytes())
	}
	if err == nil && last != nil {
		items = append(items, w.item(last))
	}
	if err == nil {
		end.Data, err = sealPlain(w.ch, tunnel.TypeEnd, w.re, w.next(), nil)
	}
	if err != nil {
		end.Data = nil
	}
	w.s.pushes.add(w.run, w.mc, true, append(items, end)...)
}

func (w *streamWriter) next() uint32 {
	n := w.seq
	w.seq++
	return n
}

func (w *streamWriter) item(data []byte) tunnel.PushItem {
	return tunnel.PushItem{Kind: tunnel.PushData, ID: w.id, Data: data}
}

func (w *tunnelWriter) Header() http.Header { return w.header }

func (w *tunnelWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func (w *tunnelWriter) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if w.over || w.body.Len()+len(b) > w.max {
		w.over = true
		return 0, errReplyLarge
	}
	return w.body.Write(b)
}

func (w *tunnelWriter) Flush() {}

func (w *tunnelWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *tunnelWriter) head() map[string]string {
	out := make(map[string]string, len(w.header))
	for k, v := range w.header {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func postErr(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return strconv.Itoa(status)
}

func logChannel(ch string) string {
	return logID(strings.TrimPrefix(ch, "ch_"))
}
