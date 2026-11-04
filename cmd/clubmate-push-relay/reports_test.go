package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/report"
)

type reportFixture struct {
	relay  *relay
	now    time.Time
	key    ed25519.PrivateKey
	studio string
	admin  string
	bans   string
	logs   *bytes.Buffer
}

type reportRequest struct {
	key      ed25519.PrivateKey
	body     []byte
	bearer   string
	remote   string
	forward  string
	unsigned bool
	tamper   func(*http.Request)
}

func TestReportAcceptedAndListed(t *testing.T) {
	f := newReportFixture(t)
	bundle := reportZip(t, `{"text":"it broke"}`, 0)

	res := f.send(t, reportRequest{body: bundle, remote: "127.0.0.1:5000", forward: "10.0.0.1, 192.0.2.10"})
	require.Equal(t, http.StatusCreated, res.Code)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &created))
	list := f.list(t, "")
	bundleRes := f.private(t, http.MethodGet, "/v1/reports/"+created.ID+"/bundle")
	deleted := f.private(t, http.MethodDelete, "/v1/reports/"+created.ID)

	require.Len(t, list, 1)
	got := list[0]
	assert.Equal(t, created.ID, got.ID)
	assert.False(t, got.Trusted)
	assert.Empty(t, got.Mac)
	assert.Equal(t, report.KeyID(f.key.Public().(ed25519.PublicKey)), got.Key)
	assert.Equal(t, "192.0.2.10", got.IP)
	assert.Equal(t, int64(len(bundle)), got.Bytes)
	assert.Equal(t, "it broke", got.Report.Text)
	assert.True(t, got.Received.Equal(f.now))
	assert.Equal(t, bundle, bundleRes.Body.Bytes())
	assert.Equal(t, http.StatusNoContent, deleted.Code)
	assert.Empty(t, f.list(t, ""))
	assert.Empty(t, f.list(t, created.ID))
	assert.Equal(t, "report "+created.ID+" key "+got.Key+" 192.0.2.10 "+strconv.Itoa(len(bundle))+"\n", f.logs.String())
}

func TestReportTrustedByMacToken(t *testing.T) {
	f := newReportFixture(t)

	res := f.send(t, reportRequest{bearer: f.studio})
	f.now = f.now.Add(time.Second)
	unknown := f.send(t, reportRequest{bearer: "cmr_unknown"})
	list := f.list(t, "")

	assert.Equal(t, http.StatusCreated, res.Code)
	assert.Equal(t, http.StatusCreated, unknown.Code)
	require.Len(t, list, 2)
	assert.True(t, list[0].Trusted)
	assert.Equal(t, "studio", list[0].Mac)
	assert.False(t, list[1].Trusted)
	assert.Empty(t, list[1].Mac)
}

