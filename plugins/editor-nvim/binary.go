package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"clubmate/pkg/pluginsdk"
)

var scrubEnv = []string{"VIM", "VIMRUNTIME"}

type launch struct {
	Path       string
	Args       []string
	Env        []string
	Unset      []string
	PathAppend []string
}

func nvimLaunch() (launch, error) {
	l := launch{
		Env:   []string{"NVIM_APPNAME=" + appName()},
		Unset: slices.Clone(scrubEnv),
	}
	if file := themeFile(); file != "" {
		if _, err := os.Stat(file); err == nil {
			l.Env = append(l.Env, themeFileEnv+"="+file)
		}
	}
	if tools := pluginsdk.ToolsDir(); tools != "" {
		l.PathAppend = []string{tools}
	}
	if l.Path = bundledNvim(); l.Path != "" {
		if distro := distroInit(); distro != "" {
			l.Args = []string{"-u", distro}
		}
		return l, nil
	}
	path, err := exec.LookPath("nvim")
	if err != nil {
		return launch{}, errNvimNotInPATH
	}
	l.Path = path
	return l, nil
}

func appName() string {
	if pluginsdk.Flavor() == "release" {
		return "clubmate"
	}
	return "clubmate-dev"
}

func distroInit() string {
	dir := pluginsdk.ResourcesDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "nvim-distro", "init.lua")
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
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
