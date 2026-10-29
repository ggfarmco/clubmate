package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	nvimBinaryKey  = "nvimBinary"
	binaryBundled  = "bundled"
	binarySystem   = "system"
	versionTimeout = 2 * time.Second
)

var (
	errBinaryMode        = errors.New("binary must be bundled or system")
	errSystemNvimMissing = fmt.Errorf("%w — switch to Built-in in Settings → Editor", errNvimNotInPATH)
)

func (a *app) nvimBinary(ctx context.Context, h *pluginsdk.Host) (string, error) {
	var mode string
	if _, err := h.StorageGet(ctx, nvimBinaryKey, &mode); err != nil {
		return "", err
	}
	if mode == binarySystem {
		return binarySystem, nil
	}
	return binaryBundled, nil
}

func (a *app) nvimCommand(ctx context.Context, h *pluginsdk.Host) (path string, env, unset, pathAppend []string, err error) {
	mode, err := a.nvimBinary(ctx, h)
	if err != nil {
		return "", nil, nil, nil, err
	}
	env, unset, err = a.sessionEnv(ctx, h)
	if err != nil {
		return "", nil, nil, nil, err
	}
	if tools := toolsDir(); tools != "" {
		pathAppend = []string{tools}
	}
	if mode == binaryBundled {
		unset = append(unset, "VIM", "VIMRUNTIME")
		if path = bundledNvim(); path != "" {
			return path, env, unset, pathAppend, nil
		}
	}
	path, err = exec.LookPath("nvim")
	if err == nil {
		return path, env, unset, pathAppend, nil
	}
	if mode == binarySystem {
		return "", nil, nil, nil, errSystemNvimMissing
	}
	return "", nil, nil, nil, errNvimNotInPATH
}

func bundledNvim() string {
	dir := pluginsdk.ResourcesDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "nvim", "bin", "nvim")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return path
}

func toolsDir() string {
	dir := pluginsdk.ResourcesDir()
	if dir == "" {
		return ""
	}
	tools := filepath.Join(dir, "tools")
	if info, err := os.Stat(tools); err != nil || !info.IsDir() {
		return ""
	}
	return tools
}

func systemNvim() string {
	path, _ := exec.LookPath("nvim")
	return path
}

func nvimVersion(ctx context.Context, path string) string {
	if path == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	line, _, _ := bytes.Cut(out, []byte("\n"))
	return string(bytes.TrimSpace(line))
}