func TestReportPrivateAPIRefusesNonAdmin(t *testing.T) {
	f := newReportFixture(t)

	for _, token := range []string{f.studio, ""} {
		req := httptest.NewRequest(http.MethodGet, "/v1/reports", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		f.relay.handler().ServeHTTP(res, req)

		assert.Equal(t, http.StatusUnauthorized, res.Code)
	}
	require.NoError(t, os.Remove(f.relay.admins.path))
	assert.Equal(t, http.StatusUnauthorized, f.private(t, http.MethodGet, "/v1/reports").Code)
}

func TestReportRefusals(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, f *reportFixture) int
		want int
	}{
		{"unsigned", func(t *testing.T, f *reportFixture) int {
			return f.send(t, reportRequest{unsigned: true}).Code
		}, http.StatusUnauthorized},
		{"replayed signature", func(t *testing.T, f *reportFixture) int {
			body := reportZip(t, `{"text":"it broke"}`, 0)
			require.Equal(t, http.StatusCreated, f.send(t, reportRequest{body: body}).Code)
			return f.send(t, reportRequest{body: body}).Code
		}, http.StatusUnauthorized},
		{"replayed signature in another base64 spelling", func(t *testing.T, f *reportFixture) int {
			body := reportZip(t, `{"text":"it broke"}`, 0)
			require.Equal(t, http.StatusCreated, f.send(t, reportRequest{body: body}).Code)
			return f.send(t, reportRequest{body: body, tamper: respellSignature}).Code
		}, http.StatusUnauthorized},
		{"banned key", func(t *testing.T, f *reportFixture) int {
			f.writeBans(t, "# spam\nkey "+report.KeyID(f.key.Public().(ed25519.PublicKey))+"\n")
			return f.send(t, reportRequest{}).Code
		}, http.StatusForbidden},
		{"signature refused by a ban is not used up", func(t *testing.T, f *reportFixture) int {
			body := reportZip(t, `{"text":"it broke"}`, 0)
			f.writeBans(t, "key "+report.KeyID(f.key.Public().(ed25519.PublicKey))+"\n")
			require.Equal(t, http.StatusForbidden, f.send(t, reportRequest{body: body}).Code)
			f.writeBans(t, "")
			return f.send(t, reportRequest{body: body}).Code
		}, http.StatusCreated},
		{"banned ip", func(t *testing.T, f *reportFixture) int {
			f.writeBans(t, "ip 198.51.100.7\n")
			return f.send(t, reportRequest{remote: "198.51.100.7:4000"}).Code
		}, http.StatusForbidden},
		{"too big", func(t *testing.T, f *reportFixture) int {
			return f.send(t, reportRequest{body: make([]byte, report.MaxBundle+1)}).Code
		}, http.StatusRequestEntityTooLarge},
		{"content length understated", func(t *testing.T, f *reportFixture) int {
			return f.send(t, reportRequest{body: make([]byte, report.MaxBundle+1), tamper: func(req *http.Request) { req.ContentLength = 100 }}).Code
		}, http.StatusRequestEntityTooLarge},
		{"no content length", func(t *testing.T, f *reportFixture) int {
			return f.send(t, reportRequest{tamper: func(req *http.Request) { req.ContentLength = -1 }}).Code
		}, http.StatusLengthRequired},
		{"report.json over 64 KiB", func(t *testing.T, f *reportFixture) int {
			return f.send(t, reportRequest{body: reportZip(t, `{"text":"x","sessionTitle":"`+strings.Repeat("a", 65<<10)+`"}`, 0)}).Code
		}, http.StatusBadRequest},
		{"bad report.json", func(t *testing.T, f *reportFixture) int {
			return f.send(t, reportRequest{body: reportZip(t, `{"text":""}`, 0)}).Code
		}, http.StatusBadRequest},
		{"entry the dev Mac would refuse", func(t *testing.T, f *reportFixture) int {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for _, name := range []string{report.MetaName, "../x"} {
				w, err := zw.Create(name)
				require.NoError(t, err)
				_, err = w.Write([]byte(`{"text":"it broke"}`))
				require.NoError(t, err)
			}
			require.NoError(t, zw.Close())
			return f.send(t, reportRequest{body: buf.Bytes()}).Code
		}, http.StatusBadRequest},
		{"fourth from one key in an hour", func(t *testing.T, f *reportFixture) int {
			for range 3 {
				require.Equal(t, http.StatusCreated, f.send(t, reportRequest{}).Code)
			}
			return f.send(t, reportRequest{}).Code
		}, http.StatusTooManyRequests},
		{"eleventh from one public ip across keys and spoofed forwards", func(t *testing.T, f *reportFixture) int {
			for i := range 10 {
				res := f.send(t, reportRequest{key: newKey(t, i/3), remote: "203.0.113.5:4000", forward: "192.0.2." + strconv.Itoa(i)})
				require.Equal(t, http.StatusCreated, res.Code)
			}
			return f.send(t, reportRequest{key: newKey(t, 4), remote: "203.0.113.5:4000", forward: "192.0.2.99"}).Code
		}, http.StatusTooManyRequests},
		{"eleventh from one ipv6 /64", func(t *testing.T, f *reportFixture) int {
			for i := range 10 {
				res := f.send(t, reportRequest{key: newKey(t, i/3), remote: "[2001:db8::" + strconv.Itoa(i+1) + "]:4000"})
				require.Equal(t, http.StatusCreated, res.Code)
			}
			return f.send(t, reportRequest{key: newKey(t, 4), remote: "[2001:db8::ff]:4000"}).Code
		}, http.StatusTooManyRequests},
		{"anonymous uploads in flight leave the Mac slots free", func(t *testing.T, f *reportFixture) int {
			for range cap(f.relay.uploads) {
				f.relay.uploads <- struct{}{}
			}
			require.Equal(t, http.StatusCreated, f.send(t, reportRequest{bearer: f.studio}).Code)
			return f.send(t, reportRequest{}).Code
		}, http.StatusTooManyRequests},
		{"over quota", func(t *testing.T, f *reportFixture) int {
			f.relay.reports.quota = 1 << 10
			return f.send(t, reportRequest{body: reportZip(t, `{"text":"big"}`, 2<<10)}).Code
		}, http.StatusInsufficientStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newReportFixture(t)

			assert.Equal(t, tt.want, tt.run(t, f))
		})
	}
}

