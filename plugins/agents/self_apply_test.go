package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const seedEdit = "\n\nClubmate-Self-Kind: edit"

var ctx = context.Background()

func TestApplyCarriesDeclaredCommits(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	declareChange(t, box, "shorter replies", "user trimmed them 3 times")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, "be brief", x.prompt())
	require.Len(t, got.Changes, 1)
	assert.Equal(t, "shorter replies", got.Changes[0].Summary)
}

func TestDeclareClipsALongSummary(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	declareChange(t, box, strings.Repeat("a", maxSummaryRunes+1), "")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", maxSummaryRunes)+"…", got.Changes[0].Summary)
}

func TestApplyDescribesTheChangeInTheRealRepo(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	declareChange(t, box, "shorter replies", "user trimmed them 3 times")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	change := got.Changes[0]
	assert.Equal(t, selfGit(t, x, "rev-parse", "HEAD"), change.ID)
	assert.Equal(t, "edit", change.Kind)
	assert.Equal(t, "user trimmed them 3 times", change.Why)
	assert.Equal(t, "w", change.Workspace)
	assert.False(t, change.At.IsZero())
}

func TestSkillFault(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"no frontmatter", "# Tidy\nkeep it short\n", ""},
		{"name and description", "---\nname: tidy\ndescription: keep it short\n---\nbody\n", ""},
		{"multi-line description", "---\nname: tidy\ndescription: >\n  keep it\n  short\n---\nbody\n", ""},
		{"crlf", "---\r\nname: tidy\r\n---\r\nbody\r\n", ""},
		{"allowed-tools", "---\nname: tidy\nallowed-tools: Bash\n---\nbody\n", dropSkillMeta},
		{"hooks", "---\nhooks:\n  Stop: x\n---\nbody\n", dropSkillMeta},
		{"quoted key", "---\n\"allowed-tools\": Bash\n---\n", dropSkillMeta},
		{"indented top level", "---\n  allowed-tools: Bash\n---\n", dropSkillMeta},
		{"indented after name", "---\nname: tidy\n  allowed-tools: Bash\n---\n", dropSkillMeta},
		{"after blank lines", "\n\n---\nallowed-tools: Bash\n---\n", dropSkillMeta},
		{"form feed before the opener", "\f---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"vertical tab before the opener", "\v---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"nbsp before the opener", "\u00a0---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"bom before the opener", "\ufeff---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"newline then bom", "\n\ufeff---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"space then bom", " \ufeff---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"two boms", "\ufeff\ufeff---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"zero width space", "\u200b---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"bare cr in frontmatter", "---\nname: a\rallowed-tools: Bash\n---\n", dropSkillChars},
		{"bare cr in the body", "# Tidy\rkeep it short\n", dropSkillChars},
		{"cr cr lf", "---\r\r\nname: a\n---\n", dropSkillChars},
		{"nel in the body", "# Tidy\u0085\n", dropSkillChars},
		{"line separator", "# Tidy\u2028\n", dropSkillChars},
		{"paragraph separator", "# Tidy\u2029\n", dropSkillChars},
		{"nbsp in the body", "keep\u00a0it\n", dropSkillChars},
		{"zero width in the body", "keep\u200bit\n", dropSkillChars},
		{"nul", "keep\x00it\n", dropSkillChars},
		{"invalid utf-8", "keep\xffit\n", dropSkillChars},
		{"ideographic space before the opener", "\u3000---\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"em space on the opener", "---\u2003\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"narrow nbsp in the body", "keep\u202fit\n", dropSkillChars},
		{"substitution joins inline shell", "run !$ARGUMENTS`uname -s`\n", dropSkillSubstitution},
		{"substitution joins a shell fence", "```$ARGUMENTS!\nuname -s\n```\n", dropSkillSubstitution},
		{"indexed argument", "first is $ARGUMENTS[0]\n", dropSkillSubstitution},
		{"positional argument", "first is $0\n", dropSkillSubstitution},
		{"braced variable", "at ${CLAUDE_SKILL_DIR}/x\n", dropSkillSubstitution},
		{"underscore variable", "at $_x\n", dropSkillSubstitution},
		{"substitution in frontmatter", "---\ndescription: $ARGUMENTS\n---\n", dropSkillSubstitution},
		{"a bare dollar", "costs 5$ or $ 5, and $.\n", ""},
		{"bang in a fence info", "``` x!y\nid\n```\n", dropSkillShell},
		{"bang after a space in a fence", "``` !\nid\n```\n", dropSkillShell},
		{"tilde fence with a bang", "~~~!\nid\n~~~\n", dropSkillShell},
		{"long tilde fence with a bang", "~~~~~ sh !\nid\n~~~~~\n", dropSkillShell},
		{"indented fence with a bang", "  ````! \nid\n````\n", dropSkillShell},
		{"bang in a plain fence's body", "```\nhello!\n```\n", ""},
		{"bang in text", "Keep it short!\n", ""},
		{"tab in the body", "keep\tit\n", ""},
		{"crlf with a forbidden key", "---\r\nallowed-tools: Bash\r\n---\r\n", dropSkillMeta},
		{"form feed on the opener", "---\f\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"vertical tab on the opener", "---\v\nhooks:\n  Stop: x\n---\n", dropSkillChars},
		{"nbsp on the opener", "---\u00a0\nallowed-tools: Bash\n---\n", dropSkillChars},
		{"space on the opener", "--- \nallowed-tools: Bash\n---\n", dropSkillMeta},
		{"text on the opener", "--- x\nbody\n", dropSkillMeta},
		{"never closed", "---\nname: tidy\n", dropSkillMeta},
		{"inline shell", "Run !`rm -rf x` first\n", dropSkillShell},
		{"shell in frontmatter", "---\ndescription: !`id`\n---\n", dropSkillShell},
		{"shell fence", "```!\nid\n```\n", dropSkillShell},
		{"longer shell fence", "````!\nid\n````\n", dropSkillShell},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, skillFault(tc.text))
		})
	}
}

