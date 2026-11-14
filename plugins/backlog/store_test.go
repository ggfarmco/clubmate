package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

type countingKV struct {
	data  map[string][]byte
	lists int
	fail  func(op, key string) bool
}

func (k *countingKV) StorageList(_ context.Context, prefix string) ([]string, error) {
	if strings.HasPrefix(prefix, "task/") {
		k.lists++
	}
	var keys []string
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (k *countingKV) StorageGet(_ context.Context, key string, out any) (bool, error) {
	raw, ok := k.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (k *countingKV) StorageSet(_ context.Context, key string, v any) error {
	if k.fail != nil && k.fail("set", key) {
		return errors.New("disk full")
	}
	raw, err := json.Marshal(v)
	k.data[key] = raw
	return err
}

func (k *countingKV) StorageDelete(_ context.Context, key string) error {
	if k.fail != nil && k.fail("delete", key) {
		return errors.New("disk full")
	}
	delete(k.data, key)
	return nil
}

func (k *countingKV) StorageDeletePrefix(_ context.Context, prefix string) error {
	for key := range k.data {
		if strings.HasPrefix(key, prefix) {
			delete(k.data, key)
		}
	}
	return nil
}

func TestSnapshotReadsStorageOnceUntilAWrite(t *testing.T) {
	kv := &countingKV{data: map[string][]byte{}}
	s := &store{h: kv}
	_, _, err := s.add(t.Context(), "ws", Task{Title: "a"})
	require.NoError(t, err)
	for range 2 {
		_, err := s.snapshot(t.Context(), "ws")
		require.NoError(t, err)
	}
	assert.Equal(t, 1, kv.lists)
	_, _, err = s.mutate(t.Context(), "ws", 1, "", func(t *Task) error {
		t.Title = "b"
		return nil
	})
	require.NoError(t, err)
	tasks, err := s.snapshot(t.Context(), "ws")
	require.NoError(t, err)
	assert.Equal(t, []any{2, "b"}, []any{kv.lists, tasks[0].Title})
}

func TestSyncLinksBumpsLinkedTask(t *testing.T) {
	s := &store{h: &countingKV{data: map[string][]byte{}}}
	b, _, err := s.add(t.Context(), "ws", Task{Title: "b"})
	require.NoError(t, err)
	_, _, err = s.add(t.Context(), "ws", Task{Title: "c"})
	require.NoError(t, err)
	a, _, err := s.add(t.Context(), "ws", Task{Title: "a", Links: []Link{{Kind: linkBlocks, Task: b.ID}}})
	require.NoError(t, err)
	before, err := s.since(t.Context(), "ws", "")
	require.NoError(t, err)

	require.NoError(t, s.remove(t.Context(), "ws", a.ID))

	got, err := s.since(t.Context(), "ws", before.Cursor)
	require.NoError(t, err)
	require.Len(t, got.Tasks, 1)
	assert.Equal(t, []any{true, b.ID, 0, []int{a.ID}}, []any{got.Delta, got.Tasks[0].ID, len(got.Tasks[0].Links), got.Removed})
	_, _, err = s.mutate(t.Context(), "ws", a.ID, before.Cursor, func(*Task) error { return nil })
	assert.Equal(t, contract.ReasonStale, pluginsdk.ReasonOf(err))
}

func TestFailedRemoveLeavesNoTombstone(t *testing.T) {
	kv := &countingKV{data: map[string][]byte{}}
	s := &store{h: kv}
	a, _, err := s.add(t.Context(), "ws", Task{Title: "a"})
	require.NoError(t, err)
	before, err := s.since(t.Context(), "ws", "")
	require.NoError(t, err)
	kv.fail = func(op, key string) bool { return op == "delete" && strings.HasPrefix(key, "task/") }

	require.Error(t, s.remove(t.Context(), "ws", a.ID))

	got, err := s.since(t.Context(), "ws", before.Cursor)
	require.NoError(t, err)
	assert.Empty(t, got.Removed)
}

func TestTrackerSaveFailureKeepsTheWrite(t *testing.T) {
	kv := &countingKV{data: map[string][]byte{}}
	s := &store{h: kv}
	a, _, err := s.add(t.Context(), "ws", Task{Title: "a"})
	require.NoError(t, err)
	before, err := s.since(t.Context(), "ws", "")
	require.NoError(t, err)
	kv.fail = func(op, key string) bool { return op == "set" && strings.HasPrefix(key, "delta/") }

	_, _, err = s.mutate(t.Context(), "ws", a.ID, "", func(t *Task) error {
		t.Title = "b"
		return nil
	})
	require.NoError(t, err)

	got, err := s.since(t.Context(), "ws", before.Cursor)
	require.NoError(t, err)
	assert.Equal(t, []any{false, "b"}, []any{got.Delta, got.Tasks[0].Title})
}