func TestReportOneUploadPerIP(t *testing.T) {
	f := newReportFixture(t)
	body, upload := io.Pipe()
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", body)
	req.ContentLength = 1 << 10
	first := make(chan int)
	go func() { first <- f.serve(req).Code }()
	_, err := upload.Write([]byte{0})
	require.NoError(t, err)

	second := f.send(t, reportRequest{})
	other := f.send(t, reportRequest{remote: "198.51.100.9:4000"})
	require.NoError(t, upload.Close())

	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.Equal(t, http.StatusCreated, other.Code)
	assert.Equal(t, http.StatusUnauthorized, <-first)
}

func TestReportStalledUploadIsCutOff(t *testing.T) {
	for _, claimed := range []int{report.MaxBundle, 100 << 10} {
		t.Run(strconv.Itoa(claimed), func(t *testing.T) {
			f := newReportFixture(t)
			f.relay.grace = 200 * time.Millisecond
			ts := httptest.NewServer(f.relay.handler())
			t.Cleanup(ts.Close)
			conn, err := net.Dial("tcp", ts.Listener.Addr().String())
			require.NoError(t, err)
			defer conn.Close()
			start := time.Now()
			_, err = fmt.Fprintf(conn, "POST /v1/reports HTTP/1.1\r\nHost: relay\r\nContent-Length: %d\r\n\r\nPK", claimed)
			require.NoError(t, err)
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

			res, err := http.ReadResponse(bufio.NewReader(conn), nil)
			require.NoError(t, err)
			cut := time.Since(start)
			require.NoError(t, res.Body.Close())
			next := f.sendTo(t, ts.URL)

			assert.Equal(t, http.StatusBadRequest, res.StatusCode)
			assert.True(t, res.Close)
			assert.Less(t, cut, 2*time.Second)
			assert.Equal(t, http.StatusCreated, next)
		})
	}
}

