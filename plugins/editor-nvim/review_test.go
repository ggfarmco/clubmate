package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

type fakeReviewer struct {
	mu       sync.Mutex
	entries  []reviewEntry
	comments []reviewComment
	layouts  []string
	opened   []reviewOpenArgs
	filters  *reviewFilters
	metas    []reviewMeta
	tabs     []reviewTab
	tab      int
	reads    int
	readErr  error
	closed   []int
}

func (f *fakeReviewer) reviewFilters(int) (reviewFilters, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.filters == nil {
		return reviewFilters{Code: reviewFilter{Shown: true}, Reviewed: reviewFilter{Shown: true}}, nil
	}
	return *f.filters, nil
}

func (f *fakeReviewer) reviewMeta(_ int, m reviewMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metas = append(f.metas, m)
	return nil
}

func (f *fakeReviewer) reviewList() ([]reviewTab, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.tabs), nil
}

func (f *fakeReviewer) reviewOpen(o reviewOpenArgs) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, o)
	f.tab++
	return f.tab, nil
}

func (f *fakeReviewer) reviewInfo(int) (reviewInfo, error) {
	return reviewInfo{Labels: reviewLabels{Left: "main", Right: "Working tree"}, Editable: true}, nil
}

func (f *fakeReviewer) reviewEntries(int) ([]reviewEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.entries), nil
}

func (f *fakeReviewer) readComments(string) ([]reviewComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return slices.Clone(f.comments), f.readErr
}

func (f *fakeReviewer) lastMeta() reviewMeta {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.metas[len(f.metas)-1]
}

func (f *fakeReviewer) reviewFocus(int) error { return nil }

func (f *fakeReviewer) reviewClose(tab int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, tab)
	return nil
}

func (f *fakeReviewer) reviewLayout(_ int, layout string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.layouts = append(f.layouts, layout)
	return nil
}

type fakeHost struct {
	mu       sync.Mutex
	views    map[string]sdui.Node
	payloads []reviewPayload
	store    map[string][]byte
	sessions []agentSession
	sent     []sendReviewArgs
}

func (f *fakeHost) RunCommand(_ context.Context, id, _ string, args any, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var res any
	switch id {
	case sessionsCmd:
		res = f.sessions
	case sendReviewCmd:
		a := args.(sendReviewArgs)
		f.sent = append(f.sent, a)
		res = sendReviewResult{Session: cmp.Or(a.Session, "agt_made")}
	default:
		return fmt.Errorf("unexpected command %s", id)
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (f *fakeHost) StorageGet(_ context.Context, key string, out any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.store[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (f *fakeHost) StorageSet(_ context.Context, key string, v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.store == nil {
		f.store = map[string][]byte{}
	}
	b, err := json.Marshal(v)
	f.store[key] = b
	return err
}

func (f *fakeHost) StorageList(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.store {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys, nil
}

func (f *fakeHost) StorageDelete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.store, key)
	return nil
}

func (f *fakeHost) Render(_ context.Context, ws, slot string, n sdui.Node) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.views == nil {
		f.views = map[string]sdui.Node{}
	}
	f.views[ws+"/"+slot] = n
	return 1, nil
}

func (f *fakeHost) Publish(_ context.Context, topic string, payload any) (pluginsdk.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if topic == reviewTopic {
		f.payloads = append(f.payloads, payload.(reviewPayload))
	}
	return pluginsdk.Event{}, nil
}

func (f *fakeHost) view(ws string) sdui.Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.views[ws+"/"+reviewSlot]
}

func (f *fakeHost) last() reviewPayload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.payloads[len(f.payloads)-1]
}

func newTestApp() *app {
	return &app{reviews: map[string]*review{}, reviewSel: map[string]string{}, reviewLayouts: map[string]string{}, reviewPublished: map[string]reviewPayload{}, reviewWatched: map[*client]bool{}, readComments: func(string) ([]reviewComment, error) { return nil, nil }}
}

func openTestReview(t *testing.T, a *app, h *fakeHost, nv *fakeReviewer) *review {
	t.Helper()
	r := newReview("ws", "/repo", reviewKey{Worktree: "/repo", Base: "main"}, 1, nv, nv.readComments)
	r.Info, _ = nv.reviewInfo(1)
	require.NoError(t, r.refresh(true))
	a.reviewMu.Lock()
	a.addReviewLocked(r)
	a.reviewChangedLocked(context.Background(), h, r)
	a.reviewMu.Unlock()
	return r
}

