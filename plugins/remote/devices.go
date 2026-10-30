package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"
)

const (
	devicesKey    = "devices"
	tokenPrefix   = "cmt_"
	tokenBytes    = 32
	seenPersistAt = time.Minute
)

var errUnknownDevice = errors.New("unknown device")

type device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	PairedAt  time.Time `json:"pairedAt"`
	LastSeen  time.Time `json:"lastSeen"`
	TokenHash string    `json:"tokenHash"`
}

type deviceStore struct {
	kv  kvStore
	now func() time.Time

	writeMu sync.Mutex
	saveMu  sync.Mutex
	mu      sync.Mutex
	devices []device
	saved   map[string]time.Time
}

func newDeviceStore(kv kvStore, now func() time.Time) *deviceStore {
	return &deviceStore{kv: kv, now: now, saved: map[string]time.Time{}}
}

func (s *deviceStore) load(ctx context.Context) error {
	var list []device
	if _, err := s.kv.StorageGet(ctx, devicesKey, &list); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices = list
	for _, d := range list {
		s.saved[d.ID] = d.LastSeen
	}
	return nil
}

func (s *deviceStore) add(ctx context.Context, id, name, model string) (string, error) {
	raw := make([]byte, tokenBytes)
	_, _ = rand.Read(raw)
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	d := device{ID: id, Name: name, Model: model, PairedAt: now, LastSeen: now, TokenHash: hashToken(token)}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	i := slices.IndexFunc(s.devices, func(old device) bool { return old.ID == id })
	var prev *device
	if i >= 0 {
		prev = new(s.devices[i])
		s.devices = slices.Delete(s.devices, i, i+1)
	}
	prevSaved, hadSaved := s.saved[id]
	s.devices = append(s.devices, d)
	s.saved[id] = now
	s.mu.Unlock()
	if err := s.save(ctx); err != nil {
		s.undoAdd(d, prev, prevSaved, hadSaved)
		return "", err
	}
	return token, nil
}

func (s *deviceStore) authenticate(ctx context.Context, token string) (device, bool) {
	sum := hashToken(token)
	now := s.now()
	s.mu.Lock()
	i := slices.IndexFunc(s.devices, func(d device) bool {
		return subtle.ConstantTimeCompare([]byte(d.TokenHash), []byte(sum)) == 1
	})
	if i < 0 {
		s.mu.Unlock()
		return device{}, false
	}
	s.devices[i].LastSeen = now
	d := s.devices[i]
	persist := now.Sub(s.saved[d.ID]) >= seenPersistAt
	if persist {
		s.saved[d.ID] = now
	}
	s.mu.Unlock()
	if persist {
		_ = s.save(ctx)
	}
	return d, true
}

func (s *deviceStore) valid(d device) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.devices, func(cur device) bool {
		return cur.ID == d.ID && subtle.ConstantTimeCompare([]byte(cur.TokenHash), []byte(d.TokenHash)) == 1
	})
}

func (s *deviceStore) revoke(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	n := len(s.devices)
	s.devices = slices.DeleteFunc(s.devices, func(d device) bool { return d.ID == id })
	removed := len(s.devices) < n
	delete(s.saved, id)
	s.mu.Unlock()
	if !removed {
		return errUnknownDevice
	}
	return s.save(ctx)
}

func (s *deviceStore) list() []device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.devices)
	slices.SortStableFunc(out, func(a, b device) int { return b.PairedAt.Compare(a.PairedAt) })
	return out
}

func (s *deviceStore) undoAdd(added device, prev *device, prevSaved time.Time, hadSaved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices = slices.DeleteFunc(s.devices, func(d device) bool { return d.TokenHash == added.TokenHash })
	if prev != nil {
		s.devices = append(s.devices, *prev)
	}
	if hadSaved {
		s.saved[added.ID] = prevSaved
	} else {
		delete(s.saved, added.ID)
	}
}

func (s *deviceStore) save(ctx context.Context) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	list := slices.Clone(s.devices)
	s.mu.Unlock()
	return s.kv.StorageSet(ctx, devicesKey, list)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
