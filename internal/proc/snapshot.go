package proc

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"clubmate/pkg/contract"
)

type Process = contract.ProcessInfo

var snapshotSampler = NewSampler()

func Snapshot() ([]Process, error) {
	procs, err := List()
	if err != nil {
		return nil, err
	}
	return snapshotSampler.Sample(procs), nil
}

func List() ([]Process, error) {
	kprocs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	uid := uint32(os.Getuid())
	procs := make([]Process, 0, len(kprocs))
	for i := range kprocs {
		k := &kprocs[i]
		if k.Eproc.Ucred.Uid != uid {
			continue
		}
		procs = append(procs, Process{
			PID:       int(k.Proc.P_pid),
			PPID:      int(k.Eproc.Ppid),
			PGID:      int(k.Eproc.Pgid),
			Comm:      commString(k.Proc.P_comm[:]),
			StartTime: startOf(k),
		})
	}
	return procs, nil
}

func commString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
