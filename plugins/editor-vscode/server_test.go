package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memPorts struct{ port int }

func (m *memPorts) Port(context.Context) (int, error) { return m.port, nil }

func (m *memPorts) SetPort(_ context.Context, p int) error {
	m.port = p
	return nil
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
}

func fakeServer(t *testing.T, state, tail string) string {
	t.Helper()
	bin := filepath.Join(state, "vscode", "cli", "serve-web", "c1", "bin", "code-server")
	writeScript(t, bin, `echo "$@" > "$(dirname "$0")/argv"
while [ $# -gt 0 ]; do [ "$1" = --port ] && port=$2; shift; done
echo "Web UI available at http://127.0.0.1:$port?tkn=x"
`+tail)
	return filepath.Join(filepath.Dir(bin), "argv")
}

func TestFindCode(t *testing.T) {
	hit := func(string) (string, error) { return "/usr/local/bin/code", nil }
	miss := func(string) (string, error) { return "", errors.New("not found") }
	yes := func(string) bool { return true }
	no := func(string) bool { return false }

	p, err := findCode(hit, yes)
	require.NoError(t, err)
	assert.Equal(t, "/usr/local/bin/code", p)

	p, err = findCode(miss, yes)
	require.NoError(t, err)
	assert.Equal(t, "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code", p)

	_, err = findCode(miss, no)
	assert.ErrorIs(t, err, errCodeMissing)
}

func TestDownloadFetchesServer(t *testing.T) {
	state := t.TempDir()
	var got atomic.Bool
	root := filepath.Join(state, "vscode", "cli", "serve-web")
	stale := filepath.Join(root, "old.staging", "bin", "code-server")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
	require.NoError(t, os.WriteFile(stale, nil, 0o755))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got.Store(true)
		staging := filepath.Join(root, "abc.staging")
		_ = os.MkdirAll(filepath.Join(staging, "bin"), 0o755)
		_ = os.WriteFile(filepath.Join(staging, "bin", "code-server"), nil, 0o755)
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = os.Rename(staging, filepath.Join(root, "abc"))
		}()
	}))
	defer srv.Close()
	pidFile := filepath.Join(t.TempDir(), "pid")
	code := filepath.Join(t.TempDir(), "code")
	writeScript(t, code, `echo $$ > `+pidFile+`
echo "Web UI available at `+srv.URL+`?tkn=x"
exec sleep 60
`)
	s := newVSCodeServer(state, code, &memPorts{})

	require.NoError(t, s.Download(context.Background()))

	assert.True(t, got.Load())
	assert.True(t, s.Downloaded())
	assert.FileExists(t, filepath.Join(root, "abc", "bin", "code-server"))
	assert.NoDirExists(t, filepath.Join(root, "old.staging"))
	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	assert.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestDownloadFailureIsReported(t *testing.T) {
	code := filepath.Join(t.TempDir(), "code")
	writeScript(t, code, "echo 'network down' >&2\nexit 1\n")
	s := newVSCodeServer(t.TempDir(), code, &memPorts{})

	err := s.Download(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "network down")
	assert.False(t, s.Downloaded())
}

func TestStartArgs(t *testing.T) {
	state := t.TempDir()
	argv := fakeServer(t, state, "exec sleep 60\n")
	kv := &memPorts{}
	s := newVSCodeServer(state, "code", kv)
	t.Cleanup(s.Stop)

	require.NoError(t, s.Start(context.Background(), true))

	port, token, ok := s.Base()
	require.True(t, ok)
	raw, err := os.ReadFile(argv)
	require.NoError(t, err)
	args := string(raw)
	assert.Contains(t, args, "--host 127.0.0.1")
	assert.NotContains(t, args, "--disable-workspace-trust")
	assert.Contains(t, args, "--connection-token-file")
	assert.NotContains(t, args, token)
	assert.Equal(t, port, kv.port)
}

