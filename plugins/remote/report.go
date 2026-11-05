package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/report"
)

const (
	defaultReportRelay = "https://clubmate-push.exe.xyz"
	reportKeyFile      = "report-key"
	reportsPath        = "/v1/reports"
	reportTimeout      = 2 * time.Minute
)

var (
	errReportTooBig = errors.New("report is over 25 MiB")
	errReportNotAbs = errors.New("report path must be absolute")
	errNoStateDir   = errors.New("no plugin state dir")
	errReportNoID   = errors.New("report relay reply has no id")
	reportRefusals  = map[int]error{
		http.StatusUnauthorized:          errors.New("the relay refused the signature"),
		http.StatusForbidden:             errors.New("this Mac is blocked from sending reports"),
		http.StatusRequestEntityTooLarge: errReportTooBig,
		http.StatusTooManyRequests:       errors.New("too many reports, try again later"),
		http.StatusInsufficientStorage:   errors.New("the report server is full"),
	}
)

type reports struct {
	relay  *relayClient
	kv     kvStore
	client *http.Client
	dir    string
	now    func() time.Time

	keyMu   sync.Mutex
	inboxMu sync.Mutex
}

type reportUploadArgs struct {
	Path string `json:"path"`
}

type reportUploadResult struct {
	ID string `json:"id"`
}

func newReports(relay *relayClient, kv kvStore, client *http.Client, dir string, now func() time.Time) *reports {
	return &reports{relay: relay, kv: kv, client: noRedirects(client), dir: dir, now: now}
}

func (r *reports) upload(ctx context.Context, path string) (reportUploadResult, error) {
	if !filepath.IsAbs(path) {
		return reportUploadResult{}, errReportNotAbs
	}
	body, err := readBundle(path)
	if err != nil {
		return reportUploadResult{}, err
	}
	key, err := r.key()
	if err != nil {
		return reportUploadResult{}, err
	}
	base, token := r.relay.reportTarget()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+reportsPath, bytes.NewReader(body))
	if err != nil {
		return reportUploadResult{}, err
	}
	req.Header.Set("Content-Type", "application/zip")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	report.Sign(req, key, body, r.now())
	res, err := r.client.Do(req)
	if err != nil {
		return reportUploadResult{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, relayMaxReply))
	if err != nil {
		return reportUploadResult{}, err
	}
	if refusal, ok := reportRefusals[res.StatusCode]; ok {
		return reportUploadResult{}, refusal
	}
	if res.StatusCode != http.StatusCreated {
		return reportUploadResult{}, fmt.Errorf("report relay: %d %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out reportUploadResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return reportUploadResult{}, fmt.Errorf("report relay reply: %w", err)
	}
	if out.ID == "" {
		return reportUploadResult{}, errReportNoID
	}
	return out, nil
}

func (r *reports) key() (ed25519.PrivateKey, error) {
	r.keyMu.Lock()
	defer r.keyMu.Unlock()
	return loadReportKey(r.dir)
}

func loadReportKey(dir string) (ed25519.PrivateKey, error) {
	if dir == "" {
		return nil, errNoStateDir
	}
	path := filepath.Join(dir, reportKeyFile)
	seed, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		seed, err = createReportKey(path)
	}
	if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s: %d bytes, want %d", path, len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func createReportKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	seed := make([]byte, ed25519.SeedSize)
	_, _ = rand.Read(seed)
	tmp, err := writeTemp(filepath.Dir(path), seed)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	err = os.Link(tmp, path)
	if errors.Is(err, fs.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	return seed, nil
}

func writeTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func readBundle(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, report.MaxBundle+1))
	if err != nil {
		return nil, err
	}
	if len(body) > report.MaxBundle {
		return nil, errReportTooBig
	}
	return body, nil
}
