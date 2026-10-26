package paths

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const maxSocketPath = 103

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
		state = filepath.Join(home, "Library", "Application Support", "clubmate")
	}
	runtime := os.Getenv("CLUBMATE_RUNTIME_DIR")
	if runtime == "" {
		runtime = "/tmp/clubmate-" + strconv.Itoa(os.Getuid())
	}
	return Dirs{State: state, Runtime: runtime}, nil
}

func (d Dirs) Ensure() error {
	if len(d.SessionSocket(strings.Repeat("x", 20)))+len(".tmp") > maxSocketPath || len(d.CoreSocket()) > maxSocketPath {
		return fmt.Errorf("runtime dir %q: %w", d.Runtime, ErrSocketPathTooLong)
	}
	for _, dir := range []string{d.State, d.SessionsDir(), d.Runtime, filepath.Join(d.Runtime, "s")} {
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

func (d Dirs) DB() string {
	return filepath.Join(d.State, "clubmate.db")
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
