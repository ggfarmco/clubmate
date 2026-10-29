package diagnostics

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	LogTailMax = 2 << 20
	ReadmeName = "README.txt"
	WindowName = "window.png"

	keepArchives = 3
	filePrefix   = "clubmate-diagnostics-"
	fileSuffix   = ".zip"
	timeLayout   = "20060102-150405"
)

type Entry struct {
	Name string
	Data []byte
}

type File struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type Result struct {
	Path  string `json:"path"`
	Files []File `json:"files"`
}

func FileName(t time.Time) string {
	return filePrefix + t.Format(timeLayout) + fileSuffix
}

func LogTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(0, info.Size()-limit)
	data, err := io.ReadAll(io.NewSectionReader(f, start, info.Size()-start))
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, nil
}

func Write(dir string, now time.Time, entries []Entry) (Result, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create %s: %w", dir, err)
	}
	entries = append(slices.Clone(entries), Entry{Name: ReadmeName, Data: readme(entries)})
	tmp, err := os.CreateTemp(dir, ".collect-*"+fileSuffix)
	if err != nil {
		return Result{}, fmt.Errorf("create archive: %w", err)
	}
	defer os.Remove(tmp.Name())
	files, err := writeZip(tmp, now, entries)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Result{}, fmt.Errorf("write archive: %w", err)
	}
	path := filepath.Join(dir, FileName(now))
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Result{}, fmt.Errorf("save archive: %w", err)
	}
	if err := prune(dir); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Files: files}, nil
}

func writeZip(w io.Writer, now time.Time, entries []Entry) ([]File, error) {
	zw := zip.NewWriter(w)
	files := make([]File, 0, len(entries))
	for _, e := range entries {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(e.Data); err != nil {
			return nil, err
		}
		files = append(files, File{Name: e.Name, Bytes: int64(len(e.Data))})
	}
	return files, zw.Close()
}

func prune(dir string) error {
	list, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list %s: %w", dir, err)
	}
	var archives []string
	for _, e := range list {
		if e.Type().IsRegular() && strings.HasPrefix(e.Name(), filePrefix) && strings.HasSuffix(e.Name(), fileSuffix) {
			archives = append(archives, e.Name())
		}
	}
	var errs []error
	for _, name := range archives[:max(0, len(archives)-keepArchives)] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func readme(entries []Entry) []byte {
	var b strings.Builder
	b.WriteString("Clubmate diagnostics\n\nFiles:\n")
	hasWindow := false
	for _, e := range entries {
		fmt.Fprintf(&b, "  %s (%d bytes): %s\n", e.Name, len(e.Data), describe(e.Name))
		hasWindow = hasWindow || e.Name == WindowName
	}
	b.WriteString("  " + ReadmeName + ": this file\n\n")
	b.WriteString("Terminal output, environment variables and plugin storage are not included, nor are the raw databases.\n")
	if !hasWindow {
		b.WriteString(WindowName + " is absent: collected from the CLI, or the app could not capture its window (it never asks for Screen Recording access).\n")
	}
	return []byte(b.String())
}

func describe(name string) string {
	switch {
	case name == "info.json":
		return "versions, macOS, architecture, core uptime, collection time"
	case name == "logs/core.log":
		return "tail of the core log"
	case strings.HasPrefix(name, "logs/plugins/"):
		return "tail of a plugin log"
	case name == "plugins.json":
		return "plugins with state, restarts and last error"
	case name == "workspaces.json":
		return "workspaces with their folders"
	case name == "sessions.json":
		return "sessions without output"
	case strings.HasPrefix(name, "ui/"):
		return "UI slots of a workspace"
	case name == "settings.json":
		return "app settings, secrets redacted"
	case name == WindowName:
		return "the app's own drawing of its key window"
	}
	return ""
}