func TestReviewFollowsNvimEvents(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}, {Path: "b.go", Kind: "modified", Category: "code"}}}
	openTestReview(t, a, h, nv)
	assert.Equal(t, 0, h.last().Reviewed)
	nv.entries[0].Reviewed = true
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "reviewed", Path: "a.go", Tab: 1})
	assert.Equal(t, 1, h.last().Reviewed)
	assert.Equal(t, 2, h.last().Total)
	a.applyReviewEvent(t.Context(), h, "ws", "/other", nvimEvent{Event: "review", Kind: "reviewed", Path: "a.go", Tab: 1})
	assert.Len(t, h.payloads, 2)
	nv.comments = []reviewComment{{ID: 4, Status: "open"}, {ID: 7, Status: "resolved"}}
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "comments", Tab: 1})
	assert.Equal(t, []commentState{{ID: 4, Status: "open"}, {ID: 7, Status: "resolved"}}, h.last().Comments)
	assert.Equal(t, 1, h.last().Open)
	nv.comments[0].Status = "resolved"
	assert.True(t, isReviewDB("/repo/.git/review.db-wal"))
	a.syncReviews(t.Context(), h, "ws")
	assert.Equal(t, 0, h.last().Open)
}

func TestReviewKeepsCommentsWhenNrevFails(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{comments: []reviewComment{{ID: 1, Status: "open"}}}
	openTestReview(t, a, h, nv)
	nv.comments, nv.readErr = nil, errors.New("nrev list: exit status 1")
	a.syncReviews(t.Context(), h, "ws")
	assert.Equal(t, 1, h.last().Open)
}

func TestNrevCommentsReadsTheWorktree(t *testing.T) {
	dir := t.TempDir()
	nrev := filepath.Join(dir, "nrev")
	script := `#!/bin/sh
[ "$PWD" = "` + dir + `" ] || { echo "wrong dir $PWD" >&2; exit 1; }
echo '[{"id":4,"file":"a.go","side":"right","line_start":3,"line_end":5,"type":"note","text":"Why?","status":"open","line_text":"x := 1","rev":"abc"}]'
`
	require.NoError(t, os.WriteFile(nrev, []byte(script), 0o755))
	list, err := nrevComments(nrev)(dir)
	require.NoError(t, err)
	assert.Equal(t, []reviewComment{{ID: 4, File: "a.go", Side: "right", LineStart: 3, LineEnd: 5, Type: "note", Text: "Why?", Status: "open", LineText: "x := 1"}}, list)
	_, err = nrevComments(nrev)(t.TempDir())
	assert.ErrorContains(t, err, "wrong dir")
}

func TestReviewClosedClearsBus(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}}}
	r := openTestReview(t, a, h, nv)
	assert.Equal(t, "empty", h.view("ws").Type)
	shown, _ := a.reviewStatus("ws", "/repo", 1)
	require.True(t, shown)
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "closed", Tab: 1})
	assert.Equal(t, reviewPayload{WorkspaceID: "ws", ID: r.ID, Worktree: "/repo", Base: "main", Total: 1, Closed: true}, h.last())
	shown, _ = a.reviewStatus("ws", "/repo", 1)
	assert.False(t, shown)
}

func TestReviewProgressCountsWhatTheForkShows(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{
		entries: []reviewEntry{{Path: "a.go", Category: "code"}, {Path: "a_test.go", Category: "test"}, {Path: "api.pb.go", Category: "generated"}},
		filters: &reviewFilters{Code: reviewFilter{Shown: true}, Tests: reviewFilter{Shown: true, Count: 1}, Generated: reviewFilter{Count: 1}},
	}
	openTestReview(t, a, h, nv)
	assert.Equal(t, 2, h.last().Total)
}

func TestReviewLayoutReachesEveryOpenReview(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{}
	openTestReview(t, a, h, nv)
	require.NoError(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: "review", Action: "layout", Value: jsonString("inline")}))
	assert.Equal(t, []string{"inline"}, nv.layouts)
	_, err := a.startReview(t.Context(), h, "ws", "/repo", nv, openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "feat"}, BaseRev: "feat"})
	require.NoError(t, err)
	assert.Equal(t, []string{"inline", "inline"}, nv.layouts)
	assert.ErrorIs(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: "review", Action: "layout", Value: jsonString("wide")}), errBadLayout)
}

