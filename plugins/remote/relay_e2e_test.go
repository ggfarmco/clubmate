package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/report"
)

func TestReportReachesTheRealRelayAndComesBackThroughTheInbox(t *testing.T) {
	t.Setenv(testModeEnv, "1")
	dir := t.TempDir()
	bin := filepath.Join(dir, "clubmate-push-relay")
	out, err := exec.Command("go", "build", "-o", bin, "clubmate/cmd/clubmate-push-relay").CombinedOutput()
	require.NoError(t, err, string(out))
	tokens, admins := filepath.Join(dir, "tokens"), filepath.Join(dir, "admins")
	macToken := relayCommand(t, bin, "token", "add", "studio", "-tokens", tokens)
	admin := relayCommand(t, bin, "admin", "add", "dev", "-admins", admins)
	addr := freeAddr(t)
	relay := exec.Command(bin, "-listen", addr, "-key", apnsKey(t, dir), "-team-id", "TEAM000001", "-tokens", tokens, "-admins", admins, "-reports", filepath.Join(dir, "reports"))
	require.NoError(t, relay.Start())
	t.Cleanup(func() {
		_ = relay.Process.Kill()
		_ = relay.Wait()
	})
	base := "http://" + addr
	testutil.Eventually(t, func() bool {
		res, err := http.Get(base + "/")
		if err == nil {
			res.Body.Close()
		}
		return err == nil
	}, 10*time.Second)

	f := newFixture(t)
	require.NoError(t, f.srv.relay.configure(t.Context(), base, macToken))
	r := newReports(f.srv.relay, f.kv, http.DefaultClient, t.TempDir(), time.Now)
	path, _ := writeBundle(t, map[string]string{report.MetaName: `{"text":"sent end to end"}`})
	sent, err := r.upload(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, r.setInbox(t.Context(), base, admin))
	got, err := r.fetch(t.Context())
	require.NoError(t, err)

	require.Len(t, got.Reports, 1)
	assert.Equal(t, sent.ID, got.Reports[0].ID)
	assert.True(t, got.Reports[0].Trusted)
	assert.Equal(t, "studio", got.Reports[0].Mac)
	assert.Equal(t, "sent end to end", got.Reports[0].Text)
	assert.FileExists(t, filepath.Join(got.Reports[0].Path, report.MetaName))
}

func relayCommand(t *testing.T, bin string, args ...string) string {
	t.Helper()
	out, err := exec.Command(bin, args...).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func apnsKey(t *testing.T, dir string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	path := filepath.Join(dir, "AuthKey_TESTKEY001.p8")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600))
	return path
}
