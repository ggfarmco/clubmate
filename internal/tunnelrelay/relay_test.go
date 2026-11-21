package tunnelrelay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/tunnel"
)

const (
	macToken   = "cmr_studio"
	otherToken = "cmr_other"
	ticketA    = "cmk_phone_a"
	ticketB    = "cmk_phone_b"
	wait       = 5 * time.Second
)

type fixture struct {
	url     string
	relay   *Relay
	logs    *syncBuffer
	tickets string
	clock   *clock

	mu      sync.Mutex
	streams map[string]string
}

type setup struct {
	limits   Limits
	server   func(*http.Server)
	liveness time.Duration
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

type fakeMac struct {
	events chan sseEvent
	limits chan string
	pings  atomic.Int32
	cancel context.CancelFunc
}

type sseEvent struct {
	name string
	data string
}

type result struct {
	res  *http.Response
	body []byte
	err  error
}

func TestOpenRoundTrip(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	f.putTickets(t, ticketA)

	done := f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
	ev := mac.next(t, "open")
	var frame openFrame
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame))
	assert.Equal(t, []byte("msg1"), frame.D)
	res := f.mac(t, http.MethodPost, "/v1/mac/open/"+frame.Ch, []byte("msg2"))
	got := <-done

	require.NoError(t, got.err)
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Equal(t, "1", res.Header.Get(HeaderRelay))
	assert.Equal(t, http.StatusOK, got.res.StatusCode)
	assert.Equal(t, "1", got.res.Header.Get(HeaderRelay))
	assert.Equal(t, frame.Ch, got.res.Header.Get(HeaderChannel))
	assert.Equal(t, []byte("msg2"), got.body)
	assert.Contains(t, f.logs.String(), "open studio ch="+short(frame.Ch))
	assert.NotContains(t, f.logs.String(), ticketA)
}

func TestCallRequestBodiesInlineAndPulled(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{"inline", 100},
		{"pulled", InlineMax + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "")
			mac := f.connect(t)
			ch := f.openChannel(t, mac, ticketA)
			body := bytes.Repeat([]byte{7}, tc.size)

			done := f.device(t, "/v1/d/call", ticketA, ch, body)
			req := mac.req(t)
			require.Equal(t, reqFrame{ID: req.ID, Ch: ch, Kind: KindCall, N: tc.size, D: req.D}, req)
			if tc.size <= InlineMax {
				assert.Equal(t, body, req.D)
			} else {
				assert.Nil(t, req.D)
				pulled := f.mac(t, http.MethodGet, "/v1/mac/req/"+req.ID+"/body", nil)
				assert.Equal(t, body, readAll(t, pulled))
			}
			reply := f.mac(t, http.MethodPost, "/v1/mac/reply/"+req.ID, []byte("reply"))
			got := <-done

			assert.Equal(t, http.StatusNoContent, reply.StatusCode)
			require.NoError(t, got.err)
			assert.Equal(t, http.StatusOK, got.res.StatusCode)
			assert.Equal(t, []byte("reply"), got.body)
			testutil.Eventually(t, func() bool {
				return strings.Contains(f.logs.String(), "in="+strconv.Itoa(tc.size)+" out=5 ") && strings.Contains(f.logs.String(), "status=200")
			}, wait)
		})
	}
}

func TestReplyOf17MiBIsPiped(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	body := bytes.Repeat([]byte("0123456789abcdef"), MaxBody/16)

	done := f.device(t, "/v1/d/call", ticketA, ch, []byte("req"))
	req := mac.req(t)
	reply := f.mac(t, http.MethodPost, "/v1/mac/reply/"+req.ID, body)
	got := <-done

	assert.Equal(t, http.StatusNoContent, reply.StatusCode)
	require.NoError(t, got.err)
	assert.True(t, bytes.Equal(body, got.body))
}

