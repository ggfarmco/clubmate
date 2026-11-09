package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

func TestPhoneCommandsAreRemoteAndTakeThePhoneAsTheUser(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	var manifest struct {
		Contributes struct {
			Commands []struct {
				ID     string `json:"id"`
				Remote bool   `json:"remote"`
			} `json:"commands"`
		} `json:"contributes"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))
	remote := map[string]bool{}
	for _, c := range manifest.Contributes.Commands {
		remote[c.ID] = c.Remote
	}
	for _, id := range []string{"agents.connect-answer", "agents.land-answer", "agents.land-commits", "agents.repo-fix", "agents.repo-leave", "agents.archive-plan", "agents.archive", "agents.folder-remove"} {
		assert.True(t, remote[id], id)
	}

	a := &app{}
	assert.True(t, a.userWord(pluginsdk.CommandRun{Via: remotePlugin}))
	assert.False(t, a.userWord(pluginsdk.CommandRun{Via: remotePlugin, Agent: true}))
	assert.False(t, a.userWord(pluginsdk.CommandRun{Via: "graphs"}))
}

func TestOnceReturnsTheFirstResultAcrossARestart(t *testing.T) {
	t.Parallel()
	kv := memStore{}
	now := time.Now()
	runs := 0
	run := func() (any, error) {
		runs++
		return map[string]int{"closed": 3}, nil
	}

	_, err := new(onceLocks).run(t.Context(), kv, "ws", "agents.archive", "k", now, run)
	require.NoError(t, err)
	again, err := new(onceLocks).run(t.Context(), kv, "ws", "agents.archive", "k", now.Add(time.Hour), run)

	require.NoError(t, err)
	assert.Equal(t, 1, runs)
	assert.JSONEq(t, `{"closed": 3}`, string(again.(json.RawMessage)))
}

func TestOnceForgetsAfterItsWindowAndNeverKeepsAFailure(t *testing.T) {
	t.Parallel()
	kv := memStore{}
	now := time.Now()
	boom := errors.New("refused")
	_, err := new(onceLocks).run(t.Context(), kv, "ws", "agents.archive", "k", now, func() (any, error) { return nil, boom })
	require.ErrorIs(t, err, boom)

	runs := 0
	run := func() (any, error) {
		runs++
		return nil, nil
	}
	for _, at := range []time.Time{now, now.Add(onceKeep)} {
		_, err = new(onceLocks).run(t.Context(), kv, "ws", "agents.archive", "k", at, run)
		require.NoError(t, err)
	}

	assert.Equal(t, 2, runs, "a failure isn't stored, and the window ends")
}

func TestOnceSaysSoWhenTheFirstTryWasCutOff(t *testing.T) {
	t.Parallel()
	kv := memStore{}
	now := time.Now()
	killed := false
	locks := new(onceLocks)
	_, _ = locks.run(t.Context(), storeThatDies{memStore: kv}, "ws", "agents.archive", "k", now, func() (any, error) {
		killed = true
		return nil, nil
	})
	require.True(t, killed)

	_, err := new(onceLocks).run(t.Context(), kv, "ws", "agents.archive", "k", now, func() (any, error) { return nil, nil })

	require.ErrorIs(t, err, errOnceCutOff)
}

func TestOnceKeysAreOwnedByTheirCommand(t *testing.T) {
	t.Parallel()
	kv := memStore{}
	locks := new(onceLocks)
	now := time.Now()
	runs := 0
	run := func() (any, error) {
		runs++
		return nil, nil
	}

	for _, command := range []string{"agents.archive", "agents.connect-answer"} {
		_, err := locks.run(t.Context(), kv, "ws", command, "k", now, run)
		require.NoError(t, err)
	}

	assert.Equal(t, 2, runs)
	assert.Empty(t, locks.keys, "a finished call keeps no lock")
}

func TestOncePrunesOldRecordsAtMostHourly(t *testing.T) {
	t.Parallel()
	kv := memStore{}
	locks := new(onceLocks)
	now := time.Now()
	stale := onceKey + "ws/stale"
	run := func() (any, error) { return nil, nil }
	_, err := locks.run(t.Context(), kv, "ws", "agents.archive", "a", now, run)
	require.NoError(t, err)
	require.NoError(t, kv.StorageSet(t.Context(), stale, onceRecord{At: now.Add(-onceKeep)}))

	_, err = locks.run(t.Context(), kv, "ws", "agents.archive", "b", now.Add(oncePrune/2), run)
	require.NoError(t, err)
	assert.Contains(t, kv, stale)

	_, err = locks.run(t.Context(), kv, "ws", "agents.archive", "c", now.Add(oncePrune), run)
	require.NoError(t, err)
	assert.NotContains(t, kv, stale)
}

func TestOneArchiveAtATimePerTask(t *testing.T) {
	t.Parallel()
	a := &app{}
	s := newSession(record{ID: "agt_task", Status: statusDone}, nil, true, nil)
	release, ok := a.archiving.claim("agt_task")
	require.True(t, ok)

	_, err := a.archive(t.Context(), "ws", s, nil, "", nil)
	require.ErrorIs(t, err, errArchiveBusy)
	_, other := a.archiving.claim("agt_other")
	assert.True(t, other)

	release()
	again, ok := a.archiving.claim("agt_task")
	assert.True(t, ok)
	again()
}

func TestRefusalsCarryAReasonAndOtherErrorsDont(t *testing.T) {
	t.Parallel()
	assert.Equal(t, contract.ReasonAnswered, pluginsdk.ReasonOf(withReason(errConnectStale)))
	assert.Equal(t, contract.ReasonStale, pluginsdk.ReasonOf(withReason(fmt.Errorf("%w: plan is running", errArchiveHeld))))
	assert.Equal(t, contract.ReasonStale, pluginsdk.ReasonOf(withReason(fmt.Errorf("%w: agt_x", errNoSession))))
	assert.Equal(t, contract.ReasonUnknown, pluginsdk.ReasonOf(withReason(errOnceCutOff)))
	assert.Empty(t, pluginsdk.ReasonOf(withReason(errors.New("dial tcp: connection refused"))))
	assert.Empty(t, pluginsdk.ReasonOf(withReason(errArchiveBusy)))
	assert.NoError(t, withReason(nil))
}

func TestWorkWithLossAndFoldersStayOnTheMac(t *testing.T) {
	t.Parallel()
	phone := pluginsdk.CommandRun{Via: remotePlugin}
	assert.ErrorIs(t, refusedChoices(phone, []archiveChoice{{Path: "/w", Anyway: true}}), errArchiveMac)
	assert.ErrorIs(t, refusedChoices(phone, []archiveChoice{{Path: "/f", Folder: true}}), errArchiveMac)
	assert.NoError(t, refusedChoices(phone, []archiveChoice{{Path: "/w", Seen: "tip"}}))
	assert.NoError(t, refusedChoices(pluginsdk.CommandRun{}, []archiveChoice{{Path: "/w", Anyway: true}}))
}

type storeThatDies struct {
	memStore
}

func (s storeThatDies) StorageSet(ctx context.Context, key string, v any) error {
	if rec, ok := v.(onceRecord); ok && rec.Done {
		return errors.New("killed")
	}
	return s.memStore.StorageSet(ctx, key, v)
}
