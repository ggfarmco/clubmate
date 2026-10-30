package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	groupsFile = "groups.json"
	reapPoll   = 50 * time.Millisecond
)

var errNoProcess = errors.New("no such process")

type groupRecord struct {
	PGID    int    `json:"pgid"`
	Started string `json:"started"`
}

type groups struct {
	mu   sync.Mutex
	path string
	recs map[int]string
}

func newGroups(stateDir string) *groups {
	return &groups{path: filepath.Join(stateDir, groupsFile), recs: map[int]string{}}
}

func (g *groups) add(pgid int) {
	started, err := processField(pgid, "lstart")
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: record agent group:", err)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recs[pgid] = started
	g.saveLocked()
}

func (g *groups) remove(pgid int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.recs[pgid]; !ok {
		return
	}
	delete(g.recs, pgid)
	g.saveLocked()
}

func (g *groups) saveLocked() {
	if len(g.recs) == 0 {
		if err := os.Remove(g.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "agents: clear agent groups:", err)
		}
		return
	}
	list := make([]groupRecord, 0, len(g.recs))
	for pgid, started := range g.recs {
		list = append(list, groupRecord{PGID: pgid, Started: started})
	}
	slices.SortFunc(list, func(x, y groupRecord) int { return x.PGID - y.PGID })
	raw, err := json.Marshal(list)
	if err == nil {
		err = writeAtomic(g.path, raw)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: save agent groups:", err)
	}
}

func (g *groups) reap(markers []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var list []groupRecord
	if raw, err := os.ReadFile(g.path); err == nil {
		_ = json.Unmarshal(raw, &list)
	}
	var wg sync.WaitGroup
	for _, r := range list {
		wg.Go(func() { reapGroup(r, markers) })
	}
	wg.Wait()
	clear(g.recs)
	g.saveLocked()
}

func reapGroup(r groupRecord, markers []string) {
	if r.PGID <= 1 || syscall.Kill(-r.PGID, 0) != nil {
		return
	}
	started, err := processField(r.PGID, "lstart")
	if err != nil || started != r.Started {
		return
	}
	command, err := processField(r.PGID, "command")
	if err != nil || !slices.ContainsFunc(markers, func(m string) bool { return m != "" && strings.Contains(command, m) }) {
		return
	}
	_ = syscall.Kill(-r.PGID, syscall.SIGTERM)
	for deadline := time.Now().Add(termGrace); time.Now().Before(deadline); time.Sleep(reapPoll) {
		if syscall.Kill(-r.PGID, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(-r.PGID, syscall.SIGKILL)
}

func processField(pid int, field string) (string, error) {
	cmd := exec.Command("ps", "-ww", "-o", field+"=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	value := strings.TrimSpace(string(out))
	if err != nil || value == "" {
		return "", fmt.Errorf("%w: %d", errNoProcess, pid)
	}
	return value, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

func orphanMarkers() []string {
	return []string{claudeBin, os.Getenv(commandEnv)}
}
