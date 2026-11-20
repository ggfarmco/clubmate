package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

const (
	tokenPrefix = "cmr_"
	selfPrefix  = "k-"
	selfMark    = " (self)"
)

var (
	errBadMacName  = errors.New("name must be one word without spaces")
	errSelfName    = errors.New("names starting with k- are for self sign-up")
	errMacExists   = errors.New("name already has a token")
	errUnknownMac  = errors.New("no token for that name")
	errBadTokenRow = errors.New("want <name> <sha256 hex> [key=<base64url> at=<unix>]")
	errNameTaken   = errors.New("name taken by another key")
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
	key  []byte
	at   int64
}

func loadTokens(path string) (*tokens, error) {
	t := &tokens{path: path}
	if err := t.refresh(); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *tokens) refresh() error {
	return t.load(false)
}

func (t *tokens) load(force bool) error {
	info, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !force && info.ModTime().Equal(t.modTime) && info.Size() == t.size {
		return nil
	}
	entries, err := readTokens(t.path)
	if err != nil {
		return err
	}
	t.modTime, t.size, t.entries = info.ModTime(), info.Size(), entries
	return nil
}

func (t *tokens) lookup(token string, banned func(hexKey string) bool) (string, bool, bool) {
	sum := sha256.Sum256([]byte(token))
	t.mu.Lock()
	defer t.mu.Unlock()
	var found *tokenEntry
	for i := range t.entries {
		if subtle.ConstantTimeCompare(t.entries[i].hash, sum[:]) == 1 {
			found = &t.entries[i]
		}
	}
	if token == "" || found == nil {
		return "", false, false
	}
	self := len(found.key) > 0
	if self && banned != nil && banned(hex.EncodeToString(found.key)) {
		return "", false, false
	}
	return found.name, self, true
}

func (t *tokens) signup(pub ed25519.PublicKey, now time.Time, admit func(fresh bool, self int) error) (string, bool, error) {
	name := macName(pub)
	var (
		token string
		fresh = true
	)
	err := withTokensLock(t.path, func() error {
		raw, err := os.ReadFile(t.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		entries, err := parseTokens(t.path, raw)
		if err != nil {
			return err
		}
		self := 0
		for _, e := range entries {
			if len(e.key) > 0 {
				self++
			}
			if e.name == name {
				if !bytes.Equal(e.key, pub) {
					return errNameTaken
				}
				fresh = false
			}
		}
		if err := admit(fresh, self); err != nil {
			return err
		}
		token = tokenPrefix + rand.Text()
		sum := sha256.Sum256([]byte(token))
		line := fmt.Sprintf("%s %s key=%s at=%d\n", name, hex.EncodeToString(sum[:]), base64.RawURLEncoding.EncodeToString(pub), now.Unix())
		var out bytes.Buffer
		replaced := false
		for l := range strings.Lines(string(raw)) {
			if fields := strings.Fields(l); len(fields) > 0 && fields[0] == name {
				if !replaced {
					out.WriteString(line)
					replaced = true
				}
				continue
			}
			out.WriteString(l)
		}
		if !replaced {
			if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
				out.WriteByte('\n')
			}
			out.WriteString(line)
		}
		return replaceFile(t.path, out.Bytes())
	})
	if err != nil {
		return "", false, err
	}
	return token, fresh, t.load(true)
}

func macName(pub ed25519.PublicKey) string {
	return selfPrefix + hex.EncodeToString(pub)[:16]
}

func addToken(path, name string) (string, error) {
	if name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return "", errBadMacName
	}
	if strings.HasPrefix(name, selfPrefix) {
		return "", errSelfName
	}
	var token string
	err := withTokensLock(path, func() error {
		entries, err := readTokens(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if slices.ContainsFunc(entries, func(e tokenEntry) bool { return e.name == name }) {
			return fmt.Errorf("%w: %s", errMacExists, name)
		}
		token = tokenPrefix + rand.Text()
		sum := sha256.Sum256([]byte(token))
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(f, "%s %s\n", name, hex.EncodeToString(sum[:])); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func removeToken(path, name string) error {
	return withTokensLock(path, func() error {
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
		return replaceFile(path, out.Bytes())
	})
}

func tokenNames(path string) ([]string, error) {
	entries, err := readTokens(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if len(e.key) > 0 {
			names = append(names, e.name+selfMark)
			continue
		}
		names = append(names, e.name)
	}
	return names, nil
}

func readTokens(path string) ([]tokenEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTokens(path, raw)
}

func parseTokens(path string, raw []byte) ([]tokenEntry, error) {
	var entries []tokenEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, ok := parseTokenLine(strings.Fields(line))
		if !ok {
			return nil, fmt.Errorf("%s:%d: %w", path, n, errBadTokenRow)
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

func parseTokenLine(fields []string) (tokenEntry, bool) {
	if len(fields) < 2 {
		return tokenEntry{}, false
	}
	hash, err := hex.DecodeString(fields[1])
	if err != nil || len(hash) != sha256.Size {
		return tokenEntry{}, false
	}
	e := tokenEntry{name: fields[0], hash: hash}
	for _, f := range fields[2:] {
		k, v, _ := strings.Cut(f, "=")
		switch k {
		case "key":
			key, err := base64.RawURLEncoding.Strict().DecodeString(v)
			if err != nil || len(key) != ed25519.PublicKeySize {
				return tokenEntry{}, false
			}
			e.key = key
		case "at":
			at, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return tokenEntry{}, false
			}
			e.at = at
		default:
			return tokenEntry{}, false
		}
	}
	return e, true
}

func withTokensLock(path string, fn func() error) error {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	return fn()
}

func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