func TestApplyDropsAnUnsafeSkill(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "skills/a/SKILL.md", "---\nallowed-tools: Bash\n---\n")
	declareChange(t, box, "grant", "")
	edit(t, box, "skills/b/SKILL.md", "Run !`id`\n")
	declareChange(t, box, "run", "")
	edit(t, box, "skills/a/SKILL.md", "plain")
	declareChange(t, box, "harmless on disk", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Equal(t, []string{"grant: " + dropSkillMeta, "run: " + dropSkillShell}, got.Dropped[:2])
	assert.NoFileExists(t, filepath.Join(x.dir, "skills/b/SKILL.md"))
}

func TestApplyKeepsRejectionLines(t *testing.T) {
	for _, line := range []string{selfRejected + "a", selfUndoneFrom + "agt_1: a"} {
		t.Run(line, func(t *testing.T) {
			x, box := selfWithSandbox(t, func(t *testing.T, x *selfStore) {
				appendTo(t, x.dir, "lessons.md", "hypothesis")
				appendTo(t, x.dir, "lessons.md", line)
				require.NoError(t, x.commit(ctx, "lessons"))
			})
			edit(t, box, "lessons.md", "hypothesis\n")
			edit(t, box, "prompt.md", "redo it")
			declareChange(t, box, "erase", "")

			got, err := x.apply(ctx, box)

			require.NoError(t, err)
			assert.Equal(t, []string{"erase: " + dropRejection}, got.Dropped)
			assert.Empty(t, x.prompt())
			assert.Contains(t, read(t, x.dir, "lessons.md"), line)
		})
	}
}

