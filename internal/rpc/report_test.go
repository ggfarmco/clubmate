package rpc_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/plugin"
	"clubmate/internal/rpc"
	internalsdui "clubmate/internal/sdui"
	"clubmate/internal/testutil"
	"clubmate/internal/workspace"
	"clubmate/pkg/report"
)

const reportCaller = "agt_0123456789abcdef"

type reportRequest struct {
	ID            string              `json:"id"`
	WorkspaceID   string              `json:"workspaceId"`
	WorkspaceName string              `json:"workspaceName"`
	RelayHost     string              `json:"relayHost"`
	Text          string              `json:"text"`
	Summary       string              `json:"summary"`
	SessionTitle  string              `json:"sessionTitle"`
	TimeoutMs     int64               `json:"timeoutMs"`
	MaxBytes      int64               `json:"maxBytes"`
	MaxText       int64               `json:"maxText"`
	Files         []reportFileRequest `json:"files"`
	missing       []string
}

type reportFileRequest struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	Path    string `json:"path"`
	Checked bool   `json:"checked"`
	Warning string `json:"warning"`
	Source  string `json:"source"`
}

type reportApp struct {
	asked     chan reportRequest
	reply     chan any
	withdrawn chan string
}

type reportRouter struct {
	rpc.Router
	mu       sync.Mutex
	fromTurn []int
	timeout  int
	zipPath  string
	zip      []byte
}

type reportFixture struct {
	h      *testutil.PluginHarness
	ws     workspace.Workspace
	folder string
	app    *reportApp
	router *reportRouter
}

func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	h := testutil.NewHarness(t)
	rpc.WithDirs(h.Dirs)(h.Srv)
	folder := t.TempDir()
	ws, err := h.Svc.CreateWorkspace(t.Context(), "demo", []string{folder})
	require.NoError(t, err)
	h.Start(t)
	f := &reportFixture{h: h, ws: ws, folder: folder, router: &reportRouter{Router: plugin.NewRouter(h.Sup)}}
	h.Srv.SetUI(h.Store, internalsdui.NewSections(nil), f.router)
	f.app = newReportApp(t, h)
	return f
}

func newReportApp(t *testing.T, h *testutil.PluginHarness) *reportApp {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server := h.Srv.ServeConn(context.Background(), jsonrpc2.NewPlainObjectStream(serverSide), rpc.Caller{})
	a := &reportApp{asked: make(chan reportRequest, 1), reply: make(chan any), withdrawn: make(chan string, 1)}
	done := make(chan struct{})
	handler := jsonrpc2.HandlerWithError(func(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
		switch req.Method {
		case "ui.confirm":
			return map[string]bool{"approved": true}, nil
		case "ui.report.cancelled":
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(*req.Params, &p); err != nil {
				return nil, err
			}
			a.withdrawn <- p.ID
			return nil, nil
		case "ui.report":
		default:
			return nil, nil
		}
		var p reportRequest
		if err := json.Unmarshal(*req.Params, &p); err != nil {
			return nil, err
		}
		for _, f := range p.Files {
			if _, err := os.Stat(f.Path); err != nil || !filepath.IsAbs(f.Path) {
				p.missing = append(p.missing, f.Path)
			}
		}
		a.asked <- p
		select {
		case r := <-a.reply:
			return r, nil
		case <-done:
			return nil, nil
		}
	})
	conn := jsonrpc2.NewConn(context.Background(), jsonrpc2.NewPlainObjectStream(clientSide), jsonrpc2.AsyncHandler(handler))
	t.Cleanup(func() {
		close(done)
		conn.Close()
		server.Close()
	})
	require.NoError(t, conn.Call(t.Context(), "client.register", map[string]any{"handles": []string{"ui.report", "ui.confirm"}}, nil))
	return a
}

func (r *reportRouter) Resolve(id string) (rpc.Command, bool) {
	switch id {
	case "agents.transcript", "remote.status", "remote.report-upload":
		return rpc.Command{CommandDef: rpc.CommandDef{ID: id}, Plugin: strings.Split(id, ".")[0]}, true
	}
	return r.Router.Resolve(id)
}

func (r *reportRouter) Command(ctx context.Context, c rpc.Command, run json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Args struct {
			Path     string `json:"path"`
			Last     bool   `json:"last"`
			FromTurn int    `json:"fromTurn"`
		} `json:"args"`
	}
	if err := json.Unmarshal(run, &p); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch c.ID {
	case "remote.status":
		return json.RawMessage(`{"reportRelay":"relay.example"}`), nil
	case "agents.transcript":
		if p.Args.Last {
			return json.RawMessage(`{"session":{"title":"Fix graphs"},"turns":[{"turn":5}],"entries":[{"kind":"user","turn":5}]}`), nil
		}
		r.fromTurn = append(r.fromTurn, p.Args.FromTurn)
		return json.RawMessage(`{"session":{"title":"Fix graphs"},"turns":[{"turn":3},{"turn":4},{"turn":5}],"entries":[]}`), nil
	case "remote.report-upload":
		data, err := os.ReadFile(p.Args.Path)
		if err != nil {
			return nil, err
		}
		r.zipPath, r.zip, r.timeout = p.Args.Path, data, c.Timeout
		return json.RawMessage(`{"id":"rpt_1"}`), nil
	}
	return r.Router.Command(ctx, c, run)
}

