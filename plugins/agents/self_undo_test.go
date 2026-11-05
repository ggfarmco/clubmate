package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/pluginsdk"
)

const patchDate = "2026-11-05"

func TestUndoRevertsAndRecordsRejection(t *testing.T) {
	x, id := selfWithChange(t, "be brief", "shorter replies")
	require.NoError(t, x.undo(ctx, id, selfRejected))
	assert.Empty(t, x.prompt())
	assert.Contains(t, read(t, x.dir, "lessons.md"), "Rejected by the user: shorter replies\n")
	list, err := x.changes(ctx, 10)
	require.NoError(t, err)
	assert.True(t, list[0].Undone)
}

func TestUndoNamesWhoUndidIt(t *testing.T) {
	assert.Equal(t, "Rejected by the user: ", undoMark(pluginsdk.CommandRun{}))
	mark := undoMark(pluginsdk.CommandRun{Agent: true, Caller: "agt_1"})
	assert.Equal(t, "Undone from session agt_1: ", mark)
	x, id := selfWithChange(t, "be brief", "shorter replies")
	require.NoError(t, x.undo(ctx, id, mark))
	assert.Contains(t, read(t, x.dir, "lessons.md"), "Undone from session agt_1: shorter replies\n")
}

func TestUndoEitherChangeOfOneNight(t *testing.T) {
	for name, order := range map[string][]int{"newest first": {1, 0}, "oldest first": {0, 1}} {
		t.Run(name, func(t *testing.T) {
			x, ids := selfWithNight(t)
			for _, i := range order {
				require.NoError(t, x.undo(ctx, ids[i], selfRejected))
			}
			assert.Empty(t, x.skills())
			lessons := read(t, x.dir, "lessons.md")
			assert.Contains(t, lessons, "hypothesis a\n")
			assert.Contains(t, lessons, "hypothesis b\n")
			assert.Contains(t, lessons, "Rejected by the user: a\n")
			assert.Contains(t, lessons, "Rejected by the user: b\n")
			list, err := x.changes(ctx, 10)
			require.NoError(t, err)
			assert.Equal(t, []bool{true, true}, undone(list))
		})
	}
}

func TestUndoStaleLeavesEverything(t *testing.T) {
	x, id := selfWithChange(t, "line", "first")
	commitOver(t, x, "prompt.md", "rewritten", "second")
	before := head(t, x)
	assert.ErrorIs(t, x.undo(ctx, id, selfRejected), errUndoStale)
	assert.Equal(t, before, head(t, x))
	assert.Equal(t, "rewritten", x.prompt())
	assert.Empty(t, read(t, x.dir, "lessons.md"))
	assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
	assert.NoFileExists(t, filepath.Join(x.dir, ".git", "REVERT_HEAD"))
}

func TestUndoTwiceRecordsOnce(t *testing.T) {
	x, id := selfWithChange(t, "be brief", "shorter replies")
	require.NoError(t, x.undo(ctx, id, selfRejected))
	before := head(t, x)
	require.NoError(t, x.undo(ctx, id, selfRejected))
	assert.Equal(t, before, head(t, x))
}

func TestUndoOfARevertBringsTheChangeBack(t *testing.T) {
	x, id := selfWithChange(t, "be brief\n", "shorter replies")
	require.NoError(t, x.revertTree(ctx, id))
	require.NoError(t, x.record(ctx, changeMessage("back", "", selfKindRevert, id, "w")))
	back := head(t, x)
	require.Empty(t, x.prompt())

	require.NoError(t, x.undo(ctx, back, selfRejected))

	assert.Equal(t, "be brief", x.prompt())
	list, err := x.changes(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false}, undone(list))
}

func TestStaleUndoKeepsHandEdits(t *testing.T) {
	x, id := selfWithChange(t, "be brief", "shorter replies")
	edit(t, x.dir, "prompt.md", "by hand")
	assert.ErrorIs(t, x.undo(ctx, id, selfRejected), errUndoStale)
	assert.Equal(t, "by hand", x.prompt())
}

func TestUndoClearsAnInterruptedRevert(t *testing.T) {
	x, id := selfWithChange(t, "line", "first")
	commitOver(t, x, "prompt.md", "rewritten", "second")
	require.Error(t, x.gitFails("revert", "--no-commit", id))
	require.FileExists(t, filepath.Join(x.dir, ".git", "REVERT_HEAD"))
	assert.ErrorIs(t, x.undo(ctx, id, selfRejected), errUndoStale)
	assert.Equal(t, "rewritten", x.prompt())
}