func TestApplyTakesOtherLessonEdits(t *testing.T) {
	x, box := selfWithSandbox(t, func(t *testing.T, x *selfStore) {
		appendTo(t, x.dir, "lessons.md", "hypothesis")
		appendTo(t, x.dir, "lessons.md", selfRejected+"a")
		require.NoError(t, x.commit(ctx, "lessons"))
	})
	edit(t, box, "lessons.md", selfRejected+"a\nnew\n")
	declareChange(t, box, "tidy lessons", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Empty(t, got.Dropped)
}

func TestApplyChecksTheSkillAsItLanded(t *testing.T) {
	const skill = "skills/a/SKILL.md"
	x, box := selfWithSandbox(t)
	edit(t, box, skill, "---\nname: a\n---\nallowed-tools: Bash\n---\nbody\n")
	declareChange(t, box, "c0", "")
	edit(t, box, skill, "Z\n---\nname: a\n---\nallowed-tools: Bash\n---\nbody\n")
	edit(t, box, "hooks.json", "{}")
	declareChange(t, box, "c1", "")
	edit(t, box, skill, "Z\n---\nname: a\nallowed-tools: Bash\n---\nbody\n")
	declareChange(t, box, "c2", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Contains(t, got.Dropped, "c2: "+dropSkillMeta)
	assert.Empty(t, skillFault(read(t, x.dir, skill)))
}

func TestApplyChecksTheSkillARevertRestores(t *testing.T) {
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
	box = sandboxOf(t, x)
	declareRevert(t, box, second.Changes[0].ID)

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	require.Len(t, got.Dropped, 1)
	assert.Contains(t, got.Dropped[0], dropSkillMeta)
	assert.Empty(t, skillFault(read(t, x.dir, skill)))
}

func TestAHandWrittenSkillBlocksNoOtherChange(t *testing.T) {
	x, box := selfWithSandbox(t, func(t *testing.T, x *selfStore) {
		edit(t, x.dir, "skills/mine/SKILL.md", "---\nallowed-tools: Bash\n---\n")
		require.NoError(t, x.commit(ctx, "by hand"))
	})
	edit(t, box, "prompt.md", "be brief")
	declareChange(t, box, "shorter replies", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Empty(t, got.Dropped)
	assert.Equal(t, "be brief", x.prompt())
}

func TestRenameDetectionCantMoveAnEditOntoTheManifest(t *testing.T) {
	x, box := selfWithSandbox(t)
	before := read(t, x.dir, selfManifest)
	edit(t, box, "skills/x/SKILL.md", before)
	require.NoError(t, os.Remove(filepath.Join(box, selfManifest)))
	declareChange(t, box, "c1", "")
	edit(t, box, "skills/x/SKILL.md", strings.Replace(before, `"self"`, `"self","hooks":{"Stop":[]}`, 1))
	declareChange(t, box, "c2", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Contains(t, got.Dropped, "c2: "+dropConflict)
	assert.Equal(t, before, read(t, x.dir, selfManifest))
}

func TestRenameDetectionCantEraseARejection(t *testing.T) {
	x, _ := selfWithSandbox(t, func(t *testing.T, x *selfStore) {
		edit(t, x.dir, selfLessons, "keep\n"+selfRejected+"a\nmore\nlines\nhere\n")
		commitKind(t, x, selfKindEdit, "seed")
	})
	box := sandboxOf(t, x)
	text := read(t, box, selfLessons)
	edit(t, box, "skills/x/SKILL.md", text)
	require.NoError(t, os.Remove(filepath.Join(box, selfLessons)))
	edit(t, box, "hooks.json", "{}")
	declareChange(t, box, "c1", "")
	edit(t, box, "skills/x/SKILL.md", "keep\nmore\nlines\nhere\n")
	declareChange(t, box, "c2", "")

	_, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Contains(t, read(t, x.dir, selfLessons), selfRejected+"a")
}

func TestLandedGateRefusesWhatTheCommitDidNotShow(t *testing.T) {
	x, _ := selfWithSandbox(t, func(t *testing.T, x *selfStore) {
		edit(t, x.dir, selfLessons, selfRejected+"a\n")
		commitKind(t, x, selfKindEdit, "seed")
	})
	before := head(t, x)
	edit(t, x.dir, selfLessons, "")
	edit(t, x.dir, selfManifest, `{"name":"self","hooks":{}}`)
	commitKind(t, x, selfKindEdit, "landed badly")

	reason, err := x.landedFault(ctx, before)

	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf(dropPath, selfManifest), reason)

	edit(t, x.dir, selfManifest, `{"name":"self"}`)
	commitKind(t, x, selfKindEdit, "only lessons")
	reason, err = x.landedFault(ctx, before)
	require.NoError(t, err)
	assert.Equal(t, dropRejection, reason)
}

func TestResetOutlivesAnExpiredCheck(t *testing.T) {
	x, id := selfWithChange(t, "be brief", "shorter replies")
	expired, cancel := context.WithCancel(ctx)
	cancel()

	require.NoError(t, x.resetTo(expired, id+"^"))

	assert.Empty(t, x.prompt())
}

func TestApplyRefusesASubstitution(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "skills/a/SKILL.md", "run !$ARGUMENTS`uname -s`\n")
	declareChange(t, box, "joined", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Equal(t, []string{"joined: " + dropSkillSubstitution}, got.Dropped)
	assert.NoFileExists(t, filepath.Join(x.dir, "skills/a/SKILL.md"))
}

func TestApplyRefusesABareCarriageReturn(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "skills/a/SKILL.md", "---\nname: a\rallowed-tools: Bash\n---\nbody\n")
	declareChange(t, box, "grant", "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Equal(t, []string{"grant: " + dropSkillChars}, got.Dropped)
	assert.NoFileExists(t, filepath.Join(x.dir, "skills/a/SKILL.md"))
}

func TestPathsFaultFailsClosedOnUnreadableLessons(t *testing.T) {
	x, _ := selfWithSandbox(t)
	expired, cancel := context.WithCancel(ctx)
	cancel()

	_, err := x.pathsFault(expired, "HEAD", "HEAD", []selfPath{{file: selfLessons, mode: memoryBlobMode}})

	assert.Error(t, err)
}

func TestLessonsAddedOrDeletedStillCompare(t *testing.T) {
	x, _ := selfWithSandbox(t)
	selfGit(t, x, "rm", "-q", selfLessons)
	require.NoError(t, x.commit(ctx, "no lessons"))
	gone := head(t, x)
	edit(t, x.dir, selfLessons, selfRejected+"a\n")
	require.NoError(t, x.commit(ctx, "lessons"))
	with := head(t, x)
	paths := []selfPath{{file: selfLessons, mode: memoryBlobMode}}

	reason, err := x.pathsFault(ctx, gone, with, paths)
	require.NoError(t, err)
	assert.Empty(t, reason)

	reason, err = x.pathsFault(ctx, with, gone, []selfPath{{file: selfLessons, mode: memoryGoneMode}})
	require.NoError(t, err)
	assert.Equal(t, dropRejection, reason)
}

func TestUndeclaredEditsAreNotApplied(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "stray")
	_, _ = x.apply(ctx, box)
	assert.Empty(t, x.prompt())
}

func TestApplyDropsForbiddenPath(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "hooks.json", "{}")
	declareChange(t, box, "grant", "")
	got, _ := x.apply(ctx, box)
	assert.Empty(t, got.Changes)
	assert.Contains(t, got.Dropped[0], "grant")
}

func TestApplyDropsOversizedPrompt(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", strings.Repeat("x", maxPromptBytes+1))
	declareChange(t, box, "huge", "")
	got, _ := x.apply(ctx, box)
	assert.Empty(t, x.prompt())
	assert.Len(t, got.Dropped, 1)
}

func TestApplyKeepsTwoEditsAndRevertsFirst(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	for _, s := range []string{"one", "two", "three"} {
		appendTo(t, box, "prompt.md", s)
		declareChange(t, box, s, "")
	}
	declareRevert(t, box, seededID(t, x))
	got, _ := x.apply(ctx, box)
	assert.Equal(t, []string{"revert", "edit", "edit"}, kinds(got.Changes))
	assert.Len(t, got.Dropped, 1)
}

func TestApplyDropsWhatACommitMayNotCarry(t *testing.T) {
	tests := []struct {
		name  string
		stage func(t *testing.T, box string)
	}{
		{"attributes", func(t *testing.T, box string) { edit(t, box, ".gitattributes", "* filter=x") }},
		{"a report", func(t *testing.T, box string) { edit(t, box, "reports/d.md", "x") }},
		{"a memory patch", func(t *testing.T, box string) { edit(t, box, "memory-patches/d/w.patch", "x") }},
		{"the manifest", func(t *testing.T, box string) { edit(t, box, ".claude-plugin/plugin.json", "{}") }},
		{"a skill's script", func(t *testing.T, box string) { edit(t, box, "skills/a/run.sh", "x") }},
		{"a skill at the wrong depth", func(t *testing.T, box string) { edit(t, box, "skills/SKILL.md", "x") }},
		{"a hidden skill", func(t *testing.T, box string) { edit(t, box, "skills/.a/SKILL.md", "x") }},
		{"a symlink", func(t *testing.T, box string) {
			require.NoError(t, os.Remove(filepath.Join(box, "prompt.md")))
			require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(box, "prompt.md")))
		}},
		{"an executable", func(t *testing.T, box string) {
			edit(t, box, "skills/a/SKILL.md", "x")
			require.NoError(t, os.Chmod(filepath.Join(box, "skills/a/SKILL.md"), 0o700))
		}},
		{"a submodule", func(t *testing.T, box string) {
			boxGit(t, box, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("1", 40)+",skills/a/SKILL.md")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, box := selfWithSandbox(t)
			before := selfGit(t, x, "rev-parse", "HEAD")
			tt.stage(t, box)
			if tt.name != "a submodule" {
				boxGit(t, box, "add", "-A")
			}
			forge(t, box, "sneak", "Clubmate-Self-Kind: edit")
			got, err := x.apply(ctx, box)
			require.NoError(t, err)
			assert.Empty(t, got.Changes)
			require.Len(t, got.Dropped, 1)
			assert.Contains(t, got.Dropped[0], "sneak")
			assert.Equal(t, before, selfGit(t, x, "rev-parse", "HEAD"))
		})
	}
}

