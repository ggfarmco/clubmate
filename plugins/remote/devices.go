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
	legacyDevicesKey = "devices"
	devicePrefix     = "device/"
	tokenPrefix      = "cmt_"
	tokenBytes       = 32
	seenPersistAt    = time.Minute
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
	keys, err := s.kv.StorageList(ctx, devicePrefix)
	if err != nil {
		return err
	}
	list := make([]device, 0, len(keys))
	for _, k := range keys {
		var d device
		found, err := s.kv.StorageGet(ctx, k, &d)
		if err != nil {
			return err
		}
		if found && d.ID != "" {
			list = append(list, d)
		}
	}
	if list, err = s.migrate(ctx, list); err != nil {
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
	if err := s.persist(ctx, id); err != nil {
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
		_ = s.persist(ctx, d.ID)
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
	return s.persist(ctx, id)
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

func (s *deviceStore) migrate(ctx context.Context, list []device) ([]device, error) {
	var legacy []device
	if found, err := s.kv.StorageGet(ctx, legacyDevicesKey, &legacy); err != nil || !found {
		return list, err
	}
	for _, d := range legacy {
		if err := s.kv.StorageSet(ctx, deviceKey(d.ID), d); err != nil {
			return nil, err
		}
		list = slices.DeleteFunc(list, func(cur device) bool { return cur.ID == d.ID })
		list = append(list, d)
	}
	return list, s.kv.StorageDelete(ctx, legacyDevicesKey)
}

func (s *deviceStore) persist(ctx context.Context, id string) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	i := slices.IndexFunc(s.devices, func(d device) bool { return d.ID == id })
	var d device
	if i >= 0 {
		d = s.devices[i]
	}
	s.mu.Unlock()
	if i < 0 {
		return s.kv.StorageDelete(ctx, deviceKey(id))
	}
	return s.kv.StorageSet(ctx, deviceKey(id), d)
}

func deviceKey(id string) string {
	return devicePrefix + id
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