func TestUndoMemoryReversesPatch(t *testing.T) {
	x, real, id := selfWithMemoryChange(t, "a", "A")
	require.NoError(t, x.undo(ctx, id, selfRejected))
	assert.Equal(t, "a", read(t, real, "MEMORY.md"))
	assert.Contains(t, read(t, x.dir, "lessons.md"), "Rejected by the user: tidy\n")
	assert.NoFileExists(t, filepath.Join(x.dir, "memory-patches", patchDate, "w.patch"))
}

func TestRejectedMemoryIsPerWorkspace(t *testing.T) {
	x, _, id := selfWithMemoryChange(t, "a", "A")
	got, err := x.rejectedMemory(ctx, "w")
	require.NoError(t, err)
	assert.Equal(t, rejectedNone, got, "not undone yet")

	require.NoError(t, x.undo(ctx, id, selfRejected))

	got, err = x.rejectedMemory(ctx, "w")
	require.NoError(t, err)
	assert.Contains(t, got, "tidy")
	assert.Contains(t, got, "+A")
	other, err := x.rejectedMemory(ctx, "other")
	require.NoError(t, err)
	assert.Equal(t, rejectedNone, other)
}

func TestRejectedMemorySkipsAnUnreadablePatch(t *testing.T) {
	x, _, id := selfWithMemoryChange(t, "a", "A")
	require.NoError(t, x.undo(ctx, id, selfRejected))
	broken := commitKind(t, x, selfKindMemory, "no patch here")
	require.NoError(t, x.record(ctx, changeMessage("undone", "", selfKindLesson, broken, "w")))

	got, err := x.rejectedMemory(ctx, "w")

	require.NoError(t, err)
	assert.Contains(t, got, "## tidy")
	assert.NotContains(t, got, "no patch here")
}

func TestMemoryForNightKeepsRejectedOutOfGit(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	write(t, filepath.Join(real, "MEMORY.md"), "a")
	dir := filepath.Join(t.TempDir(), "box")

	m, err := memoryForNight(ctx, dir, []string{real}, "the day", "undone")

	require.NoError(t, err)
	assert.Equal(t, "undone", read(t, dir, "rejected.md"))
	assert.Equal(t, "the day", read(t, dir, "digest.md"))
	assert.Empty(t, gitOut(t, m, "", "status", "--porcelain"))
}

func selfWithSkillChain(t *testing.T) (*selfStore, selfChange, selfChange) {
	t.Helper()
	const skill = "skills/a/SKILL.md"
	x, box := selfWithSandbox(t)
	edit(t, box, skill, "---\nname: a\n---\nallowed-tools: Bash\n---\nbody\n")
	declareChange(t, box, "c0", "")
	_, err := x.apply(ctx, box)
	require.NoError(t, err)
	box = sandboxOf(t, x)
	edit(t, box, skill, "Z\n---\nname: a\n---\nallowed-tools: Bash\n---\nbody\n")
	declareChange(t, box, "e", "")
	edit(t, box, skill, "Z\n---\nname: a\nallowed-tools: Bash\n---\nbody\n")
	declareChange(t, box, "f", "")
	second, err := x.apply(ctx, box)
	require.NoError(t, err)
	require.Len(t, second.Changes, 2)
	require.Empty(t, second.Dropped)
	return x, second.Changes[0], second.Changes[1]
}

func TestUndoRefusesToLeaveAnUnsafeSkill(t *testing.T) {
	x, e, _ := selfWithSkillChain(t)
	before := read(t, x.dir, "skills/a/SKILL.md")
	lessons := read(t, x.dir, "lessons.md")
	commits := selfGit(t, x, "rev-list", "--count", "HEAD")

	assert.ErrorIs(t, x.undo(ctx, e.ID, selfRejected), errUndoUnsafe)

	assert.Equal(t, before, read(t, x.dir, "skills/a/SKILL.md"))
	assert.Equal(t, lessons, read(t, x.dir, "lessons.md"))
	assert.Equal(t, commits, selfGit(t, x, "rev-list", "--count", "HEAD"))
	assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
}

func TestUndoNewestFirstStillWorks(t *testing.T) {
	x, e, f := selfWithSkillChain(t)

	require.NoError(t, x.undo(ctx, f.ID, selfRejected))
	require.NoError(t, x.undo(ctx, e.ID, selfRejected))

	assert.Empty(t, skillFault(read(t, x.dir, "skills/a/SKILL.md")))
}

