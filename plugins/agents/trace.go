package main

import (
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/internal/buildinfo"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	traceVersion = "0.1"
	traceTool    = "clubmate"
	traceURL     = "clubmate://session/%s#turn=%d"
	traceIDSalt  = "clubmate-trace:"
	traceLimit   = 200

	contributorAI      = "ai"
	contributorHuman   = "human"
	contributorUnknown = "unknown"
)

type traceArgs struct {
	Rev string `json:"rev"`
	Cwd string `json:"cwd"`
}

type traceResult struct {
	Records []traceRecord `json:"records"`
}

type traceRecord struct {
	Version   string       `json:"version"`
	ID        string       `json:"id"`
	Timestamp string       `json:"timestamp"`
	VCS       traceVCS     `json:"vcs"`
	Tool      traceTooling `json:"tool"`
	Files     []traceFile  `json:"files"`
}

type traceVCS struct {
	Type     string `json:"type"`
	Revision string `json:"revision"`
}

type traceTooling struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type traceFile struct {
	Path          string              `json:"path"`
	Conversations []traceConversation `json:"conversations"`
}

type traceConversation struct {
	URL         string           `json:"url,omitempty"`
	Contributor traceContributor `json:"contributor"`
	Ranges      []traceRange     `json:"ranges"`
}

type traceContributor struct {
	Type string `json:"type"`
}

type traceRange struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

type source struct {
	url  string
	kind string
}

type snapshotRef struct {
	sha      string
	parent   string
	tree     string
	worktree string
	session  string
	turn     int
	end      bool
	time     int64
}

func (s snapshotRef) key() string {
	return s.session + "/" + strconv.Itoa(s.turn)
}

func (a *app) traceCommand(ctx context.Context, cmd pluginsdk.CommandRun) (traceResult, error) {
	var args traceArgs
	if err := decode(cmd.Args, &args); err != nil {
		return traceResult{}, err
	}
	dir := a.callerDir(cmd, args.Cwd)
	if !filepath.IsAbs(dir) {
		return traceResult{}, errBlameDir
	}
	root, ok := gitrepo.RepoRoot(dir)
	if !ok {
		return traceResult{}, errNotRepo
	}
	records, err := trace(ctx, root, cmp.Or(args.Rev, "HEAD"))
	return traceResult{Records: records}, err
}

func (a *app) endSnapshot(s *session, rec record, turn int, checkpointSha string) {
	if checkpointSha == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	if _, err := endCheckpoint(ctx, rec.Worktree, rec.ID, turn); err != nil {
		fmt.Fprintln(os.Stderr, "agents: end checkpoint:", err)
	}
	s.mu.Lock()
	deleted := s.deleted
	s.mu.Unlock()
	if deleted {
		if err := deleteCheckpoints(ctx, rec.Worktree, rec.ID); err != nil {
			fmt.Fprintln(os.Stderr, "agents: delete checkpoints:", err)
		}
	}
}

func trace(ctx context.Context, root, rev string) ([]traceRecord, error) {
	commits, err := traceCommits(ctx, root, rev)
	if err != nil {
		return nil, err
	}
	snaps, err := snapshots(ctx, root)
	if err != nil {
		return nil, err
	}
	records := []traceRecord{}
	for _, commit := range commits {
		rec, ok, err := traceCommit(ctx, root, commit, snaps)
		if err != nil {
			return records, err
		}
		if ok {
			records = append(records, rec)
		}
	}
	return records, nil
}

func traceCommits(ctx context.Context, root, rev string) ([]string, error) {
	if strings.Contains(rev, "..") {
		return gitrepo.RevList(ctx, root, rev, traceLimit)
	}
	commit, err := gitrepo.ResolveCommit(ctx, root, rev)
	if err != nil {
		return nil, fmt.Errorf("unknown revision %s", rev)
	}
	return []string{commit}, nil
}

