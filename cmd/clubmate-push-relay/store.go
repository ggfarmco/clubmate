package main

import (
	"bytes"
	"encoding/json"
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

	"clubmate/pkg/report"
)

const (
	reportQuota = 2 << 30
	reportTTL   = 30 * 24 * time.Hour
	metaFile    = "meta.json"
	bundleFile  = "bundle.zip"
	idPrefix    = "rpt_"
	maxIDTries  = 5
	maxIDSuffix = 0xffffff
)

var (
	errQuota       = errors.New("report storage is full")
	errIDCollision = errors.New("no free report id")
)

type store struct {
	dir   string
	quota int64

	mu      sync.Mutex
	used    int64
	last    string
	entries []entry
}

type entry struct {
	id       string
	received time.Time
	size     int64
}

func openStore(dir string, quota int64) (*store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	s := &store{dir: dir, quota: quota}
	for _, d := range dirents {
		if !d.IsDir() || !strings.HasPrefix(d.Name(), idPrefix) {
			continue
		}
		e, ok := s.read(d.Name())
		if !ok {
			if err := os.RemoveAll(filepath.Join(dir, d.Name())); err != nil {
				return nil, err
			}
			continue
		}
		s.entries = append(s.entries, e)
		s.used += e.size
	}
	slices.SortFunc(s.entries, func(a, b entry) int { return strings.Compare(a.id, b.id) })
	if n := len(s.entries); n > 0 {
		s.last = s.entries[n-1].id
	}
	return s, nil
}

func (s *store) add(st report.Stored, bundle []byte) (report.Stored, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.nextID(st.Received)
	if err != nil {
		return report.Stored{}, err
	}
	st.ID, st.Bytes = id, int64(len(bundle))
	meta, err := encodeJSON(st)
	if err != nil {
		return report.Stored{}, err
	}
	size := st.Bytes + int64(len(meta))
	if s.used+size > s.quota {
		return report.Stored{}, errQuota
	}
	dir, err := s.mkdir(&st)
	if err != nil {
		return report.Stored{}, err
	}
	if st.ID != id {
		if meta, err = encodeJSON(st); err != nil {
			_ = os.RemoveAll(dir)
			return report.Stored{}, err
		}
	}
	if err := writeFile(dir, bundleFile, bundle); err != nil {
		_ = os.RemoveAll(dir)
		return report.Stored{}, err
	}
	if err := writeFile(dir, metaFile, meta); err != nil {
		_ = os.RemoveAll(dir)
		return report.Stored{}, err
	}
	s.entries = append(s.entries, entry{id: st.ID, received: st.Received, size: size})
	s.used += size
	s.last = st.ID
	return st, nil
}

func (s *store) list(after string, limit int) ([]report.Stored, error) {
	s.mu.Lock()
	i, found := s.find(after)
	if found {
		i++
	}
	page := slices.Clone(s.entries[i:min(len(s.entries), i+limit)])
	s.mu.Unlock()
	out := make([]report.Stored, 0, len(page))
	for _, e := range page {
		st, err := s.meta(e.id)
		if errors.Is(err, fs.ErrNotExist) && !s.has(e.id) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func (s *store) bundle(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.find(id); !ok {
		return "", false
	}
	return filepath.Join(s.dir, id, bundleFile), true
}

func (s *store) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.find(id)
	return ok
}

func (s *store) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.find(id)
	if !ok {
		return nil
	}
	return s.removeAt(i)
}

func (s *store) sweep(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for i := len(s.entries) - 1; i >= 0; i-- {
		if now.Sub(s.entries[i].received) > reportTTL {
			errs = append(errs, s.removeAt(i))
		}
	}
	return errors.Join(errs...)
}

func (s *store) nextID(received time.Time) (string, error) {
	if id := report.NewID(received); id > s.last {
		return id, nil
	}
	return bumpID(s.last)
}

func (s *store) mkdir(st *report.Stored) (string, error) {
	for range maxIDTries {
		dir := filepath.Join(s.dir, st.ID)
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if st.ID, err = bumpID(st.ID); err != nil {
			return "", err
		}
	}
	return "", errIDCollision
}

func (s *store) find(id string) (int, bool) {
	return slices.BinarySearchFunc(s.entries, id, func(e entry, id string) int { return strings.Compare(e.id, id) })
}

func (s *store) removeAt(i int) error {
	e := s.entries[i]
	if err := os.RemoveAll(filepath.Join(s.dir, e.id)); err != nil {
		return fmt.Errorf("remove %s: %w", e.id, err)
	}
	s.entries = slices.Delete(s.entries, i, i+1)
	s.used -= e.size
	return nil
}

func (s *store) read(id string) (entry, bool) {
	st, err := s.meta(id)
	if err != nil || st.ID != id {
		return entry{}, false
	}
	metaInfo, err := os.Stat(filepath.Join(s.dir, id, metaFile))
	if err != nil {
		return entry{}, false
	}
	bundleInfo, err := os.Stat(filepath.Join(s.dir, id, bundleFile))
	if err != nil {
		return entry{}, false
	}
	return entry{id: id, received: st.Received, size: bundleInfo.Size() + metaInfo.Size()}, true
}

func (s *store) meta(id string) (report.Stored, error) {
	raw, err := os.ReadFile(filepath.Join(s.dir, id, metaFile))
	if err != nil {
		return report.Stored{}, err
	}
	var st report.Stored
	if err := json.Unmarshal(raw, &st); err != nil {
		return report.Stored{}, fmt.Errorf("%s: %w", id, err)
	}
	return st, nil
}

func bumpID(id string) (string, error) {
	i := strings.LastIndexByte(id, '_')
	n, err := strconv.ParseUint(id[i+1:], 16, 32)
	if i < 0 || err != nil || n >= maxIDSuffix {
		return "", errIDCollision
	}
	return fmt.Sprintf("%s%06x", id[:i+1], n+1), nil
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func writeFile(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}
