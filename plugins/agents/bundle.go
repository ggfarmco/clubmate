package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	bundleRecord  = "session.json"
	bundleLog     = "log.jsonl"
	bundleRefs    = "refs.bundle"
	exportNotes   = "refs/notes/clubmate-export"
	importedNotes = "refs/notes/clubmate-import"
)

var (
	errExportRunning = errors.New("stop the session's running turn before exporting it")
	errBundleDir     = errors.New("dir must be an absolute path")
	errBadBundle     = errors.New("not a Clubmate session bundle")
)

type exportArgs struct {
	Session string `json:"session"`
	Dir     string `json:"dir"`
}

type exportResult struct {
	Dir string `json:"dir"`
}

type importArgs struct {
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	Note   string `json:"note"`
}

type importResult struct {
	Session  string `json:"session"`
	Imported string `json:"imported"`
	Worktree string `json:"worktree"`
}

func (a *app) export(ctx context.Context, s *session, dir string) error {
	if !filepath.IsAbs(dir) {
		return errBundleDir
	}
	rec := s.snapshot()
	if turnActive(rec) {
		return errExportRunning
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	s.log.flush()
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, bundleRecord), raw, 0o600); err != nil {
		return err
	}
	if err := copyFile(logPath(a.stateDir, rec.ID), filepath.Join(dir, bundleLog)); err != nil {
		return err
	}
	var commits []string
	for _, t := range s.view(0).Turns {
		commits = append(commits, t.Commits...)
	}
	return bundleRefsOf(ctx, rec, commits, filepath.Join(dir, bundleRefs))
}

func bundleRefsOf(ctx context.Context, rec record, commits []string, path string) error {
	root, ok := gitrepo.RepoRoot(rec.Worktree)
	if !ok {
		return nil
	}
	out, err := gitOutput(ctx, root, nil, "for-each-ref", "--format=%(refname)", checkpointRefs+rec.ID+"/")
	if err != nil {
		return err
	}
	refs := strings.Fields(out)
	if len(refs) == 0 {
		return nil
	}
	dropRef(ctx, root, exportNotes)
	defer dropRef(ctx, root, exportNotes)
	noted, err := sessionNotes(ctx, root, commits)
	if err != nil {
		return err
	}
	if noted {
		refs = append(refs, exportNotes)
	}
	args := append([]string{"bundle", "create", "-q", path}, refs...)
	_, err = gitOutput(ctx, root, nil, append(args, "--not", "--branches", "--remotes", "--tags")...)
	return err
}

func sessionNotes(ctx context.Context, root string, commits []string) (bool, error) {
	noted := false
	for _, commit := range commits {
		blob, err := gitOutput(ctx, root, nil, "notes", "--ref", notesRef, "list", commit)
		if err != nil {
			continue
		}
		if _, err := gitOutput(ctx, root, checkpointIdentity, "notes", "--ref", exportNotes, "add", "-f", "-C", blob, commit); err != nil {
			return noted, err
		}
		noted = true
	}
	return noted, nil
}

