package main

import (
	"bytes"
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const gitTimeout = 10 * time.Second

func parsePorcelain(top string, out []byte) map[string]string {
	status := map[string]string{}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		x, y := f[0], f[1]
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++
		}
		if code := statusCode(x, y); code != "" {
			status[filepath.Join(top, strings.TrimSuffix(string(f[3:]), "/"))] = code
		}
	}
	return status
}

func statusCode(x, y byte) string {
	switch {
	case x == '?' && y == '?':
		return "U"
	case x == '!':
		return ""
	case x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D'):
		return "U"
	case x == 'A':
		return "A"
	}
	c := y
	if c == ' ' {
		c = x
	}
	switch c {
	case 'M', 'T':
		return "M"
	case 'A', 'C':
		return "A"
	case 'D':
		return "D"
	case 'R':
		return "R"
	}
	return ""
}

func gitStatus(ctx context.Context, repo string) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	top, err := git(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	out, err := git(ctx, repo, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return nil
	}
	return parsePorcelain(strings.TrimSuffix(string(top), "\n"), out)
}

func gitStatusAll(ctx context.Context, repos []string) map[string]string {
	all := map[string]string{}
	for _, repo := range repos {
		maps.Copy(all, gitStatus(ctx, repo))
	}
	return all
}

func git(ctx context.Context, repo string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd.Output()
}
