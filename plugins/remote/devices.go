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
	Macs      []macInfo `json:"macs,omitempty"`

	PushToken       string `json:"pushToken,omitempty"`
	PushEnvironment string `json:"pushEnvironment,omitempty"`
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

func (s *deviceStore) add(ctx context.Context, info deviceInfo) (string, error) {
	raw := make([]byte, tokenBytes)
	_, _ = rand.Read(raw)
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	id := info.ID
	d := device{ID: id, Name: info.Name, Model: info.Model, PairedAt: now, LastSeen: now, TokenHash: hashToken(token)}
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

func (s *deviceStore) setMacs(ctx context.Context, id string, macs []macInfo) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	i := slices.IndexFunc(s.devices, func(d device) bool { return d.ID == id })
	if i < 0 {
		s.mu.Unlock()
		return false, errUnknownDevice
	}
	prev := s.devices[i].Macs
	if slices.Equal(prev, macs) {
		s.mu.Unlock()
		return false, nil
	}
	s.devices[i].Macs = slices.Clone(macs)
	s.mu.Unlock()
	if err := s.persist(ctx, id); err != nil {
		s.mu.Lock()
		if i := slices.IndexFunc(s.devices, func(d device) bool { return d.ID == id }); i >= 0 {
			s.devices[i].Macs = prev
		}
		s.mu.Unlock()
		return false, err
	}
	return true, nil
}

func (s *deviceStore) setPush(ctx context.Context, id, token, env string) error {
	return s.editPush(ctx, id, func(d device) (string, string) {
		switch {
		case d.ID == id:
			return token, env
		case token != "" && d.PushToken == token:
			return "", ""
		}
		return d.PushToken, d.PushEnvironment
	})
}

func (s *deviceStore) dropPush(ctx context.Context, token string) error {
	return s.editPush(ctx, "", func(d device) (string, string) {
		if d.PushToken == token {
			return "", ""
		}
		return d.PushToken, d.PushEnvironment
	})
}

func (s *deviceStore) usedColours() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, d := range s.devices {
		for _, m := range d.Macs {
			out = append(out, m.Colour)
		}
	}
	return out
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

func (s *deviceStore) editPush(ctx context.Context, need string, edit func(device) (string, string)) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if need != "" && !slices.ContainsFunc(s.devices, func(d device) bool { return d.ID == need }) {
		s.mu.Unlock()
		return errUnknownDevice
	}
	var prev []device
	for i, d := range s.devices {
		token, env := edit(d)
		if token == d.PushToken && env == d.PushEnvironment {
			continue
		}
		prev = append(prev, d)
		s.devices[i].PushToken, s.devices[i].PushEnvironment = token, env
	}
	s.mu.Unlock()
	for _, p := range prev {
		if err := s.persist(ctx, p.ID); err != nil {
			s.restorePush(prev)
			return err
		}
	}
	return nil
}

func (s *deviceStore) restorePush(prev []device) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range prev {
		if i := slices.IndexFunc(s.devices, func(d device) bool { return d.ID == p.ID }); i >= 0 {
			s.devices[i].PushToken, s.devices[i].PushEnvironment = p.PushToken, p.PushEnvironment
		}
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
