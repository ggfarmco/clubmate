package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	maxPromptBytes   = 6 << 10
	maxSkills        = 5
	maxEditsPerNight = 2

	selfKindEdit   = "edit"
	selfKindRevert = "revert"
	selfKindMemory = "memory"

	selfKindTrailer      = "Clubmate-Self-Kind"
	selfRevertsTrailer   = "Clubmate-Self-Reverts"
	selfWorkspaceTrailer = "Clubmate-Self-Workspace"

	selfHandEdits = "Keep edits made outside a night"
	gitDirName    = ".git"
	gitNoRenames  = "-Xno-renames"

	dropUndeclared        = "not declared"
	dropReverts           = "reverts nothing in the self"
	dropEdits             = "more than 2 edits tonight"
	dropPrompt            = "prompt over 6 KB"
	dropSkills            = "more than 5 skills"
	dropConflict          = "conflict"
	dropPath              = "touches %q"
	dropSkillMeta         = "a skill's frontmatter may hold only name and description"
	dropSkillShell        = "shell in a skill"
	dropSkillChars        = "control or invisible characters in a skill"
	dropSkillSubstitution = "substitution in a skill"
	dropRejection         = "removes a rejection from lessons.md"

	skillFrontmatter = "---"
	skillInlineShell = "!`"
	skillFenceShell  = "```!"
)

var (
	selfSkillPath     = regexp.MustCompile(`^skills/[A-Za-z0-9][A-Za-z0-9._-]*/SKILL\.md$`)
	selfSha           = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)
	skillSubstitution = regexp.MustCompile(`\$[\p{L}\p{N}_{]`)
	skillBangFence    = regexp.MustCompile("(?m)^[ \t]*(```|~~~)[^\n]*!")
	skillMetaKey      = regexp.MustCompile(`^(name|description):`)
	skillNestedKey    = regexp.MustCompile(`^\s+["']?[A-Za-z0-9_-]+["']?\s*:`)
	revertKinds       = []string{selfKindEdit, selfKindRevert}

	revertAbort = []string{"reset", "-q", "--merge"}

	selfOperationHeads = []string{"CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_HEAD"}

	selfGitConfig = []string{
		"-c", "user.name=Clubmate",
		"-c", "user.email=clubmate@localhost",
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.excludesFile=/dev/null",
		"-c", "core.attributesFile=/dev/null",
		"-c", "core.autocrlf=false",
	}

	errNotNight     = errors.New("only a night session may declare changes")
	errNoSummary    = errors.New("a change needs a summary")
	errNoChange     = errors.New("nothing to declare")
	errBadReverts   = errors.New("reverts must name a change in the self's history")
	errSleepReverts = errors.New("a sleep session has nothing to revert")
	errSandboxRepo  = errors.New("a git repository in the sandbox")
)

type selfChange struct {
	ID        string    `json:"change"`
	Kind      string    `json:"kind"`
	Summary   string    `json:"summary"`
	Why       string    `json:"why"`
	Workspace string    `json:"workspace"`
	At        time.Time `json:"at"`
	Undone    bool      `json:"undone"`
}

type applied struct {
	Changes []selfChange
	Dropped []string
}

type declareArgs struct {
	Summary string `json:"summary"`
	Why     string `json:"why"`
	Reverts string `json:"reverts"`
}

type declareResult struct {
	Change string `json:"change"`
}

type selfCommit struct {
	selfChange
	reverts string
}

type selfPath struct {
	file, mode string
}

type gitRunner func(ctx context.Context, args ...string) (string, error)

func (a *app) selfDeclareCommand(ctx context.Context, cmd pluginsdk.CommandRun, args declareArgs) (any, error) {
	if !cmd.Agent {
		return nil, errNotNight
	}
	s, err := a.lookup(cmd.WorkspaceID, cmd.Caller)
	if err != nil {
		return nil, errNotNight
	}
	rec := s.snapshot()
	if rec.Night == "" {
		return nil, errNotNight
	}
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	change, err := a.declare(ctx, rec, args)
	if err != nil {
		return nil, err
	}
	return declareResult{Change: change}, nil
}

