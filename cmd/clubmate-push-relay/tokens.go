package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

const tokenPrefix = "cmr_"

var (
	errBadMacName  = errors.New("mac name must be one word without spaces")
	errMacExists   = errors.New("mac already has a token")
	errUnknownMac  = errors.New("no token for that mac")
	errBadTokenRow = errors.New("want <name> <sha256 hex>")
)

type tokens struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	entries []tokenEntry
}

type tokenEntry struct {
	name string
	hash []byte
}

func loadTokens(path string) (*tokens, error) {
	t := &tokens{path: path}
	if err := t.refresh(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *tokens) refresh() error {
	info, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if info.ModTime().Equal(t.modTime) && info.Size() == t.size {
		return nil
	}
	entries, err := readTokens(t.path)
	if err != nil {
		return err
	}
	t.modTime, t.size, t.entries = info.ModTime(), info.Size(), entries
	return nil
}

func (t *tokens) lookup(token string) (string, bool) {
	sum := sha256.Sum256([]byte(token))
	t.mu.Lock()
	defer t.mu.Unlock()
	var name string
	for _, e := range t.entries {
		if subtle.ConstantTimeCompare(e.hash, sum[:]) == 1 {
			name = e.name
		}
	}
	return name, token != "" && name != ""
}

func addToken(path, name string) (string, error) {
	if name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return "", errBadMacName
	}
	entries, err := readTokens(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if slices.ContainsFunc(entries, func(e tokenEntry) bool { return e.name == name }) {
		return "", fmt.Errorf("%w: %s", errMacExists, name)
	}
	token := tokenPrefix + rand.Text()
	sum := sha256.Sum256([]byte(token))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(f, "%s %s\n", name, hex.EncodeToString(sum[:])); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return token, nil
}

func removeToken(path, name string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	removed := false
	for line := range strings.Lines(string(raw)) {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == name {
			removed = true
			continue
		}
		out.WriteString(line)
	}
	if !removed {
		return fmt.Errorf("%w: %s", errUnknownMac, name)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func tokenNames(path string) ([]string, error) {
	entries, err := readTokens(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	return names, nil
}

func readTokens(path string) ([]tokenEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []tokenEntry
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s:%d: %w", path, n, errBadTokenRow)
		}
		hash, err := hex.DecodeString(fields[1])
		if err != nil || len(hash) != sha256.Size {
			return nil, fmt.Errorf("%s:%d: %w", path, n, errBadTokenRow)
		}
		entries = append(entries, tokenEntry{name: fields[0], hash: hash})
	}
	return entries, sc.Err()
}
