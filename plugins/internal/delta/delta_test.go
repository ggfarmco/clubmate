package delta_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/internal/delta"
)

type memKV struct {
	data    map[string][]byte
	failSet bool
	fail    func(key string, raw []byte) bool
}

func newKV() *memKV { return &memKV{data: map[string][]byte{}} }

func (k *memKV) StorageList(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (k *memKV) StorageGet(_ context.Context, key string, out any) (bool, error) {
	raw, ok := k.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (k *memKV) StorageSet(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if k.failSet || k.fail != nil && k.fail(key, raw) {
		return errors.New("disk full")
	}
	k.data[key] = raw
	return err
}

func (k *memKV) StorageDelete(_ context.Context, key string) error {
	delete(k.data, key)
	return nil
}

func (k *memKV) StorageDeletePrefix(_ context.Context, prefix string) error {
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			delete(k.data, key)
		}
	}
	return nil
}

func TestStampOrdersChanges(t *testing.T) {
	t.Parallel()
	tr := delta.New("tasks/ws")
	tr.Stamp("1")
	tr.Stamp("2")
	cursor := tr.Cursor()
	tr.Stamp("1")

	changed, removed, full := tr.Since(cursor)
	assert.Equal(t, []any{[]string{"1"}, []string(nil), false}, []any{changed, removed, full})
	assert.Greater(t, tr.SeqOf("1"), tr.SeqOf("2"))
	_, _, full = tr.Since("")
	assert.True(t, full)
}

func TestRemovedComesAsTombstone(t *testing.T) {
	t.Parallel()
	tr := delta.New("docs/ws")
	tr.Observe(map[string]any{"a.md": 1, "b.md": 1})
	cursor := tr.Cursor()
	tr.Observe(map[string]any{"a.md": 1, "b.md": 1})
	assert.Equal(t, cursor, tr.Cursor())
	tr.Observe(map[string]any{"a.md": 2})

	changed, removed, full := tr.Since(cursor)
	assert.Equal(t, []any{[]string{"a.md"}, []string{"b.md"}, false}, []any{changed, removed, full})
}

func TestCursorBelowFloorIsFull(t *testing.T) {
	t.Parallel()
	tr := delta.New("tasks/ws")
	before := tr.Cursor()
	for i := range delta.KeptRemoved + 1 {
		tr.Drop(strconv.Itoa(i))
	}
	after := tr.Cursor()

	_, _, full := tr.Since(before)
	assert.True(t, full)
	_, removed, full := tr.Since(after)
	assert.False(t, full)
	assert.Empty(t, removed)
}

func TestForeignEpochIsFull(t *testing.T) {
	t.Parallel()
	kv := newKV()
	old, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)
	old.Stamp("1")
	require.NoError(t, old.Save(t.Context(), kv))
	cursor := old.Cursor()

	wiped, err := delta.Load(t.Context(), newKV(), "tasks/ws")
	require.NoError(t, err)
	wiped.Stamp("1")
	wiped.Stamp("2")

	_, _, full := wiped.Since(cursor)
	assert.True(t, full)
	assert.True(t, wiped.Stale("2", cursor))
}

func TestStateSurvivesReload(t *testing.T) {
	t.Parallel()
	kv := newKV()
	long := strings.Repeat("/long", 80) + ".md"
	before := &delta.Observed{Store: kv}
	first, err := before.Observe(t.Context(), "docs/ws", map[string]any{long: 1, "b.md": 1}, "")
	require.NoError(t, err)

	after := &delta.Observed{Store: kv}
	got, err := after.Observe(t.Context(), "docs/ws", map[string]any{long: 1}, first.Cursor)
	require.NoError(t, err)

	assert.Equal(t, delta.View{Removed: []string{"b.md"}, Cursor: got.Cursor}, got)
	assert.NotEqual(t, first.Cursor, got.Cursor)
}

func TestFreshEpochIsKept(t *testing.T) {
	t.Parallel()
	kv := newKV()
	first, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)
	again, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)

	assert.Equal(t, first.Cursor(), again.Cursor())
}

func TestCursorAfterFailedSaveIsFullAfterRestart(t *testing.T) {
	t.Parallel()
	kv := newKV()
	tr, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)
	tr.Stamp("1")
	require.NoError(t, tr.Save(t.Context(), kv))
	saved := tr.Cursor()
	tr.Stamp("2")
	kv.failSet = true
	require.Error(t, tr.Save(t.Context(), kv))
	lost := tr.Cursor()

	again, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)
	again.Stamp("3")
	_, _, full := again.Since(lost)
	assert.True(t, full)
	changed, _, full := again.Since(saved)
	assert.Equal(t, []any{[]string{"3"}, false}, []any{changed, full})
}

func TestPartialSaveIsFullAfterRestart(t *testing.T) {
	t.Parallel()
	for name, fail := range map[string]func(key string, raw []byte) bool{
		"entries written, head not": func(key string, raw []byte) bool {
			return strings.HasSuffix(key, "/head") && !strings.Contains(string(raw), "dirty")
		},
		"head written, entries not": func(key string, _ []byte) bool { return strings.Contains(key, "/e/") },
	} {
		t.Run(name, func(t *testing.T) {
			kv := newKV()
			tr, err := delta.Load(t.Context(), kv, "tasks/ws")
			require.NoError(t, err)
			tr.Stamp("1")
			require.NoError(t, tr.Save(t.Context(), kv))
			saved := tr.Cursor()
			tr.Stamp("2")
			kv.fail = fail
			require.Error(t, tr.Save(t.Context(), kv))
			kv.fail = nil

			again, err := delta.Load(t.Context(), kv, "tasks/ws")
			require.NoError(t, err)
			_, _, full := again.Since(saved)
			assert.True(t, full)
		})
	}
}

func TestUnreadableEntryStartsANewEpoch(t *testing.T) {
	t.Parallel()
	kv := newKV()
	tr, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)
	tr.Stamp("1")
	require.NoError(t, tr.Save(t.Context(), kv))
	for key := range kv.data {
		if strings.Contains(key, "/e/") {
			kv.data[key] = []byte("{")
		}
	}

	again, err := delta.Load(t.Context(), kv, "tasks/ws")
	require.NoError(t, err)
	_, _, full := again.Since(tr.Cursor())
	assert.True(t, full)
}