func TestApplyDropsAWholeCommitForOneBadPath(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	edit(t, box, "hooks.json", "{}")
	declareChange(t, box, "mixed", "")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Empty(t, got.Changes)
	assert.Empty(t, x.prompt())
}

func TestApplyDropsTooManySkills(t *testing.T) {
	x, box := selfWithSandbox(t)
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		edit(t, box, "skills/"+name+"/SKILL.md", name)
	}
	declareChange(t, box, "six", "")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, []string{"six: more than 5 skills"}, got.Dropped)
	assert.Empty(t, x.skills())
}

func TestApplyDoesNotCountLessons(t *testing.T) {
	x, box := selfWithSandbox(t)
	appendTo(t, box, "lessons.md", "hypothesis")
	declareChange(t, box, "note", "")
	for _, s := range []string{"one", "two"} {
		appendTo(t, box, "prompt.md", s)
		declareChange(t, box, s, "")
	}
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Len(t, got.Changes, 3)
	assert.Empty(t, got.Dropped)
}

func TestApplyDropsAConflict(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "ours")
	declareChange(t, box, "clash", "")
	edit(t, x.dir, "prompt.md", "theirs")
	require.NoError(t, x.commit(ctx, "moved"))

	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Empty(t, got.Changes)
	assert.Equal(t, "theirs", x.prompt())
	assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
	assert.NoFileExists(t, filepath.Join(x.dir, ".git", "CHERRY_PICK_HEAD"))
}

