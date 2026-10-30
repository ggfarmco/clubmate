package sdui_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalsdui "clubmate/internal/sdui"
	"clubmate/pkg/sdui"
)

const prj = "prj_0123456789abcdef"

func TestStoreKeepsPreviousTreeOnInvalidRender(t *testing.T) {
	s := internalsdui.NewStore()
	rev, err := s.Render(t.Context(), prj, "terminals.main", "terminals", sdui.Empty("none"))
	require.NoError(t, err)
	_, err = s.Render(t.Context(), prj, "terminals.main", "terminals", sdui.Node{Type: "marquee"})
	require.ErrorIs(t, err, internalsdui.ErrInvalidTree)
	views := s.Get(prj)
	require.Len(t, views, 1)
	assert.Equal(t, rev, views[0].Rev)
	assert.Equal(t, "empty", views[0].Tree.Type)
}

func TestStoreRejectsRenderFromCancelledCaller(t *testing.T) {
	s := internalsdui.NewStore()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.Render(ctx, prj, "a.main", "a", sdui.Empty("late"))
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, s.Get(prj))
}

func TestStoreRevPerSlotAndSortedGet(t *testing.T) {
	s := internalsdui.NewStore()
	_, _ = s.Render(t.Context(), prj, "b.main", "b", sdui.Empty("1"))
	_, _ = s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("1"))
	rev, _ := s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("2"))
	assert.EqualValues(t, 2, rev)
	views := s.Get(prj)
	require.Len(t, views, 2)
	assert.Equal(t, []string{"a.main", "b.main"}, []string{views[0].Slot, views[1].Slot})
	assert.Equal(t, []internalsdui.SlotView{}, s.Get("prj_other"))
}

func TestSubscribeDeliversCurrentThenUpdates(t *testing.T) {
	s := internalsdui.NewStore()
	_, _ = s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("old"))
	_, _ = s.Render(t.Context(), "prj_other", "a.main", "a", sdui.Empty("other"))
	sub, err := s.Subscribe(prj)
	require.NoError(t, err)
	t.Cleanup(sub.Close)
	assert.Equal(t, "old", recv(t, sub).Tree.Props["text"])
	_, _ = s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("new"))
	got := recv(t, sub)
	assert.Equal(t, "new", got.Tree.Props["text"])
	assert.EqualValues(t, 2, got.Rev)
}

func TestClearOwnerRemovesSlotsAndNotifies(t *testing.T) {
	s := internalsdui.NewStore()
	_, _ = s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("x"))
	_, _ = s.Render(t.Context(), "prj_other", "a.sidebar", "a", sdui.Empty("x"))
	_, _ = s.Render(t.Context(), prj, "b.main", "b", sdui.Empty("x"))
	sub, _ := s.Subscribe(prj)
	t.Cleanup(sub.Close)
	for range 2 {
		recv(t, sub)
	}
	s.ClearOwner("a")
	got := recv(t, sub)
	assert.Equal(t, "a.main", got.Slot)
	assert.EqualValues(t, 2, got.Rev)
	assert.Equal(t, sdui.Empty(""), got.Tree)
	assert.Len(t, s.Get(prj), 1)
	assert.Empty(t, s.Get("prj_other"))
}

func TestDropProjectRemovesSlotsAndNotifies(t *testing.T) {
	s := internalsdui.NewStore()
	_, _ = s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("x"))
	_, _ = s.Render(t.Context(), "prj_other", "a.main", "a", sdui.Empty("x"))
	sub, _ := s.Subscribe(prj)
	t.Cleanup(sub.Close)
	recv(t, sub)
	s.DropProject(prj)
	got := recv(t, sub)
	assert.Equal(t, "a.main", got.Slot)
	assert.EqualValues(t, 2, got.Rev)
	assert.Equal(t, sdui.Empty(""), got.Tree)
	assert.Empty(t, s.Get(prj))
	assert.Len(t, s.Get("prj_other"), 1)
}

func TestRenderAfterDropRejected(t *testing.T) {
	s := internalsdui.NewStore()
	s.DropProject(prj)
	_, err := s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("x"))
	assert.ErrorIs(t, err, internalsdui.ErrUnknownProject)
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	s := internalsdui.NewStore()
	sub, _ := s.Subscribe(prj)
	for range 200 {
		_, _ = s.Render(t.Context(), prj, "a.main", "a", sdui.Empty("x"))
	}
	for range sub.C() {
	}
	assert.ErrorIs(t, sub.Err(), internalsdui.ErrSlowSubscriber)
	sub.Close()
}

func TestCloseIsIdempotentAndClosesChannel(t *testing.T) {
	s := internalsdui.NewStore()
	sub, _ := s.Subscribe(prj)
	sub.Close()
	sub.Close()
	_, ok := <-sub.C()
	assert.False(t, ok)
	assert.NoError(t, sub.Err())
}

func TestSectionsOrderOwnerAndSlots(t *testing.T) {
	sec := internalsdui.NewSections([]internalsdui.SectionDef{
		{ID: "b", Title: "B", Order: 1, Plugin: "pb"},
		{ID: "a", Title: "A", Order: 1, Plugin: "pa"},
		{ID: "z", Title: "Z", Order: 0, Plugin: "pz"},
	})
	var ids []string
	for _, s := range sec.List() {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{"z", "a", "b"}, ids)
	owner, ok := sec.Owner("a")
	assert.True(t, ok)
	assert.Equal(t, "pa", owner)
	_, ok = sec.Owner("nope")
	assert.False(t, ok)

	section, ok := internalsdui.SlotSection("a.sidebar")
	assert.True(t, ok)
	assert.Equal(t, "a", section)
	_, ok = internalsdui.SlotSection("a.footer")
	assert.False(t, ok)
}

func TestSectionsKeepOnlyValidHosts(t *testing.T) {
	sec := internalsdui.NewSections([]internalsdui.SectionDef{
		{ID: "code", Plugin: "pc"},
		{ID: "git", Plugin: "pg", Host: "code"},
		{ID: "sub", Plugin: "ps", Host: "git"},
		{ID: "lost", Plugin: "pl", Host: "nope"},
		{ID: "late", Plugin: "pt", Host: "lost"},
		{ID: "app", Plugin: "pa", Scope: internalsdui.ScopeApp, Host: "code"},
	})
	hosts := map[string]string{}
	for _, s := range sec.List() {
		hosts[s.ID] = s.Host
	}
	assert.Equal(t, map[string]string{"code": "", "git": "code", "sub": "", "lost": "", "late": "lost", "app": ""}, hosts)
}

func recv(t *testing.T, sub *internalsdui.Subscription) internalsdui.SlotView {
	t.Helper()
	select {
	case v, ok := <-sub.C():
		require.True(t, ok, "subscription closed: %v", sub.Err())
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("no view within 2s")
		return internalsdui.SlotView{}
	}
}
