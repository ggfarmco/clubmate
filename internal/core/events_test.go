package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/bus"
	"clubmate/internal/session"
	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

func TestCoreEventsForWorkspaceAndSessionLifecycle(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	require.NoError(t, svc.CheckTransitions(t.Context()))
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	s, err := svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Command: []string{"sh", "-c", "sleep 0.2; exit 4"}})
	require.NoError(t, err)
	require.NoError(t, svc.CheckTransitions(t.Context()))
	testutil.Eventually(t, func() bool {
		_ = svc.CheckTransitions(t.Context())
		evs, _ := b.Read(t.Context(), "session.exited", 0, 0)
		return len(evs) == 1
	}, 3*time.Second)
	require.NoError(t, svc.CheckTransitions(t.Context()))
	exited := testutil.WaitEvents(t, b, "session.exited", func(e []bus.Event) bool { return len(e) > 0 })
	require.Len(t, exited, 1)
	assert.JSONEq(t, `{"id":"`+s.ID+`","workspace":"`+p.ID+`","owner":"","state":"exited","exitCode":4}`, string(exited[0].Payload))
	_, err = svc.AddRepo(t.Context(), p.ID, t.TempDir(), contract.FolderMeta{})
	require.NoError(t, err)
	require.NoError(t, svc.RemoveSession(t.Context(), s.ID))
	require.NoError(t, svc.DeleteWorkspace(t.Context(), p.ID))
	all := testutil.WaitEvents(t, b, "*", func(e []bus.Event) bool { return len(e) >= 6 })
	assert.Equal(t, []string{"workspace.created", "session.created", "session.exited", "workspace.updated", "session.removed", "workspace.deleted"}, topics(all))
}

func TestKillSessionPublishesKilled(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	s, err := svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	require.NoError(t, svc.KillSession(t.Context(), s.ID))
	killed := testutil.WaitEvents(t, b, "session.killed", func(e []bus.Event) bool { return len(e) > 0 })
	require.Len(t, killed, 1)
	assert.JSONEq(t, `{"id":"`+s.ID+`"}`, string(killed[0].Payload))
}

func TestKilledSessionCanBeRemovedImmediately(t *testing.T) {
	svc, _ := testutil.NewServiceWithBus(t)
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	s, err := svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Command: []string{"sh", "-c", "exec cat"}})
	require.NoError(t, err)
	require.NoError(t, svc.KillSession(t.Context(), s.ID))
	assert.NoError(t, svc.RemoveSession(t.Context(), s.ID))
}

func TestEventSurvivesCancelledRequest(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	s, err := svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Command: []string{"sh", "-c", "exit 0"}})
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		got, _ := svc.GetSession(t.Context(), s.ID)
		return got.State == session.StateExited
	}, 3*time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, svc.RemoveSession(ctx, s.ID))
	removed := testutil.WaitEvents(t, b, "session.removed", func(e []bus.Event) bool { return len(e) > 0 })
	assert.Len(t, removed, 1)
}

func topics(evs []bus.Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Topic)
	}
	return out
}

func TestFastExitAfterBaselinePublishesOnce(t *testing.T) {
	svc, b := testutil.NewServiceWithBus(t)
	require.NoError(t, svc.CheckTransitions(t.Context()))
	p, err := svc.CreateWorkspace(t.Context(), "demo", nil)
	require.NoError(t, err)
	s, err := svc.CreateSession(t.Context(), session.CreateRequest{WorkspaceID: p.ID, Command: []string{"sh", "-c", "exit 2"}})
	require.NoError(t, err)
	testutil.Eventually(t, func() bool {
		got, _ := svc.GetSession(t.Context(), s.ID)
		return got.State == session.StateExited
	}, 3*time.Second)
	require.NoError(t, svc.CheckTransitions(t.Context()))
	require.NoError(t, svc.CheckTransitions(t.Context()))
	exited := testutil.WaitEvents(t, b, "session.exited", func(e []bus.Event) bool { return len(e) > 0 })
	require.Len(t, exited, 1)
	assert.JSONEq(t, `{"id":"`+s.ID+`","workspace":"`+p.ID+`","owner":"","state":"exited","exitCode":2}`, string(exited[0].Payload))
}