func TestStreamFramesReachTheDeviceInOrder(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)

	res := f.stream(t, t.Context(), ticketA, ch)
	req := mac.req(t)
	require.Equal(t, KindStream, req.Kind)
	gone := f.push(t, pushItem(tunnel.PushData, req.ID, "one"), pushItem(tunnel.PushData, req.ID, "two"), pushItem(tunnel.PushEnd, req.ID, "end"))
	var frames []string
	for line := range dataLines(t, res.Body) {
		b, err := tunnel.ParseSSEData(line)
		require.NoError(t, err)
		frames = append(frames, string(b))
	}

	assert.Empty(t, gone)
	assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	assert.Equal(t, []string{"one", "two", "end"}, frames)
}

func TestDeviceLeavingClosesItsStreamOnTheMac(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	ctx, cancel := context.WithCancel(t.Context())

	res := f.stream(t, ctx, ticketA, ch)
	req := mac.req(t)
	cancel()
	_ = res.Body.Close()
	ev := mac.next(t, "close")
	var frame closeFrame
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame))
	gone := f.push(t, pushItem(tunnel.PushData, req.ID, "late"))

	assert.Equal(t, closeFrame{Ch: ch, ID: req.ID, Why: WhyDeviceGone}, frame)
	assert.Equal(t, []string{req.ID}, gone)
	assert.Contains(t, f.logs.String(), "stream studio ch="+short(ch)+" lived=")
}

func TestNewMacStreamReplacesTheOld(t *testing.T) {
	f := newFixture(t, "")
	first := f.connect(t)
	old := f.relay.route.mac("studio")

	f.connect(t)
	first.next(t, "replaced")
	_, open := <-first.events

	assert.False(t, open)
	assert.NotSame(t, old, f.relay.route.mac("studio"))
	testutil.Eventually(t, func() bool { return strings.Contains(f.logs.String(), "mac studio stream replaced") }, wait)
}

func TestShutdownEndsMacStreamsAndCutsTheirRequests(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	res := f.stream(t, t.Context(), ticketA, ch)
	mac.req(t)

	f.relay.Shutdown()
	_, open := <-mac.events
	_, err := io.ReadAll(res.Body)

	assert.False(t, open)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestRefusals(t *testing.T) {
	f := newFixture(t, "")
	_, err := f.relay.tickets.set("studio", []Ticket{{Hash: hashTicket(ticketA)}}, func() bool { return true })
	require.NoError(t, err)

	noMac := <-f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
	badTicket := <-f.device(t, "/v1/d/open", "cmk_unknown", "", []byte("msg1"))
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	badChannel := <-f.device(t, "/v1/d/call", ticketA, "ch_unknown", []byte("x"))
	tooBig := <-f.device(t, "/v1/d/call", ticketA, ch, make([]byte, MaxBody+1))
	badToken := f.do(t, http.MethodGet, "/v1/mac/stream", "cmr_wrong", nil)
	lateReply := f.mac(t, http.MethodPost, "/v1/mac/reply/"+strings.Repeat("0", 32), []byte("r"))

	for name, tc := range map[string]struct {
		res  *http.Response
		body []byte
		want int
	}{
		"Mac not connected": {noMac.res, noMac.body, http.StatusServiceUnavailable},
		"unknown ticket":    {badTicket.res, badTicket.body, http.StatusUnauthorized},
		"unknown channel":   {badChannel.res, badChannel.body, http.StatusGone},
		"body too large":    {tooBig.res, tooBig.body, http.StatusRequestEntityTooLarge},
		"bad Mac token":     {badToken, readAll(t, badToken), http.StatusUnauthorized},
		"reply too late":    {lateReply, readAll(t, lateReply), http.StatusGone},
	} {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, tc.res)
			assert.Equal(t, tc.want, tc.res.StatusCode)
			assert.Equal(t, "1", tc.res.Header.Get(HeaderRelay))
			assert.Equal(t, "application/json", tc.res.Header.Get("Content-Type"))
			var e map[string]string
			require.NoError(t, json.Unmarshal(tc.body, &e))
			assert.NotEmpty(t, e["error"])
		})
	}
}

