package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"clubmate/pkg/pluginsdk"
)

const (
	nvimConfigKey = "nvimConfig"
	modeShared    = "shared"
	modeClubmate  = "clubmate"
	appNameEnv    = "NVIM_APPNAME"
	sourceApp     = "nvim"
	copyPollWait  = time.Second
	tempInfix     = ".clubmate-tmp-"
)

var (
	errConfigMode  = errors.New("config: mode must be shared or clubmate")
	errDestExists  = errors.New("already exists")
	errNoSource    = errors.New("config-copy: no Neovim config to copy at")
	errCopyRunning = errors.New("config-copy: a copy is already running")
	errUnknownCopy = errors.New("config-copy: unknown id")
)

type configArgs struct {
	Mode string `json:"mode"`
}

type configInfo struct {
	Mode              string `json:"mode"`
	App               string `json:"app"`
	ConfigDir         string `json:"configDir"`
	DataDir           string `json:"dataDir"`
	ConfigExists      bool   `json:"configExists"`
	DataExists        bool   `json:"dataExists"`
	SourceConfigBytes int64  `json:"sourceConfigBytes"`
	SourceDataBytes   int64  `json:"sourceDataBytes"`
}

type copyArgs struct {
	Data bool `json:"data"`
}

type copyIDArgs struct {
	ID string `json:"id"`
}

type copyStarted struct {
	ID string `json:"id"`
}

type copyStatus struct {
	Copied int64  `json:"copied"`
	Total  int64  `json:"total"`
	Done   bool   `json:"done"`
	Error  string `json:"error,omitempty"`
}

type nvimDirs struct {
	srcConfig string
	srcData   string
	config    string
	data      string
}

type copyPair struct {
	src string
	dst string
}

type copyJob struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	status copyStatus
	wake   chan struct{}
}

type copies struct {
	seq  atomic.Int64
	mu   sync.Mutex
	jobs map[string]*copyJob
}

type dirMode struct {
	path string
	mode fs.FileMode
}

type progressWriter struct {
	ctx context.Context
	w   io.Writer
	add func(int64)
}

func appName() string {
	if pluginsdk.Flavor() == "release" {
		return "clubmate"
	}
	return "clubmate-dev"
}

func resolveDirs() (nvimDirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nvimDirs{}, err
	}
	configHome := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	dataHome := cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	app := appName()
	return nvimDirs{
		srcConfig: filepath.Join(configHome, sourceApp),
		srcData:   filepath.Join(dataHome, sourceApp),
		config:    filepath.Join(configHome, app),
		data:      filepath.Join(dataHome, app),
	}, nil
}

func (a *app) nvimMode(ctx context.Context, h *pluginsdk.Host) (string, error) {
	var mode string
	if _, err := h.StorageGet(ctx, nvimConfigKey, &mode); err != nil {
		return "", err
	}
	if mode == modeClubmate {
		return modeClubmate, nil
	}
	return modeShared, nil
}

func (a *app) sessionEnv(ctx context.Context, h *pluginsdk.Host) (env, unset []string, err error) {
	mode, err := a.nvimMode(ctx, h)
	if err != nil {
		return nil, nil, err
	}
	if mode == modeClubmate {
		return []string{appNameEnv + "=" + appName()}, nil, nil
	}
	return nil, []string{appNameEnv}, nil
}

func (a *app) config(ctx context.Context, h *pluginsdk.Host, args configArgs) (any, error) {
	if args.Mode != "" {
		if args.Mode != modeShared && args.Mode != modeClubmate {
			return nil, errConfigMode
		}
		return nil, h.StorageSet(ctx, nvimConfigKey, args.Mode)
	}
	mode, err := a.nvimMode(ctx, h)
	if err != nil {
		return nil, err
	}
	dirs, err := resolveDirs()
	if err != nil {
		return nil, err
	}
	return configInfo{
		Mode:              mode,
		App:               appName(),
		ConfigDir:         dirs.config,
		DataDir:           dirs.data,
		ConfigExists:      exists(dirs.config),
		DataExists:        exists(dirs.data),
		SourceConfigBytes: sourceSize(ctx, dirs.srcConfig),
		SourceDataBytes:   sourceSize(ctx, dirs.srcData),
	}, nil
}

func (a *app) configCopy(args copyArgs) (copyStarted, error) {
	dirs, err := resolveDirs()
	if err != nil {
		return copyStarted{}, err
	}
	if !exists(dirs.srcConfig) {
		return copyStarted{}, fmt.Errorf("%w %s", errNoSource, dirs.srcConfig)
	}
	pairs := []copyPair{{src: dirs.srcConfig, dst: dirs.config}}
	if args.Data && exists(dirs.srcData) {
		pairs = append(pairs, copyPair{src: dirs.srcData, dst: dirs.data})
	}
	for _, p := range pairs {
		if exists(p.dst) {
			return copyStarted{}, fmt.Errorf("config-copy: %s %w", p.dst, errDestExists)
		}
	}
	id, err := a.copies.start(pairs)
	return copyStarted{ID: id}, err
}

