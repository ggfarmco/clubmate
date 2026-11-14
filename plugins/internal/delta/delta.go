package delta

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const (
	KeptRemoved = 256
	sep         = "."
)

type Storage interface {
	StorageList(ctx context.Context, prefix string) ([]string, error)
	StorageGet(ctx context.Context, key string, out any) (bool, error)
	StorageSet(ctx context.Context, key string, v any) error
	StorageDelete(ctx context.Context, key string) error
	StorageDeletePrefix(ctx context.Context, prefix string) error
}

type Entry struct {
	Key     string `json:"key"`
	Seq     int64  `json:"seq"`
	Sum     uint64 `json:"sum,omitempty"`
	Removed bool   `json:"removed,omitempty"`
}

type head struct {
	Epoch string `json:"epoch"`
	Head  int64  `json:"head"`
	Floor int64  `json:"floor"`
	Dirty bool   `json:"dirty,omitempty"`
}

type Tracker struct {
	Name    string
	Epoch   string
	Head    int64
	Floor   int64
	entries map[string]Entry
	dirty   map[string]bool
	saved   head
}

func New(name string) *Tracker {
	return &Tracker{Name: name, Epoch: newEpoch(), entries: map[string]Entry{}, dirty: map[string]bool{}}
}

func Load(ctx context.Context, s Storage, name string) (*Tracker, error) {
	if t, ok := load(ctx, s, name); ok {
		return t, nil
	}
	t := New(name)
	if err := s.StorageDeletePrefix(ctx, prefix(name)); err != nil {
		return nil, err
	}
	return t, t.Save(ctx, s)
}

func load(ctx context.Context, s Storage, name string) (*Tracker, bool) {
	var h head
	ok, err := s.StorageGet(ctx, headKey(name), &h)
	if err != nil || !ok || h.Epoch == "" || h.Dirty {
		return nil, false
	}
	keys, err := s.StorageList(ctx, entryPrefix(name))
	if err != nil {
		return nil, false
	}
	t := New(name)
	t.Epoch, t.Head, t.Floor = h.Epoch, h.Head, h.Floor
	for _, k := range keys {
		var e Entry
		ok, err := s.StorageGet(ctx, k, &e)
		if err != nil {
			return nil, false
		}
		if ok {
			t.entries[e.Key] = e
			t.Head = max(t.Head, e.Seq)
		}
	}
	t.Head = max(t.Head, t.Floor)
	t.saved = t.head()
	return t, true
}

func Delete(ctx context.Context, s Storage, name string) error {
	return s.StorageDeletePrefix(ctx, prefix(name))
}

func (t *Tracker) Save(ctx context.Context, s Storage) error {
	if len(t.dirty) == 0 && t.saved == t.head() {
		return nil
	}
	if err := t.save(ctx, s); err != nil {
		t.Epoch = newEpoch()
		return fmt.Errorf("save delta %s: %w", t.Name, err)
	}
	clear(t.dirty)
	t.saved = t.head()
	return nil
}