func TestMacDropCutsACallInFlight(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)

	done := f.device(t, "/v1/d/call", ticketA, ch, []byte("req"))
	req := mac.req(t)
	mac.cancel()
	got := <-done
	reply := f.mac(t, http.MethodPost, "/v1/mac/reply/"+req.ID, []byte("r"))

	require.NotNil(t, got.res)
	assert.Equal(t, http.StatusOK, got.res.StatusCode)
	assert.ErrorIs(t, got.err, io.ErrUnexpectedEOF)
	assert.Equal(t, http.StatusGone, reply.StatusCode)
	testutil.Eventually(t, func() bool { return strings.Contains(f.logs.String(), "status=502") }, wait)
}

func TestCallTimeoutClosesTheRequestOnTheMac(t *testing.T) {
	f := newFixture(t, "")
	f.relay.callWait = 100 * time.Millisecond
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)

	done := f.device(t, "/v1/d/call", ticketA, ch, []byte("req"))
	req := mac.req(t)
	ev := mac.next(t, "close")
	var frame closeFrame
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame))
	got := <-done

	assert.Equal(t, closeFrame{Ch: ch, ID: req.ID, Why: WhyTimeout}, frame)
	require.NotNil(t, got.res)
	assert.ErrorIs(t, got.err, io.ErrUnexpectedEOF)
	testutil.Eventually(t, func() bool { return strings.Contains(f.logs.String(), "status=504") }, wait)
}

func TestMacCloseAnswersTheWaitingRequestGone(t *testing.T) {
	for _, kind := range []string{KindCall, KindStream} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "")
			f.relay.headWait = wait
			mac := f.connect(t)
			ch := f.openChannel(t, mac, ticketA)

			done := f.device(t, "/v1/d/"+kind, ticketA, ch, []byte("req"))
			mac.req(t)
			closed := f.mac(t, http.MethodPost, "/v1/mac/close/"+ch, nil)
			got := <-done
			again := <-f.device(t, "/v1/d/"+kind, ticketA, ch, []byte("req"))

			assert.Equal(t, http.StatusNoContent, closed.StatusCode)
			require.NoError(t, got.err)
			assert.Equal(t, http.StatusGone, got.res.StatusCode)
			assert.Equal(t, http.StatusGone, again.res.StatusCode)
			assert.Contains(t, f.logs.String(), "close studio ch="+short(ch)+" why=mac")
		})
	}
}

func TestTicketSetReplacesAndClosesChannels(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)

	f.putTickets(t, ticketB)
	ev := mac.next(t, "close")
	var frame closeFrame
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame))
	revoked := <-f.device(t, "/v1/d/call", ticketA, ch, []byte("x"))
	other := <-f.device(t, "/v1/d/call", ticketB, ch, []byte("x"))

	assert.Equal(t, closeFrame{Ch: ch, Why: WhyTicketRevoked}, frame)
	assert.Equal(t, http.StatusUnauthorized, revoked.res.StatusCode)
	assert.Equal(t, http.StatusGone, other.res.StatusCode)
}

func TestCallsFromAStaleStreamAreRefused(t *testing.T) {
	f := newFixture(t, "")
	f.connect(t)
	stale := f.streamID(macToken)
	live := f.connect(t)
	ch := f.openChannel(t, live, ticketA)
	set, err := json.Marshal(ticketsRequest{Tickets: []Ticket{{Hash: hashTicket(ticketB)}}})
	require.NoError(t, err)

	refused := map[string]*http.Response{
		"tickets":           f.send(t, http.MethodPut, "/v1/mac/tickets", macToken, stale, set),
		"tickets no stream": f.send(t, http.MethodPut, "/v1/mac/tickets", macToken, "", set),
		"push":              f.send(t, http.MethodPost, "/v1/mac/push", macToken, stale, pushItem(tunnel.PushEnd, strings.Repeat("0", 32), "")),
		"close":             f.send(t, http.MethodPost, "/v1/mac/close/"+ch, macToken, stale, nil),
	}
	done := f.device(t, "/v1/d/call", ticketA, ch, []byte("req"))
	req := live.req(t)
	reply := f.mac(t, http.MethodPost, "/v1/mac/reply/"+req.ID, []byte("ok"))
	got := <-done

	for name, res := range refused {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusConflict, res.StatusCode)
			assert.Equal(t, "1", res.Header.Get(HeaderRelay))
			var e map[string]string
			require.NoError(t, json.Unmarshal(readAll(t, res), &e))
			assert.Equal(t, NotCurrentStream, e["error"])
		})
	}
	assert.Equal(t, http.StatusNoContent, reply.StatusCode)
	require.NoError(t, got.err)
	assert.Equal(t, []byte("ok"), got.body)
}

func TestTicketsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickets.json")
	f := newFixture(t, path)
	f.connect(t)
	f.putTickets(t, ticketA)

	again := newFixture(t, path)
	known := <-again.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
	unknown := <-again.device(t, "/v1/d/open", ticketB, "", []byte("msg1"))

	assert.Equal(t, http.StatusServiceUnavailable, known.res.StatusCode)
	assert.Equal(t, http.StatusUnauthorized, unknown.res.StatusCode)
}

func TestMacStreamOutlivesServerTimeouts(t *testing.T) {
	f := newFixture(t, "", setup{server: func(s *http.Server) {
		s.ReadTimeout, s.WriteTimeout = 100*time.Millisecond, 100*time.Millisecond
	}})
	f.relay.ping = 50 * time.Millisecond
	mac := f.connect(t)

	testutil.Eventually(t, func() bool { return mac.pings.Load() >= 10 }, wait)
	f.openChannel(t, mac, ticketA)
}

func TestTicketRevokeCutsAPipingReply(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	call := f.call(t, ticketA, ch)
	req := mac.req(t)
	pr, pw := io.Pipe()
	reply := f.macAsync(t, http.MethodPost, "/v1/mac/reply/"+req.ID, pr)

	_, err := pw.Write([]byte("part1"))
	require.NoError(t, err)
	head := make([]byte, 5)
	_, err = io.ReadFull(call.Body, head)
	require.NoError(t, err)
	f.putTickets(t, ticketB)
	_, err = io.ReadAll(call.Body)
	_ = pw.Close()
	got := <-reply

	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.NoError(t, got.err)
	assert.Equal(t, http.StatusGone, got.res.StatusCode)
}

func TestOpenStoppedByRevokeIsUnknownTicket(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)
	f.putTickets(t, ticketA)

	done := f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
	mac.next(t, "open")
	f.putTickets(t, ticketB)
	got := <-done

	require.NoError(t, got.err)
	assert.Equal(t, http.StatusUnauthorized, got.res.StatusCode)
}

func TestExpiredTicketOfAnotherMacIsFree(t *testing.T) {
	f := newFixture(t, "")
	f.connect(t)
	f.connectAs(t, otherToken, "other")
	pairing := Ticket{Hash: hashTicket(ticketA), Exp: f.clock.now().Add(time.Minute).Unix()}
	require.Equal(t, http.StatusNoContent, f.setTickets(t, macToken, pairing).StatusCode)

	taken := f.setTickets(t, otherToken, Ticket{Hash: pairing.Hash})
	f.clock.add(2 * time.Minute)
	free := f.setTickets(t, otherToken, Ticket{Hash: pairing.Hash})

	assert.Equal(t, http.StatusConflict, taken.StatusCode)
	assert.Equal(t, http.StatusNoContent, free.StatusCode)
}

func TestTicketsLogCountsStoredTickets(t *testing.T) {
	f := newFixture(t, "")
	f.connect(t)
	a := Ticket{Hash: hashTicket(ticketA)}
	expired := Ticket{Hash: hashTicket(ticketB), Exp: f.clock.now().Unix()}

	res := f.setTickets(t, macToken, a, a, expired)

	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Contains(t, f.logs.String(), "tickets studio n=1 closed=0")
}

func TestQuotaRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits Limits
		refuse func(t *testing.T, f *fixture, mac *fakeMac, ch string) (*http.Response, []byte)
		retry  string
		log    string
	}{
		{"bytes per day", Limits{BytesPerDay: 8}, func(t *testing.T, f *fixture, _ *fakeMac, ch string) (*http.Response, []byte) {
			got := <-f.device(t, "/v1/d/call", ticketA, ch, []byte("x"))
			return got.res, got.body
		}, "43200", "quota studio bytes"},
		{"open channels", Limits{Channels: 1}, func(t *testing.T, f *fixture, mac *fakeMac, ch string) (*http.Response, []byte) {
			f.stream(t, t.Context(), ticketA, ch)
			mac.req(t)
			got := <-f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
			return got.res, got.body
		}, "1", "quota studio channels"},
		{"calls in flight", Limits{Calls: 1}, func(t *testing.T, f *fixture, mac *fakeMac, ch string) (*http.Response, []byte) {
			f.call(t, ticketA, ch)
			mac.req(t)
			got := <-f.device(t, "/v1/d/call", ticketA, ch, []byte("x"))
			return got.res, got.body
		}, "1", "quota studio calls"},
		{"device streams", Limits{Streams: 1}, func(t *testing.T, f *fixture, mac *fakeMac, ch string) (*http.Response, []byte) {
			f.stream(t, t.Context(), ticketA, ch)
			mac.req(t)
			got := <-f.device(t, "/v1/d/stream", ticketA, ch, []byte("x"))
			return got.res, got.body
		}, "1", "quota studio streams"},
		{"requests per second", Limits{Burst: 1}, func(t *testing.T, f *fixture, _ *fakeMac, ch string) (*http.Response, []byte) {
			got := <-f.device(t, "/v1/d/call", ticketA, ch, []byte("x"))
			return got.res, got.body
		}, "1", "quota studio rate"},
		{"open per ticket", Limits{OpenBurst: 1}, func(t *testing.T, f *fixture, _ *fakeMac, _ string) (*http.Response, []byte) {
			got := <-f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
			return got.res, got.body
		}, "1", "quota studio open-rate"},
		{"Mac streams", Limits{MacStreams: 1}, func(t *testing.T, f *fixture, _ *fakeMac, _ string) (*http.Response, []byte) {
			res := f.do(t, http.MethodGet, "/v1/mac/stream", otherToken, nil)
			return res, readAll(t, res)
		}, "60", "quota relay mac-streams"},
		{"all channels", Limits{AllChannels: 1}, func(t *testing.T, f *fixture, _ *fakeMac, _ string) (*http.Response, []byte) {
			got := <-f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
			return got.res, got.body
		}, "1", "quota relay all-channels"},
		{"buffered bytes", Limits{Buffered: 1000}, func(t *testing.T, f *fixture, _ *fakeMac, ch string) (*http.Response, []byte) {
			got := <-f.device(t, "/v1/d/call", ticketA, ch, make([]byte, 2000))
			return got.res, got.body
		}, "1", "quota relay buffered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "", setup{limits: tc.limits})
			mac := f.connect(t)
			ch := f.openChannel(t, mac, ticketA)

			res, body := tc.refuse(t, f, mac, ch)

			require.NotNil(t, res)
			assert.Equal(t, http.StatusTooManyRequests, res.StatusCode)
			assert.Equal(t, tc.retry, res.Header.Get("Retry-After"))
			assert.Equal(t, "1", res.Header.Get(HeaderRelay))
			assert.Equal(t, "application/json", res.Header.Get("Content-Type"))
			var e map[string]string
			require.NoError(t, json.Unmarshal(body, &e))
			assert.NotEmpty(t, e["error"])
			assert.Contains(t, f.logs.String(), tc.log)
		})
	}
}

func TestDayRolloverFreesBytes(t *testing.T) {
	f := newFixture(t, "", setup{limits: Limits{BytesPerDay: 8}})
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	refused := <-f.device(t, "/v1/d/call", ticketA, ch, []byte("x"))
	require.Equal(t, http.StatusTooManyRequests, refused.res.StatusCode)

	f.clock.add(12 * time.Hour)
	f.call(t, ticketA, ch)
	req := mac.req(t)

	assert.Equal(t, KindCall, req.Kind)
}

