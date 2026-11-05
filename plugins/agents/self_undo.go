package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	selfKindLesson   = "lesson"
	selfChangesLimit = 50
	selfPatchesDir   = "memory-patches"
	selfPatchExt     = ".patch"
	selfDirsExt      = ".dirs.json"
	selfRejected     = "Rejected by the user: "
	selfUndoneFrom   = "Undone from session "
	selfLogFormat    = "--format=%H%x00%cI%x00%B%x00"
	selfLogFields    = 3
)

var (
	undoKinds      = []string{selfKindEdit, selfKindRevert, selfKindMemory}
	reversingKinds = []string{selfKindRevert, selfKindLesson}

	errUndoStale  = errors.New("this change no longer applies cleanly")
	errUndoUnsafe = errors.New("this undo would leave an unsafe skill")
	errNightUndo  = errors.New("a night session can't undo a change")
	errNoPatch    = errors.New("no memory patch in this change")
)

type selfChangesResult struct {
	Changes []selfChange `json:"changes"`
}

type selfUndoArgs struct {
	Change string `json:"change"`
}

func (a *app) selfChangesCommand(ctx context.Context, _ pluginsdk.CommandRun) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	list, err := a.self.changes(ctx, selfChangesLimit)
	if err != nil {
		return nil, err
	}
	return selfChangesResult{Changes: list}, nil
}

func (a *app) selfUndoCommand(ctx context.Context, cmd pluginsdk.CommandRun, args selfUndoArgs) (any, error) {
	if s := a.session(cmd.Caller); cmd.Agent && s != nil && s.snapshot().Night != "" {
		return nil, errNightUndo
	}
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	err := a.self.undo(ctx, args.Change, undoMark(cmd))
	a.refreshSettings(ctx)
	return nil, err
}

func undoMark(cmd pluginsdk.CommandRun) string {
	if cmd.Agent {
		return selfUndoneFrom + cmd.Caller + ": "
	}
	return selfRejected
}

func (a *app) undoOffers(ctx context.Context, items []openItem) error {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	var picked []string
	for _, it := range items {
		picked = append(picked, it.Changes...)
	}
	ids, err := a.self.newestFirst(ctx, picked)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := a.self.undo(ctx, id, selfRejected); err != nil {
			return err
		}
	}
	return nil
}

func (x *selfStore) changes(ctx context.Context, limit int) ([]selfChange, error) {
	list, err := x.history(ctx)
	if err != nil {
		return nil, err
	}
	return list[:min(limit, len(list))], nil
}

func (x *selfStore) newestFirst(ctx context.Context, ids []string) ([]string, error) {
	list, err := x.history(ctx)
	if err != nil {
		return nil, err
	}
	at := make(map[string]int, len(list))
	for i, c := range list {
		at[c.ID] = i
	}
	out := slices.Clone(ids)
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(at[a], at[b]) })
	return out, nil
}

func (x *selfStore) undo(ctx context.Context, id, mark string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	c, err := x.change(ctx, id, undoKinds...)
	if err != nil {
		return err
	}
	list, err := x.history(ctx)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(list, func(h selfChange) bool { return h.ID == c.ID }); i >= 0 && list[i].Undone {
		return nil
	}
	if err := x.settle(ctx); err != nil {
		return err
	}
	if err := x.reverse(ctx, c); err != nil {
		if !errors.Is(err, errUndoUnsafe) {
			err = fmt.Errorf("%w: %w", errUndoStale, err)
		}
		return errors.Join(err, x.resetTo(ctx, "HEAD"))
	}
	line := mark + c.Summary
	if err := appendLine(filepath.Join(x.dir, selfLessons), line); err != nil {
		return err
	}
	return x.record(ctx, changeMessage(line, "", selfKindLesson, c.ID, c.Workspace))
}

func (x *selfStore) saveMemoryPatch(ctx context.Context, date, workspace string, patch []byte, dirs map[string]string, summary string) (selfChange, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := x.settle(ctx); err != nil {
		return selfChange{}, err
	}
	raw, err := json.Marshal(dirs)
	if err != nil {
		return selfChange{}, err
	}
	base := filepath.Join(x.dir, selfPatchesDir, date, workspace)
	if err := errors.Join(writeAtomic(base+selfPatchExt, patch), writeAtomic(base+selfDirsExt, raw)); err != nil {
		return selfChange{}, err
	}
	if err := x.record(ctx, changeMessage(summary, "", selfKindMemory, "", workspace)); err != nil {
		return selfChange{}, err
	}
	c, err := x.commitAt(ctx, "HEAD")
	return c.selfChange, err
}

