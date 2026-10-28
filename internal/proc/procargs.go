package proc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

var ErrProcArgsShort = errors.New("procargs buffer too short")

func ReadEnv(pid int) ([]string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, fmt.Errorf("read procargs of %d: %w", pid, err)
	}
	_, env, err := ParseProcArgs(raw)
	return env, err
}

func ReadArgs(pid int) []string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return []string{}
	}
	argv, _, err := splitArgv(raw)
	if err != nil || argv == nil {
		return []string{}
	}
	return argv
}

func ParseProcArgs(b []byte) (argv, env []string, err error) {
	argv, rest, err := splitArgv(b)
	if err != nil {
		return nil, nil, err
	}
	for len(rest) > 0 {
		var s string
		s, rest = cutString(rest)
		if s == "" {
			break
		}
		if strings.Contains(s, "=") {
			env = append(env, s)
		}
	}
	return argv, env, nil
}

func splitArgv(b []byte) ([]string, []byte, error) {
	if len(b) < 4 {
		return nil, nil, ErrProcArgsShort
	}
	argc := int(int32(binary.LittleEndian.Uint32(b)))
	rest := b[4:]

	_, rest = cutString(rest)
	rest = bytes.TrimLeft(rest, "\x00")

	var argv []string
	for i := 0; i < argc && len(rest) > 0; i++ {
		var s string
		s, rest = cutString(rest)
		argv = append(argv, s)
	}
	return argv, rest, nil
}

func cutString(b []byte) (string, []byte) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return string(b), nil
	}
	return string(b[:i]), b[i+1:]
}