func (f *reportFixture) send(t *testing.T, args map[string]any) <-chan json.RawMessage {
	t.Helper()
	out := make(chan json.RawMessage, 1)
	c := f.h.Client(t)
	go func() {
		var res json.RawMessage
		if err := c.Call(context.Background(), "command.run", map[string]any{"id": "report.send", "workspaceId": f.ws.ID, "agent": true, "caller": reportCaller, "args": args}, &res); err != nil {
			res, _ = json.Marshal(map[string]string{"error": err.Error()})
		}
		out <- res
	}()
	return out
}

func (r reportRequest) file(t *testing.T, name string) reportFileRequest {
	t.Helper()
	i := slices.IndexFunc(r.Files, func(f reportFileRequest) bool { return f.Name == name })
	require.GreaterOrEqual(t, i, 0, name)
	return r.Files[i]
}

func (f *reportFixture) leftovers(t *testing.T) []string {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(f.h.Dirs.DiagnosticsDir(), "report-*"))
	require.NoError(t, err)
	return left
}

func TestReportSendUploadsKeptFiles(t *testing.T) {
	f := newReportFixture(t)
	require.NoError(t, os.WriteFile(f.h.Dirs.CoreLog(), []byte("core says hi\n"), 0o600))
	shot := filepath.Join(f.folder, "shot.png")
	require.NoError(t, os.WriteFile(shot, []byte("png"), 0o600))

	res := f.send(t, map[string]any{"text": "graphs keeps timing out", "summary": "run hung", "files": []string{shot}})
	asked := <-f.app.asked
	assert.Equal(t, [7]any{f.ws.ID, "demo", "relay.example", "Fix graphs", int64(600000), int64(report.MaxBundle), int64(report.MaxText)}, [7]any{asked.WorkspaceID, asked.WorkspaceName, asked.RelayHost, asked.SessionTitle, asked.TimeoutMs, asked.MaxBytes, asked.MaxText})
	var kept []string
	for _, file := range asked.Files {
		assert.True(t, file.Checked, file.Name)
		if file.Name != "logs/core.log" {
			kept = append(kept, file.Name)
		}
	}
	assert.Contains(t, kept, "transcript.json")
	assert.Contains(t, kept, "files/shot.png")
	copied := asked.file(t, "files/shot.png")
	resolved, err := filepath.EvalSymlinks(shot)
	require.NoError(t, err)
	assert.Equal(t, resolved, copied.Source)
	assert.True(t, strings.HasPrefix(copied.Path, f.h.Dirs.DiagnosticsDir()), copied.Path)
	f.app.reply <- map[string]any{"send": true, "appVersion": "0.0.16", "text": "graphs times out after wake", "summary": "run hung", "files": kept, "extra": map[string][]byte{"settings.json": []byte(`{"a":1}`)}}

	assert.JSONEq(t, `{"sent":true,"id":"rpt_1"}`, string(<-res))
	f.router.mu.Lock()
	defer f.router.mu.Unlock()
	assert.Equal(t, []int{3}, f.router.fromTurn)
	assert.Equal(t, 120, f.router.timeout)
	assert.NoFileExists(t, f.router.zipPath)
	assert.Empty(t, f.leftovers(t))
	bundle := filepath.Join(t.TempDir(), "report.zip")
	require.NoError(t, os.WriteFile(bundle, f.router.zip, 0o600))
	files := readZip(t, bundle)
	assert.NotContains(t, files, "logs/core.log")
	assert.Equal(t, "png", files["files/shot.png"])
	assert.Equal(t, `{"a":1}`, files["settings.json"])
	meta, err := report.ReadMeta(f.router.zip)
	require.NoError(t, err)
	assert.Equal(t, [4]string{"graphs times out after wake", "run hung", reportCaller, "Fix graphs"}, [4]string{meta.Text, meta.Summary, meta.SessionID, meta.SessionTitle})
	assert.Contains(t, meta.Files, "settings.json")
	assert.Equal(t, "0.0.16", meta.AppVersion)
}

func TestReportSendUnsent(t *testing.T) {
	for reason, want := range map[string]string{
		"":          `{"sent":false,"reason":"cancelled"}`,
		"timed out": `{"sent":false,"reason":"timed out"}`,
		"no window": `{"error":"no app to show the report"}`,
	} {
		t.Run(reason, func(t *testing.T) {
			f := newReportFixture(t)
			res := f.send(t, map[string]any{"text": "broken"})
			asked := <-f.app.asked
			assert.NotEmpty(t, asked.Files)
			assert.Empty(t, asked.missing)
			f.app.reply <- map[string]any{"send": false, "reason": reason}

			assert.JSONEq(t, want, string(<-res))
			assert.Empty(t, f.leftovers(t))
			f.router.mu.Lock()
			defer f.router.mu.Unlock()
			assert.Empty(t, f.router.zipPath)
		})
	}
}

