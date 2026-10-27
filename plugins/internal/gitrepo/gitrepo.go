package gitrepo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const timeout = 10 * time.Second

type Entry struct {
	Path, Orig string
	X, Y       byte
}

func Run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_CEILING_DIRECTORIES="+filepath.Dir(repo))
	return cmd.Output()
}

func Status(ctx context.Context, repo string, ignored bool) (string, []Entry, error) {
	out, err := Run(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, err
	}
	top := strings.TrimSuffix(string(out), "\n")
	args := []string{"status", "--porcelain=v1", "-z", "--untracked-files=normal"}
	if ignored {
		args = append(args, "--ignored=traditional")
	}
	out, err = Run(ctx, repo, args...)
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
