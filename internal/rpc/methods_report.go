package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/sys/unix"

	"clubmate/internal/buildinfo"
	"clubmate/internal/diagnostics"
	"clubmate/internal/workspace"
	"clubmate/pkg/report"
)

const (
	reportMethod         = "report.send"
	reportTimeout        = 10 * time.Minute
	reportTurns          = 3
	reportTranscriptMax  = 2 << 20
	reportTranscriptName = "transcript.json"
	reportFilesDir       = "files"
	reportOutside        = "outside the workspace"
	reportReasonCancel   = "cancelled"
	reportReasonTimeout  = "timed out"
	reportReasonNoWindow = "no window"
	reportTimeLayout     = "20060102-150405"
	reportUploadSeconds  = 120
	reportMaxFiles       = 50
	reportDirPrefix      = "report-"

	cmdAgentsTranscript = "agents.transcript"
	cmdRemoteStatus     = "remote.status"
	cmdReportUpload     = "remote.report-upload"
)

var errNoReportApp = deniedError("no app to show the report")

type reportSendParams struct {
	WorkspaceID string   `json:"workspaceId"`
	Text        string   `json:"text"`
	Summary     string   `json:"summary"`
	SessionID   string   `json:"sessionId"`
	Files       []string `json:"files"`
}

type reportSendResult struct {
	Sent   bool   `json:"sent"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type reportParams struct {
	WorkspaceID   string       `json:"workspaceId"`
	WorkspaceName string       `json:"workspaceName"`
	RelayHost     string       `json:"relayHost"`
	Text          string       `json:"text"`
	Summary       string       `json:"summary"`
	SessionTitle  string       `json:"sessionTitle"`
	TimeoutMs     int64        `json:"timeoutMs"`
	MaxBytes      int64        `json:"maxBytes"`
	MaxText       int64        `json:"maxText"`
	Files         []reportFile `json:"files"`
}

type reportFile struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	Path    string `json:"path"`
	Checked bool   `json:"checked"`
	Warning string `json:"warning"`
	Source  string `json:"source,omitempty"`
}

type reportAnswer struct {
	Send       bool              `json:"send"`
	AppVersion string            `json:"appVersion"`
	Reason     string            `json:"reason"`
	Text       string            `json:"text"`
	Summary    string            `json:"summary"`
	Files      []string          `json:"files"`
	Extra      map[string][]byte `json:"extra"`
}

type reportTranscript struct {
	Session struct {
		Title string `json:"title"`
	} `json:"session"`
	Turns []struct {
		Turn int `json:"turn"`
	} `json:"turns"`
}

type reportBundle struct {
	dir      string
	files    []reportFile
	sources  map[string]string
	attached int64
}

func (s *Server) reportMethods() map[string]handlerFunc {
	return map[string]handlerFunc{reportMethod: method(s.reportSend)}
}

func (s *Server) reportSend(ctx context.Context, p reportSendParams) (reportSendResult, error) {
	if CallerFrom(ctx).IsPlugin() {
		return reportSendResult{}, invalidParams("report.send is not available to plugins")
	}
	if err := checkReportText(p.Text, p.Summary); err != nil {
		return reportSendResult{}, err
	}
	if p.Text == "" {
		return reportSendResult{}, invalidParams("text is required")
	}
	if len(p.Files) > reportMaxFiles {
		return reportSendResult{}, invalidParams(fmt.Sprintf("files: at most %d files", reportMaxFiles))
	}
	if s.dirs == nil {
		return reportSendResult{}, errNoDiagnostics
	}
	ws, err := s.svc.GetWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return reportSendResult{}, err
	}
	conn, err := s.clientFor(handleUIReport, p.WorkspaceID)
	if err != nil {
		return reportSendResult{}, errNoReportApp
	}
	host, err := s.reportRelayHost(ctx)
	if err != nil {
		return reportSendResult{}, err
	}
	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = originFrom(ctx).caller
	}
	now := time.Now()
	if err := os.MkdirAll(s.dirs.DiagnosticsDir(), 0o700); err != nil {
		return reportSendResult{}, err
	}
	dir, err := os.MkdirTemp(s.dirs.DiagnosticsDir(), reportDirPrefix+now.Format(reportTimeLayout)+"-")
	if err != nil {
		return reportSendResult{}, err
	}
	defer os.RemoveAll(dir)
	b := &reportBundle{dir: dir, sources: map[string]string{}}
	entries, err := s.diagnosticsEntries(ctx, diagnosticsParams{WorkspaceID: p.WorkspaceID}, now)
	if err != nil {
		return reportSendResult{}, err
	}
	for _, e := range entries {
		if err := b.write(e.Name, e.Data); err != nil {
			return reportSendResult{}, err
		}
	}
	transcript, title := s.reportTranscript(ctx, p.WorkspaceID, sessionID)
	if transcript != nil {
		if err := b.write(reportTranscriptName, transcript); err != nil {
			return reportSendResult{}, err
		}
	}
	roots := workspaceRoots(ws.Folders)
	for _, src := range p.Files {
		if err := b.attach(src, roots); err != nil {
			return reportSendResult{}, err
		}
	}
	timeout := s.reportTimeout
	params := reportParams{WorkspaceID: p.WorkspaceID, WorkspaceName: ws.Name, RelayHost: host, Text: p.Text, Summary: p.Summary, SessionTitle: title, TimeoutMs: timeout.Milliseconds(), MaxBytes: report.MaxBundle, MaxText: report.MaxText, Files: b.files}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var ans reportAnswer
	if err := conn.Call(callCtx, handleUIReport, params, &ans); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return reportSendResult{Reason: reportReasonTimeout}, nil
		}
		if errors.Is(err, jsonrpc2.ErrClosed) {
			return reportSendResult{}, errNoReportApp
		}
		return reportSendResult{}, err
	}
	switch {
	case ans.Send:
	case ans.Reason == reportReasonTimeout:
		return reportSendResult{Reason: reportReasonTimeout}, nil
	case ans.Reason == reportReasonNoWindow:
		return reportSendResult{}, errNoReportApp
	default:
		return reportSendResult{Reason: reportReasonCancel}, nil
	}
	kept, err := b.kept(ans, report.Meta{SessionID: sessionID, SessionTitle: title}, now)
	if err != nil {
		return reportSendResult{}, err
	}
	zipPath := dir + ".zip"
	defer os.Remove(zipPath)
	if _, err := diagnostics.Save(zipPath, now, kept); err != nil {
		return reportSendResult{}, err
	}
	fi, err := os.Stat(zipPath)
	if err != nil {
		return reportSendResult{}, err
	}
	if fi.Size() > report.MaxBundle {
		return reportSendResult{}, fmt.Errorf("report is over %d MiB", report.MaxBundle>>20)
	}
	raw, err := s.runPluginCommandWithin(ctx, cmdReportUpload, "", map[string]string{"path": zipPath}, reportUploadSeconds)
	if err != nil {
		return reportSendResult{}, err
	}
	var up struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &up); err != nil {
		return reportSendResult{}, fmt.Errorf("%s: %w", cmdReportUpload, err)
	}
	return reportSendResult{Sent: true, ID: up.ID}, nil
}

func (s *Server) RemoveStaleReports() error {
	if s.dirs == nil {
		return nil
	}
	entries, err := os.ReadDir(s.dirs.DiagnosticsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), reportDirPrefix) {
			errs = append(errs, os.RemoveAll(filepath.Join(s.dirs.DiagnosticsDir(), e.Name())))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) runPluginCommand(ctx context.Context, id, workspaceID string, args any) (json.RawMessage, error) {
	return s.runPluginCommandWithin(ctx, id, workspaceID, args, 0)
}

func (s *Server) runPluginCommandWithin(ctx context.Context, id, workspaceID string, args any, minSeconds int) (json.RawMessage, error) {
	u, err := s.uiState()
	if err != nil {
		return nil, err
	}
	c, ok := u.router.Resolve(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownCommand, id)
	}
	c.Timeout = max(c.Timeout, minSeconds)
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	run, err := json.Marshal(commandRunParams{ID: c.ID, WorkspaceID: workspaceID, Args: rawArgs})
	if err != nil {
		return nil, err
	}
	return u.router.Command(ctx, c, run)
}

func (s *Server) reportRelayHost(ctx context.Context) (string, error) {
	raw, err := s.runPluginCommand(ctx, cmdRemoteStatus, "", struct{}{})
	if err != nil {
		return "", err
	}
	var st struct {
		ReportRelay string `json:"reportRelay"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", fmt.Errorf("%s: %w", cmdRemoteStatus, err)
	}
	if u, err := url.Parse(st.ReportRelay); err == nil && u.Host != "" {
		return u.Host, nil
	}
	return st.ReportRelay, nil
}