func (a *app) declare(ctx context.Context, rec record, args declareArgs) (string, error) {
	summary := oneLine(args.Summary)
	if summary == "" {
		return "", errNoSummary
	}
	if rec.Night == nightSleep && args.Reverts != "" {
		return "", errSleepReverts
	}
	a.declareMu.Lock()
	defer a.declareMu.Unlock()
	if s := a.session(rec.ID); s != nil && s.snapshot().NightSealed {
		return "", errNightApplied
	}
	if err := refuseRepos(rec.Worktree); err != nil {
		return "", err
	}
	kind, reverts := selfKindEdit, ""
	switch {
	case rec.Night == nightSleep:
		if err := dropEmptyMemory(rec.Worktree); err != nil {
			return "", err
		}
		kind = selfKindMemory
	case args.Reverts != "":
		sha, err := a.self.resolve(ctx, args.Reverts)
		if err != nil {
			return "", err
		}
		kind, reverts = selfKindRevert, sha
	}
	return commitChange(ctx, rec.Worktree, reverts, changeMessage(summary, args.Why, kind, reverts, rec.Workspace))
}

func (x *selfStore) apply(ctx context.Context, sandbox string) (applied, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out applied
	if err := x.settle(ctx); err != nil {
		return out, err
	}
	commits, err := x.declared(ctx, sandbox)
	if err != nil {
		return out, err
	}
	edits := 0
	for _, c := range commits {
		counts, reason, err := x.check(ctx, c, edits)
		if err != nil {
			return out, err
		}
		if reason == "" {
			if reason, err = x.pick(ctx, c); err != nil {
				return out, err
			}
		}
		if reason != "" {
			out.Dropped = append(out.Dropped, c.Summary+": "+reason)
			continue
		}
		landed, err := x.commitAt(ctx, "HEAD")
		if err != nil {
			return out, err
		}
		out.Changes = append(out.Changes, landed.selfChange)
		if counts {
			edits++
		}
	}
	return out, nil
}

func (x *selfStore) settle(ctx context.Context) error {
	if err := x.clearInterrupted(ctx); err != nil {
		return err
	}
	return x.commit(ctx, selfHandEdits)
}

func (x *selfStore) clearInterrupted(ctx context.Context) error {
	interrupted, err := x.interrupted(ctx)
	if err != nil || !interrupted {
		return err
	}
	_, err = x.git(ctx, "reset", "-q", "--hard", "HEAD")
	return err
}

func (x *selfStore) interrupted(ctx context.Context) (bool, error) {
	for _, head := range selfOperationHeads {
		if _, err := x.git(ctx, "rev-parse", "-q", "--verify", head); err == nil {
			return true, nil
		}
	}
	staged, err := x.git(ctx, "diff", "--cached", "--name-only")
	return staged != "", err
}

func (x *selfStore) declared(ctx context.Context, sandbox string) ([]selfCommit, error) {
	source := filepath.Clean(sandbox) + gitDirName
	if _, err := x.git(ctx, "fetch", "-q", "--no-tags", "--no-recurse-submodules", source, "HEAD"); err != nil {
		return nil, err
	}
	base, err := x.git(ctx, "merge-base", "HEAD", "FETCH_HEAD")
	if err != nil {
		return nil, err
	}
	list, err := x.git(ctx, "rev-list", "--reverse", "--topo-order", base+"..FETCH_HEAD")
	if err != nil {
		return nil, err
	}
	shas := strings.Fields(list)
	out := make([]selfCommit, 0, len(shas))
	for _, sha := range shas {
		c, err := x.commitAt(ctx, sha)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(a, b selfCommit) int {
		return cmp.Compare(applyRank(a), applyRank(b))
	})
	return out, nil
}

func (x *selfStore) check(ctx context.Context, c selfCommit, edits int) (bool, string, error) {
	if c.Kind != selfKindEdit && c.Kind != selfKindRevert {
		return false, dropUndeclared, nil
	}
	if c.Kind == selfKindRevert {
		if _, err := x.resolve(ctx, c.reverts); err != nil {
			return false, dropReverts, nil
		}
		return false, "", nil
	}
	if c.reverts != "" {
		return false, dropUndeclared, nil
	}
	paths, err := x.touched(ctx, c.ID)
	if err != nil {
		return false, "", err
	}
	if reason, err := x.pathsFault(ctx, c.ID+"^", c.ID, paths); reason != "" || err != nil {
		return false, reason, err
	}
	counts := slices.ContainsFunc(paths, func(p selfPath) bool { return p.file != selfLessons })
	if counts && edits >= maxEditsPerNight {
		return false, dropEdits, nil
	}
	return counts, "", nil
}

