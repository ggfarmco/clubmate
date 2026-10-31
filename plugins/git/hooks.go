package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	hooksDir       = ".clubmate"
	setupScript    = ".clubmate/setup.sh"
	teardownScript = ".clubmate/teardown.sh"
	includeList    = ".clubmate/worktree-include"
	hooksCommand   = "git.worktree-hooks"
	trustPrefix    = "hooks-trust/"
	separator      = "────"
	reviewBytes    = 64 << 10
	envPrefix      = "CLUBMATE_"
	outputTail     = 4 << 10
	hookWaitDelay  = 5 * time.Second
	cleanupTimeout = time.Minute
)

var errTooLarge = fmt.Errorf("%s/ holds more than %d KiB to review", hooksDir, reviewBytes>>10)

type hooks struct {
	h         *pluginsdk.Host
	projectID string
	primary   string
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if extra := len(b.buf) - outputTail; extra > 0 {
		b.buf = b.buf[extra:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(strings.ToValidUTF8(string(b.buf), ""))
}

func (k hooks) run(ctx context.Context, dir, script string) (string, error) {
	path := filepath.Join(dir, script)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return fmt.Sprintf("Skipped %s: not a regular file", script), nil
	}
	data, err := readAtMost(path, reviewBytes)
	if err != nil {
		return "", err
	}
	if len(data) > reviewBytes {
		return fmt.Sprintf("Skipped %s: %s", script, errTooLarge), nil
	}
	if err := k.trust(ctx, dir, script, data); err != nil {
		return fmt.Sprintf("Skipped %s: %s", script, rpcMessage(err)), nil
	}
	var out tailBuffer
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", string(data), script)
	cmd.Dir = dir
	cmd.Env = hookEnv(os.Environ())
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = hookWaitDelay
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		if tail := out.String(); tail != "" {
			return "", fmt.Errorf("%s failed: %w: %s", script, err, tail)
		}
		return "", fmt.Errorf("%s failed: %w", script, err)
	}
	return "", nil
}

type hookEntry struct {
	rel     string
	mode    fs.FileMode
	content []byte
}

func (k hooks) trust(ctx context.Context, dir, script string, data []byte) error {
	tree, entries, err := readTree(filepath.Join(dir, hooksDir))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	repo := sha256.Sum256([]byte(gitrepo.Canonical(k.primary)))
	bound := sha256.Sum256(slices.Concat([]byte(script), []byte{0}, sum[:], tree))
	key := trustPrefix + hex.EncodeToString(repo[:]) + "/" + hex.EncodeToString(bound[:])
	var trusted bool
	if found, err := k.h.StorageGet(ctx, key, &trusted); err != nil || found && trusted {
		return err
	}
	args := map[string]string{
		"repo":     k.primary,
		"worktree": dir,
		"sha256":   hex.EncodeToString(sum[:]),
		"size":     fmt.Sprintf("%d bytes, %d lines", len(data), bytes.Count(data, []byte("\n"))),
		"trusts":   fmt.Sprintf("all of %s/ (%d entries, shown below)", hooksDir, len(entries)),
	}
	if err := k.h.Approve(ctx, k.projectID, "Run "+script, hooksCommand, args, review(script, data, entries)); err != nil {
		return err
	}
	return k.h.StorageSet(ctx, key, true)
}

func readTree(root string) ([]byte, []hookEntry, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, errors.New(hooksDir + " is not a folder")
	}
	h := sha256.New()
	var entries []hookEntry
	budget := reviewBytes
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		var content []byte
		switch {
		case d.IsDir():
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			content = []byte(target)
		case d.Type().IsRegular():
			if content, err = readAtMost(path, budget); err != nil {
				return err
			}
		}
		if budget -= len(rel) + len(content); budget < 0 {
			return errTooLarge
		}
		if !d.IsDir() {
			entries = append(entries, hookEntry{rel: filepath.Join(hooksDir, rel), mode: d.Type(), content: content})
		}
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00", rel, d.Type(), len(content))
		h.Write(content)
		return nil
	})
	return h.Sum(nil), entries, err
}

func readAtMost(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(n)+1))
}

func review(script string, data []byte, entries []hookEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n%s", separator, script, displayable(string(data), false))
	for _, e := range entries {
		name := displayable(e.rel, true)
		switch {
		case e.rel == script:
		case e.mode&fs.ModeSymlink != 0:
			fmt.Fprintf(&b, "\n%s %s → %s\n", separator, name, displayable(string(e.content), true))
		case e.mode.IsRegular():
			fmt.Fprintf(&b, "\n%s %s\n%s", separator, name, displayable(string(e.content), false))
		default:
			fmt.Fprintf(&b, "\n%s %s (not a regular file)\n", separator, name)
		}
	}
	return b.String()
}

func displayable(text string, name bool) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(text, "\uFFFD") {
		switch {
		case r == '─', name && (r == '\n' || r == '\t' || r == '→'):
			fmt.Fprintf(&b, `\u{%x}`, r)
		case r == '\n' || r == '\t' || !unicode.IsControl(r) && !unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp):
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, `\u{%x}`, r)
		}
	}
	return b.String()
}

func hookEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, envPrefix) {
			out = append(out, kv)
		}
	}
	return out
}

func copyIncluded(src, dst string) error {
	data, err := os.ReadFile(filepath.Join(dst, includeList))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !filepath.IsLocal(line) {
			errs = append(errs, fmt.Errorf("%s: %q is not a path inside the repo", includeList, line))
			continue
		}
		if err := copyFile(src, dst, filepath.Clean(line)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %s: %w", includeList, line, err))
		}
	}
	return errors.Join(append(errs, lines.Err())...)
}

func copyFile(src, dst, rel string) error {
	from := filepath.Join(src, rel)
	info, err := os.Lstat(from)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !gitrepo.Within(gitrepo.Canonical(from), gitrepo.Canonical(src)) {
		return errors.New("not a regular file in the repo")
	}
	to := filepath.Join(dst, rel)
	if !gitrepo.Within(gitrepo.Canonical(filepath.Dir(to)), gitrepo.Canonical(dst)) {
		return errors.New("target folder leaves the worktree")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func removeFailed(ctx context.Context, repo, dir string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	_, err := mutate(ctx, repo, "worktree", "remove", "--force", dir)
	return errors.Join(cause, err)
}

func rpcMessage(err error) string {
	if rpcErr, ok := errors.AsType[*jsonrpc2.Error](err); ok {
		return rpcErr.Message
	}
	return err.Error()
}