func newCopies() *copies {
	return &copies{jobs: map[string]*copyJob{}}
}

func (cs *copies) start(pairs []copyPair) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for id, j := range cs.jobs {
		if !j.snapshot().Done {
			return "", errCopyRunning
		}
		delete(cs.jobs, id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &copyJob{cancel: cancel, wake: make(chan struct{})}
	id := "copy-" + strconv.FormatInt(cs.seq.Add(1), 10)
	cs.jobs[id] = j
	go func() {
		defer cancel()
		j.finish(copyAll(ctx, pairs, j.progress))
	}()
	return id, nil
}

func (cs *copies) lookup(id string) (*copyJob, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	j, ok := cs.jobs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownCopy, id)
	}
	return j, nil
}

func (cs *copies) status(ctx context.Context, id string) (copyStatus, error) {
	j, err := cs.lookup(id)
	if err != nil {
		return copyStatus{}, err
	}
	timer := time.NewTimer(copyPollWait)
	defer timer.Stop()
	select {
	case <-j.wake:
	case <-timer.C:
	case <-ctx.Done():
	}
	return j.snapshot(), nil
}

func (cs *copies) cancel(id string) error {
	j, err := cs.lookup(id)
	if err != nil {
		return err
	}
	j.cancel()
	return nil
}

func (j *copyJob) progress(copied, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Copied, j.status.Total = copied, total
}

func (j *copyJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Done = true
	switch {
	case errors.Is(err, context.Canceled):
		j.status.Error = "cancelled"
	case err != nil:
		j.status.Error = err.Error()
	}
	close(j.wake)
}

func (j *copyJob) snapshot() copyStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func copyAll(ctx context.Context, pairs []copyPair, progress func(copied, total int64)) error {
	srcs := make([]string, len(pairs))
	var total int64
	for i, p := range pairs {
		src, err := filepath.EvalSymlinks(p.src)
		if err != nil {
			return err
		}
		n, err := treeSize(ctx, src)
		if err != nil {
			return err
		}
		srcs[i], total = src, total+n
	}
	progress(0, total)
	temps := make([]string, 0, len(pairs))
	defer func() {
		for _, tmp := range temps {
			removeTemp(tmp)
		}
	}()
	var copied int64
	add := func(n int64) {
		copied += n
		progress(copied, total)
	}
	for i, p := range pairs {
		if err := os.MkdirAll(filepath.Dir(p.dst), 0o755); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp(filepath.Dir(p.dst), filepath.Base(p.dst)+tempInfix)
		if err != nil {
			return err
		}
		temps = append(temps, tmp)
		if err := copyTree(ctx, srcs[i], tmp, add); err != nil {
			return err
		}
	}
	for i, p := range pairs {
		if err := unix.RenamexNp(temps[i], p.dst, unix.RENAME_EXCL); err != nil {
			if errors.Is(err, unix.EEXIST) {
				return fmt.Errorf("config-copy: %s %w", p.dst, errDestExists)
			}
			return &os.LinkError{Op: "rename", Old: temps[i], New: p.dst, Err: err}
		}
	}
	return nil
}

func copyTree(ctx context.Context, src, dst string, add func(int64)) error {
	var dirs []dirMode
	err := walkSource(src, func(rel string, d fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		from, to := filepath.Join(src, rel), filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if rel != "." {
				if err := os.Mkdir(to, 0o700); err != nil {
					return err
				}
			}
			dirs = append(dirs, dirMode{path: to, mode: info.Mode().Perm()})
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(from)
			if err != nil {
				return err
			}
			return os.Symlink(link, to)
		case d.Type().IsRegular():
			return copyFile(ctx, from, to, info.Mode().Perm(), add)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, d := range slices.Backward(dirs) {
		if err := os.Chmod(d.path, d.mode); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(ctx context.Context, src, dst string, perm fs.FileMode, add func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(progressWriter{ctx: ctx, w: out, add: add}, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

func (p progressWriter) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	p.add(int64(n))
	return n, err
}

func walkSource(root string, fn func(rel string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(rel, d)
	})
}

func treeSize(ctx context.Context, root string) (int64, error) {
	var total int64
	err := walkSource(root, func(_ string, d fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func sourceSize(ctx context.Context, dir string) int64 {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return 0
	}
	n, _ := treeSize(ctx, root)
	return n
}

func removeTemp(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