func TestReportSendTimesOut(t *testing.T) {
	f := newReportFixture(t)
	rpc.SetReportTimeout(f.h.Srv, 100*time.Millisecond)
	res := f.send(t, map[string]any{"text": "broken"})
	assert.Equal(t, int64(100), (<-f.app.asked).TimeoutMs)

	assert.JSONEq(t, `{"sent":false,"reason":"timed out"}`, string(<-res))
	assert.Empty(t, f.leftovers(t))
}

func TestReportCallerGoneWithdrawsSheet(t *testing.T) {
	f := newReportFixture(t)
	f.h.Client(t)
	c, err := rpc.Dial(t.Context(), f.h.Dirs.CoreSocket())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	go func() {
		_ = c.Call(t.Context(), "command.run", map[string]any{"id": "report.send", "workspaceId": f.ws.ID, "agent": true, "caller": reportCaller, "args": map[string]any{"text": "broken"}}, nil)
	}()
	asked := <-f.app.asked
	require.NotEmpty(t, asked.ID)
	require.NoError(t, c.Close())
	select {
	case id := <-f.app.withdrawn:
		assert.Equal(t, asked.ID, id)
	case <-time.After(5 * time.Second):
		t.Fatal("report not withdrawn")
	}
	testutil.Eventually(t, func() bool { return len(f.leftovers(t)) == 0 }, 5*time.Second)
}

func TestReportFileOutsideWorkspace(t *testing.T) {
	f := newReportFixture(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "id_rsa"), []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.folder, "link")))
	notes := filepath.Join(f.folder, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("notes"), 0o600))

	res := f.send(t, map[string]any{"text": "broken", "files": []string{filepath.Join(f.folder, "link", "id_rsa"), notes}})
	asked := <-f.app.asked
	f.app.reply <- map[string]any{"send": false}
	<-res

	secret := asked.file(t, "files/id_rsa")
	realOutside, err := filepath.EvalSymlinks(outside)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realOutside, "id_rsa"), secret.Source)
	assert.Equal(t, [3]any{false, "outside the workspace", int64(6)}, [3]any{secret.Checked, secret.Warning, secret.Bytes})
	inside := asked.file(t, "files/notes.txt")
	assert.Equal(t, [2]any{true, ""}, [2]any{inside.Checked, inside.Warning})
}

func TestReportDoesNotBlockConfirm(t *testing.T) {
	f := newReportFixture(t)
	res := f.send(t, map[string]any{"text": "broken"})
	<-f.app.asked

	err := f.h.PluginCall(t, "demo", "ui.approve", map[string]any{"workspaceId": f.ws.ID, "title": "Run setup", "command": "demo.run"})
	require.NoError(t, err)
	f.app.reply <- map[string]any{"send": false}
	<-res
}

func TestReportSendRefusesPlugins(t *testing.T) {
	f := newReportFixture(t)
	err := f.h.PluginCall(t, "demo", "report.send", map[string]any{"workspaceId": f.ws.ID, "text": "broken"})
	assert.ErrorContains(t, err, "not available to plugins")
	assert.Empty(t, f.leftovers(t))
}

func TestReportRefusesBadFiles(t *testing.T) {
	f := newReportFixture(t)
	fifo := filepath.Join(f.folder, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	half := report.MaxBundle/2 + 1
	var big []string
	for _, name := range []string{"a.bin", "b.bin"} {
		p := filepath.Join(f.folder, name)
		require.NoError(t, os.WriteFile(p, nil, 0o600))
		require.NoError(t, os.Truncate(p, int64(half)))
		big = append(big, p)
	}

	assert.Contains(t, string(<-f.send(t, map[string]any{"text": "broken", "files": []string{fifo}})), "is not a regular file")
	assert.Contains(t, string(<-f.send(t, map[string]any{"text": "broken", "files": big})), "b.bin takes the attached files over 25 MiB")
	assert.Contains(t, string(<-f.send(t, map[string]any{"text": "broken", "files": slices.Repeat([]string{fifo}, 51)})), "at most 50 files")
	assert.Empty(t, f.app.asked)
	assert.Empty(t, f.leftovers(t))
}

func TestRemoveStaleReports(t *testing.T) {
	dirs := testutil.ShortDirs(t)
	srv, _ := testutil.NewRPCServerIn(t, dirs)
	diag := dirs.DiagnosticsDir()
	require.NoError(t, os.MkdirAll(filepath.Join(diag, "report-20261104-120000-1", "files"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(diag, "report-20261104-120000-1.zip"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(diag, "clubmate-diagnostics-20261104-120000.zip"), nil, 0o600))

	require.NoError(t, srv.RemoveStaleReports())

	left, err := os.ReadDir(diag)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, "clubmate-diagnostics-20261104-120000.zip", left[0].Name())
}