func findNode(n sdui.Node, id string) (sdui.Node, bool) {
	if n.ID == id {
		return n, true
	}
	for _, c := range n.Children {
		if got, ok := findNode(c, id); ok {
			return got, true
		}
	}
	return sdui.Node{}, false
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func TestReviewHeadResolution(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := reviewRepo(t)
	git := func(dir string, args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	feat := filepath.Join(repo, ".worktrees", "feat")
	git(repo, "worktree", "add", "-q", feat, "-b", "feat")
	git(repo, "branch", "other")
	fork := git(repo, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(feat, "b.go"), []byte("package a\n"), 0o600))
	git(feat, "add", ".")
	git(feat, "commit", "-qm", "feat")
	for _, tc := range []struct {
		name, from, head, worktree, rev string
	}{
		{"no head is the caller's working tree", filepath.Join(feat, "sub"), "", feat, ""},
		{"head checked out in another worktree", repo, "feat", feat, ""},
		{"head checked out nowhere is its tip in the caller", feat, "other", feat, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.MkdirAll(tc.from, 0o700))
			worktree, rev, err := resolveReviewHead(t.Context(), tc.from, tc.head)
			require.NoError(t, err)
			assert.Equal(t, tc.worktree, worktree)
			assert.Equal(t, tc.rev, rev)
		})
	}
	_, _, err := resolveReviewHead(t.Context(), repo, "nope")
	assert.ErrorContains(t, err, `unknown head "nope"`)
	baseRev, left, err := reviewBase(t.Context(), feat, "main", "")
	require.NoError(t, err)
	assert.Equal(t, []string{fork, "main"}, []string{baseRev, left})
	baseRev, left, err = reviewBase(t.Context(), feat, "main", "other")
	require.NoError(t, err)
	assert.Equal(t, []string{"main", ""}, []string{baseRev, left})
}

func TestReviewOpenReusesAndMovesOpener(t *testing.T) {
	a, h := newTestApp(), &fakeHost{sessions: []agentSession{{ID: "agt_1", Worktree: "/repo", Title: "First", Status: "idle"}, {ID: "agt_2", Worktree: "/repo", Title: "Second", Status: "idle"}}}
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}}}
	req := openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "main"}, BaseRev: "abc1234", Left: "main", Opener: "agt_1", OpenerTitle: "First"}
	first, err := a.startReview(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	assert.Equal(t, []reviewMeta{{Subtitle: "Claude · First", Base: "main", Changes: "repo · working tree", Recipient: "First"}}, nv.metas)
	req.Opener, req.OpenerTitle = "agt_2", "Second"
	second, err := a.startReview(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	assert.Same(t, first, second)
	assert.Equal(t, []reviewOpenArgs{{
		Cwd: "/repo", Base: "abc1234", Left: "main",
		Host: reviewTabHost{ID: first.ID, Worktree: "/repo", Base: "main"},
	}}, nv.opened)
	assert.Equal(t, "agt_2", h.last().Opener)
	assert.Equal(t, reviewMeta{Subtitle: "Claude · Second", Base: "main", Changes: "repo · working tree", Recipient: "Second"}, nv.lastMeta())
	_, err = a.startReview(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	assert.Len(t, nv.metas, 2)
}

func TestReviewOpenInAnotherWorkspaceMovesIt(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	first, second := &fakeReviewer{}, &fakeReviewer{tab: 4}
	req := openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "main"}, BaseRev: "abc1234", Left: "main"}
	old, err := a.startReview(t.Context(), h, "a", "/repo", first, req)
	require.NoError(t, err)
	moved, err := a.startReview(t.Context(), h, "b", "/repo", second, req)
	require.NoError(t, err)
	assert.Equal(t, []int{old.Tab}, first.closed)
	assert.Equal(t, old.ID, moved.ID)
	assert.Empty(t, a.workspaceReviewsLocked("a"))
	assert.Equal(t, []*review{moved}, a.workspaceReviewsLocked("b"))
	assert.Contains(t, h.payloads, reviewPayload{WorkspaceID: "a", ID: old.ID, Worktree: "/repo", Base: "main", Closed: true})
	assert.Equal(t, reviewPayload{WorkspaceID: "b", ID: old.ID, Worktree: "/repo", Base: "main"}, h.last())
}

func TestReviewAdoptsOpenTabsAfterRestart(t *testing.T) {
	a := newTestApp()
	h := &fakeHost{sessions: []agentSession{{ID: "agt_1", Worktree: "/repo", Title: "First", Status: "idle"}}}
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}}}
	req := openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "main"}, BaseRev: "abc1234", Left: "main", Opener: "agt_1", OpenerTitle: "First"}
	first, err := a.startReview(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	nv.tabs = []reviewTab{{Tab: first.Tab, Layout: layoutInline, Host: nv.opened[0].Host}, {Tab: 9}}

	restarted := newTestApp()
	restarted.adoptReviews(t.Context(), h, "other", "/other", &fakeReviewer{tabs: nv.tabs[:1]})
	adopted, err := restarted.startReview(t.Context(), h, "ws", "/repo", nv, openReviewReq{Key: req.Key, BaseRev: req.BaseRev, Left: req.Left})
	require.NoError(t, err)
	assert.Len(t, nv.opened, 1)
	assert.Equal(t, first.Tab, adopted.Tab)
	assert.Equal(t, "agt_1", h.last().Opener)

	listed := newTestApp()
	listed.adoptReviews(t.Context(), h, "ws", "/repo", nv)
	assert.Equal(t, layoutInline, listed.reviewLayouts["ws"])
	assert.Len(t, listed.reviews, 1)
}

