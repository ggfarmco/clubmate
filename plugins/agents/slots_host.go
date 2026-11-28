package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"clubmate/internal/proc"
)

const (
	slotHostEnv   = "CLUBMATE_AGENTS_SLOT_HOST"
	cpuWindow     = 10 * time.Second
	simsCacheFor  = 5 * time.Second
	simsIdleFor   = 30 * time.Second
	simsTimeout   = 10 * time.Second
	agentSimStart = "cm-"
	gigabyte      = 1 << 30
)

type simDevice struct {
	Name string `json:"name"`
	UDID string `json:"udid"`
}

type hostLoad interface {
	CPU() float64
	MemFreeGB() float64
	BootedSims() []simDevice
}

type tickSample struct {
	at    time.Time
	ticks proc.HostTicks
}

type machineLoad struct {
	mu      sync.Mutex
	samples []tickSample
	sims    []simDevice
	simsAt  time.Time
}

func newHostLoad() hostLoad {
	if path := os.Getenv(slotHostEnv); path != "" {
		return fileLoad(path)
	}
	return &machineLoad{}
}

func (m *machineLoad) sample(now time.Time) {
	t, err := proc.HostCPUTicks()
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, tickSample{at: now, ticks: t})
	for len(m.samples) > 2 && now.Sub(m.samples[1].at) >= cpuWindow {
		m.samples = m.samples[1:]
	}
}

func (m *machineLoad) CPU() float64 {
	m.sample(time.Now())
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.samples) < 2 {
		return 0
	}
	first, last := m.samples[0].ticks, m.samples[len(m.samples)-1].ticks
	if last.Total <= first.Total {
		return 0
	}
	return float64(last.Busy-first.Busy) / float64(last.Total-first.Total) * 100
}

func (m *machineLoad) MemFreeGB() float64 {
	free, err := proc.HostMemFree()
	if err != nil {
		return 0
	}
	return float64(free) / gigabyte
}

func (m *machineLoad) BootedSims() []simDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sims
}

func (m *machineLoad) refreshSims(now time.Time, maxAge time.Duration) {
	m.mu.Lock()
	fresh := now.Sub(m.simsAt) < maxAge
	m.mu.Unlock()
	if fresh {
		return
	}
	sims := bootedSims()
	m.mu.Lock()
	m.sims, m.simsAt = sims, now
	m.mu.Unlock()
}

func bootedSims() []simDevice {
	ctx, cancel := context.WithTimeout(context.Background(), simsTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xcrun", "simctl", "list", "devices", "booted", "-j").Output()
	if err != nil {
		return nil
	}
	var list struct {
		Devices map[string][]struct {
			simDevice
			State string `json:"state"`
		} `json:"devices"`
	}
	if json.Unmarshal(out, &list) != nil {
		return nil
	}
	var sims []simDevice
	for _, devices := range list.Devices {
		for _, d := range devices {
			if d.State == "Booted" {
				sims = append(sims, d.simDevice)
			}
		}
	}
	return sims
}

func agentSims(sims []simDevice) []string {
	var names []string
	for _, s := range sims {
		if strings.HasPrefix(s.Name, agentSimStart) {
			names = append(names, s.Name)
		}
	}
	return names
}

type fileLoad string

type fileLoadData struct {
	CPU       float64     `json:"cpu"`
	MemFreeGB float64     `json:"memFreeGB"`
	Sims      []simDevice `json:"sims"`
}

func (f fileLoad) read() fileLoadData {
	var d fileLoadData
	if raw, err := os.ReadFile(string(f)); err == nil {
		_ = json.Unmarshal(raw, &d)
	}
	return d
}

func (f fileLoad) CPU() float64 { return f.read().CPU }

func (f fileLoad) MemFreeGB() float64 { return f.read().MemFreeGB }

func (f fileLoad) BootedSims() []simDevice { return f.read().Sims }
