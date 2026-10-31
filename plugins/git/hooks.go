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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	setupScript    = ".clubmate/setup.sh"
	teardownScript = ".clubmate/teardown.sh"
	includeList    = ".clubmate/worktree-include"
	hooksCommand   = "git.worktree-hooks"
	trustPrefix    = "hooks-trust/"
	envPrefix      = "CLUBMATE_"
	outputTail     = 4 << 10
	previewRunes   = 200
	hookWaitDelay  = 5 * time.Second
	cleanupTimeout = time.Minute
)

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
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
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

func (k hooks) trust(ctx context.Context, dir, script string, data []byte) error {
	sum := sha256.Sum256(data)
	repo := sha256.Sum256([]byte(gitrepo.Canonical(k.primary)))
	key := trustPrefix + hex.EncodeToString(repo[:]) + "/" + hex.EncodeToString(sum[:])
	var trusted bool
	if found, err := k.h.StorageGet(ctx, key, &trusted); err != nil || found && trusted {
		return err
	}
	args := map[string]string{"repo": k.primary, "worktree": dir, "sha256": hex.EncodeToString(sum[:]), "script": preview(data), "size": fmt.Sprintf("%d bytes, %d lines", len(data), bytes.Count(data, []byte("\n")))}
	if err := k.h.Approve(ctx, k.projectID, "Run "+script, hooksCommand, args); err != nil {
		return err
	}
	return k.h.StorageSet(ctx, key, true)
}

func preview(data []byte) string {
	lines := strings.Split(strings.TrimSpace(strings.ToValidUTF8(string(data), "")), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	text := []rune(strings.Join(lines, " ↵ "))
	if len(text) > previewRunes {
		return string(text[:previewRunes-1]) + "…"
	}
	return string(text)
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