func TestReviewAdoptClosesTabOwnedByAnotherWorkspace(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	key := reviewKey{Worktree: "/repo", Base: "main"}
	host := reviewTabHost{ID: reviewID(key), Worktree: key.Worktree, Base: key.Base}
	owner, other := &fakeReviewer{tabs: []reviewTab{{Tab: 2, Host: host}}}, &fakeReviewer{tabs: []reviewTab{{Tab: 5, Host: host}}}
	a.adoptReviews(t.Context(), h, "a", "/repo", owner)
	a.adoptReviews(t.Context(), h, "b", "/repo", other)
	assert.Equal(t, []int{5}, other.closed)
	assert.Empty(t, owner.closed)
	assert.Same(t, owner, a.reviews[host.ID].nv)
	reconnected := &fakeReviewer{tabs: owner.tabs}
	a.adoptReviews(t.Context(), h, "a", "/repo", reconnected)
	assert.Empty(t, reconnected.closed)
}

func TestReviewMetaNamesBaseAndChanges(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{}
	_, err := a.startReview(t.Context(), h, "ws", "/repo", nv, openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "main"}, BaseRev: "main"})
	require.NoError(t, err)
	assert.Equal(t, reviewMeta{Base: "main", Changes: "repo · working tree", Recipient: newSessionTitle}, nv.lastMeta())

	r := &review{Key: reviewKey{Worktree: "/repo", Base: "97a29b4c0ffee", Head: "feat"}, Branch: "DUE-970", OpenerTitle: "Coder", Info: reviewInfo{Base: "97a29b4c0ffee", Labels: reviewLabels{Left: "Before turn 1", Right: "feat"}}}
	assert.Equal(t, reviewMeta{Subtitle: "Claude · Coder", Base: "Before turn 1 · 97a29b4", Changes: "feat"}, r.meta())
	r.Key.Head = ""
	assert.Equal(t, "DUE-970 · working tree", r.meta().Changes)
}

func TestReviewRestoresSentAfterReopen(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{}
	req := openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "main", Head: "feat"}, BaseRev: "main", Opener: "agt_1", OpenerTitle: "Coder"}
	r, err := a.openReviewLocked(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	r.Recipient, r.Sent = "agt_1", 3
	a.persistReview(t.Context(), h, r)
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "closed", Tab: r.Tab})
	again, err := a.openReviewLocked(t.Context(), h, "ws", "/repo", nv, openReviewReq{Key: req.Key, BaseRev: "main"})
	require.NoError(t, err)
	assert.NotSame(t, r, again)
	assert.Equal(t, []any{"agt_1", "Coder", "agt_1", 3}, []any{again.Opener, again.OpenerTitle, again.Recipient, again.Sent})
	assert.Equal(t, 3, h.last().Sent)
}

func TestPruneReviewsDropsStaleAndGone(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	now := time.Now()
	live := t.TempDir()
	put := func(id string, st reviewState) {
		require.NoError(t, h.StorageSet(t.Context(), reviewStorePrefix+id, st))
	}
	put("stale", reviewState{Worktree: live, Sent: 1, At: now.Add(-31 * 24 * time.Hour)})
	put("gone", reviewState{Worktree: filepath.Join(live, "removed"), Sent: 1, At: now})
	put("fresh", reviewState{Worktree: live, Sent: 2, At: now.Add(-29 * 24 * time.Hour)})
	put("legacy", reviewState{Sent: 3})
	a.pruneReviews(t.Context(), h, now)
	keys, err := h.StorageList(t.Context(), reviewStorePrefix)
	require.NoError(t, err)
	assert.Equal(t, []string{reviewStorePrefix + "fresh", reviewStorePrefix + "legacy"}, keys)
	var legacy reviewState
	_, err = h.StorageGet(t.Context(), reviewStorePrefix+"legacy", &legacy)
	require.NoError(t, err)
	assert.Equal(t, []any{3, true}, []any{legacy.Sent, legacy.At.Equal(now)})
	a.pruneReviews(t.Context(), h, now.Add(31*24*time.Hour))
	keys, err = h.StorageList(t.Context(), reviewStorePrefix)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestReviewDBChangesCoalesce(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	a.reviewDBDelay = 50 * time.Millisecond
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}}}
	openTestReview(t, a, h, nv)
	reads := func() int {
		nv.mu.Lock()
		defer nv.mu.Unlock()
		return nv.reads
	}
	base := reads()
	for range 3 {
		a.scheduleReviewDB(h, "ws")
	}
	assert.Equal(t, base, reads())
	require.Eventually(t, func() bool { return reads() == base+1 }, time.Second, 5*time.Millisecond)
	assert.Never(t, func() bool { return reads() > base+1 }, 150*time.Millisecond, 10*time.Millisecond)
}