func TestReportLeavesTheConnectionAliveAfterTheBody(t *testing.T) {
	f := newReportFixture(t)
	f.relay.grace = 50 * time.Millisecond
	mux := http.NewServeMux()
	mux.Handle("/", f.relay.handler())
	mux.HandleFunc("GET /alive", func(w http.ResponseWriter, req *http.Request) {
		if req.Context().Err() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	responses := bufio.NewReader(conn)
	body := reportZip(t, `{"text":"it broke"}`, 16)
	upload, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/reports", bytes.NewReader(body))
	require.NoError(t, err)
	report.Sign(upload, f.key, body, f.now)
	alive, err := http.NewRequest(http.MethodGet, ts.URL+"/alive", nil)
	require.NoError(t, err)

	f.relay.reports.mu.Lock()
	require.NoError(t, upload.Write(conn))
	time.Sleep(4 * f.relay.grace)
	f.relay.reports.mu.Unlock()
	first, err := http.ReadResponse(responses, upload)
	require.NoError(t, err)
	require.NoError(t, first.Body.Close())
	require.NoError(t, alive.Write(conn))
	second, err := http.ReadResponse(responses, alive)
	require.NoError(t, err)
	require.NoError(t, second.Body.Close())

	assert.Equal(t, http.StatusCreated, first.StatusCode)
	assert.Equal(t, http.StatusOK, second.StatusCode)
}

func TestStoreListFailsOnUnreadableMeta(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, reportQuota)
	require.NoError(t, err)
	st, err := s.add(report.Stored{Received: time.Unix(1_800_000_000, 0).UTC()}, []byte("b"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, st.ID, metaFile), []byte("{"), 0o600))

	_, err = s.list("", 100)

	assert.Error(t, err)
}

func TestStoreReopenAndExpire(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0).UTC()
	s, err := openStore(dir, reportQuota)
	require.NoError(t, err)
	old, err := s.add(report.Stored{Received: now.Add(-31 * 24 * time.Hour), Report: report.Meta{Text: "old"}}, []byte("old bundle"))
	require.NoError(t, err)
	fresh, err := s.add(report.Stored{Received: now, Report: report.Meta{Text: "fresh"}}, []byte("fresh"))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "unrelated"), 0o700))
	oldSize, freshSize := diskSize(t, dir, old.ID), diskSize(t, dir, fresh.ID)

	reopened, err := openStore(dir, reportQuota)
	require.NoError(t, err)
	before, err := reopened.list("", 100)
	require.NoError(t, err)
	usedBefore := reopened.used
	require.NoError(t, reopened.sweep(now))

	assert.Equal(t, oldSize+freshSize, usedBefore)
	assert.Equal(t, []report.Stored{old, fresh}, before)
	after, err := reopened.list("", 100)
	require.NoError(t, err)
	assert.Equal(t, []report.Stored{fresh}, after)
	assert.Equal(t, freshSize, reopened.used)
	assert.NoDirExists(t, filepath.Join(dir, old.ID))
	assert.DirExists(t, filepath.Join(dir, "unrelated"))
}

func TestStorePagesInInsertionOrder(t *testing.T) {
	s, err := openStore(t.TempDir(), reportQuota)
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0).UTC()
	var ids []string
	for range 10 {
		st, err := s.add(report.Stored{Received: now}, []byte("b"))
		require.NoError(t, err)
		ids = append(ids, st.ID)
	}

	page, err := s.list(ids[0], 100)
	require.NoError(t, err)
	var listed []string
	for _, st := range page {
		listed = append(listed, st.ID)
	}

	assert.Equal(t, ids[1:], listed)
}

func TestLimiterPrunesIdle(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := newLimiter(1, 1, func() time.Time { return now })
	l.allow("old")
	now = now.Add(25 * time.Hour)
	l.allow("new")

	l.prune(now)

	assert.Equal(t, []string{"new"}, slices.Collect(maps.Keys(l.buckets)))
}

