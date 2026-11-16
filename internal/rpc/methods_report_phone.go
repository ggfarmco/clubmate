package rpc

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clubmate/internal/diagnostics"
	"clubmate/pkg/report"
)

const (
	reportSubmitMethod = "report.submit"
	reportSubmitPlugin = "remote"
	reportSource       = "phone"
	phoneMaxFiles      = 20
)

var (
	errReportSubmitCaller = deniedError("report.submit is only for the remote plugin")
	phoneFileName         = regexp.MustCompile(`^phone/[a-z0-9_-][a-z0-9._-]{0,63}$`)
)

type reportSubmitParams struct {
	Text       string      `json:"text"`
	Device     string      `json:"device"`
	Model      string      `json:"model"`
	AppVersion string      `json:"appVersion"`
	Mac        bool        `json:"mac"`
	Files      []phoneFile `json:"files"`
}

type phoneFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

type reportSubmitResult struct {
	ID string `json:"id"`
}

func (s *Server) reportSubmit(ctx context.Context, p reportSubmitParams) (reportSubmitResult, error) {
	if CallerFrom(ctx).PluginID != reportSubmitPlugin {
		return reportSubmitResult{}, errReportSubmitCaller
	}
	if err := checkPhoneReport(p); err != nil {
		return reportSubmitResult{}, err
	}
	if s.dirs == nil {
		return reportSubmitResult{}, errNoDiagnostics
	}
	now := time.Now()
	var entries []diagnostics.Entry
	if p.Mac {
		mac, err := s.macLogEntries("", now)
		if err != nil {
			return reportSubmitResult{}, err
		}
		entries = mac
	}
	for _, f := range p.Files {
		entries = append(entries, diagnostics.Entry{Name: f.Name, Data: f.Data})
	}
	entries, err := withMeta(entries, report.Meta{Text: p.Text, AppVersion: p.AppVersion, Source: reportSource, Device: p.Device, Model: p.Model}, now)
	if err != nil {
		return reportSubmitResult{}, err
	}
	if err := os.MkdirAll(s.dirs.DiagnosticsDir(), 0o700); err != nil {
		return reportSubmitResult{}, err
	}
	zipPath := filepath.Join(s.dirs.DiagnosticsDir(), reportDirPrefix+now.Format(reportTimeLayout)+"-"+rand.Text()+".zip")
	id, err := s.uploadReport(ctx, zipPath, now, entries)
	if err != nil {
		return reportSubmitResult{}, err
	}
	return reportSubmitResult{ID: id}, nil
}

func checkPhoneReport(p reportSubmitParams) error {
	if err := checkReportText(p.Text, ""); err != nil {
		return err
	}
	if strings.TrimSpace(p.Text) == "" {
		return invalidParams("text is required")
	}
	if len(p.Files) > phoneMaxFiles {
		return invalidParams(fmt.Sprintf("files: at most %d files", phoneMaxFiles))
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range p.Files {
		if !phoneFileName.MatchString(f.Name) {
			return invalidParams(fmt.Sprintf("files: %q must be phone/ and a name of up to 64 a-z, 0-9, '.', '_' or '-' that doesn't start with '.'", f.Name))
		}
		if seen[f.Name] {
			return invalidParams(fmt.Sprintf("files: %q is listed twice", f.Name))
		}
		seen[f.Name] = true
		total += int64(len(f.Data))
	}
	if total > report.MaxBundle {
		return invalidParams(fmt.Sprintf("files: over %d MiB", report.MaxBundle>>20))
	}
	return nil
}
