package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	timeout       = 10 * time.Second
	fetchTimeout  = 2 * time.Minute
	waitDelay     = 2 * time.Second
	branchTimeout = 2 * time.Second
	originPrefix  = "origin/"
	filterKey     = `filter\..*\.(clean|smudge|process|required)`
	hookKey       = `hook\..*\.command`
)

var (
	errLimit         = errors.New("output limit reached")
	ErrNoDefaultBase = errors.New("no default base branch")
)

func Run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return runWith(ctx, repo, nil, args...)
}

func runWith(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(ctx, repo, args...)
	cmd.Env = append(cmd.Env, env...)
	return cmd.Output()
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

func Inert(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return inertWith(ctx, repo, nil, args...)
}

func inertWith(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(ctx, repo, append([]string{"-c", "log.showSignature=false"}, args...)...)
	cmd.Env = append(cmd.Env, "GIT_NO_LAZY_FETCH=1")
	filters, err := filterOverrides(ctx, repo)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, filters...)
	cmd.Env = append(cmd.Env, env...)
	return cmd.Output()
}

func MutateInert(ctx context.Context, repo string, args ...string) ([]byte, error) {
	cmd := command(ctx, repo, append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Env = append(cmd.Env, "GIT_NO_LAZY_FETCH=1")
	env, err := overrides(ctx, repo, "^("+filterKey+"|"+hookKey+")$")
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, env...)
	return cmd.Output()
}

func filterOverrides(ctx context.Context, repo string) ([]string, error) {
	return overrides(ctx, repo, "^"+filterKey+"$")
}

func overrides(ctx context.Context, repo, keys string) ([]string, error) {
	out, err := Run(ctx, repo, "config", "--null", "--name-only", "--get-regexp", keys)
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	env := make([]string, 0, 2*len(names)+1)
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(names)))
	for i, name := range names {
		key, value := override(name)
		env = append(env, "GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+key, "GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+value)
	}
	return env, nil
}

func override(name string) (string, string) {
	if hook, ok := strings.CutSuffix(name, ".command"); ok {
		return hook + ".enabled", "false"
	}
	if strings.HasSuffix(name, ".required") {
		return name, "false"
	}
	return name, ""
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

func BranchAt(ctx context.Context, dir string) string {
	repo, ok := RepoRoot(dir)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, branchTimeout)
	defer cancel()
	branch, err := Branch(ctx, repo)
	if err != nil {
		return ""
	}
	return branch
}

func DefaultBase(ctx context.Context, repo string) (string, error) {
	return defaultBase(ctx, runWith, repo)
}

func defaultBase(ctx context.Context, git runner, repo string) (string, error) {
	if out, err := git(ctx, repo, nil, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if base := strings.TrimSpace(string(out)); base != "" {
			return base, nil
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := git(ctx, repo, nil, "rev-parse", "-q", "--verify", "refs/heads/"+name); err == nil {
			return name, nil
		}
	}
	return "", ErrNoDefaultBase
}

func DefaultBranch(ctx context.Context, repo string) (string, error) {
	base, err := DefaultBase(ctx, repo)
	return strings.TrimPrefix(base, originPrefix), err
}

func Fetch(ctx context.Context, repo, remote string) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	return unattended(ctx, repo, "-c", "core.hooksPath=/dev/null", "fetch", "--quiet", "--no-auto-maintenance", "--no-recurse-submodules", "--", remote)
}

func Clone(ctx context.Context, url, into string) error {
	return unattended(ctx, string(filepath.Separator), "clone", "--quiet", "--", url, into)
}

func unattended(ctx context.Context, repo string, args ...string) error {
	cmd := command(ctx, repo, args...)
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	if _, err := cmd.Output(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return err
	}
	return nil
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