func TestLimitsFrameOnConnect(t *testing.T) {
	f := newFixture(t, "")
	mac := f.connect(t)

	var got limitsFrame
	select {
	case raw := <-mac.limits:
		require.NoError(t, json.Unmarshal([]byte(raw), &got))
	case <-time.After(testutil.Timeout(wait)):
		require.FailNow(t, "no limits frame on the Mac stream")
	}

	assert.Equal(t, limitsFrame{
		BytesPerDay: 2 << 30, Channels: 32, Calls: 64, Streams: 16, Body: MaxBody,
		Rate: 20, Burst: 100, OpenRate: 1, OpenBurst: 10, IdleSeconds: 600,
	}, got)
}

func TestIdleChannelIsClosed(t *testing.T) {
	f := newFixture(t, "")
	f.relay.ping = 20 * time.Millisecond
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)

	f.clock.add(10 * time.Minute)
	ev := mac.next(t, "close")
	var frame closeFrame
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame))
	got := <-f.device(t, "/v1/d/call", ticketA, ch, []byte("x"))

	assert.Equal(t, closeFrame{Ch: ch, Why: WhyIdle}, frame)
	assert.Equal(t, http.StatusGone, got.res.StatusCode)
}

func TestFullChannelSetDropsTheOldestUnusedChannel(t *testing.T) {
	f := newFixture(t, "", setup{limits: Limits{Channels: 1}})
	mac := f.connect(t)
	old := f.openChannel(t, mac, ticketA)

	done := f.device(t, "/v1/d/open", ticketA, "", []byte("msg1"))
	ev := mac.next(t, "close")
	var frame closeFrame
	require.NoError(t, json.Unmarshal([]byte(ev.data), &frame))
	var open openFrame
	require.NoError(t, json.Unmarshal([]byte(mac.next(t, "open").data), &open))
	f.mac(t, http.MethodPost, "/v1/mac/open/"+open.Ch, []byte("msg2"))
	got := <-done

	assert.Equal(t, closeFrame{Ch: old, Why: WhyIdle}, frame)
	require.NoError(t, got.err)
	assert.Equal(t, http.StatusOK, got.res.StatusCode)
}

func TestMacThatStopsReadingIsDropped(t *testing.T) {
	f := newFixture(t, "", setup{liveness: 100 * time.Millisecond})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.url+"/v1/mac/stream", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+macToken)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	m := f.relay.route.mac("studio")
	require.NotNil(t, m)
	frame := make([]byte, 1<<20)
	for range 64 {
		m.send(frame, true)
	}

	testutil.Eventually(t, func() bool {
		return f.relay.route.mac("studio") == nil && f.relay.buf.used.Load() == 0
	}, wait)
}

func TestDeviceThatStopsReadingEndsItsStream(t *testing.T) {
	f := newFixture(t, "", setup{liveness: 100 * time.Millisecond})
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	f.stream(t, t.Context(), ticketA, ch)
	id := mac.req(t).ID
	data := make([]byte, 1<<20)
	for range 64 {
		f.relay.route.deliver("studio", id, item{data: data})
	}

	testutil.Eventually(t, func() bool { return f.relay.buf.used.Load() == 0 }, wait)
}

func TestReplyToADeviceThatStopsReadingEnds(t *testing.T) {
	f := newFixture(t, "", setup{liveness: 100 * time.Millisecond})
	mac := f.connect(t)
	ch := f.openChannel(t, mac, ticketA)
	f.call(t, ticketA, ch)
	id := mac.req(t).ID

	done := f.macAsync(t, http.MethodPost, "/v1/mac/reply/"+id, bytes.NewReader(make([]byte, MaxBody)))

	select {
	case got := <-done:
		require.NoError(t, got.err)
		assert.Equal(t, http.StatusGone, got.res.StatusCode)
	case <-time.After(testutil.Timeout(wait)):
		require.FailNow(t, "the reply to a stuck device did not end")
	}
}