func TestApplySaysWhyAPickFailed(t *testing.T) {
	logged := filepath.Join(t.TempDir(), "stderr")
	f, err := os.Create(logged)
	require.NoError(t, err)
	defer f.Close()
	stderr := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = stderr }()
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "ours")
	declareChange(t, box, "clash", "")
	edit(t, x.dir, "prompt.md", "theirs")
	require.NoError(t, x.commit(ctx, "moved"))

	_, err = x.apply(ctx, box)

	require.NoError(t, err)
	assert.Contains(t, readBox(t, filepath.Dir(logged), "stderr"), "git cherry-pick")
}

func TestApplyClearsAStaleIndexLock(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	declareChange(t, box, "shorter replies", "")
	lock := filepath.Join(x.dir, ".git", "index.lock")
	write(t, lock, "")

	got, err := x.apply(ctx, box)

	require.NoError(t, err)
	assert.Len(t, got.Changes, 1)
	assert.NoFileExists(t, lock)
}

func TestApplyClearsAnInterruptedPick(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "ours")
	declareChange(t, box, "clash", "")
	edit(t, x.dir, "prompt.md", "theirs")
	require.NoError(t, x.commit(ctx, "moved"))
	selfGit(t, x, "fetch", "-q", box+".git", "HEAD")
	require.Error(t, exec.Command("git", "-C", x.dir, "cherry-pick", "FETCH_HEAD").Run())
	require.FileExists(t, filepath.Join(x.dir, ".git", "CHERRY_PICK_HEAD"))

	_, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, "theirs", x.prompt())
	assert.NoFileExists(t, filepath.Join(x.dir, ".git", "CHERRY_PICK_HEAD"))
}

func TestApplyClearsAnInterruptedRevert(t *testing.T) {
	heads := []string{"CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_HEAD"}
	revert := func(t *testing.T, x *selfStore, rev string) error {
		t.Helper()
		return exec.Command("git", "-C", x.dir, "revert", "--no-commit", rev).Run()
	}
	tests := []struct {
		name  string
		leave func(t *testing.T, x *selfStore)
	}{
		{"conflicted", func(t *testing.T, x *selfStore) {
			require.Error(t, revert(t, x, "HEAD~1"))
			require.Contains(t, x.prompt(), "<<<<<<<")
		}},
		{"clean", func(t *testing.T, x *selfStore) {
			require.NoError(t, revert(t, x, "HEAD"))
		}},
		{"staged with no head", func(t *testing.T, x *selfStore) {
			require.NoError(t, revert(t, x, "HEAD"))
			selfGit(t, x, "update-ref", "-d", "REVERT_HEAD")
		}},
	}
	for _, head := range heads {
		tests = append(tests, struct {
			name  string
			leave func(t *testing.T, x *selfStore)
		}{head + " alone", func(t *testing.T, x *selfStore) {
			edit(t, x.dir, ".git/"+head, selfGit(t, x, "rev-parse", "HEAD")+"\n")
		}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, box := selfWithSandbox(t, seedLine("old"), seedLine("old\nnext"))
			before := selfGit(t, x, "rev-parse", "HEAD")
			tt.leave(t, x)

			_, err := x.apply(ctx, box)
			require.NoError(t, err)
			assert.Equal(t, "old\nnext", x.prompt())
			assert.Equal(t, before, selfGit(t, x, "rev-parse", "HEAD"))
			assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
			for _, head := range heads {
				assert.NoFileExists(t, filepath.Join(x.dir, ".git", head))
			}
		})
	}
}

func TestApplyKeepsEditsMadeByHand(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, x.dir, "prompt.md", "by hand")
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		edit(t, box, "skills/"+name+"/SKILL.md", name)
	}
	declareChange(t, box, "six", "")
	_, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, "by hand", x.prompt())
}

func TestApplyDropsACommitNobodyDeclared(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "stray")
	boxGit(t, box, "add", "-A")
	forge(t, box, "stray")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, []string{"stray: not declared"}, got.Dropped)
	assert.Empty(t, x.prompt())
}

func TestApplyDropsARevertOfNothing(t *testing.T) {
	x, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	boxGit(t, box, "add", "-A")
	forge(t, box, "free edit", "Clubmate-Self-Kind: revert", "Clubmate-Self-Reverts: "+strings.Repeat("1", 40))
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Empty(t, got.Changes)
	assert.Empty(t, x.prompt())
}

