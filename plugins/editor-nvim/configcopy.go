package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	nvimConfigKey = "nvimConfig"
	modeShared    = "shared"
	modeClubmate  = "clubmate"
	appNameEnv    = "NVIM_APPNAME"
	sourceApp     = "nvim"
	copyPollWait  = time.Second
	sizeBudget    = 1500 * time.Millisecond
	shutdownWait  = 1500 * time.Millisecond
	unknownSize   = -1
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
	configHome := xdgHome("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dataHome := xdgHome("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	app := appName()
	return nvimDirs{
		srcConfig: filepath.Join(configHome, sourceApp),
		srcData:   filepath.Join(dataHome, sourceApp),
		config:    filepath.Join(configHome, app),
		data:      filepath.Join(dataHome, app),
	}, nil
}

func xdgHome(env, fallback string) string {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v
	}
	return fallback
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
	sizeCtx, cancel := context.WithTimeout(ctx, sizeBudget)
	defer cancel()
	return configInfo{
		Mode:              mode,
		App:               appName(),
		ConfigDir:         dirs.config,
		DataDir:           dirs.data,
		ConfigExists:      exists(dirs.config),
		DataExists:        exists(dirs.data),
		SourceConfigBytes: sourceSize(sizeCtx, dirs.srcConfig),
		SourceDataBytes:   sourceSize(sizeCtx, dirs.srcData),
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
	id, err := a.copies.start(pairs, []string{dirs.config, dirs.data})
	return copyStarted{ID: id}, err
}

func (a *app) shutdown(context.Context, *pluginsdk.Host) {
	a.copies.stop(shutdownWait)
}

func newCopies() *copies {
	return &copies{jobs: map[string]*copyJob{}}
}

func (cs *copies) start(pairs []copyPair, sweep []string) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for id, j := range cs.jobs {
		if !j.snapshot().Done {
			return "", errCopyRunning
		}
		delete(cs.jobs, id)
	}
	for _, dst := range sweep {
		sweepTemps(dst)
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

func (cs *copies) stop(wait time.Duration) {
	cs.mu.Lock()
	jobs := slices.Collect(maps.Values(cs.jobs))
	cs.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for _, j := range jobs {
		j.cancel()
		select {
		case <-j.wake:
		case <-timer.C:
			return
		}
	}
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

func sourceSize(ctx context.Context, dir string) int64 {
	root, err := filepath.EvalSymlinks(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		return unknownSize
	}
	n, err := treeSize(ctx, root)
	if err != nil {
		return unknownSize
	}
	return n
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