func TestUndoMemoryLeavesTheSkillsAlone(t *testing.T) {
	x, real, id := selfWithMemoryChange(t, "a", "A")
	edit(t, x.dir, "skills/mine/SKILL.md", "---\nallowed-tools: Bash\n---\n")
	require.NoError(t, x.commit(ctx, "by hand"))
	paths, err := x.touched(ctx, id)
	require.NoError(t, err)
	for _, p := range paths {
		assert.True(t, strings.HasPrefix(p.file, selfPatchesDir+"/"), p.file)
	}

	require.NoError(t, x.undo(ctx, id, selfRejected))

	assert.Equal(t, "a", read(t, real, "MEMORY.md"))
}

func TestUndoMemoryKeepsThePatchWhole(t *testing.T) {
	x, real, id := selfWithMemoryChange(t, "a\n\n", "A\n\n")
	require.NoError(t, x.undo(ctx, id, selfRejected))
	assert.Equal(t, "a\n\n", read(t, real, "MEMORY.md"))
}

func TestUndoStaleMemoryLeavesEverything(t *testing.T) {
	x, real, id := selfWithMemoryChange(t, "a", "A")
	write(t, filepath.Join(real, "MEMORY.md"), "moved on")
	before := head(t, x)
	assert.ErrorIs(t, x.undo(ctx, id, selfRejected), errUndoStale)
	assert.Equal(t, before, head(t, x))
	assert.Equal(t, "moved on", read(t, real, "MEMORY.md"))
	assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
	assert.FileExists(t, filepath.Join(x.dir, "memory-patches", patchDate, "w.patch"))
}

func TestAnEditCannotMarkAChangeUndone(t *testing.T) {
	x, id := selfWithChange(t, "be brief\n", "shorter replies")
	edit(t, x.dir, "skills/mine/SKILL.md", "mine")
	require.NoError(t, x.record(ctx, changeMessage("forged", "", selfKindEdit, id, "w")))

	list, err := x.changes(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, []bool{false, false}, undone(list))

	require.NoError(t, x.undo(ctx, id, selfRejected))
	assert.Empty(t, x.prompt())
}

func TestApplyDropsAnEditCarryingReverts(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	edit(t, box, "prompt.md", "be brief")
	boxGit(t, box, "add", "-A")
	forge(t, box, "sneak", "Clubmate-Self-Kind: edit", "Clubmate-Self-Reverts: "+seededID(t, x))
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Empty(t, got.Changes)
	assert.Equal(t, []string{"sneak: " + dropUndeclared}, got.Dropped)
	assert.Empty(t, x.prompt())
}

func TestUndoTakesOnlyChanges(t *testing.T) {
	x, id := selfWithChange(t, "be brief", "shorter replies")
	for _, rev := range []string{commitKind(t, x, "lesson", "a lesson"), commitKind(t, x, "report", "a report"), commitKind(t, x, "", "by hand"), "--all", "HEAD", id[:2]} {
		t.Run(rev, func(t *testing.T) {
			assert.ErrorIs(t, x.undo(ctx, rev, selfRejected), errBadReverts)
		})
	}
}

func TestChangesHidesLessonsAndReports(t *testing.T) {
	x, _ := selfWithChange(t, "x", "shown")
	commitKind(t, x, "lesson", "hidden")
	commitKind(t, x, "report", "hidden too")
	list, err := x.changes(ctx, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "shown", list[0].Summary)
}

func TestChangesListNewestFirstUpToLimit(t *testing.T) {
	x, _ := selfWithChange(t, "x", "one")
	commitKind(t, x, "edit", "two")
	commitKind(t, x, "edit", "three")
	list, err := x.changes(ctx, 2)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "three", list[0].Summary)
	assert.Equal(t, "two", list[1].Summary)
}

func TestSaveMemoryPatchIsAChange(t *testing.T) {
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(ctx))
	edit(t, x.dir, "prompt.md", "by hand")
	patch := []byte("diff\n")
	got, err := x.saveMemoryPatch(ctx, patchDate, "w", patch, map[string]string{"0": "/m"}, "tidy")
	require.NoError(t, err)
	assert.Equal(t, head(t, x), got.ID)
	assert.Equal(t, "memory", got.Kind)
	assert.Equal(t, "tidy", got.Summary)
	assert.Equal(t, "w", got.Workspace)
	assert.Equal(t, "diff\n", read(t, x.dir, "memory-patches/"+patchDate+"/w.patch"))
	var dirs map[string]string
	require.NoError(t, json.Unmarshal([]byte(read(t, x.dir, "memory-patches/"+patchDate+"/w.dirs.json")), &dirs))
	assert.Equal(t, map[string]string{"0": "/m"}, dirs)
	assert.NotContains(t, selfGit(t, x, "show", "--name-only", "--format=", "HEAD"), "prompt.md")
}

