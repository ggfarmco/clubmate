package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
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
	unusedTTL   = 7 * 24 * time.Hour
	idleTTL     = 90 * 24 * time.Hour

	recheckTokens = time.Second
)

var (
	errBadMacName  = errors.New("name must be one word without spaces")
	errSelfName    = errors.New("names starting with k- are for self sign-up")
	errMacExists   = errors.New("name already has a token")
	errUnknownMac  = errors.New("no token for that name")
	errBadTokenRow = errors.New("want <name> <sha256 hex> [key=<base64url> at=<unix> used=<unix>]")
	errNameTaken   = errors.New("name taken by another key")
)

type tokens struct {
	path string

	recheck time.Duration

	mu       sync.Mutex
	checked  time.Time
	modTime  time.Time
	size     int64
	entries  []tokenEntry
	byHash   map[string]int
	usedFile map[string]bool
	used     map[string]int64
}

type tokenEntry struct {
	name string
	hash []byte
	key  []byte
	at   int64
	used int64
}

func loadTokens(path string) (*tokens, error) {
	t := &tokens{path: path, used: map[string]int64{}}
	if err := t.load(false); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *tokens) refresh() error {
	if t.recheck > 0 {
		t.mu.Lock()
		skip := time.Since(t.checked) < t.recheck
		if !skip {
			t.checked = time.Now()
		}
		t.mu.Unlock()
		if skip {
			return nil
		}
	}
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
	t.byHash = make(map[string]int, len(entries))
	t.usedFile = map[string]bool{}
	for i, e := range entries {
		t.byHash[string(e.hash)] = i
		if e.used > 0 {
			t.usedFile[e.name] = true
		}
	}
	return nil
}

func (t *tokens) lookup(token string, now time.Time, banned func(hexKey string) bool) (string, bool, bool) {
	sum := sha256.Sum256([]byte(token))
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.byHash[string(sum[:])]
	if token == "" || !ok {
		return "", false, false
	}
	found := &t.entries[i]
	self := len(found.key) > 0
	if self && !found.live(now, t.used[found.name]) {
		return "", false, false
	}
	if self && banned != nil && banned(hex.EncodeToString(found.key)) {
		return "", false, false
	}
	return found.name, self, true
}

func (t *tokens) touch(name string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	first := t.used[name] == 0 && !t.usedFile[name]
	t.used[name] = now.Unix()
	return first
}

func (t *tokens) sweep(now time.Time) ([]string, error) {
	var pruned []string
	t.mu.Lock()
	pending := maps.Clone(t.used)
	t.mu.Unlock()
	err := withTokensLock(t.path, func() error {
		raw, err := os.ReadFile(t.path)
		if err != nil {
			return err
		}
		if _, err := parseTokens(t.path, raw); err != nil {
			return err
		}
		out, changed := editTokens(raw, func(e tokenEntry) (string, bool) {
			if len(e.key) == 0 {
				return "", true
			}
			if !e.live(now, pending[e.name]) {
				pruned = append(pruned, e.name)
				return "", false
			}
			if u := pending[e.name]; u > e.used {
				e.used = u
				return e.line(), true
			}
			return "", true
		})
		if !changed {
			return nil
		}
		return replaceFile(t.path, out)
	})
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	for name, u := range pending {
		if t.used[name] <= u {
			delete(t.used, name)
		}
	}
	t.mu.Unlock()
	return pruned, t.load(true)
}

func (e tokenEntry) live(now time.Time, used int64) bool {
	if len(e.key) == 0 {
		return true
	}
	if last := max(e.used, used); last > 0 {
		return now.Sub(time.Unix(last, 0)) < idleTTL
	}
	return now.Sub(time.Unix(e.at, 0)) < unusedTTL
}

func (e tokenEntry) line() string {
	l := fmt.Sprintf("%s %s key=%s at=%d", e.name, hex.EncodeToString(e.hash), base64.RawURLEncoding.EncodeToString(e.key), e.at)
	if e.used > 0 {
		l += fmt.Sprintf(" used=%d", e.used)
	}
	return l + "\n"
}

func editTokens(raw []byte, edit func(e tokenEntry) (string, bool)) ([]byte, bool) {
	var out bytes.Buffer
	changed := false
	for l := range strings.Lines(string(raw)) {
		e, ok := parseTokenLine(strings.Fields(l))
		if !ok {
			out.WriteString(l)
			continue
		}
		line, keep := edit(e)
		switch {
		case !keep:
			changed = true
		case line != "":
			out.WriteString(line)
			changed = true
		default:
			out.WriteString(l)
		}
	}
	return out.Bytes(), changed
}

func (t *tokens) signup(pub ed25519.PublicKey, now time.Time, admit func(fresh bool, self int) error) (string, bool, []string, error) {
	name := macName(pub)
	var (
		token  string
		fresh  = true
		pruned []string
	)
	t.mu.Lock()
	pending := maps.Clone(t.used)
	t.mu.Unlock()
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
			if e.name == name {
				if !bytes.Equal(e.key, pub) {
					return errNameTaken
				}
				fresh = false
				continue
			}
			if len(e.key) > 0 && e.live(now, pending[e.name]) {
				self++
			}
		}
		if err := admit(fresh, self); err != nil {
			return err
		}
		token = tokenPrefix + rand.Text()
		sum := sha256.Sum256([]byte(token))
		line := tokenEntry{name: name, hash: sum[:], key: pub, at: now.Unix()}.line()
		replaced := false
		out, _ := editTokens(raw, func(e tokenEntry) (string, bool) {
			if e.name == name {
				if replaced {
					return "", false
				}
				replaced = true
				return line, true
			}
			if !e.live(now, pending[e.name]) {
				pruned = append(pruned, e.name)
				return "", false
			}
			return "", true
		})
		if !replaced {
			if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
				out = append(out, '\n')
			}
			out = append(out, line...)
		}
		return replaceFile(t.path, out)
	})
	if err != nil {
		return "", false, nil, err
	}
	t.mu.Lock()
	delete(t.used, name)
	t.mu.Unlock()
	return token, fresh, pruned, t.load(true)
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
		case "used":
			used, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return tokenEntry{}, false
			}
			e.used = used
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