func snapshots(ctx context.Context, root string) ([]snapshotRef, error) {
	refs, err := gitrepo.Refs(ctx, root, checkpointRefs, worktreeTrailer)
	if err != nil {
		return nil, err
	}
	var snaps []snapshotRef
	for _, ref := range refs {
		session, name, ok := strings.Cut(strings.TrimPrefix(ref.Name, checkpointRefs), "/")
		if !ok {
			continue
		}
		name, end := strings.CutSuffix(name, endSuffix)
		turn, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		snaps = append(snaps, snapshotRef{sha: ref.Commit, parent: ref.Parent, tree: ref.Tree, time: ref.Time, worktree: ref.Trailer, session: session, turn: turn, end: end})
	}
	return snaps, nil
}

func traceCommit(ctx context.Context, root, commit string, snaps []snapshotRef) (traceRecord, bool, error) {
	info, err := gitrepo.CommitOf(ctx, root, commit)
	if err != nil {
		return traceRecord{}, false, err
	}
	if info.Tree == "" || len(info.Parents) > 1 {
		return traceRecord{}, false, nil
	}
	parent := ""
	if len(info.Parents) == 1 {
		parent = info.Parents[0]
	}
	tree, date := info.Tree, info.Date
	at, _ := commitAttribution(ctx, root, commit)
	picked := pickSnapshots(snaps, parent, at, root)
	base := parent
	baseTree := parent + "^{tree}"
	if parent == "" {
		if baseTree, err = gitrepo.EmptyTree(ctx, root); err != nil {
			return traceRecord{}, false, err
		}
		base = baseTree
	}
	sources, tip, err := buildChain(ctx, root, baseTree, tree, picked, labels(snaps, picked, at))
	if err != nil {
		return traceRecord{}, false, err
	}
	names, err := gitrepo.ChangedFiles(ctx, root, base, commit)
	if err != nil {
		return traceRecord{}, false, err
	}
	rec := traceRecord{
		Version:   traceVersion,
		ID:        traceID(commit),
		Timestamp: date,
		VCS:       traceVCS{Type: "git", Revision: commit},
		Tool:      traceTooling{Name: traceTool, Version: buildinfo.Version},
		Files:     []traceFile{},
	}
	for _, path := range names {
		file, err := traceFileOf(ctx, root, base, commit, tip, path, sources)
		if err != nil {
			return traceRecord{}, false, err
		}
		if len(file.Conversations) > 0 {
			rec.Files = append(rec.Files, file)
		}
	}
	return rec, true, nil
}

func pickSnapshots(snaps []snapshotRef, parent string, at attribution, cwdTop string) []snapshotRef {
	worktree := ""
	if i := slices.IndexFunc(snaps, func(s snapshotRef) bool { return s.sha == at.Checkpoint && !s.end }); at.Checkpoint != "" && i >= 0 {
		worktree = snaps[i].worktree
	}
	if worktree == "" && at.Session == "" {
		var trees []string
		for _, s := range snaps {
			if s.parent == parent && s.worktree != "" && !slices.Contains(trees, s.worktree) {
				trees = append(trees, s.worktree)
			}
		}
		switch {
		case slices.Contains(trees, cwdTop):
			worktree = cwdTop
		case len(trees) == 1:
			worktree = trees[0]
		}
	}
	var picked []snapshotRef
	for _, s := range snaps {
		forced := at.Checkpoint != "" && s.sha == at.Checkpoint && !s.end
		mine := worktree != "" && s.worktree == worktree || worktree == "" && at.Session != "" && s.session == at.Session
		if forced || s.parent == parent && mine {
			picked = append(picked, s)
		}
	}
	slices.SortFunc(picked, func(x, y snapshotRef) int {
		if x.session == y.session {
			return cmp.Or(cmp.Compare(x.turn, y.turn), cmpBool(x.end, y.end))
		}
		return cmp.Or(cmp.Compare(x.time, y.time), cmp.Compare(x.session, y.session), cmp.Compare(x.turn, y.turn), cmpBool(x.end, y.end))
	})
	return picked
}