func TestUndoOffersUndoesEveryChange(t *testing.T) {
	x, first := selfWithChange(t, "be brief", "shorter replies")
	second := commitSkill(t, x, "tidy")
	a := &app{self: x}
	require.NoError(t, a.undoOffers(ctx, []openItem{{Changes: []string{first}}, {Changes: []string{second}}}))
	assert.Empty(t, x.prompt())
	assert.Empty(t, x.skills())
}

func TestUndoOffersGoNewestFirstWhateverThePickOrder(t *testing.T) {
	tests := []struct {
		name  string
		items func(a, b string) []openItem
	}{
		{"apply order", func(a, b string) []openItem { return []openItem{{Changes: []string{a}}, {Changes: []string{b}}} }},
		{"picked backwards", func(a, b string) []openItem { return []openItem{{Changes: []string{b}}, {Changes: []string{a}}} }},
		{"one item", func(a, b string) []openItem { return []openItem{{Changes: []string{a, b}}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, box := selfWithSandbox(t)
			for _, line := range []string{"one", "two"} {
				appendTo(t, box, "prompt.md", line)
				declareChange(t, box, line, "")
			}
			got, err := x.apply(ctx, box)
			require.NoError(t, err)
			require.Len(t, got.Changes, 2)
			a := &app{self: x}
			require.NoError(t, a.undoOffers(ctx, tt.items(got.Changes[0].ID, got.Changes[1].ID)))
			assert.Empty(t, x.prompt())
		})
	}
}

func TestUndoOffersStopsAtStale(t *testing.T) {
	x, id := selfWithChange(t, "line", "first")
	commitOver(t, x, "prompt.md", "rewritten", "second")
	a := &app{self: x}
	assert.ErrorIs(t, a.undoOffers(ctx, []openItem{{Changes: []string{id}}}), errUndoStale)
}

func selfWithChange(t *testing.T, content, summary string) (*selfStore, string) {
	t.Helper()
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", content)
	declareChange(t, box, summary, "")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	require.Len(t, got.Changes, 1)
	return x, got.Changes[0].ID
}

func selfWithNight(t *testing.T) (*selfStore, []string) {
	t.Helper()
	x, box := selfWithSandbox(t)
	for _, name := range []string{"a", "b"} {
		edit(t, box, "skills/"+name+"/SKILL.md", name)
		appendTo(t, box, "lessons.md", "hypothesis "+name)
		declareChange(t, box, name, "")
	}
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	require.Len(t, got.Changes, 2)
	return x, []string{got.Changes[0].ID, got.Changes[1].ID}
}

func selfWithMemoryChange(t *testing.T, before, after string) (*selfStore, string, string) {
	t.Helper()
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(ctx))
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": before})
	editInSandbox(t, m, "MEMORY.md", after)
	declare(t, m)
	patch, err := m.apply(ctx)
	require.NoError(t, err)
	got, err := x.saveMemoryPatch(ctx, patchDate, "w", patch, m.Dirs, "tidy")
	require.NoError(t, err)
	return x, real, got.ID
}

func commitOver(t *testing.T, x *selfStore, name, content, summary string) {
	t.Helper()
	edit(t, x.dir, name, content)
	commitKind(t, x, selfKindEdit, summary)
}

func commitSkill(t *testing.T, x *selfStore, name string) string {
	t.Helper()
	edit(t, x.dir, "skills/"+name+"/SKILL.md", name)
	return commitKind(t, x, selfKindEdit, "skill "+name)
}

func commitKind(t *testing.T, x *selfStore, kind, summary string) string {
	t.Helper()
	require.NoError(t, x.record(ctx, changeMessage(summary, "", kind, "", "w")))
	return head(t, x)
}

func head(t *testing.T, x *selfStore) string {
	t.Helper()
	return selfGit(t, x, "rev-parse", "HEAD")
}

func undone(list []selfChange) []bool {
	out := make([]bool, 0, len(list))
	for _, c := range list {
		out = append(out, c.Undone)
	}
	return out
}

func (x *selfStore) gitFails(args ...string) error {
	_, err := x.git(ctx, args...)
	return err
}
