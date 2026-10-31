package sessionhost

import "syscall"

const noFileTarget = 10240

func raiseNoFile() error {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return err
	}
	want := min(noFileTarget, lim.Max)
	if lim.Cur >= want {
		return nil
	}
	lim.Cur = want
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