func TestPushToAnUnknownIDIsCharged(t *testing.T) {
	f := newFixture(t, "")
	f.connect(t)
	id := strings.Repeat("ab", tunnel.IDSize)

	gone := f.push(t, pushItem(tunnel.PushData, id, "lost bytes"))

	assert.Equal(t, []string{id}, gone)
	assert.Equal(t, int64(len("lost bytes")), f.relay.quota.used("studio"))
}

func TestUnchangedTicketSetKeepsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickets.json")
	f := newFixture(t, path)
	f.connect(t)
	f.putTickets(t, ticketA)
	before, err := os.Stat(path)
	require.NoError(t, err)

	f.putTickets(t, ticketA)

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after))
}

func TestQuotaSweepRunsOncePerInterval(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	q := newQuota(DefaultLimits(), func() time.Time { return now }, func(string, ...any) {})
	q.sweep(now)
	q.tickets["a"] = &bucket{last: now.Add(-bucketIdleTime)}

	q.sweep(now.Add(time.Second))
	assert.Contains(t, q.tickets, "a")
	q.sweep(now.Add(sweepEvery))
	assert.NotContains(t, q.tickets, "a")
}

func newFixture(t *testing.T, tickets string, opts ...setup) *fixture {
	t.Helper()
	logs := &syncBuffer{}
	c := &clock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	var o setup
	if len(opts) > 0 {
		o = opts[0]
	}
	r, err := New(Config{
		Auth: func(b string) (string, bool) {
			switch b {
			case macToken:
				return "studio", true
			case otherToken:
				return "other", true
			}
			return "", false
		},
		TicketsPath: tickets,
		Log:         log.New(logs, "", 0),
		Now:         c.now,
		Limits:      o.limits,
	})
	require.NoError(t, err)
	r.headWait = 50 * time.Millisecond
	if o.liveness > 0 {
		r.liveness = o.liveness
	}
	ts := httptest.NewUnstartedServer(r.Handler())
	if o.server != nil {
		o.server(ts.Config)
	}
	ts.Start()
	t.Cleanup(ts.Close)
	return &fixture{url: ts.URL, relay: r, logs: logs, tickets: tickets, clock: c, streams: map[string]string{}}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (f *fixture) connect(t *testing.T) *fakeMac {
	t.Helper()
	return f.connectAs(t, macToken, "studio")
}

func (f *fixture) connectAs(t *testing.T, token, name string) *fakeMac {
	t.Helper()
	before := f.relay.route.mac(name)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url+"/v1/mac/stream", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.NotEmpty(t, res.Header.Get(HeaderStream))
	f.mu.Lock()
	f.streams[token] = res.Header.Get(HeaderStream)
	f.mu.Unlock()
	m := &fakeMac{events: make(chan sseEvent, 64), limits: make(chan string, 1), cancel: cancel}
	go m.read(res.Body)
	testutil.Eventually(t, func() bool {
		now := f.relay.route.mac(name)
		return now != nil && now != before
	}, wait)
	return m
}

func (f *fixture) streamID(token string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streams[token]
}

func (m *fakeMac) read(body io.ReadCloser) {
	defer close(m.events)
	defer body.Close()
	br := bufio.NewReader(body)
	var ev sseEvent
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "":
			switch ev.name {
			case "":
			case "limits":
				m.limits <- ev.data
			default:
				m.events <- ev
			}
			ev = sseEvent{}
		case strings.HasPrefix(line, "event: "):
			ev.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.data = strings.TrimPrefix(line, "data: ")
		case line == ": ping":
			m.pings.Add(1)
		}
	}
}

func (m *fakeMac) next(t *testing.T, name string) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-m.events:
		require.True(t, ok, "Mac stream ended before %s", name)
		require.Equal(t, name, ev.name, "data: %s", ev.data)
		return ev
	case <-time.After(testutil.Timeout(wait)):
		require.FailNow(t, "no "+name+" frame on the Mac stream")
		return sseEvent{}
	}
}

func (m *fakeMac) req(t *testing.T) reqFrame {
	t.Helper()
	var f reqFrame
	require.NoError(t, json.Unmarshal([]byte(m.next(t, "req").data), &f))
	return f
}