func (x *selfStore) pick(ctx context.Context, c selfCommit) (string, error) {
	before, err := x.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	prompt, skills := len(x.prompt()), len(x.skills())
	reason := dropConflict
	var fault error
	if x.land(ctx, c) == nil {
		reason = x.overLimit(prompt, skills)
		if reason == "" {
			reason, fault = x.landedFault(ctx, before)
		}
	}
	if reason == "" && fault == nil {
		return "", nil
	}
	return reason, errors.Join(fault, x.resetTo(ctx, before))
}

func (x *selfStore) resetTo(ctx context.Context, rev string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), selfTimeout)
	defer cancel()
	_, err := x.git(ctx, "reset", "-q", "--hard", rev)
	return err
}

func (x *selfStore) landedFault(ctx context.Context, before string) (string, error) {
	out, err := x.git(ctx, "diff-tree", "-r", "-z", "--no-renames", before, "HEAD")
	if err != nil {
		return "", err
	}
	return x.pathsFault(ctx, before, "HEAD", parsePaths(out))
}

func (x *selfStore) pathsFault(ctx context.Context, before, after string, paths []selfPath) (string, error) {
	if i := slices.IndexFunc(paths, func(p selfPath) bool { return !p.allowed() }); i >= 0 {
		return fmt.Sprintf(dropPath, paths[i].file), nil
	}
	if reason, err := x.skillsFault(ctx, after, paths); reason != "" || err != nil {
		return reason, err
	}
	if !slices.ContainsFunc(paths, func(p selfPath) bool { return p.file == selfLessons }) {
		return "", nil
	}
	loses, err := x.losesRejection(ctx, before, after)
	if loses {
		return dropRejection, err
	}
	return "", err
}

func (x *selfStore) land(ctx context.Context, c selfCommit) error {
	if c.Kind != selfKindRevert {
		_, err := x.git(ctx, "cherry-pick", gitNoRenames, c.ID)
		return err
	}
	if err := x.revertTree(ctx, c.reverts); err != nil {
		return err
	}
	_, err := x.git(ctx, "commit", "-q", "--no-verify", "--reuse-message="+c.ID)
	return err
}

func (x *selfStore) revertTree(ctx context.Context, sha string) error {
	return revertTree(ctx, x.git, sha)
}

func (x *selfStore) overLimit(promptBefore, skillsBefore int) string {
	prompt, skills := len(x.prompt()), len(x.skills())
	switch {
	case prompt > maxPromptBytes && prompt > promptBefore:
		return dropPrompt
	case skills > maxSkills && skills > skillsBefore:
		return dropSkills
	}
	return ""
}

func (x *selfStore) resolve(ctx context.Context, rev string) (string, error) {
	c, err := x.change(ctx, rev, revertKinds...)
	return c.ID, err
}

func (x *selfStore) change(ctx context.Context, rev string, kinds ...string) (selfCommit, error) {
	if !selfSha.MatchString(rev) {
		return selfCommit{}, fmt.Errorf("%w: %q", errBadReverts, rev)
	}
	sha, err := x.git(ctx, "rev-parse", "-q", "--verify", rev+"^{commit}")
	if err != nil {
		return selfCommit{}, fmt.Errorf("%w: %q", errBadReverts, rev)
	}
	if _, err := x.git(ctx, "merge-base", "--is-ancestor", sha, "HEAD"); err != nil {
		return selfCommit{}, fmt.Errorf("%w: %q", errBadReverts, rev)
	}
	c, err := x.commitAt(ctx, sha)
	if err != nil {
		return selfCommit{}, err
	}
	if !slices.Contains(kinds, c.Kind) {
		return selfCommit{}, fmt.Errorf("%w: %q", errBadReverts, rev)
	}
	return c, nil
}