func labels(all, picked []snapshotRef, at attribution) []source {
	ended := map[string]bool{}
	for _, s := range all {
		if s.end {
			ended[s.key()] = true
		}
	}
	var open []snapshotRef
	between := func() source {
		switch {
		case len(open) == 0:
			return source{kind: contributorHuman}
		case len(open) == 1 && ended[open[0].key()]:
			return turnSource(open[0].session, open[0].turn)
		}
		return source{kind: contributorUnknown}
	}
	out := make([]source, 0, len(picked)+1)
	for _, s := range picked {
		if s.end {
			out = append(out, turnSource(s.session, s.turn))
			open = slices.DeleteFunc(open, func(o snapshotRef) bool { return o.key() == s.key() })
			continue
		}
		out = append(out, between())
		open = slices.DeleteFunc(open, func(o snapshotRef) bool { return o.session == s.session })
		open = append(open, s)
	}
	switch {
	case at.Session != "":
		out = append(out, turnSource(at.Session, at.Turn))
	case len(picked) == 0:
		out = append(out, source{kind: contributorUnknown})
	default:
		out = append(out, between())
	}
	return out
}

func turnSource(session string, turn int) source {
	return source{url: fmt.Sprintf(traceURL, session, turn), kind: contributorAI}
}

func buildChain(ctx context.Context, root, baseTree, tree string, picked []snapshotRef, steps []source) (map[string]source, string, error) {
	trees := make([]string, 0, len(picked)+1)
	for _, s := range picked {
		trees = append(trees, s.tree)
	}
	trees = append(trees, tree)
	prev, err := gitrepo.CommitTree(ctx, root, baseTree, "", "clubmate trace base")
	if err != nil {
		return nil, "", err
	}
	sources := map[string]source{}
	for i, t := range trees {
		if prev, err = gitrepo.CommitTree(ctx, root, t, prev, "clubmate trace step "+strconv.Itoa(i)); err != nil {
			return nil, "", err
		}
		sources[prev] = steps[i]
	}
	return sources, prev, nil
}

func traceFileOf(ctx context.Context, root, base, commit, tip, path string, sources map[string]source) (traceFile, error) {
	out, err := gitrepo.DiffZero(ctx, root, base, commit, path)
	if err != nil {
		return traceFile{}, err
	}
	var ranges [][2]int
	for start, count := range hunks(out) {
		ranges = append(ranges, [2]int{start, count})
	}
	file := traceFile{Path: path, Conversations: []traceConversation{}}
	if len(ranges) == 0 {
		return file, nil
	}
	blamed, err := gitrepo.BlameLines(ctx, root, tip, path, ranges)
	if err != nil {
		return traceFile{}, fmt.Errorf("git blame: %w", err)
	}
	index := map[source]int{}
	for sha, line := range blameLines(blamed) {
		src, ok := sources[sha]
		if !ok {
			continue
		}
		i, seen := index[src]
		if !seen {
			i = len(file.Conversations)
			index[src] = i
			file.Conversations = append(file.Conversations, traceConversation{URL: src.url, Contributor: traceContributor{Type: src.kind}})
		}
		c := &file.Conversations[i]
		if n := len(c.Ranges); n > 0 && c.Ranges[n-1].EndLine == line-1 {
			c.Ranges[n-1].EndLine = line
			continue
		}
		c.Ranges = append(c.Ranges, traceRange{StartLine: line, EndLine: line})
	}
	return file, nil
}

func hunks(diff string) func(func(int, int) bool) {
	return func(yield func(int, int) bool) {
		for line := range strings.Lines(diff) {
			if !strings.HasPrefix(line, "@@ ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
				continue
			}
			from, n, found := strings.Cut(fields[2][1:], ",")
			start, err := strconv.Atoi(from)
			count := 1
			if found {
				count, err = strconv.Atoi(n)
			}
			if err != nil || count == 0 {
				continue
			}
			if !yield(start, count) {
				return
			}
		}
	}
}

func traceID(commit string) string {
	sum := sha1.Sum([]byte(traceIDSalt + commit))
	sum[6] = sum[6]&0x0f | 0x50
	sum[8] = sum[8]&0x3f | 0x80
	h := hex.EncodeToString(sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func cmpBool(x, y bool) int {
	switch {
	case x == y:
		return 0
	case y:
		return -1
	}
	return 1
}
