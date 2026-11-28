package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/toolgate"
)

const (
	queueEditFormat = "[Clubmate landing queue] %s is in the queued landing of \"%s\" (%s) into %s. An edit here makes it wait for your change and may make it rebase. Prefer to make this change in your own worktree."
	queueToldMax    = 200
	queueToldSep    = "\x00"
)

type editedCall struct {
	id    string
	paths []string
	dir   string
}

func (a *app) queuedRepo(primary string) bool {
	if primary == "" {
		return false
	}
	return slices.ContainsFunc(a.queueList(), func(q landQueue) bool { return q.Repo == primary })
}

func (a *app) queuedAny() bool {
	return a.queues.live.Load()
}

func (a *app) queuedRepos() []string {
	var repos []string
	for _, q := range a.queueList() {
		if q.Repo != "" && !slices.Contains(repos, q.Repo) {
			repos = append(repos, q.Repo)
		}
	}
	return repos
}

func (s *session) editedCallLocked(agentSession, kind string, payload json.RawMessage) *editedCall {
	if s.queuedLand != nil && !s.queuedLand() || agentSession != "" || s.hookDir == "" || isSlotDir(s.hookDir) || !hookable(s.rec) || kind != acp.UpdateToolCall && kind != acp.UpdateToolCallUpdate {
		return nil
	}
	var u struct {
		ID string `json:"toolCallId"`
	}
	if json.Unmarshal(payload, &u) != nil || !toolgate.SafeID(u.ID) {
		return nil
	}
	en := s.toolEntryLocked(agentSession, u.ID)
	if en == nil {
		return nil
	}
	paths := editTargets(en.Tool)
	if len(paths) == 0 {
		return nil
	}
	return &editedCall{id: u.ID, paths: paths, dir: s.hookDir}
}

func editTargets(tool map[string]json.RawMessage) []string {
	f, _, _ := parseTool(tool)
	if f.Status == toolFailed || !slices.Contains(editKinds, f.Kind) {
		return nil
	}
	var out []string
	for _, l := range f.Locations {
		out = append(out, l.Path)
	}
	for _, c := range f.Content {
		if c.Type == "diff" {
			out = append(out, c.Path)
		}
	}
	for _, key := range []string{"file_path", "notebook_path"} {
		if p, ok := f.RawInput[key].(string); ok {
			out = append(out, p)
		}
	}
	out = slices.DeleteFunc(out, func(p string) bool { return p == "" })
	slices.Sort(out)
	return slices.Compact(out)
}

func (a *app) warnEdit(s *session, c editedCall) {
	if !a.queuedAny() {
		return
	}
	ctx := context.Background()
	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	notes, keys := a.queuedFileNotes(ctx, s.snapshot(), c.paths)
	if len(notes) == 0 {
		return
	}
	path := filepath.Join(c.dir, toolHookFindings, c.id+toolgate.QueueSuffix)
	text := strings.Join(notes, "\n\n")
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		text = string(old) + "\n\n" + text
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		logError("write a queue note", err)
		return
	}
	if err := writeAtomic(path, []byte(text)); err != nil {
		logError("write a queue note", err)
		return
	}
	a.markQueueTold(ctx, s, a.queueList(), keys)
}

func (a *app) queuedFileNotes(_ context.Context, rec record, files []string) ([]string, []string) {
	queues := a.queueList()
	if len(queues) == 0 || len(files) == 0 {
		return nil, nil
	}
	told := rec.QueueTold
	var notes, keys []string
	for _, file := range files {
		abs := file
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(rec.Worktree, abs)
		}
		abs = gitrepo.Canonical(abs)
		root, ok := gitrepo.RepoRoot(filepath.Dir(abs))
		if !ok {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, q := range queues {
			if gitrepo.Canonical(q.Target) != root {
				continue
			}
			for _, e := range q.Entries {
				if e.Mode == queueModeDirty || e.Session == rec.ID || !slices.Contains(e.Files, rel) {
					continue
				}
				key := strings.Join([]string{rec.ID, rel, e.ID}, queueToldSep)
				if slices.Contains(told, key) || slices.Contains(keys, key) {
					continue
				}
				keys = append(keys, key)
				notes = append(notes, fmt.Sprintf(queueEditFormat, rel, a.entryTitle(e), entryWhat(e), q.Into))
			}
		}
	}
	return notes, keys
}

func (a *app) markQueueTold(ctx context.Context, s *session, queues []landQueue, keys []string) {
	if len(keys) == 0 {
		return
	}
	live := map[string]bool{}
	for _, q := range queues {
		for _, e := range q.Entries {
			live[e.ID] = true
		}
	}
	s.mu.Lock()
	all := append(slices.Clone(s.rec.QueueTold), keys...)
	all = slices.DeleteFunc(all, func(key string) bool {
		parts := strings.Split(key, queueToldSep)
		return !live[parts[len(parts)-1]]
	})
	if len(all) > queueToldMax {
		all = all[len(all)-queueToldMax:]
	}
	s.rec.QueueTold = all
	s.mu.Unlock()
	a.notes.persist(ctx, s)
}
