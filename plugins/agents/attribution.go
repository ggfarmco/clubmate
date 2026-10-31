package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	notesRef         = "refs/notes/clubmate"
	attributionLimit = 200

	noteSession    = "Clubmate-Session"
	noteTurn       = "Clubmate-Turn"
	noteAgent      = "Clubmate-Agent"
	noteTitle      = "Clubmate-Title"
	notePrompt     = "Clubmate-Prompt"
	noteCheckpoint = "Clubmate-Checkpoint"
)

var (
	errBlameTarget = errors.New("pass path or commit")
	errBlameDir    = errors.New("pass cwd (absolute) or call from an agent session")
	errNotRepo     = errors.New("not inside a git repository")
)

type attribution struct {
	Commit     string `json:"commit,omitempty"`
	StartLine  int    `json:"startLine,omitempty"`
	EndLine    int    `json:"endLine,omitempty"`
	Session    string `json:"session"`
	Turn       int    `json:"turn"`
	Agent      string `json:"agent,omitempty"`
	Title      string `json:"title,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
}

type blameArgs struct {
	Path      string `json:"path"`
	Commit    string `json:"commit"`
	Cwd       string `json:"cwd"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

type blameResult struct {
	Attributions []attribution `json:"attributions"`
}

type commitsPayload struct {
	Turn    int      `json:"turn"`
	Commits []string `json:"commits"`
}

func (a *app) blameCommand(ctx context.Context, cmd pluginsdk.CommandRun) (blameResult, error) {
	var args blameArgs
	if err := decode(cmd.Args, &args); err != nil {
		return blameResult{}, err
	}
	if (args.Path == "") == (args.Commit == "") {
		return blameResult{}, errBlameTarget
	}
	dir := a.callerDir(cmd, args.Cwd)
	if args.Path != "" && filepath.IsAbs(args.Path) {
		ranges, err := blame(ctx, args.Path, args.StartLine, args.EndLine)
		return blameResult{Attributions: ranges}, err
	}
	if !filepath.IsAbs(dir) {
		return blameResult{}, errBlameDir
	}
	if args.Path != "" {
		ranges, err := blame(ctx, filepath.Join(dir, args.Path), args.StartLine, args.EndLine)
		return blameResult{Attributions: ranges}, err
	}
	at, err := commitAttribution(ctx, dir, args.Commit)
	if err != nil || at.Session == "" {
		return blameResult{}, err
	}
	return blameResult{Attributions: []attribution{at}}, nil
}

func (a *app) callerDir(cmd pluginsdk.CommandRun, cwd string) string {
	if cwd == "" && cmd.Caller != "" {
		if s, err := a.lookup(cmd.ProjectID, cmd.Caller); err == nil {
			return s.snapshot().Worktree
		}
	}
	return cwd
}

func (a *app) attributeTurn(rec record, turn int, checkpointSha, prompt string) []string {
	if checkpointSha == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	commits, err := attribute(ctx, rec.Worktree, attribution{
		Session:    rec.ID,
		Turn:       turn,
		Agent:      rec.Agent,
		Title:      rec.Title,
		Prompt:     prompt,
		Checkpoint: checkpointSha,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: attribute:", err)
	}
	return commits
}

func attribute(ctx context.Context, root string, at attribution) ([]string, error) {
	head, _ := gitOutput(ctx, root, nil, "rev-parse", "--verify", "-q", "HEAD")
	if head == "" {
		return nil, nil
	}
	args := []string{"rev-list", "--max-count=" + strconv.Itoa(attributionLimit), head}
	if start, _ := gitOutput(ctx, root, nil, "rev-parse", "--verify", "-q", at.Checkpoint+"^"); start != "" {
		args = append(args, "^"+start)
	}
	args = append(args, "--not")
	if branch, _ := gitOutput(ctx, root, nil, "symbolic-ref", "--short", "-q", "HEAD"); branch != "" {
		args = append(args, "--exclude="+branch, "--branches", "--exclude=*/"+branch, "--remotes")
	} else {
		args = append(args, "--branches", "--remotes")
	}
	out, err := gitOutput(ctx, root, nil, args...)
	if err != nil {
		return nil, err
	}
	note := formatNote(at)
	var noted []string
	for commit := range strings.FieldsSeq(out) {
		if _, err := gitOutput(ctx, root, nil, "notes", "--ref", notesRef, "show", commit); err == nil {
			continue
		}
		if _, err := gitOutput(ctx, root, checkpointIdentity, "notes", "--ref", notesRef, "add", "-m", note, commit); err != nil {
			return noted, err
		}
		noted = append(noted, commit)
	}
	return noted, nil
}

func blame(ctx context.Context, path string, startLine, endLine int) ([]attribution, error) {
	root, ok := gitrepo.RepoRoot(filepath.Dir(path))
	if !ok {
		return nil, errNotRepo
	}
	args := []string{"blame", "--porcelain"}
	if startLine > 0 {
		args = append(args, "-L", strconv.Itoa(startLine)+","+lineEnd(endLine))
	}
	out, err := gitrepo.Run(ctx, root, append(args, "--", path)...)
	if err != nil {
		return nil, fmt.Errorf("git blame: %w", err)
	}
	notes := map[string]attribution{}
	var ranges []attribution
	for commit, line := range blameLines(out) {
		at, seen := notes[commit]
		if !seen {
			at, _ = commitAttribution(ctx, root, commit)
			notes[commit] = at
		}
		if at.Session == "" {
			continue
		}
		if n := len(ranges); n > 0 && ranges[n-1].Commit == commit && ranges[n-1].EndLine == line-1 {
			ranges[n-1].EndLine = line
			continue
		}
		at.StartLine, at.EndLine = line, line
		ranges = append(ranges, at)
	}
	return ranges, nil
}

func commitAttribution(ctx context.Context, root, rev string) (attribution, error) {
	commit, err := gitOutput(ctx, root, nil, "rev-parse", "--verify", "-q", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return attribution{}, err
	}
	note, err := gitOutput(ctx, root, nil, "notes", "--ref", notesRef, "show", commit)
	if err != nil {
		return attribution{Commit: commit}, nil
	}
	return parseNote(commit, note), nil
}

func formatNote(at attribution) string {
	var b strings.Builder
	for _, kv := range [][2]string{
		{noteSession, at.Session},
		{noteTurn, strconv.Itoa(at.Turn)},
		{noteAgent, at.Agent},
		{noteTitle, at.Title},
		{notePrompt, titleOf(at.Prompt)},
		{noteCheckpoint, at.Checkpoint},
	} {
		if v := strings.Join(strings.Fields(kv[1]), " "); v != "" {
			b.WriteString(kv[0] + ": " + v + "\n")
		}
	}
	return b.String()
}

func parseNote(commit, note string) attribution {
	at := attribution{Commit: commit}
	for line := range strings.Lines(note) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		switch key {
		case noteSession:
			at.Session = value
		case noteTurn:
			at.Turn, _ = strconv.Atoi(value)
		case noteAgent:
			at.Agent = value
		case noteTitle:
			at.Title = value
		case notePrompt:
			at.Prompt = value
		case noteCheckpoint:
			at.Checkpoint = value
		}
	}
	return at
}

func blameLines(out []byte) func(func(string, int) bool) {
	return func(yield func(string, int) bool) {
		sc := bufio.NewScanner(bytes.NewReader(out))
		sc.Buffer(nil, len(out)+1)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "\t") {
				continue
			}
			fields := strings.Fields(sc.Text())
			if len(fields) < 3 || !isObjectID(fields[0]) || strings.Trim(fields[0], "0") == "" {
				continue
			}
			line, err := strconv.Atoi(fields[2])
			if err != nil {
				continue
			}
			if !yield(fields[0], line) {
				return
			}
		}
	}
}

func isObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

func lineEnd(endLine int) string {
	if endLine <= 0 {
		return ""
	}
	return strconv.Itoa(endLine)
}
