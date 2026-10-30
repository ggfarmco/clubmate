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
)

func Run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return command(ctx, repo, args...).Output()
}

func Mutate(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return command(ctx, repo, args...).Output()
}

func MutateNotice(ctx context.Context, repo string, args ...string) ([]byte, string, error) {
	var stderr bytes.Buffer
	cmd := command(ctx, repo, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		ee.Stderr = stderr.Bytes()
	}
	return out, stderr.String(), err
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

func Remotes(ctx context.Context, repo string) ([]string, error) {
	out, err := Run(ctx, repo, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func ValidBranchName(name string) bool {
	if name == "" || name == "@" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") || strings.ContainsAny(name, " ~^:?*[\\\x7f") {
		return false
	}
	for _, bad := range []string{"..", "@{"} {
		if strings.Contains(name, bad) {
			return false
		}
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func RecentBranches(ctx context.Context, repo string) ([]string, []string, error) {
	out, err := Run(ctx, repo, "for-each-ref", "--format=%(refname)", "--sort=-committerdate", "refs/heads", "refs/remotes")
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
