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

func ParseProcArgs(b []byte) (argv, env []string, err error) {
	if len(b) < 4 {
		return nil, nil, ErrProcArgsShort
	}
	argc := int(int32(binary.LittleEndian.Uint32(b)))
	rest := b[4:]

	_, rest = cutString(rest)
	rest = bytes.TrimLeft(rest, "\x00")

	for i := 0; i < argc && len(rest) > 0; i++ {
		var s string
		s, rest = cutString(rest)
		argv = append(argv, s)
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

func cutString(b []byte) (string, []byte) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return string(b), nil
	}
	return string(b[:i]), b[i+1:]
}
