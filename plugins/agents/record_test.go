package main

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/plugins/agents/api"
)

func TestRecordJSONContract(t *testing.T) {
	const want = `{"id":"agt_1","workspace":"ws","worktree":"/r","agent":"claude","agentSessionId":"acp-1",` +
		`"parentSession":"agt_p","advisor":"opus","fork":"abc","handoffFrom":"agt_f","handoffTo":"agt_t",` +
		`"importedFrom":"agt_i","title":"T","titleSet":true,"status":"closed","mode":"plan","finish":"merge",` +
		`"afk":true,"createdAt":"2026-10-31T12:00:00Z","updatedAt":"2026-10-31T12:00:00Z",` +
		`"asks":true,"notify":"muted","error":"boom"}`
	var rec record
	require.NoError(t, json.Unmarshal([]byte(want), &rec))
	assert.Equal(t, "agt_p", rec.ParentSession)
	assert.Equal(t, "agt_i", rec.ImportedFrom)
	raw, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.Equal(t, want, string(raw))

	info, err := json.Marshal(sessionInfo{record: record{ID: "agt_2", lineage: lineage{ParentSession: "agt_p"}}, NotifyEffective: notifyAll})
	require.NoError(t, err)
	assert.Equal(t, `{"id":"agt_2","workspace":"","worktree":"","agent":"","agentSessionId":"","parentSession":"agt_p","title":"","status":"","mode":"",`+
		`"createdAt":"0001-01-01T00:00:00Z","updatedAt":"0001-01-01T00:00:00Z","asks":false,"notifyEffective":"all","closed":false}`, string(info))
}

func TestSessionInfoDecodesAsAPISession(t *testing.T) {
	info := sessionInfo{record: record{
		ID:        "agt_1",
		Workspace: "ws",
		Worktree:  "/r",
		Title:     "T",
		lineage:   lineage{ParentSession: "agt_p", HandoffFrom: "agt_f"},
		Status:    statusClosed,
	}, Closed: true}
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	var got api.Session
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, api.Session{
		ID:            "agt_1",
		Workspace:     "ws",
		Worktree:      "/r",
		Title:         "T",
		ParentSession: "agt_p",
		HandoffFrom:   "agt_f",
		Status:        statusClosed,
	}, got)
}

func TestLoadRecordsMigratesLegacySessions(t *testing.T) {
	kv := memStore{
		"session/agt_000000000000000a":       `{"id":"agt_000000000000000a","workspace":"prj_1","status":"done","closed":true}`,
		"session/agt_000000000000000b":       `{"id":"agt_000000000000000b","workspace":"prj_1","title":"flat","updatedAt":"2026-10-31T12:00:00Z"}`,
		"session/prj_1/agt_000000000000000b": `{"id":"agt_000000000000000b","workspace":"prj_1","title":"nested","updatedAt":"2026-10-31T11:00:00Z"}`,
		"session/agt_000000000000000c":       `{"id":"agt_000000000000000c","workspace":"prj_2","title":"flat","updatedAt":"2026-10-31T11:00:00Z"}`,
		"session/prj_2/agt_000000000000000c": `{"id":"agt_000000000000000c","workspace":"prj_2","title":"nested","updatedAt":"2026-10-31T12:00:00Z"}`,
		"session/prj_2/agt_000000000000000d": `{"id":"agt_000000000000000d","workspace":"prj_2","status":"idle"}`,
	}
	want := map[string]string{"agt_000000000000000a": statusClosed + "/", "agt_000000000000000b": "/flat", "agt_000000000000000c": "/nested", "agt_000000000000000d": statusIdle + "/"}

	for range 2 {
		recs, err := loadRecords(t.Context(), kv)
		require.NoError(t, err)
		got := map[string]string{}
		for _, r := range recs {
			got[r.ID] = r.Status + "/" + r.Title
		}
		assert.Equal(t, want, got)
		assert.ElementsMatch(t, []string{"session/prj_1/agt_000000000000000a", "session/prj_1/agt_000000000000000b", "session/prj_2/agt_000000000000000c", "session/prj_2/agt_000000000000000d"}, slices.Collect(maps.Keys(kv)))
		assert.NotContains(t, kv["session/prj_1/agt_000000000000000a"], `"closed":`)
	}
}

type memStore map[string]string

func (m memStore) StorageList(_ context.Context, prefix string) ([]string, error) {
	return slices.Sorted(func(yield func(string) bool) {
		for k := range m {
			if strings.HasPrefix(k, prefix) && !yield(k) {
				return
			}
		}
	}), nil
}

func (m memStore) StorageGet(_ context.Context, key string, out any) (bool, error) {
	raw, ok := m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal([]byte(raw), out)
}

func (m memStore) StorageSet(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	m[key] = string(raw)
	return err
}

func (m memStore) StorageDelete(_ context.Context, key string) error {
	delete(m, key)
	return nil
}
