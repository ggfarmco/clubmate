package diagnostics

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"clubmate/internal/buildinfo"
	"clubmate/internal/paths"
	"clubmate/internal/sessionhost"
)

const (
	LogTailMax   = 2 << 20
	ReadmeName   = "README.txt"
	InfoName     = "info.json"
	SettingsName = "settings.json"
	WindowName   = "window.png"
	Unreachable  = "unreachable"

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

type Info struct {
	Version           string    `json:"version"`
	Commit            string    `json:"commit"`
	Flavor            string    `json:"flavor"`
	SessionProtocol   int       `json:"sessionProtocol"`
	MacOS             string    `json:"macOS"`
	Arch              string    `json:"arch"`
	CollectedAt       time.Time `json:"collectedAt"`
	Core              string    `json:"core,omitempty"`
	CoreError         string    `json:"coreError,omitempty"`
	CoreVersion       string    `json:"coreVersion,omitempty"`
	CoreUptimeSeconds int64     `json:"coreUptimeSeconds,omitempty"`
	AppVersion        string    `json:"appVersion,omitempty"`
}

func NewInfo(now time.Time) Info {
	macOS, _ := unix.Sysctl("kern.osproductversion")
	return Info{
		Version:         buildinfo.Version,
		Commit:          buildinfo.Commit,
		Flavor:          buildinfo.Flavor,
		SessionProtocol: sessionhost.Protocol,
		MacOS:           macOS,
		Arch:            runtime.GOARCH,
		CollectedAt:     now.UTC(),
	}
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

func Logs(dirs paths.Dirs) ([]Entry, error) {
	plugins, err := filepath.Glob(filepath.Join(dirs.PluginsDir(), "*.log"))
	if err != nil {
		return nil, err
	}
	sources := [][2]string{{"logs/core.log", dirs.CoreLog()}}
	for _, path := range plugins {
		sources = append(sources, [2]string{"logs/plugins/" + filepath.Base(path), path})
	}
	var entries []Entry
	for _, src := range sources {
		data, err := LogTail(src[1], LogTailMax)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", src[0], err)
		}
		entries = append(entries, Entry{Name: src[0], Data: data})
	}
	return entries, nil
}

func Write(dir string, now time.Time, entries []Entry) (Result, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := createUnique(dir, now)
	if err != nil {
		return Result{}, err
	}
	files, err := writeZip(f, now, withReadme(entries, nil))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(f.Name())
		return Result{}, fmt.Errorf("write archive: %w", err)
	}
	if err := prune(dir); err != nil {
		return Result{}, err
	}
	return Result{Path: f.Name(), Files: files}, nil
}

func Save(path string, now time.Time, entries []Entry, notes ...string) ([]File, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".collect-*"+fileSuffix)
	if err != nil {
		return nil, fmt.Errorf("create archive: %w", err)
	}
	defer os.Remove(tmp.Name())
	files, err := writeZip(tmp, now, withReadme(entries, notes))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("write archive: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return nil, fmt.Errorf("save archive: %w", err)
	}
	return files, nil
}

func createUnique(dir string, now time.Time) (*os.File, error) {
	base := strings.TrimSuffix(FileName(now), fileSuffix)
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name += "-" + strconv.Itoa(i)
		}
		f, err := os.OpenFile(filepath.Join(dir, name+fileSuffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("create archive: %w", err)
		}
		return f, nil
	}
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
	slices.SortFunc(archives, func(a, b string) int {
		return strings.Compare(strings.TrimSuffix(a, fileSuffix), strings.TrimSuffix(b, fileSuffix))
	})
	var errs []error
	for _, name := range archives[:max(0, len(archives)-keepArchives)] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func withReadme(entries []Entry, notes []string) []Entry {
	return append(slices.Clone(entries), Entry{Name: ReadmeName, Data: readme(entries, notes)})
}

func readme(entries []Entry, notes []string) []byte {
	var b strings.Builder
	b.WriteString("Clubmate diagnostics\n\nFiles:\n")
	hasWindow := false
	for _, e := range entries {
		fmt.Fprintf(&b, "  %s (%d bytes): %s\n", e.Name, len(e.Data), describe(e.Name))
		hasWindow = hasWindow || e.Name == WindowName
	}
	b.WriteString("  " + ReadmeName + ": this file\n\n")
	b.WriteString("Terminal output, environment variables and plugin storage are not included, nor are the raw databases.\n")
	if hasWindow {
		b.WriteString(WindowName + " may show terminal and editor contents.\n")
	} else {
		b.WriteString(WindowName + " is absent: collected from the CLI, the screenshot was not chosen, or the window could not be captured (the app never asks for Screen Recording access).\n")
	}
	for _, note := range notes {
		b.WriteString(note + "\n")
	}
	return []byte(b.String())
}

func describe(name string) string {
	switch {
	case name == InfoName:
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
	case name == SettingsName:
		return "app settings, secrets redacted"
	case name == WindowName:
		return "screenshot of the key window, taken by the app itself"
	}
	return ""
}
