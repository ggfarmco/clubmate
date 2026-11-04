package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/report"
)

type reportCall struct {
	path, auth, contentType string
	body                    []byte
	verifyErr               error
}

func TestReportUploadSignsAndSendsToken(t *testing.T) {
	f := newFixture(t)
	ts, calls := f.reportRelay(t, http.StatusCreated, `{"id":"rpt_1"}`)
	require.NoError(t, f.srv.relay.configure(t.Context(), ts.URL, relayToken))
	r := f.reports(t, ts.Client())
	path, bundle := writeBundle(t, map[string]string{report.MetaName: `{"text":"broken"}`})

	res, err := r.upload(t.Context(), path)

	require.NoError(t, err)
	assert.Equal(t, reportUploadResult{ID: "rpt_1"}, res)
	require.Len(t, *calls, 1)
	c := (*calls)[0]
	assert.NoError(t, c.verifyErr)
	assert.Equal(t, reportsPath, c.path)
	assert.Equal(t, "Bearer "+relayToken, c.auth)
	assert.Equal(t, "application/zip", c.contentType)
	assert.Equal(t, bundle, c.body)
	assert.Equal(t, ts.Listener.Addr().String(), f.statusApp().status().ReportRelay)
}

func TestReportUploadWithoutPushRelay(t *testing.T) {
	f := newFixture(t)
	ts, calls := f.reportRelay(t, http.StatusCreated, `{"id":"rpt_2"}`)
	assert.Equal(t, "clubmate-push.exe.xyz", f.statusApp().status().ReportRelay)
	f.srv.relay.reportURL = ts.URL
	r := f.reports(t, ts.Client())
	path, _ := writeBundle(t, map[string]string{report.MetaName: `{"text":"broken"}`})

	res, err := r.upload(t.Context(), path)

	require.NoError(t, err)
	assert.Equal(t, "rpt_2", res.ID)
	require.Len(t, *calls, 1)
	assert.Empty(t, (*calls)[0].auth)
	assert.NoError(t, (*calls)[0].verifyErr)
}

func TestReportUploadRefusals(t *testing.T) {
	f := newFixture(t)
	ts, calls := f.reportRelay(t, http.StatusTooManyRequests, "slow down")
	f.srv.relay.reportURL = ts.URL
	r := f.reports(t, ts.Client())
	small, _ := writeBundle(t, map[string]string{report.MetaName: `{"text":"broken"}`})
	big := filepath.Join(t.TempDir(), "big.zip")
	require.NoError(t, os.WriteFile(big, make([]byte, report.MaxBundle+1), 0o600))

	_, tooBig := r.upload(t.Context(), big)
	_, limited := r.upload(t.Context(), small)

	assert.EqualError(t, tooBig, "report is over 25 MiB")
	assert.EqualError(t, limited, "too many reports, try again later")
	assert.Len(t, *calls, 1)
}

func TestReportKeyStable(t *testing.T) {
	dir := t.TempDir()

	first, err := loadReportKey(dir)
	require.NoError(t, err)
	second, err := loadReportKey(dir)
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(dir, reportKeyFile))
	require.NoError(t, err)
	bad := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bad, reportKeyFile), []byte("short"), 0o600))
	_, badErr := loadReportKey(bad)

	assert.True(t, first.Equal(second))
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.ErrorContains(t, badErr, "5 bytes")
}

func (f *fixture) reports(t *testing.T, client *http.Client) *reports {
	t.Helper()
	return newReports(f.srv.relay, f.kv, client, t.TempDir(), f.clock.Now)
}

func (f *fixture) statusApp() *app {
	return &app{srv: f.srv, ident: f.srv.identity}
}

func (f *fixture) reportRelay(t *testing.T, code int, reply string) (*httptest.Server, *[]reportCall) {
	t.Helper()
	var calls []reportCall
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		_, err := report.Verify(req, body, f.clock.Now())
		calls = append(calls, reportCall{path: req.URL.Path, auth: req.Header.Get("Authorization"), contentType: req.Header.Get("Content-Type"), body: body, verifyErr: err})
		w.WriteHeader(code)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(ts.Close)
	return ts, &calls
}

func writeBundle(t *testing.T, files map[string]string) (string, []byte) {
	t.Helper()
	bundle := zipBytes(t, files)
	path := filepath.Join(t.TempDir(), "report.zip")
	require.NoError(t, os.WriteFile(path, bundle, 0o600))
	return path, bundle
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(w, content)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