func TestServerMovesOffTakenPort(t *testing.T) {
	state := t.TempDir()
	fakeServer(t, state, "exec sleep 60\n")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	taken := l.Addr().(*net.TCPAddr).Port
	kv := &memPorts{port: taken}
	s := newVSCodeServer(state, "code", kv)
	t.Cleanup(s.Stop)

	require.NoError(t, s.Start(context.Background(), false))

	port, _, ok := s.Base()
	require.True(t, ok)
	assert.NotEqual(t, taken, port)
	assert.Equal(t, port, kv.port)
}

func TestStartKillsOrphanAndKeepsPort(t *testing.T) {
	state := t.TempDir()
	fakeServer(t, state, `perl -MIO::Socket::INET -e 'my $s = IO::Socket::INET->new(LocalAddr => "127.0.0.1:$ARGV[0]", Listen => 1) or die; sleep 60' "$port"
`)
	kv := &memPorts{}
	orphan := newVSCodeServer(state, "code", kv)
	t.Cleanup(orphan.Stop)
	require.NoError(t, orphan.Start(context.Background(), false))
	port, _, _ := orphan.Base()
	raw, err := os.ReadFile(filepath.Join(state, "vscode", "server.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", net.JoinHostPort(vscodeHost, strconv.Itoa(port)))
		if err != nil {
			return false
		}
		return c.Close() == nil
	}, 5*time.Second, 20*time.Millisecond)

	s := newVSCodeServer(state, "code", kv)
	t.Cleanup(s.Stop)
	require.NoError(t, s.Start(context.Background(), false))

	assert.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH)
	again, _, ok := s.Base()
	require.True(t, ok)
	assert.Equal(t, port, again)
	s.Stop()
	assert.NoFileExists(t, filepath.Join(state, "vscode", "server.pid"))
}

func TestServerRestartsOnceThenFails(t *testing.T) {
	state := t.TempDir()
	fakeServer(t, state, "exit 0\n")
	s := newVSCodeServer(state, "code", &memPorts{})
	t.Cleanup(s.Stop)

	require.NoError(t, s.Start(context.Background(), false))

	assert.Error(t, <-s.Exited())
	_, _, ok := s.Base()
	assert.False(t, ok)
}

func TestWriteMachineSettingsMerges(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "data", "Machine", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte(`{"editor.fontSize": 15}`), 0o644))
	read := func() map[string]any {
		raw, err := os.ReadFile(file)
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		return m
	}

	require.NoError(t, writeMachineSettings(dir, true))
	m := read()
	assert.Equal(t, map[string]any{
		"editor.fontSize":                              float64(15),
		"workbench.colorTheme":                         "Default Dark Modern",
		"chat.disableAIFeatures":                       true,
		"workbench.secondarySideBar.defaultVisibility": "hidden",
		"task.allowAutomaticTasks":                     "off",
	}, m)

	require.NoError(t, writeMachineSettings(dir, false))
	assert.Equal(t, "Default Light Modern", read()["workbench.colorTheme"])
}

func TestWorkbenchURLEscapes(t *testing.T) {
	folder := "/tmp/a b#?%é"

	u, err := url.Parse(workbenchURL(4242, "t+k", folder, &openArgs{Path: folder + "/x y.go", Line: 3, Col: 2}))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:4242", u.Host)
	q := u.Query()
	assert.Equal(t, "t+k", q.Get("tkn"))
	assert.Equal(t, folder, q.Get("folder"))
	var payload [][]string
	require.NoError(t, json.Unmarshal([]byte(q.Get("payload")), &payload))
	require.Len(t, payload, 2)
	assert.Equal(t, []string{"gotoLineMode", "true"}, payload[1])
	uri, err := url.Parse(payload[0][1])
	require.NoError(t, err)
	assert.Equal(t, "vscode-remote", uri.Scheme)
	assert.Equal(t, "127.0.0.1:4242", uri.Host)
	assert.Equal(t, folder+"/x y.go:3:2", uri.Path)
}
