package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"time"
)

const liveEvery = 2 * time.Second

type liveOutput struct {
	tail  string
	lines int
}

func readLive(path string) (liveOutput, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return liveOutput{}, false
	}
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	tail := strings.TrimSpace(stripControl(strings.ToValidUTF8(string(data[max(0, len(data)-4*outputTail):]), "")))
	return liveOutput{tail: tail, lines: lines}, true
}

func (r *Run) runningStep() string {
	for i := len(r.Moves) - 1; i >= 0; i-- {
		if m := r.Moves[i]; m.Step != nil && m.Step.Status == stepRunning {
			return m.Node
		}
	}
	return ""
}

func (a *app) followLive(ctx context.Context) {
	tick := time.NewTicker(liveEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		a.tickLive(ctx)
	}
}

func (a *app) tickLive(ctx context.Context) {
	a.mu.Lock()
	workspaces := map[string]bool{}
	for _, r := range a.runs {
		if r.Status == statusRunning && r.runningStep() != "" {
			workspaces[r.Workspace] = true
		}
	}
	a.mu.Unlock()
	for ws := range workspaces {
		section, err := a.host.SelectedSection(ctx, ws)
		if err != nil || section != "" && section != sectionID {
			continue
		}
		a.mu.Lock()
		a.touch(ws)
		a.mu.Unlock()
	}
	a.flush(ctx)
}
