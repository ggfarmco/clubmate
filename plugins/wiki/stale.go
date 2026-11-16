package main

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/internal/gitrepo"
)

const staleTimeout = 10 * time.Second

type staleCheck struct {
	blobs   map[string]string
	touched map[string]int64
}

func blobsAt(ctx context.Context, worktree string, sources []string) map[string]string {
	if len(sources) == 0 {
		return nil
	}
	out := make(map[string]string, len(sources))
	for _, src := range sources {
		out[src] = ""
		if worktree == "" {
			continue
		}
		if b, err := gitrepo.Inert(ctx, worktree, "hash-object", "--", src); err == nil {
			out[src] = strings.TrimSpace(string(b))
		}
	}
	return out
}

func newStaleCheck(ctx context.Context, checkout string, metas []Meta) *staleCheck {
	var sources []string
	for _, m := range metas {
		for _, src := range m.Sources {
			if !strings.ContainsAny(src, "\n\r") {
				sources = append(sources, src)
			}
		}
	}
	slices.Sort(sources)
	sources = slices.Compact(sources)
	if len(sources) == 0 {
		return nil
	}
	if _, ok := gitrepo.RepoRoot(checkout); !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, staleTimeout)
	defer cancel()
	rev := defaultRev(ctx, checkout)
	if rev == "" {
		return nil
	}
	blobs, err := blobsOf(ctx, checkout, rev, sources)
	if err != nil {
		logError("stale blobs "+checkout, err)
		return nil
	}
	var changed []string
	since := int64(0)
	for _, m := range metas {
		for _, src := range m.Sources {
			if blobs[src] == m.Blobs[src] {
				continue
			}
			if !slices.Contains(changed, src) {
				changed = append(changed, src)
			}
			if at := m.WrittenAt.Unix(); since == 0 || at < since {
				since = at
			}
		}
	}
	c := &staleCheck{blobs: blobs, touched: map[string]int64{}}
	if len(changed) > 0 {
		c.touched = lastTouched(ctx, checkout, rev, since, changed)
	}
	return c
}

func (c *staleCheck) stale(m Meta) bool {
	if c == nil {
		return false
	}
	for _, src := range m.Sources {
		if c.blobs[src] != m.Blobs[src] && c.touched[src] > m.WrittenAt.Unix() {
			return true
		}
	}
	return false
}

func blobsOf(ctx context.Context, checkout, rev string, sources []string) (map[string]string, error) {
	var in strings.Builder
	for _, src := range sources {
		in.WriteString(rev + ":" + src + "\n")
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-C", checkout, "cat-file", "--batch-check=%(objectname)")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	blobs := make(map[string]string, len(sources))
	for i, src := range sources {
		blobs[src] = ""
		if i < len(lines) && !strings.Contains(lines[i], " ") {
			blobs[src] = lines[i]
		}
	}
	return blobs, nil
}

func lastTouched(ctx context.Context, checkout, rev string, since int64, sources []string) map[string]int64 {
	args := []string{"-c", "core.quotePath=false", "log", "--no-renames", "--format=%x00%ct", "--name-only", "--since=@" + strconv.FormatInt(since, 10), rev, "--"}
	out, err := gitrepo.RunEnv(ctx, checkout, []string{"GIT_LITERAL_PATHSPECS=1"}, append(args, sources...)...)
	touched := map[string]int64{}
	if err != nil {
		logError("stale log "+checkout, err)
		return touched
	}
	for chunk := range strings.SplitSeq(string(out), "\x00") {
		ct, files, _ := strings.Cut(chunk, "\n")
		at, err := strconv.ParseInt(strings.TrimSpace(ct), 10, 64)
		if err != nil {
			continue
		}
		for file := range strings.SplitSeq(files, "\n") {
			if file == "" {
				continue
			}
			for _, src := range sources {
				if (file == src || strings.HasPrefix(file, src+"/")) && at > touched[src] {
					touched[src] = at
				}
			}
		}
	}
	return touched
}

func defaultRev(ctx context.Context, checkout string) string {
	name, err := gitrepo.DefaultBranch(ctx, checkout)
	if err != nil {
		return ""
	}
	if _, err := gitrepo.Run(ctx, checkout, "rev-parse", "-q", "--verify", "refs/heads/"+name); err == nil {
		return "refs/heads/" + name
	}
	base, err := gitrepo.DefaultBase(ctx, checkout)
	if err != nil {
		return ""
	}
	return base
}
