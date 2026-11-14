package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingKV struct {
	data  map[string][]byte
	lists int
}

func (k *countingKV) StorageList(_ context.Context, prefix string) ([]string, error) {
	k.lists++
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
	raw, err := json.Marshal(v)
	k.data[key] = raw
	return err
}

func (k *countingKV) StorageDelete(_ context.Context, key string) error {
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
	_, err := s.add(t.Context(), "ws", Task{Title: "a"})
	require.NoError(t, err)
	for range 2 {
		_, err := s.snapshot(t.Context(), "ws")
		require.NoError(t, err)
	}
	assert.Equal(t, 1, kv.lists)
	_, err = s.mutate(t.Context(), "ws", 1, func(t *Task) error {
		t.Title = "b"
		return nil
	})
	require.NoError(t, err)
	tasks, err := s.snapshot(t.Context(), "ws")
	require.NoError(t, err)
	assert.Equal(t, []any{2, "b"}, []any{kv.lists, tasks[0].Title})
}
