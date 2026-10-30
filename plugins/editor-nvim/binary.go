package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"clubmate/pkg/pluginsdk"
)

var scrubEnv = []string{"VIM", "VIMRUNTIME", "NVIM_APPNAME"}

func nvimCommand() (path string, unset, pathAppend []string, err error) {
	if tools := toolsDir(); tools != "" {
		pathAppend = []string{tools}
	}
	if path = bundledNvim(); path != "" {
		return path, slices.Clone(scrubEnv), pathAppend, nil
	}
	if path, err = exec.LookPath("nvim"); err != nil {
		return "", nil, nil, errNvimNotInPATH
	}
	return path, slices.Clone(scrubEnv), pathAppend, nil
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