func TestApplyRevertIgnoresTheSandboxContent(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	edit(t, box, "prompt.md", "smuggled")
	boxGit(t, box, "add", "-A")
	forge(t, box, "free edit", "Clubmate-Self-Kind: revert", "Clubmate-Self-Reverts: "+seededID(t, x))
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, []string{"revert"}, kinds(got.Changes))
	assert.Equal(t, "free edit", got.Changes[0].Summary)
	assert.Empty(t, x.prompt())
	assert.Empty(t, x.skills())
}

func TestRevertLeavesLessonsAtHead(t *testing.T) {
	tests := []struct {
		name  string
		seeds []func(*testing.T, *selfStore)
	}{
		{"lessons untouched since", []func(*testing.T, *selfStore){seedHypothesis("be brief", "H1")}},
		{"lessons grew next to it", []func(*testing.T, *selfStore){seedHypothesis("be brief", "H1"), seedLesson("H2")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, box := selfWithSandbox(t, tt.seeds...)
			lessons := readBox(t, x.dir, "lessons.md")
			sha := selfGit(t, x, "rev-list", "--max-parents=1", "--grep=hypothesis", "-1", "HEAD")
			declareRevert(t, box, sha)
			assert.Empty(t, readBox(t, box, "prompt.md"))
			assert.Equal(t, lessons, readBox(t, box, "lessons.md"))

			got, err := x.apply(ctx, box)
			require.NoError(t, err)
			assert.Equal(t, []string{"revert"}, kinds(got.Changes))
			assert.Empty(t, x.prompt())
			assert.Equal(t, lessons, readBox(t, x.dir, "lessons.md"))
			assert.Empty(t, selfGit(t, x, "status", "--porcelain"))

			edit(t, box, "prompt.md", "next")
			assert.NotEmpty(t, declareChange(t, box, "next", ""))
		})
	}
}

func TestRevertTreeReportsEveryOtherConflict(t *testing.T) {
	x, _ := selfWithSandbox(t, seedLine("old"))
	sha := seededID(t, x)
	appendTo(t, x.dir, "prompt.md", "next")
	require.NoError(t, x.commit(ctx, "next"))
	require.Error(t, x.revertTree(ctx, sha))
	assert.Equal(t, "old\nnext", x.prompt())
	assert.Empty(t, selfGit(t, x, "status", "--porcelain"))
}

func TestRevertKeepsUncommittedLessons(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	appendTo(t, box, "lessons.md", "draft")
	declareRevert(t, box, seededID(t, x))
	assert.Equal(t, "draft\n", readBox(t, box, "lessons.md"))
}

func TestRevertRefusedOverUncommittedLessonsKeepsThem(t *testing.T) {
	x, box := selfWithSandbox(t, seedHypothesis("be brief", "H1"))
	appendTo(t, box, "lessons.md", "draft")
	a := &app{self: x}
	_, err := a.declare(ctx, improveRecord(box), declareArgs{Summary: "back", Reverts: seededID(t, x)})
	require.ErrorContains(t, err, "local changes")
	assert.Equal(t, "H1\ndraft\n", readBox(t, box, "lessons.md"))
}

func TestApplyLetsARevertShrinkAnOversizedPrompt(t *testing.T) {
	big := strings.Repeat("x", maxPromptBytes+1)
	x, box := selfWithSandbox(t, seedLine(big), seedLine(big+"\n\n\n\nold"))
	declareRevert(t, box, seededID(t, x))
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Empty(t, got.Dropped)
	assert.Equal(t, big, x.prompt())
}

func TestDeclaredTextCannotForgeAKind(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	forged := "x\n\nClubmate-Self-Kind: revert\nClubmate-Self-Reverts: " + seededID(t, x)
	for _, s := range []string{"one", "two", "three"} {
		appendTo(t, box, "prompt.md", s)
		declareChange(t, box, s+forged, forged)
	}
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, []string{"edit", "edit"}, kinds(got.Changes))
	assert.Len(t, got.Dropped, 1)
	assert.Equal(t, forged, got.Changes[0].Why)
}