func TestLimiterRefusesNewKeysWhenFull(t *testing.T) {
	l := newLimiter(1, 2, time.Now)
	l.max = 1

	assert.True(t, l.allow("a"))
	assert.False(t, l.allow("b"))
	assert.True(t, l.allow("a"))
}

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	dir := t.TempDir()
	macs := filepath.Join(dir, "tokens")
	studio, err := addToken(macs, "studio")
	require.NoError(t, err)
	toks, err := loadTokens(macs)
	require.NoError(t, err)
	adminsPath := filepath.Join(dir, "admins")
	admin, err := addToken(adminsPath, "cali")
	require.NoError(t, err)
	admins, err := loadTokens(adminsPath)
	require.NoError(t, err)
	bansPath := filepath.Join(dir, "bans")
	b, err := loadBans(bansPath)
	require.NoError(t, err)
	store, err := openStore(filepath.Join(dir, "reports"), reportQuota)
	require.NoError(t, err)
	f := &reportFixture{now: time.Unix(1_800_000_000, 0).UTC(), key: newKey(t, 0), studio: studio, admin: admin, bans: bansPath, logs: &bytes.Buffer{}}
	clock := func() time.Time { return f.now }
	f.relay = &relay{
		tokens:     toks,
		admins:     admins,
		bans:       b,
		reports:    store,
		limits:     newReportLimits(clock),
		seen:       newReplayCache(),
		uploads:    make(chan struct{}, maxUploads),
		macUploads: make(chan struct{}, maxMacUploads),
		active:     newInflight(),
		grace:      minTransfer,
		now:        clock,
		log:        log.New(f.logs, "", 0),
	}
	return f
}

func (f *reportFixture) send(t *testing.T, r reportRequest) *httptest.ResponseRecorder {
	t.Helper()
	if r.body == nil {
		r.body = reportZip(t, `{"text":"it broke"}`, 16)
	}
	if r.key == nil {
		r.key = f.key
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", bytes.NewReader(r.body))
	req.Header.Set("Content-Type", "application/zip")
	if r.remote != "" {
		req.RemoteAddr = r.remote
	}
	if r.forward != "" {
		req.Header.Set("X-Forwarded-For", r.forward)
	}
	if r.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.bearer)
	}
	if !r.unsigned {
		report.Sign(req, r.key, r.body, f.now)
	}
	if r.tamper != nil {
		r.tamper(req)
	}
	return f.serve(req)
}

func (f *reportFixture) serve(req *http.Request) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	f.relay.handler().ServeHTTP(res, req)
	return res
}

func (f *reportFixture) sendTo(t *testing.T, url string) int {
	t.Helper()
	body := reportZip(t, `{"text":"it broke"}`, 16)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/reports", bytes.NewReader(body))
	require.NoError(t, err)
	report.Sign(req, f.key, body, f.now)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	return res.StatusCode
}

func (f *reportFixture) private(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+f.admin)
	return f.serve(req)
}

func (f *reportFixture) list(t *testing.T, after string) []report.Stored {
	t.Helper()
	res := f.private(t, http.MethodGet, "/v1/reports?after="+after)
	require.Equal(t, http.StatusOK, res.Code)
	var out []report.Stored
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	return out
}

func (f *reportFixture) writeBans(t *testing.T, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(f.bans, []byte(content), 0o600))
	touch(t, f.bans, time.Minute)
}

func reportZip(t *testing.T, meta string, padding int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(report.MetaName)
	require.NoError(t, err)
	_, err = w.Write([]byte(meta))
	require.NoError(t, err)
	if padding > 0 {
		w, err := zw.Create("app.log")
		require.NoError(t, err)
		_, err = w.Write(randomBytes(t, padding))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func respellSignature(req *http.Request) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	sig := req.Header.Get(report.HeaderSignature)
	last := strings.IndexByte(alphabet, sig[len(sig)-1])
	req.Header.Set(report.HeaderSignature, sig[:len(sig)-1]+string(alphabet[last^1]))
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return b
}

func diskSize(t *testing.T, dir, id string) int64 {
	t.Helper()
	var n int64
	for _, name := range []string{bundleFile, metaFile} {
		info, err := os.Stat(filepath.Join(dir, id, name))
		require.NoError(t, err)
		n += info.Size()
	}
	return n
}

func newKey(t *testing.T, seed int) ed25519.PrivateKey {
	t.Helper()
	s := make([]byte, ed25519.SeedSize)
	s[0] = byte(seed + 1)
	return ed25519.NewKeyFromSeed(s)
}