func (t *Tracker) save(ctx context.Context, s Storage) error {
	pending := t.head()
	pending.Dirty = true
	if err := s.StorageSet(ctx, headKey(t.Name), pending); err != nil {
		return err
	}
	var errs []error
	for key := range t.dirty {
		if e, ok := t.entries[key]; ok {
			errs = append(errs, s.StorageSet(ctx, entryKey(t.Name, key), e))
		} else {
			errs = append(errs, s.StorageDelete(ctx, entryKey(t.Name, key)))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return s.StorageSet(ctx, headKey(t.Name), t.head())
}

func (t *Tracker) Stamp(key string) int64 {
	t.set(key, Entry{Key: key})
	return t.Head
}

func (t *Tracker) Touch(key string, v any) bool {
	s := sum(v)
	if old, ok := t.entries[key]; ok && !old.Removed && old.Sum == s {
		return false
	}
	t.set(key, Entry{Key: key, Sum: s})
	return true
}

func (t *Tracker) Observe(items map[string]any) {
	for _, key := range slices.Sorted(maps.Keys(items)) {
		s := sum(items[key])
		if old, ok := t.entries[key]; !ok || old.Removed || old.Sum != s {
			t.set(key, Entry{Key: key, Sum: s})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(t.entries)) {
		if _, live := items[key]; !live && !t.entries[key].Removed {
			t.set(key, Entry{Key: key, Removed: true})
		}
	}
	t.prune()
}

func (t *Tracker) Drop(key string) {
	if e, ok := t.entries[key]; ok && e.Removed {
		return
	}
	t.set(key, Entry{Key: key, Removed: true})
	t.prune()
}

func (t *Tracker) Since(cursor string) ([]string, []string, bool) {
	after, ok := t.parse(cursor)
	if !ok || after < t.Floor {
		return nil, nil, true
	}
	var changed, removed []string
	for key, e := range t.entries {
		switch {
		case e.Seq <= after:
		case e.Removed:
			removed = append(removed, key)
		default:
			changed = append(changed, key)
		}
	}
	slices.Sort(changed)
	slices.Sort(removed)
	return changed, removed, false
}

func (t *Tracker) Cursor() string {
	return t.Epoch + sep + strconv.FormatInt(t.Head, 10)
}

func (t *Tracker) SeqOf(key string) int64 {
	return t.entries[key].Seq
}

func (t *Tracker) Stale(key, seen string) bool {
	after, ok := t.parse(seen)
	return !ok || t.SeqOf(key) > after
}

func (t *Tracker) set(key string, e Entry) {
	t.Head++
	e.Seq = t.Head
	t.entries[key] = e
	t.dirty[key] = true
}

func (t *Tracker) prune() {
	var removed []Entry
	for _, e := range t.entries {
		if e.Removed {
			removed = append(removed, e)
		}
	}
	if len(removed) <= KeptRemoved {
		return
	}
	slices.SortFunc(removed, func(x, y Entry) int { return cmp.Compare(x.Seq, y.Seq) })
	for _, e := range removed[:len(removed)-KeptRemoved] {
		t.Floor = max(t.Floor, e.Seq)
		delete(t.entries, e.Key)
		t.dirty[e.Key] = true
	}
}

func (t *Tracker) parse(cursor string) (int64, bool) {
	epoch, seq, ok := strings.Cut(cursor, sep)
	if !ok || epoch != t.Epoch {
		return 0, false
	}
	n, err := strconv.ParseInt(seq, 10, 64)
	return n, err == nil && n >= 0 && n <= t.Head
}

func (t *Tracker) head() head {
	return head{Epoch: t.Epoch, Head: t.Head, Floor: t.Floor}
}

func sum(v any) uint64 {
	h := fnv.New64a()
	if err := json.NewEncoder(h).Encode(v); err != nil {
		return 0
	}
	return h.Sum64()
}

func newEpoch() string {
	return strconv.FormatUint(rand.Uint64(), 36)
}

func prefix(name string) string {
	return "delta/" + name + "/"
}

func headKey(name string) string {
	return prefix(name) + "head"
}

func entryPrefix(name string) string {
	return prefix(name) + "e/"
}

func entryKey(name, key string) string {
	h := fnv.New64a()
	h.Write([]byte(key))
	return fmt.Sprintf("%s%016x", entryPrefix(name), h.Sum64())
}

type Observed struct {
	Store    Storage
	mu       sync.Mutex
	trackers map[string]*Tracker
}

type View struct {
	Changed []string
	Removed []string
	Cursor  string
	Full    bool
}

func (o *Observed) Observe(ctx context.Context, name string, items map[string]any, since string) (View, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	t, err := o.tracker(ctx, name)
	t.Observe(items)
	if o.Store != nil {
		err = errors.Join(err, t.Save(ctx, o.Store))
	}
	changed, removed, full := t.Since(since)
	return View{Changed: changed, Removed: removed, Cursor: t.Cursor(), Full: full}, err
}

func (o *Observed) Forget(ctx context.Context, name string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.trackers, name)
	if o.Store == nil {
		return nil
	}
	return Delete(ctx, o.Store, name)
}

func (o *Observed) tracker(ctx context.Context, name string) (*Tracker, error) {
	if t := o.trackers[name]; t != nil {
		return t, nil
	}
	if o.trackers == nil {
		o.trackers = map[string]*Tracker{}
	}
	t, err := New(name), error(nil)
	if o.Store != nil {
		var loaded *Tracker
		if loaded, err = Load(ctx, o.Store, name); err == nil {
			t = loaded
		}
	}
	o.trackers[name] = t
	return t, err
}