func (x *selfStore) losesRejection(ctx context.Context, before, after string) (bool, error) {
	was, err := x.lessonsAt(ctx, before)
	if err != nil {
		return false, err
	}
	now, err := x.lessonsAt(ctx, after)
	if err != nil {
		return false, err
	}
	kept := strings.Split(now, "\n")
	for line := range strings.SplitSeq(was, "\n") {
		rejection := strings.HasPrefix(line, selfRejected) || strings.HasPrefix(line, selfUndoneFrom)
		if rejection && !slices.Contains(kept, line) {
			return true, nil
		}
	}
	return false, nil
}

func (x *selfStore) lessonsAt(ctx context.Context, rev string) (string, error) {
	find := []string{"ls-tree", rev, "--", selfLessons}
	if rev == "" {
		find = []string{"ls-files", "--", selfLessons}
	}
	found, err := x.git(ctx, find...)
	if err != nil || found == "" {
		return "", err
	}
	text, err := x.blob(ctx, rev, selfLessons)
	return string(text), err
}

func (x *selfStore) skillsFault(ctx context.Context, sha string, paths []selfPath) (string, error) {
	for _, p := range paths {
		if !selfSkillPath.MatchString(p.file) || p.mode == memoryGoneMode {
			continue
		}
		text, err := x.blob(ctx, sha, p.file)
		if err != nil {
			return "", err
		}
		if reason := skillFault(string(text)); reason != "" {
			return reason, nil
		}
	}
	return "", nil
}

func skillFault(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !utf8.ValidString(text) || strings.ContainsFunc(text, skillForbidden) {
		return dropSkillChars
	}
	if skillSubstitution.MatchString(text) {
		return dropSkillSubstitution
	}
	if strings.Contains(text, skillInlineShell) || strings.Contains(text, skillFenceShell) || skillBangFence.MatchString(text) {
		return dropSkillShell
	}
	lines := strings.Split(strings.TrimLeft(text, " \t\n"), "\n")
	if !strings.HasPrefix(lines[0], skillFrontmatter) {
		return ""
	}
	if lines[0] != skillFrontmatter {
		return dropSkillMeta
	}
	keyed := false
	for _, line := range lines[1:] {
		line = strings.TrimRight(line, " \t")
		switch {
		case line == skillFrontmatter:
			return ""
		case line == "":
		case skillMetaKey.MatchString(line):
			keyed = true
		case !keyed, line[0] != ' ' && line[0] != '\t', skillNestedKey.MatchString(line):
			return dropSkillMeta
		}
	}
	return dropSkillMeta
}

func skillForbidden(r rune) bool {
	if r == ' ' || r == '\n' || r == '\t' {
		return false
	}
	return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

func (x *selfStore) touched(ctx context.Context, sha string) ([]selfPath, error) {
	out, err := x.git(ctx, "diff-tree", "-r", "-z", "--no-renames", "--no-commit-id", "--root", sha)
	if err != nil {
		return nil, err
	}
	return parsePaths(out), nil
}

func parsePaths(raw string) []selfPath {
	var paths []selfPath
	for entry := range slices.Chunk(strings.Split(raw, "\x00"), 2) {
		if len(entry) != 2 {
			break
		}
		_, modes, _ := strings.Cut(entry[0], " ")
		mode, _, _ := strings.Cut(modes, " ")
		paths = append(paths, selfPath{file: entry[1], mode: mode})
	}
	return paths
}

func (x *selfStore) commitAt(ctx context.Context, rev string) (selfCommit, error) {
	out, err := x.git(ctx, "log", "-1", "--format=%H%x00%cI%x00%B", rev)
	if err != nil {
		return selfCommit{}, err
	}
	fields := strings.SplitN(out, "\x00", 3)
	if len(fields) != 3 {
		return selfCommit{}, fmt.Errorf("unexpected commit: %q", rev)
	}
	return parseSelfCommit(fields[0], fields[1], fields[2])
}

func (x *selfStore) git(ctx context.Context, args ...string) (string, error) {
	out, err := gitrepo.Mutate(ctx, x.dir, slices.Concat(selfGitConfig, args)...)
	return gitResult(args, out, err)
}

func (p selfPath) allowed() bool {
	return allowedSelfPath(p.file) && (p.mode == memoryBlobMode || p.mode == memoryGoneMode)
}

func allowedSelfPath(file string) bool {
	return file == selfPrompt || file == selfLessons || selfSkillPath.MatchString(file)
}

func applyRank(c selfCommit) int {
	if c.Kind == selfKindRevert {
		return 0
	}
	return 1
}

func parseSelfCommit(sha, date, message string) (selfCommit, error) {
	at, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return selfCommit{}, err
	}
	c := parseSelfMessage(strings.TrimSpace(message))
	c.ID, c.At = strings.TrimSpace(sha), at
	return c, nil
}

