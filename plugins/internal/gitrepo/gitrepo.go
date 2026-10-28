package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	timeout   = 10 * time.Second
	waitDelay = 2 * time.Second
)

var (
	errLimit         = errors.New("output limit reached")
	ErrNoDefaultBase = errors.New("no default base branch")
	ErrNoMergeBase   = errors.New("no merge base")
	ErrUnknownRef    = errors.New("unknown ref")
)

type Entry struct {
	Path, Orig string
	X, Y       byte
}

func Run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return run(ctx, repo, nil, args...)
}

func Mutate(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return run(ctx, repo, nil, args...)
}

func RunLimited(ctx context.Context, repo string, limit int, args ...string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := &limitWriter{limit: limit}
	var stderr bytes.Buffer
	cmd := command(ctx, repo, args...)
	cmd.Stdout, cmd.Stderr = out, &stderr
	err := cmd.Run()
	if out.full {
		return out.buf, true, nil
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		ee.Stderr = stderr.Bytes()
	}
	return out.buf, false, err
}

func run(ctx context.Context, repo string, stdin []byte, args ...string) ([]byte, error) {
	cmd := command(ctx, repo, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.Output()
}

func command(ctx context.Context, repo string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", "GIT_CEILING_DIRECTORIES="+filepath.Dir(repo))
	cmd.WaitDelay = waitDelay
	return cmd
}

type limitWriter struct {
	buf   []byte
	limit int
	full  bool
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if room := w.limit - len(w.buf); len(p) > room {
		w.buf = append(w.buf, p[:room]...)
		w.full = true
		return room, errLimit
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func Ignored(ctx context.Context, repo string, paths []string) (map[string]bool, error) {
	ignored := map[string]bool{}
	if len(paths) == 0 {
		return ignored, nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(p)
		in.WriteByte(0)
	}
	out, err := run(ctx, repo, in.Bytes(), "check-ignore", "--stdin", "-z")
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 {
			return ignored, nil
		}
		return nil, err
	}
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) > 0 {
			ignored[string(p)] = true
		}
	}
	return ignored, nil
}

func Status(ctx context.Context, repo string) (string, []Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(ctx, repo, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, err
	}
	top := strings.TrimSuffix(string(out), "\n")
	out, err = run(ctx, repo, nil, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return "", nil, err
	}
	return top, parse(top, out), nil
}

func parse(top string, out []byte) []Entry {
	var entries []Entry
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		e := Entry{X: f[0], Y: f[1], Path: abs(top, string(f[3:]))}
		if e.X == 'R' || e.X == 'C' || e.Y == 'R' || e.Y == 'C' {
			i++
			if i < len(fields) {
				e.Orig = abs(top, string(fields[i]))
			}
		}
		entries = append(entries, e)
	}
	return entries
}

func abs(top, rel string) string {
	return filepath.Join(top, strings.TrimSuffix(rel, "/"))
}

func Dirs(ctx context.Context, repo string) (string, string, error) {
	out, err := Run(ctx, repo, "rev-parse", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		return "", "", fmt.Errorf("unexpected rev-parse output: %q", out)
	}
	gitDir, commonDir := lines[0], lines[1]
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repo, commonDir)
	}
	return resolve(gitDir), resolve(commonDir), nil
}

func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

func Branch(ctx context.Context, repo string) (string, error) {
	if out, err := Run(ctx, repo, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	out, err := Run(ctx, repo, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return "(detached " + strings.TrimSpace(string(out)) + ")", nil
}

func DefaultBase(ctx context.Context, repo string) (string, error) {
	if out, err := Run(ctx, repo, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if base := strings.TrimSpace(string(out)); base != "" {
			return base, nil
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := Run(ctx, repo, "rev-parse", "-q", "--verify", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", ErrNoDefaultBase
}

func MergeBase(ctx context.Context, repo, base, head string) (string, error) {
	for _, ref := range []string{base, head} {
		if _, err := Run(ctx, repo, "rev-parse", "-q", "--verify", ref+"^{commit}"); err != nil {
			return "", fmt.Errorf("%w: %s", ErrUnknownRef, ref)
		}
	}
	out, err := Run(ctx, repo, "merge-base", base, head)
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 {
			return "", ErrNoMergeBase
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func Branches(ctx context.Context, repo string) ([]string, error) {
	out, err := Run(ctx, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for _, name := range strings.Split(string(out), "\n") {
		if name == "" || name == "origin/HEAD" || name == "origin" {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

func ChangedSince(ctx context.Context, repo, rev string) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(ctx, repo, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top := strings.TrimSuffix(string(out), "\n")
	out, err = run(ctx, repo, nil, "diff", "--name-status", "-z", "--no-renames", rev, "HEAD", "--")
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i+1 < len(fields); i += 2 {
		if len(fields[i]) == 0 {
			break
		}
		entries = append(entries, Entry{X: fields[i][0], Y: ' ', Path: abs(top, string(fields[i+1]))})
	}
	return entries, nil
}