func (x *selfStore) rejectedMemory(ctx context.Context, workspace string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	x.mu.Lock()
	defer x.mu.Unlock()
	list, err := x.history(ctx)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, c := range list {
		if c.Kind != selfKindMemory || !c.Undone || c.Workspace != workspace {
			continue
		}
		patch, _, err := x.memoryPatch(ctx, c.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: rejected memory:", err)
			continue
		}
		parts = append(parts, "## "+c.Summary+"\n\n"+diffBlock(string(patch)))
		if len(parts) == rejectedLimit {
			break
		}
	}
	if len(parts) == 0 {
		return rejectedNone, nil
	}
	return strings.Join(parts, "\n\n") + "\n", nil
}

func (x *selfStore) reverse(ctx context.Context, c selfCommit) error {
	if c.Kind != selfKindMemory {
		if err := x.revertTree(ctx, c.ID); err != nil {
			return err
		}
		return x.stagedFault(ctx)
	}
	patch, dirs, err := x.memoryPatch(ctx, c.ID)
	if err != nil {
		return err
	}
	if err := x.revertTree(ctx, c.ID); err != nil {
		return err
	}
	return reverseMemory(ctx, patch, dirs)
}

func (x *selfStore) stagedFault(ctx context.Context) error {
	out, err := x.git(ctx, "diff-index", "--cached", "-r", "-z", "--no-renames", "HEAD")
	if err != nil {
		return err
	}
	reason, err := x.pathsFault(ctx, "HEAD", "", parsePaths(out))
	if err != nil {
		return err
	}
	if reason != "" {
		return fmt.Errorf("%w: %s", errUndoUnsafe, reason)
	}
	return nil
}

func (x *selfStore) memoryPatch(ctx context.Context, sha string) ([]byte, map[string]string, error) {
	paths, err := x.touched(ctx, sha)
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(paths, func(p selfPath) bool { return strings.HasSuffix(p.file, selfPatchExt) })
	if i < 0 {
		return nil, nil, errNoPatch
	}
	patch, err := x.blob(ctx, sha, paths[i].file)
	if err != nil {
		return nil, nil, err
	}
	raw, err := x.blob(ctx, sha, strings.TrimSuffix(paths[i].file, selfPatchExt)+selfDirsExt)
	if err != nil {
		return nil, nil, err
	}
	var dirs map[string]string
	if err := json.Unmarshal(raw, &dirs); err != nil {
		return nil, nil, err
	}
	return patch, dirs, nil
}

func (x *selfStore) blob(ctx context.Context, sha, file string) ([]byte, error) {
	out, err := gitrepo.Mutate(ctx, x.dir, slices.Concat(selfGitConfig, []string{"cat-file", "blob", sha + ":" + file})...)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	return out, nil
}

func (x *selfStore) history(ctx context.Context) ([]selfChange, error) {
	return x.historyOf(ctx, "HEAD")
}

func (x *selfStore) historyOf(ctx context.Context, revs string) ([]selfChange, error) {
	out, err := x.git(ctx, "log", "--topo-order", selfLogFormat, revs)
	if err != nil {
		return nil, err
	}
	list := []selfChange{}
	reverted := map[string]bool{}
	for entry := range slices.Chunk(strings.Split(out, "\x00"), selfLogFields) {
		if len(entry) != selfLogFields {
			break
		}
		c, err := parseSelfCommit(entry[0], entry[1], entry[2])
		if err != nil {
			return nil, err
		}
		c.Undone = reverted[c.ID]
		if c.reverts != "" && !c.Undone && slices.Contains(reversingKinds, c.Kind) {
			reverted[c.reverts] = true
		}
		if slices.Contains(undoKinds, c.Kind) {
			list = append(list, c.selfChange)
		}
	}
	return list, nil
}

func (x *selfStore) record(ctx context.Context, message []string) error {
	if _, err := x.git(ctx, "add", "-A"); err != nil {
		return err
	}
	_, err := x.git(ctx, slices.Concat([]string{"commit", "-q", "--no-verify", "--allow-empty"}, message)...)
	return err
}

func appendLine(file, line string) error {
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return os.WriteFile(file, append(data, line+"\n"...), 0o600)
}
