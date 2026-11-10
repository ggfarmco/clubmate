package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"clubmate/pkg/apns"
)

const maxDevicesPerMac = 32

var (
	errTooManyDevices = errors.New("at most 32 device tokens per Mac")
	errDeviceNotBound = errors.New("device token not registered for this Mac")
)

type bindings struct {
	path string

	mu   sync.Mutex
	macs map[string][]string
}

type devicesRequest struct {
	Tokens []string `json:"tokens"`
}

func loadBindings(path string) (*bindings, error) {
	b := &bindings{path: path, macs: map[string][]string{}}
	if path == "" {
		return b, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &b.macs); err != nil {
		return nil, err
	}
	if b.macs == nil {
		b.macs = map[string][]string{}
	}
	return b, nil
}

func (b *bindings) allowed(mac, deviceToken string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	bound, ok := b.macs[mac]
	return !ok || slices.Contains(bound, deviceHash(deviceToken))
}

func (b *bindings) set(mac string, deviceTokens []string) error {
	hashes := make([]string, 0, len(deviceTokens))
	for _, t := range deviceTokens {
		hashes = append(hashes, deviceHash(t))
	}
	slices.Sort(hashes)
	hashes = slices.Compact(hashes)
	b.mu.Lock()
	defer b.mu.Unlock()
	prev, had := b.macs[mac]
	b.macs[mac] = hashes
	if err := b.save(); err != nil {
		if had {
			b.macs[mac] = prev
		} else {
			delete(b.macs, mac)
		}
		return err
	}
	return nil
}

func (b *bindings) save() error {
	if b.path == "" {
		return nil
	}
	raw, err := json.Marshal(b.macs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(b.path), filepath.Base(b.path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), b.path)
}

func (r *relay) putDevices(w http.ResponseWriter, req *http.Request) {
	if err := r.tokens.refresh(); err != nil {
		r.log.Printf("reload tokens: %v", err)
	}
	mac, ok := r.tokens.lookup(bearer(req))
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !r.limiter.allow(mac) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	var d devicesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxRequest)).Decode(&d); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	if len(d.Tokens) > maxDevicesPerMac {
		http.Error(w, errTooManyDevices.Error(), http.StatusBadRequest)
		return
	}
	for _, t := range d.Tokens {
		if !apns.ValidDeviceToken(t) {
			http.Error(w, errBadDeviceToken.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := r.devices.set(mac, d.Tokens); err != nil {
		r.log.Printf("devices %s: %v", mac, err)
		http.Error(w, "could not save devices", http.StatusInternalServerError)
		return
	}
	r.log.Printf("devices %s %d", mac, len(d.Tokens))
	w.WriteHeader(http.StatusNoContent)
}

func deviceHash(deviceToken string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(deviceToken)))
	return hex.EncodeToString(sum[:])
}
