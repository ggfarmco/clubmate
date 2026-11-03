package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

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
	selected []string
	opened   []reviewOpenArgs
	filters  []string
	counts   reviewCategories
	tab      int
}

func (f *fakeReviewer) reviewFilter(_ int, name string, hidden bool) (reviewCategories, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch name {
	case "tests":
		f.counts.HideTests = hidden
	case "generated":
		f.counts.HideGenerated = hidden
	}
	if name != "" {
		f.filters = append(f.filters, fmt.Sprintf("%s=%t", name, hidden))
	}
	return f.counts, nil
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

func (f *fakeReviewer) reviewComments(int) ([]reviewComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.comments), nil
}

func (f *fakeReviewer) reviewSelect(_ int, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.selected = append(f.selected, path)
	return nil
}

func (f *fakeReviewer) reviewSetReviewed(_ int, path string, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.entries {
		if f.entries[i].Path == path {
			f.entries[i].Reviewed, f.entries[i].Stale = on, false
		}
	}
	return nil
}

func (f *fakeReviewer) reviewNext(int, int) error                 { return nil }
func (f *fakeReviewer) reviewExcludes(int, []string) error        { return nil }
func (f *fakeReviewer) reviewClose(int) error                     { return nil }
func (f *fakeReviewer) reviewFocus(int) error                     { return nil }
func (f *fakeReviewer) reviewJump(int, string, int, string) error { return nil }

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
	return &app{reviews: map[string]*review{}, reviewSel: map[string]string{}, reviewLayouts: map[string]string{}, reviewPublished: map[string]reviewPayload{}, reviewWatched: map[*client]bool{}}
}

func openTestReview(t *testing.T, a *app, h *fakeHost, nv *fakeReviewer) *review {
	t.Helper()
	r := newReview("ws", "/repo", reviewKey{Worktree: "/repo", Base: "main"}, 1, nv)
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
	require.NoError(t, nv.reviewSetReviewed(1, "a.go", true))
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "reviewed", Path: "a.go", Tab: 1})
	assert.Equal(t, "checkmark", findItem(t, h.view("ws"), "review/tree", "file:a.go").Icon)
	assert.Equal(t, 1, h.last().Reviewed)
	assert.Equal(t, 2, h.last().Total)
	require.NoError(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: "review/tree", Action: "toggleReviewed", Value: jsonString("file:b.go")}))
	assert.True(t, nv.entries[1].Reviewed)
	assert.Equal(t, 2, h.last().Reviewed)
	a.applyReviewEvent(t.Context(), h, "ws", "/other", nvimEvent{Event: "review", Kind: "reviewed", Path: "a.go", Tab: 1})
	assert.Len(t, h.payloads, 3)
}

func TestReviewClosedClearsSlotAndBus(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}}}
	r := openTestReview(t, a, h, nv)
	require.Equal(t, "stack", h.view("ws").Type)
	a.applyReviewEvent(t.Context(), h, "ws", "/repo", nvimEvent{Event: "review", Kind: "closed", Tab: 1})
	assert.Equal(t, "empty", h.view("ws").Type)
	assert.Equal(t, reviewPayload{WorkspaceID: "ws", ID: r.ID, Worktree: "/repo", Base: "main", Total: 1, Closed: true}, h.last())
	open, _ := a.reviewOpen("ws")
	assert.False(t, open)
}

func TestReviewTestsFilterDrivesNvim(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{counts: reviewCategories{Tests: 2, HideTests: true}}
	r := openTestReview(t, a, h, nv)
	assert.False(t, r.Filters.Tests)
	require.NoError(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: "review/filters/tests", Action: "filter", Value: json.RawMessage("true")}))
	assert.Equal(t, []string{"tests=false"}, nv.filters)
	assert.True(t, r.Filters.Tests)
	box, ok := findNode(h.view("ws"), "review/filters/tests")
	require.True(t, ok)
	assert.Equal(t, []any{"tests 2", true}, []any{box.Props["label"], box.Props["value"]})
}

func TestReviewLayoutReachesEveryOpenReview(t *testing.T) {
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{}
	openTestReview(t, a, h, nv)
	require.NoError(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: "review", Action: "layout", Value: jsonString("inline")}))
	assert.Equal(t, []string{"inline"}, nv.layouts)
	_, layout := a.reviewOpen("ws")
	assert.Equal(t, layoutInline, layout)
	assert.ErrorIs(t, a.reviewUIEvent(t.Context(), h, pluginsdk.UIEvent{WorkspaceID: "ws", Slot: reviewSlot, NodeID: "review", Action: "layout", Value: jsonString("wide")}), errBadLayout)
}

func findItem(t *testing.T, n sdui.Node, id, item string) sdui.TreeItem {
	t.Helper()
	tree, ok := findNode(n, id)
	require.True(t, ok, "no node %s", id)
	var walk func([]sdui.TreeItem) (sdui.TreeItem, bool)
	walk = func(items []sdui.TreeItem) (sdui.TreeItem, bool) {
		for _, it := range items {
			if it.ID == item {
				return it, true
			}
			if got, ok := walk(it.Children); ok {
				return got, true
			}
		}
		return sdui.TreeItem{}, false
	}
	got, ok := walk(tree.Props["items"].([]sdui.TreeItem))
	require.True(t, ok, "no item %s", item)
	return got
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
	a, h := newTestApp(), &fakeHost{}
	nv := &fakeReviewer{entries: []reviewEntry{{Path: "a.go", Kind: "modified", Category: "code"}}}
	req := openReviewReq{Key: reviewKey{Worktree: "/repo", Base: "main"}, BaseRev: "abc1234", Left: "main", Opener: "agt_1", OpenerTitle: "First"}
	first, err := a.openReviewLocked(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	req.Opener, req.OpenerTitle = "agt_2", "Second"
	second, err := a.openReviewLocked(t.Context(), h, "ws", "/repo", nv, req)
	require.NoError(t, err)
	assert.Same(t, first, second)
	assert.Equal(t, []reviewOpenArgs{{Cwd: "/repo", Base: "abc1234", Left: "main"}}, nv.opened)
	assert.Equal(t, "agt_2", h.last().Opener)
	assert.Equal(t, "Second", findNodeProp(t, h.view("ws"), "review/header"))
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

func findNodeProp(t *testing.T, n sdui.Node, id string) any {
	t.Helper()
	header, ok := findNode(n, id)
	require.True(t, ok)
	return header.Children[0].Props["meta"]
}