func parseSelfMessage(message string) selfCommit {
	head, tail := message, ""
	if i := strings.LastIndex(message, "\n\n"); i >= 0 {
		head, tail = message[:i], message[i+2:]
	}
	summary, why, _ := strings.Cut(head, "\n\n")
	c := selfCommit{selfChange: selfChange{Summary: oneLine(summary), Why: why}}
	for line := range strings.SplitSeq(tail, "\n") {
		key, value, _ := strings.Cut(line, ": ")
		switch key {
		case selfKindTrailer:
			c.Kind = value
		case selfRevertsTrailer:
			c.reverts = value
		case selfWorkspaceTrailer:
			c.Workspace = value
		}
	}
	return c
}

func changeMessage(summary, why, kind, reverts, workspace string) []string {
	trailers := []string{selfKindTrailer + ": " + kind}
	if reverts != "" {
		trailers = append(trailers, selfRevertsTrailer+": "+reverts)
	}
	trailers = append(trailers, selfWorkspaceTrailer+": "+workspace)
	message := []string{"-m", summary}
	if why = strings.TrimSpace(why); why != "" {
		message = append(message, "-m", why)
	}
	return append(message, "-m", strings.Join(trailers, "\n"))
}

func commitChange(ctx context.Context, dir, reverts string, message []string) (string, error) {
	git := sandboxRunner(dir)
	stage, unstage := func() error {
		_, err := git(ctx, "add", "-A")
		return err
	}, []string{"reset", "-q"}
	if reverts != "" {
		stage, unstage = func() error { return revertTree(ctx, git, reverts) }, revertAbort
	}
	sha, err := commitStaged(ctx, git, stage, message)
	if err != nil {
		_, undo := git(ctx, unstage...)
		return "", errors.Join(err, undo)
	}
	return sha, nil
}

func commitStaged(ctx context.Context, git gitRunner, stage func() error, message []string) (string, error) {
	if err := stage(); err != nil {
		return "", err
	}
	staged, err := git(ctx, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if staged == "" {
		return "", errNoChange
	}
	if _, err := git(ctx, append([]string{"commit", "-q", "--no-verify"}, message...)...); err != nil {
		return "", err
	}
	return git(ctx, "rev-parse", "HEAD")
}

func revertTree(ctx context.Context, git gitRunner, sha string) error {
	err := revertKeepingLessons(ctx, git, sha)
	if err == nil {
		return nil
	}
	_, undo := git(ctx, revertAbort...)
	return errors.Join(err, undo)
}

func revertKeepingLessons(ctx context.Context, git gitRunner, sha string) error {
	_, reverted := git(ctx, "revert", "--no-commit", gitNoRenames, sha)
	unmerged, err := git(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return err
	}
	conflicts := strings.Fields(unmerged)
	if reverted != nil && len(conflicts) == 0 {
		return reverted
	}
	if slices.ContainsFunc(conflicts, func(file string) bool { return file != selfLessons }) {
		return reverted
	}
	touched, err := git(ctx, "diff", "--cached", "--name-only", "--", selfLessons)
	if err != nil || touched == "" {
		return err
	}
	_, err = git(ctx, "checkout", "HEAD", "--", selfLessons)
	return err
}

func sandboxRunner(dir string) gitRunner {
	return func(ctx context.Context, args ...string) (string, error) {
		out, err := sandboxGit(ctx, dir, slices.Concat(selfGitConfig, args)...)
		return gitResult(args, out, err)
	}
}

func gitResult(args []string, out []byte, err error) (string, error) {
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(ee.Stderr)))
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

func refuseRepos(dir string) error {
	return filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.EqualFold(d.Name(), gitDirName) {
			return fmt.Errorf("%w: %s", errSandboxRepo, file)
		}
		return nil
	})
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