func (f *fixture) openChannel(t *testing.T, m *fakeMac, ticket string) string {
	t.Helper()
	f.putTickets(t, ticket)
	done := f.device(t, "/v1/d/open", ticket, "", []byte("msg1"))
	var frame openFrame
	require.NoError(t, json.Unmarshal([]byte(m.next(t, "open").data), &frame))
	require.Equal(t, http.StatusNoContent, f.mac(t, http.MethodPost, "/v1/mac/open/"+frame.Ch, []byte("msg2")).StatusCode)
	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.res.StatusCode)
	return frame.Ch
}

func (f *fixture) putTickets(t *testing.T, list ...string) {
	t.Helper()
	var tickets []Ticket
	for _, tk := range list {
		tickets = append(tickets, Ticket{Hash: hashTicket(tk)})
	}
	require.Equal(t, http.StatusNoContent, f.setTickets(t, macToken, tickets...).StatusCode)
}

func (f *fixture) setTickets(t *testing.T, token string, list ...Ticket) *http.Response {
	t.Helper()
	raw, err := json.Marshal(ticketsRequest{Tickets: list})
	require.NoError(t, err)
	return f.do(t, http.MethodPut, "/v1/mac/tickets", token, raw)
}

func (f *fixture) push(t *testing.T, items ...[]byte) []string {
	t.Helper()
	res := f.mac(t, http.MethodPost, "/v1/mac/push", bytes.Join(items, nil))
	require.Equal(t, http.StatusOK, res.StatusCode)
	var reply pushReply
	require.NoError(t, json.Unmarshal(readAll(t, res), &reply))
	return reply.Gone
}

func pushItem(kind byte, id, data string) []byte {
	it := tunnel.PushItem{Kind: kind, Data: []byte(data)}
	raw, _ := hex.DecodeString(id)
	copy(it.ID[:], raw)
	return tunnel.AppendPush(nil, it)
}

func (f *fixture) mac(t *testing.T, method, path string, body []byte) *http.Response {
	t.Helper()
	return f.do(t, method, path, macToken, body)
}

func (f *fixture) do(t *testing.T, method, path, token string, body []byte) *http.Response {
	t.Helper()
	return f.send(t, method, path, token, f.streamID(token), body)
}

func (f *fixture) send(t *testing.T, method, path, token, stream string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.url+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	if stream != "" {
		req.Header.Set(HeaderStream, stream)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	if method != http.MethodGet {
		raw, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		res.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return res
}

func (f *fixture) device(t *testing.T, path, ticket, ch string, body []byte) <-chan result {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.url+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set(HeaderTicket, ticket)
	if ch != "" {
		req.Header.Set(HeaderChannel, ch)
	}
	done := make(chan result, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		done <- result{res: res, body: raw, err: err}
	}()
	return done
}

func (f *fixture) call(t *testing.T, ticket, ch string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.url+"/v1/d/call", strings.NewReader("req"))
	require.NoError(t, err)
	req.Header.Set(HeaderTicket, ticket)
	req.Header.Set(HeaderChannel, ch)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)
	return res
}

func (f *fixture) macAsync(t *testing.T, method, path string, body io.Reader) <-chan result {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.url+path, body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+macToken)
	req.Header.Set(HeaderStream, f.streamID(macToken))
	done := make(chan result, 1)
	go func() {
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		done <- result{res: res, body: raw, err: err}
	}()
	return done
}

func (f *fixture) stream(t *testing.T, ctx context.Context, ticket, ch string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url+"/v1/d/stream", strings.NewReader("events"))
	require.NoError(t, err)
	req.Header.Set(HeaderTicket, ticket)
	req.Header.Set(HeaderChannel, ch)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	require.Equal(t, http.StatusOK, res.StatusCode)
	return res
}

func dataLines(t *testing.T, body io.Reader) func(func(string) bool) {
	return func(yield func(string) bool) {
		sc := bufio.NewScanner(body)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") && !yield(sc.Text()) {
				return
			}
		}
		require.NoError(t, sc.Err())
	}
}

func readAll(t *testing.T, res *http.Response) []byte {
	t.Helper()
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return raw
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
