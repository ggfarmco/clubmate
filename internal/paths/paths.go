package paths

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"

	"clubmate/internal/buildinfo"
)

const (
	maxSocketPath = 103
	LogRotateSize = 10 << 20
)

var (
	ErrSocketPathTooLong  = errors.New("socket path too long")
	ErrCoreRunning        = errors.New("core already running")
	ErrInsecureRuntimeDir = errors.New("insecure runtime dir")
	ErrUntrustedPeer      = errors.New("untrusted socket peer")
)

type Dirs struct {
	State   string
	Runtime string
}

func Resolve() (Dirs, error) {
	state := os.Getenv("CLUBMATE_STATE_DIR")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Dirs{}, fmt.Errorf("resolve home dir: %w", err)
		}
		state = defaultState(home)
	}
	return Dirs{State: state, Runtime: cmp.Or(os.Getenv("CLUBMATE_RUNTIME_DIR"), defaultRuntime())}, nil
}

func Defaults() (Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return Dirs{State: defaultState(home), Runtime: defaultRuntime()}, nil
}

func StripDefaults(env []string) []string {
	defaults, err := Defaults()
	if err != nil {
		return env
	}
	return slices.DeleteFunc(env, func(kv string) bool {
		return kv == "CLUBMATE_STATE_DIR="+defaults.State || kv == "CLUBMATE_RUNTIME_DIR="+defaults.Runtime
	})
}

func defaultState(home string) string {
	return filepath.Join(home, "Library", "Application Support", dirName())
}

func defaultRuntime() string {
	return "/tmp/" + dirName() + "-" + strconv.Itoa(os.Getuid())
}

func dirName() string {
	if buildinfo.Flavor == "release" {
		return "clubmate"
	}
	return "clubmate-dev"
}

func (d Dirs) Ensure() error {
	if len(d.SessionSocket(strings.Repeat("x", 20)))+len(".tmp") > maxSocketPath || len(d.CoreSocket()) > maxSocketPath {
		return fmt.Errorf("runtime dir %q: %w", d.Runtime, ErrSocketPathTooLong)
	}
	for _, dir := range []string{d.State, d.SessionsDir(), d.PluginsDir(), d.Runtime, filepath.Join(d.Runtime, "s")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return d.CheckRuntime()
}

func (d Dirs) CheckRuntime() error {
	for _, dir := range []string{d.Runtime, filepath.Join(d.Runtime, "s")} {
		if err := CheckPrivate(dir); err != nil {
			return err
		}
	}
	return nil
}

func VerifyPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("peer credentials: %w", err)
	}
	var cred *unix.Xucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return fmt.Errorf("peer credentials: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("peer credentials: %w", credErr)
	}
	if int(cred.Uid) != os.Getuid() {
		return ErrUntrustedPeer
	}
	return nil
}

func PrivateDB(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	f.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("chmod %s: %w", p, err)
		}
	}
	return nil
}

var logMu sync.Mutex

func OpenLog(path string) (*os.File, error) {
	logMu.Lock()
	defer logMu.Unlock()
	if info, err := os.Stat(path); err == nil && info.Size() > LogRotateSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("rotate %s: %w", path, err)
		}
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

func TrimLog(path string) error {
	logMu.Lock()
	defer logMu.Unlock()
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && info.Size() <= LogRotateSize {
		return nil
	}
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := path + ".1.tmp"
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy %s: %w", path, err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path+".1"); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Truncate(path, 0)
}

func (d Dirs) DB() string {
	return filepath.Join(d.State, "clubmate.db")
}

func (d Dirs) BusDB() string {
	return filepath.Join(d.State, "bus.db")
}

func (d Dirs) PluginsDB() string {
	return filepath.Join(d.State, "plugins.db")
}

func (d Dirs) CoreLog() string {
	return filepath.Join(d.State, "core.log")
}

func (d Dirs) DiagnosticsDir() string {
	return filepath.Join(d.State, "diagnostics")
}

func (d Dirs) CoreSocket() string {
	return filepath.Join(d.Runtime, "core.sock")
}

func (d Dirs) SessionSocket(id string) string {
	return filepath.Join(d.Runtime, "s", id+".sock")
}

func (d Dirs) SessionsDir() string {
	return filepath.Join(d.State, "sessions")
}

func (d Dirs) SessionMeta(id string) string {
	return filepath.Join(d.SessionsDir(), id+".json")
}

func (d Dirs) SessionLog(id string) string {
	return filepath.Join(d.SessionsDir(), id+".log")
}

func (d Dirs) SessionLogs() []string {
	logs, _ := filepath.Glob(filepath.Join(d.SessionsDir(), "*.log"))
	return logs
}

func (d Dirs) PluginsDir() string {
	return filepath.Join(d.State, "plugins")
}

func (d Dirs) PluginRuntime(id string) string {
	return filepath.Join(d.Runtime, "p", id)
}

func (d Dirs) PluginState(id string) string {
	return filepath.Join(d.State, "plugin-data", id)
}

func (d Dirs) PluginLog(id string) string {
	return filepath.Join(d.PluginsDir(), id+".log")
}

func LockCore(d Dirs) (func(), error) {
	f, err := os.OpenFile(filepath.Join(d.State, "core.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open core lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrCoreRunning
		}
		return nil, fmt.Errorf("lock core: %w", err)
	}
	return func() { f.Close() }, nil
}

func CheckPrivate(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || int(st.Uid) != os.Getuid() || fi.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s: %w", dir, ErrInsecureRuntimeDir)
	}
	return nil
}