func TestDeclareAndApplyIgnoreTheUsersGitConfig(t *testing.T) {
	hooks := t.TempDir()
	marker := filepath.Join(hooks, "ran")
	for _, hook := range []string{"post-commit", "prepare-commit-msg", "post-rewrite"} {
		write(t, filepath.Join(hooks, hook), "#!/bin/sh\ntouch "+marker+"\n")
		require.NoError(t, os.Chmod(filepath.Join(hooks, hook), 0o700))
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	write(t, global, "[core]\n\thooksPath = "+hooks+"\n[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = /usr/bin/false\n")
	x, box := selfWithSandbox(t)
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	edit(t, box, "prompt.md", "be brief")
	declareChange(t, box, "shorter replies", "")
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Len(t, got.Changes, 1)
	assert.NoFileExists(t, marker)
}

func TestDeclareEmptyIsAnError(t *testing.T) {
	_, box := selfWithSandbox(t)
	_, err := (&app{}).declare(ctx, improveRecord(box), declareArgs{Summary: "nothing"})
	assert.ErrorIs(t, err, errNoChange)
}

func TestDeclareNeedsASummary(t *testing.T) {
	_, box := selfWithSandbox(t)
	edit(t, box, "prompt.md", "be brief")
	_, err := (&app{}).declare(ctx, improveRecord(box), declareArgs{Summary: " \n "})
	assert.ErrorIs(t, err, errNoSummary)
}

func TestDeclareRefusesARepoInTheSandbox(t *testing.T) {
	_, box := selfWithSandbox(t)
	edit(t, box, "sub/.git/HEAD", strings.Repeat("1", 40))
	_, err := (&app{}).declare(ctx, improveRecord(box), declareArgs{Summary: "nested"})
	assert.ErrorIs(t, err, errSandboxRepo)
}

func TestDeclareRefusesRevertsThatNameNoChange(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	edit(t, box, "prompt.md", "be brief")
	sandboxOnly := declareChange(t, box, "tonight", "")
	unreachable := selfGit(t, x, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "loose", "HEAD^{tree}")
	for _, reverts := range []string{"HEAD", "main", "--all", "HEAD~1", "-n", "abc", strings.Repeat("1", 40), sandboxOnly, unreachable} {
		t.Run(reverts, func(t *testing.T) {
			a := &app{self: x}
			_, err := a.declare(ctx, improveRecord(box), declareArgs{Summary: "bad", Reverts: reverts})
			assert.ErrorIs(t, err, errBadReverts)
		})
	}
}

func TestDeclareRevertsOnlyEditsAndReverts(t *testing.T) {
	for _, kind := range []string{"lesson", "memory", "report", ""} {
		t.Run("kind "+kind, func(t *testing.T) {
			x, box := selfWithSandbox(t, seedKind(kind))
			a := &app{self: x}
			_, err := a.declare(ctx, improveRecord(box), declareArgs{Summary: "bad", Reverts: seededID(t, x)})
			assert.ErrorIs(t, err, errBadReverts)
		})
	}
}

func TestApplyDropsARevertOfALesson(t *testing.T) {
	x, box := selfWithSandbox(t, seedKind("lesson"))
	before := readBox(t, x.dir, "lessons.md")
	forge(t, box, "forget", "Clubmate-Self-Kind: revert", "Clubmate-Self-Reverts: "+seededID(t, x))
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Empty(t, got.Changes)
	assert.Equal(t, before, readBox(t, x.dir, "lessons.md"))
}

func TestDeclareRevertTakesAShortSha(t *testing.T) {
	x, box := selfWithSandbox(t, seedChange("old"))
	declareRevert(t, box, seededID(t, x)[:8])
	assert.NoFileExists(t, filepath.Join(box, "skills", "old", "SKILL.md"))
	got, err := x.apply(ctx, box)
	require.NoError(t, err)
	assert.Equal(t, []string{"revert"}, kinds(got.Changes))
}

func TestFailedRevertLeavesTheSandboxUsable(t *testing.T) {
	x, box := selfWithSandbox(t, seedLine("old"))
	appendTo(t, box, "prompt.md", "next")
	declareChange(t, box, "next", "")
	a := &app{self: x}
	_, err := a.declare(ctx, improveRecord(box), declareArgs{Summary: "back", Reverts: seededID(t, x)})
	require.Error(t, err)
	assert.Equal(t, "old\nnext\n", readBox(t, box, "prompt.md"))

	appendTo(t, box, "lessons.md", "hypothesis")
	assert.NotEmpty(t, declareChange(t, box, "note", ""))
}

func TestDeclareFromSleepIsMemory(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a"})
	write(t, filepath.Join(m.Dir, "memory/0/MEMORY.md"), "A")
	rec := record{Night: nightSleep, Worktree: m.Dir, Workspace: "w"}

	_, err := (&app{}).declare(ctx, rec, declareArgs{Summary: "tidy", Reverts: "abcd"})
	require.ErrorIs(t, err, errSleepReverts)

	_, err = (&app{}).declare(ctx, rec, declareArgs{Summary: "tidy"})
	require.NoError(t, err)
	assert.Contains(t, boxGit(t, m.Dir, "log", "-1", "--format=%B"), "Clubmate-Self-Kind: memory")
	_, err = m.apply(ctx)
	require.NoError(t, err)
	assert.Equal(t, "A", readBox(t, real, "MEMORY.md"))
}

func TestDeclareFromSleepDeletesAFileLeftEmpty(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "old.md": "stale", "kept.md": "b"})
	write(t, filepath.Join(m.Dir, "memory/0/old.md"), " \n")
	rec := record{Night: nightSleep, Worktree: m.Dir, Workspace: "w"}

	_, err := (&app{}).declare(ctx, rec, declareArgs{Summary: "tidy"})
	require.NoError(t, err)
	_, err = m.apply(ctx)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(real, "old.md"))
	assert.Equal(t, "b", readBox(t, real, "kept.md"))
}