func (s *Server) reportTranscript(ctx context.Context, workspaceID, sessionID string) ([]byte, string) {
	if sessionID == "" {
		return nil, ""
	}
	last, err := s.runPluginCommand(ctx, cmdAgentsTranscript, workspaceID, map[string]any{"session": sessionID, "last": true, "compact": true})
	if err != nil {
		return nil, ""
	}
	var head reportTranscript
	if json.Unmarshal(last, &head) != nil {
		return nil, ""
	}
	candidates := [][]byte{last}
	if n := len(head.Turns); n > 0 {
		from := max(head.Turns[n-1].Turn-reportTurns+1, 0)
		if full, err := s.runPluginCommand(ctx, cmdAgentsTranscript, workspaceID, map[string]any{"session": sessionID, "fromTurn": from, "compact": true}); err == nil {
			candidates = [][]byte{full, last}
		}
	}
	for _, data := range candidates {
		if len(data) <= reportTranscriptMax {
			return data, head.Session.Title
		}
	}
	return nil, head.Session.Title
}

func (b *reportBundle) write(name string, data []byte) error {
	dst := filepath.Join(b.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return err
	}
	b.add(reportFile{Name: name, Bytes: int64(len(data)), Path: dst, Checked: true}, dst)
	return nil
}

func (b *reportBundle) add(f reportFile, source string) {
	b.files = append(b.files, f)
	b.sources[f.Name] = source
}

