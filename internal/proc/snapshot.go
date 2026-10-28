package proc

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type Process struct {
	PID       int       `json:"pid"`
	PPID      int       `json:"ppid"`
	PGID      int       `json:"pgid"`
	Comm      string    `json:"comm"`
	Args      []string  `json:"args"`
	StartTime time.Time `json:"startTime"`
	RSSKiB    int64     `json:"rssKiB"`
	CPU       float64   `json:"cpu"`
}

type usage struct {
	rssKiB int64
	cpu    float64
}

func Snapshot() ([]Process, error) {
	kprocs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	usages, err := readUsage()
	if err != nil {
		return nil, err
	}

	uid := uint32(os.Getuid())
	procs := make([]Process, 0, len(kprocs))
	for i := range kprocs {
		k := &kprocs[i]
		if k.Eproc.Ucred.Uid != uid {
			continue
		}
		pid := int(k.Proc.P_pid)
		u := usages[pid]
		procs = append(procs, Process{
			PID:       pid,
			PPID:      int(k.Eproc.Ppid),
			PGID:      int(k.Eproc.Pgid),
			Comm:      commString(k.Proc.P_comm[:]),
			Args:      readArgs(pid),
			StartTime: startOf(k),
			RSSKiB:    u.rssKiB,
			CPU:       u.cpu,
		})
	}
	return procs, nil
}

func readUsage() (map[int]usage, error) {
	out, err := exec.Command("/bin/ps", "-axo", "pid=,rss=,pcpu=").Output()
	if err != nil {
		return nil, fmt.Errorf("run ps: %w", err)
	}
	usages := make(map[int]usage)
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		rss, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		usages[pid] = usage{rssKiB: rss, cpu: cpu}
	}
	return usages, nil
}

func readArgs(pid int) []string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return []string{}
	}
	argv, _, err := ParseProcArgs(raw)
	if err != nil || argv == nil {
		return []string{}
	}
	return argv
}

func commString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
