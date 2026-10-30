package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	timeout       = 10 * time.Second
	waitDelay     = 2 * time.Second
	maxCountBytes = 1 << 20
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

type Change struct {
	Path      string
	Status    string
	Additions int
	Deletions int
}

func Run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return run(ctx, repo, nil, args...)
}

func Mutate(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return run(ctx, repo, nil, args...)
}

func RunEnv(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
	cmd := command(ctx, repo, args...)
	cmd.Env = append(cmd.Env, env...)
	return cmd.Output()
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
	return Canonical(gitDir), Canonical(commonDir), nil
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

func Branches(ctx context.Context, repo string) ([]string, []string, error) {
	out, err := Run(ctx, repo, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, nil, err
	}
	var local, remote []string
	for name := range strings.SplitSeq(string(out), "\n") {
		if short, ok := strings.CutPrefix(name, "refs/heads/"); ok {
			local = append(local, short)
		} else if short, ok := strings.CutPrefix(name, "refs/remotes/"); ok && !strings.HasSuffix(short, "/HEAD") {
			remote = append(remote, short)
		}
	}
	return local, remote, nil
}

func WorkingChanges(ctx context.Context, repo, rev string) ([]Change, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(ctx, repo, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top := strings.TrimSuffix(string(out), "\n")
	status, err := run(ctx, top, nil, "diff", "--name-status", "-z", "--no-renames", rev, "--")
	if err != nil {
		return nil, err
	}
	stats, err := run(ctx, top, nil, "diff", "--numstat", "-z", "--no-renames", rev, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := run(ctx, top, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	byPath := map[string]*Change{}
	var changes []Change
	fields := bytes.Split(status, []byte{0})
	for i := 0; i+1 < len(fields); i += 2 {
		if len(fields[i]) == 0 {
			break
		}
		changes = append(changes, Change{Path: string(fields[i+1]), Status: string(fields[i][:1])})
	}
	for i := range changes {
		byPath[changes[i].Path] = &changes[i]
	}
	for line := range bytes.SplitSeq(stats, []byte{0}) {
		parts := strings.SplitN(string(line), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if c := byPath[parts[2]]; c != nil {
			c.Additions, _ = strconv.Atoi(parts[0])
			c.Deletions, _ = strconv.Atoi(parts[1])
		}
	}
	for p := range bytes.SplitSeq(untracked, []byte{0}) {
		if len(p) > 0 {
			changes = append(changes, Change{Path: string(p), Status: "?", Additions: countLines(filepath.Join(top, string(p)))})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	return changes, nil
}

func countLines(path string) int {
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		return 0
	case info.Mode()&fs.ModeSymlink != 0:
		return 1
	case !info.Mode().IsRegular():
		return 0
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return 0
	}
	data, _ := io.ReadAll(io.LimitReader(f, maxCountBytes))
	if len(data) == 0 || bytes.IndexByte(data, 0) >= 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