func (b *reportBundle) attach(src string, roots []string) error {
	if !filepath.IsAbs(src) {
		return invalidParams(fmt.Sprintf("files: %q is not an absolute path", src))
	}
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return invalidParams(fmt.Sprintf("files: %v", err))
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.Mode().IsRegular() {
		return invalidParams(fmt.Sprintf("files: %s is not a regular file", src))
	}
	in, err := openNoFollow(resolved)
	if err != nil {
		return invalidParams(fmt.Sprintf("files: %v", err))
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return invalidParams(fmt.Sprintf("files: %s is not a regular file", src))
	}
	name := b.uniqueName(filepath.Base(resolved))
	dst := filepath.Join(b.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	n, err := io.CopyN(out, in, report.MaxBundle-b.attached+1)
	if closeErr := out.Close(); err == nil || errors.Is(err, io.EOF) {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	b.attached += n
	if b.attached > report.MaxBundle {
		return invalidParams(fmt.Sprintf("files: %s takes the attached files over %d MiB", src, report.MaxBundle>>20))
	}
	f := reportFile{Name: name, Bytes: n, Path: dst, Checked: true, Source: resolved}
	if !insideAny(resolved, roots) {
		f.Checked, f.Warning = false, reportOutside
	}
	b.add(f, dst)
	return nil
}

func (b *reportBundle) uniqueName(base string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	name := path.Join(reportFilesDir, base)
	for i := 2; b.sources[name] != ""; i++ {
		name = path.Join(reportFilesDir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	}
	return name
}

func (b *reportBundle) kept(ans reportAnswer, meta report.Meta, now time.Time) ([]diagnostics.Entry, error) {
	if err := checkReportText(ans.Text, ans.Summary); err != nil {
		return nil, err
	}
	var entries []diagnostics.Entry
	for _, f := range b.files {
		if !slices.Contains(ans.Files, f.Name) {
			continue
		}
		data, err := os.ReadFile(b.sources[f.Name])
		if err != nil {
			return nil, err
		}
		entries = append(entries, diagnostics.Entry{Name: f.Name, Data: data})
	}
	for name := range ans.Extra {
		if !slices.Contains(diagnosticsExtras, name) {
			return nil, invalidParams(fmt.Sprintf("extra: unknown file %q", name))
		}
	}
	for _, name := range diagnosticsExtras {
		data, ok := ans.Extra[name]
		if !ok {
			continue
		}
		if len(data) > diagnosticsExtraMax {
			return nil, invalidParams(fmt.Sprintf("extra: %s is over %d bytes", name, diagnosticsExtraMax))
		}
		entries = append(entries, diagnostics.Entry{Name: name, Data: data})
	}
	info := diagnostics.NewInfo(now)
	meta.Text, meta.Summary, meta.AppVersion = ans.Text, ans.Summary, ans.AppVersion
	meta.CoreVersion, meta.Commit, meta.MacOS, meta.Arch = buildinfo.String(), info.Commit, info.MacOS, info.Arch
	for _, e := range entries {
		meta.Files = append(meta.Files, e.Name)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(entries, diagnostics.Entry{Name: report.MetaName, Data: append(data, '\n')}), nil
}

func checkReportText(text, summary string) error {
	if len(text) > report.MaxText || len(summary) > report.MaxText {
		return invalidParams(fmt.Sprintf("text and summary: at most %d KiB each", report.MaxText>>10))
	}
	return nil
}

func workspaceRoots(folders []workspace.Folder) []string {
	var roots []string
	for _, f := range folders {
		for _, p := range []string{f.Path, f.Worktree} {
			if p == "" {
				continue
			}
			if resolved, err := filepath.EvalSymlinks(p); err == nil {
				roots = append(roots, resolved)
			}
		}
	}
	return roots
}

func openNoFollow(p string) (*os.File, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: "/", Err: err}
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(p), "/"), "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if err != nil {
			return nil, &os.PathError{Op: "open", Path: p, Err: err}
		}
		fd = next
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		unix.Close(fd)
		return nil, &os.PathError{Op: "open", Path: p, Err: err}
	}
	return os.NewFile(uintptr(fd), p), nil
}

func insideAny(p string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, p)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