func TestDeclareFromSleepKeepsAnEmptyIndex(t *testing.T) {
	real, m := sandboxWith(t, map[string]string{"MEMORY.md": "a", "kept.md": "b"})
	write(t, filepath.Join(m.Dir, "memory/0/MEMORY.md"), "")
	rec := record{Night: nightSleep, Worktree: m.Dir, Workspace: "w"}

	_, err := (&app{}).declare(ctx, rec, declareArgs{Summary: "tidy"})
	require.NoError(t, err)
	_, err = m.apply(ctx)
	require.NoError(t, err)
	assert.Empty(t, readBox(t, real, "MEMORY.md"))
}

func selfWithSandbox(t *testing.T, seeds ...func(*testing.T, *selfStore)) (*selfStore, string) {
	t.Helper()
	x := newSelfStore(t.TempDir())
	require.NoError(t, x.ensure(ctx))
	for _, seed := range seeds {
		seed(t, x)
	}
	return x, sandboxOf(t, x)
}

func sandboxOf(t *testing.T, x *selfStore) string {
	t.Helper()
	box := filepath.Join(t.TempDir(), "box")
	out, err := exec.Command("git", "clone", "-q", "--separate-git-dir="+box+".git", x.dir, box).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.Remove(filepath.Join(box, ".git")))
	return box
}

func seedChange(name string) func(*testing.T, *selfStore) {
	return func(t *testing.T, x *selfStore) {
		t.Helper()
		edit(t, x.dir, "skills/"+name+"/SKILL.md", name)
		require.NoError(t, x.commit(ctx, "seed "+name+seedEdit))
	}
}

func seedLine(line string) func(*testing.T, *selfStore) {
	return func(t *testing.T, x *selfStore) {
		t.Helper()
		edit(t, x.dir, "prompt.md", line+"\n")
		require.NoError(t, x.commit(ctx, "seed"+seedEdit))
	}
}

func seedHypothesis(line, hypothesis string) func(*testing.T, *selfStore) {
	return func(t *testing.T, x *selfStore) {
		t.Helper()
		edit(t, x.dir, "prompt.md", line+"\n")
		edit(t, x.dir, "lessons.md", hypothesis+"\n")
		require.NoError(t, x.commit(ctx, "hypothesis"+seedEdit))
	}
}

func seedLesson(hypothesis string) func(*testing.T, *selfStore) {
	return func(t *testing.T, x *selfStore) {
		t.Helper()
		appendTo(t, x.dir, "lessons.md", hypothesis)
		require.NoError(t, x.commit(ctx, "another"+seedEdit))
	}
}

func seedKind(kind string) func(*testing.T, *selfStore) {
	return func(t *testing.T, x *selfStore) {
		t.Helper()
		appendTo(t, x.dir, "lessons.md", "rejected")
		message := "seed"
		if kind != "" {
			message += "\n\nClubmate-Self-Kind: " + kind
		}
		require.NoError(t, x.commit(ctx, message))
	}
}

func seededID(t *testing.T, x *selfStore) string {
	t.Helper()
	return selfGit(t, x, "rev-parse", "HEAD")
}

func edit(t *testing.T, dir, name, content string) {
	t.Helper()
	write(t, filepath.Join(dir, filepath.FromSlash(name)), content)
}

func appendTo(t *testing.T, dir, name, line string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	edit(t, dir, name, string(data)+line+"\n")
}

func readBox(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(data)
}

func improveRecord(box string) record {
	return record{Night: nightImprove, Worktree: box, Workspace: "w"}
}

func declareChange(t *testing.T, box, summary, why string) string {
	t.Helper()
	sha, err := (&app{}).declare(ctx, improveRecord(box), declareArgs{Summary: summary, Why: why})
	require.NoError(t, err)
	return sha
}

func declareRevert(t *testing.T, box, sha string) {
	t.Helper()
	origin := strings.TrimSpace(boxGit(t, box, "config", "remote.origin.url"))
	a := &app{self: newSelfStore(filepath.Dir(origin))}
	_, err := a.declare(ctx, improveRecord(box), declareArgs{Summary: "back to " + sha, Reverts: sha})
	require.NoError(t, err)
}

func boxGit(t *testing.T, box string, args ...string) string {
	t.Helper()
	out, err := sandboxRunner(box)(ctx, args...)
	require.NoError(t, err)
	return out
}

func forge(t *testing.T, box, summary string, trailers ...string) {
	t.Helper()
	args := []string{"commit", "-q", "--no-verify", "--allow-empty", "-m", summary}
	if len(trailers) > 0 {
		args = append(args, "-m", strings.Join(trailers, "\n"))
	}
	boxGit(t, box, args...)
}

func kinds(changes []selfChange) []string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Kind)
	}
	return out
}