func (a *app) importSession(ctx context.Context, h *pluginsdk.Host, projectID, from string, args importArgs) (importResult, error) {
	if !filepath.IsAbs(args.Dir) {
		return importResult{}, errBundleDir
	}
	for _, name := range []string{bundleRecord, bundleLog} {
		if info, err := os.Lstat(filepath.Join(args.Dir, name)); err == nil && !info.Mode().IsRegular() {
			return importResult{}, errBadBundle
		}
	}
	var old record
	raw, err := os.ReadFile(filepath.Join(args.Dir, bundleRecord))
	if err != nil {
		return importResult{}, err
	}
	if err := json.Unmarshal(raw, &old); err != nil || !sessionIDPattern.MatchString(old.ID) {
		return importResult{}, errBadBundle
	}
	_, root, err := projectRoot(ctx, h, projectID)
	if err != nil {
		return importResult{}, err
	}
	if parent, err := a.lookup(projectID, from); err == nil {
		root = parent.snapshot().Worktree
	}
	if err := fetchRefs(ctx, root, filepath.Join(args.Dir, bundleRefs), old.ID); err != nil {
		return importResult{}, err
	}
	res, err := a.newSession(ctx, h, projectID, from, newArgs{Agent: old.Agent, Branch: args.Branch, Title: old.Title})
	if err != nil {
		return importResult{}, err
	}
	copied := record{
		ID:        newSessionID(),
		Workspace: projectID,
		Worktree:  res.Worktree,
		Agent:     old.Agent,
		Title:     old.Title,
		TitleSet:  old.TitleSet,
		lineage:   lineage{HandoffTo: res.Session, ImportedFrom: old.ID},
		Status:    statusClosed,
		CreatedAt: old.CreatedAt,
		UpdatedAt: old.UpdatedAt,
		Closed:    true,
	}
	if err := copyFile(filepath.Join(args.Dir, bundleLog), logPath(a.stateDir, copied.ID)); err != nil {
		return importResult{}, err
	}
	imported := a.addSession(copied, false, nil)
	a.notes.mark(imported, changeCreated, true)
	next := a.session(res.Session)
	next.mu.Lock()
	next.rec.HandoffFrom = copied.ID
	next.mu.Unlock()
	next.changed(changeUpdate, true)
	first := promptArgs{Session: res.Session, Text: handoffBrief(imported.view(0), args.Note), From: from}
	if first.Text != "" {
		if err := a.submit(next, &first); err != nil {
			return importResult{}, err
		}
	}
	return importResult{Session: res.Session, Imported: copied.ID, Worktree: res.Worktree}, nil
}

func fetchRefs(ctx context.Context, root, bundle, session string) error {
	if _, err := os.Stat(bundle); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, ok := gitrepo.RepoRoot(root); !ok {
		return nil
	}
	heads, err := gitOutput(ctx, root, nil, "bundle", "list-heads", bundle)
	if err != nil {
		return err
	}
	refspecs := []string{checkpointRefs + session + "/*:" + checkpointRefs + session + "/*"}
	notes := slices.Contains(strings.Fields(heads), exportNotes)
	if notes {
		refspecs = append(refspecs, "+"+exportNotes+":"+importedNotes)
	}
	if _, err := gitOutput(ctx, root, nil, append([]string{"fetch", "-q", "--no-tags", bundle}, refspecs...)...); err != nil {
		return fmt.Errorf("the bundle's checkpoints need commits this repository lacks; fetch the session's branch first: %w", err)
	}
	if !notes {
		return nil
	}
	defer dropRef(ctx, root, importedNotes)
	list, err := gitOutput(ctx, root, nil, "notes", "--ref", importedNotes, "list")
	if err != nil {
		return err
	}
	checkpoints, err := gitOutput(ctx, root, nil, "for-each-ref", "--format=%(objectname)", checkpointRefs+session+"/")
	if err != nil {
		return err
	}
	for line := range strings.Lines(list) {
		blob, commit, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if _, err := gitOutput(ctx, root, nil, "rev-parse", "--verify", "-q", commit+"^{commit}"); err != nil {
			continue
		}
		if _, err := gitOutput(ctx, root, nil, "notes", "--ref", notesRef, "show", commit); err == nil {
			continue
		}
		note, err := gitOutput(ctx, root, nil, "cat-file", "blob", blob)
		if at := parseNote(commit, note); err != nil || at.Session != session || !slices.Contains(strings.Fields(checkpoints), at.Checkpoint) {
			continue
		}
		if _, err := gitOutput(ctx, root, checkpointIdentity, "notes", "--ref", notesRef, "add", "-C", blob, commit); err != nil {
			return err
		}
	}
	return nil
}

func dropRef(ctx context.Context, root, ref string) {
	_, _ = gitOutput(ctx, root, nil, "update-ref", "-d", ref)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(dst, nil, 0o600)
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
