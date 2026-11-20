package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/internal/tunnelrelay"
	"clubmate/pkg/pluginsdk"
)

const (
	liveRelayEnv      = "CLUBMATE_TEST_LIVE_RELAY"
	liveRelayTokenEnv = "CLUBMATE_TEST_LIVE_RELAY_TOKEN"
)

func TestHelloAndAnEventStreamThroughTheRelayBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "clubmate-push-relay")
	out, err := exec.Command("go", "build", "-o", bin, "clubmate/cmd/clubmate-push-relay").CombinedOutput()
	require.NoError(t, err, string(out))
	tokens := filepath.Join(dir, "tokens")
	macToken := relayCommand(t, bin, "token", "add", "studio", "-tokens", tokens)
	addr := freeAddr(t)
	relay := exec.Command(bin, "-listen", addr, "-key", apnsKey(t, dir), "-team-id", "TEAM000001", "-tokens", tokens, "-tickets", filepath.Join(dir, "tickets.json"), "-signup", "off")
	require.NoError(t, relay.Start())
	t.Cleanup(func() {
		_ = relay.Process.Kill()
		_ = relay.Wait()
	})
	base := "http://" + addr
	testutil.Eventually(t, func() bool {
		res, err := http.Get(base + "/healthz")
		if err == nil {
			res.Body.Close()
		}
		return err == nil
	}, 10*time.Second)

	checkRelayPath(t, base, macToken)
}

func TestHelloAndAnEventStreamThroughTheLiveRelay(t *testing.T) {
	base, macToken := os.Getenv(liveRelayEnv), os.Getenv(liveRelayTokenEnv)
	if base == "" || macToken == "" {
		t.Skip("set " + liveRelayEnv + " and " + liveRelayTokenEnv + " (a test cmr_ token) to run against a deployed relay")
	}

	checkRelayPath(t, strings.TrimRight(base, "/"), macToken)
}

func checkRelayPath(t *testing.T, base, macToken string) {
	t.Helper()
	t.Setenv(testModeEnv, "1")
	f := newFixture(t)
	require.NoError(t, f.srv.relay.configure(t.Context(), base, macToken))
	tc := newTestTunnel(t, f, fileStore{path: filepath.Join(t.TempDir(), "key")})
	ticket := "cmk_" + rand.Text()
	var cleared atomic.Bool
	tc.tickets = func(keep ...string) []tunnelrelay.Ticket {
		if cleared.Load() {
			return nil
		}
		return append(f.srv.ticketSet(keep...), tunnelrelay.Ticket{Hash: hashToken(ticket)})
	}
	tc.sess.classify = func(hash string) ticketKind {
		if hash == hashToken(ticket) {
			return ticketDevice
		}
		return f.srv.ticketKind(hash)
	}
	t.Cleanup(func() {
		cleared.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, tc.syncTickets(ctx))
	})
	require.NoError(t, tc.configure(t.Context(), true))
	token := f.pair(t, "phone")
	f.backend.events = []pluginsdk.Event{
		{Seq: 1, Topic: "agents.changed", Payload: json.RawMessage(`{}`)},
		{Seq: 2, Topic: "agents.changed", Payload: json.RawMessage(`{}`)},
	}
	phone := &fakePhone{base: base, ticket: ticket}
	testutil.Eventually(t, func() bool { return phone.open(t.Context(), t, macKey(t, tc)) == http.StatusOK }, 30*time.Second)

	head, hello := phone.rpc(t, token, "hello", nil)
	st := phone.stream(t, token, "1", "")
	connected := st.frame(t)
	st.streamID(t)
	replayed := st.frame(t)
	f.srv.hub.publish(pluginsdk.Event{Seq: 3, Topic: "agents.changed", Payload: json.RawMessage(`{}`)})
	live := st.frame(t)

	assert.Equal(t, http.StatusOK, head.Status)
	assert.Contains(t, string(hello.Result), `"id":"cmp"`)
	assert.Equal(t, http.StatusOK, st.status)
	assert.Equal(t, ": connected", connected)
	assert.True(t, strings.HasPrefix(replayed, "id: 2\n"), replayed)
	assert.True(t, strings.HasPrefix(live, "id: 3\n"), live)
}
