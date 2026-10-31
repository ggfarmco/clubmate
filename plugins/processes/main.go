package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	sectionID       = "processes"
	sidebarSlot     = "processes.sidebar"
	mainSlot        = "processes.main"
	killedTopic     = "processes.killed"
	refreshInterval = time.Second
	deletedTTL      = time.Minute
	autoKillEvery   = 10 * time.Second
	bytesPerKiB     = 1024
)

type app struct {
	renderMu sync.Mutex
	hist     *history
	memKiB   int64
	mu       sync.Mutex
	projects map[string]*projectState
	present  map[string]bool
	sections map[string]string
	deleted  map[string]time.Time
}

func main() {
	a := newApp(memoryKiB())
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, UIEvent: a.uiEvent, Command: a.command, BusEvent: a.busEvent})
}

func newApp(memKiB int64) *app {
	return &app{
		hist:     newHistory(),
		memKiB:   memKiB,
		projects: map[string]*projectState{},
		present:  map[string]bool{},
		sections: map[string]string{},
		deleted:  map[string]time.Time{},
	}
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	ready, err := h.Publish(ctx, h.PluginID()+".ready", nil)
	if err != nil {
		return err
	}
	present, err := h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, pattern := range []string{"ui.*", "project.*"} {
		if _, err := h.Subscribe(ctx, pattern, ready.Seq); err != nil {
			return err
		}
	}
	projects, err := h.ListProjects(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	for _, id := range present {
		a.present[id] = true
	}
	a.mu.Unlock()
	for _, id := range present {
		a.loadSection(ctx, h, id)
	}
	for _, p := range projects {
		a.renderLogged(ctx, h, p.ID)
	}
	go every(ctx, refreshInterval, func() { a.tick(ctx, h) })
	go every(ctx, autoKillEvery, func() { a.autoKill(ctx, h) })
	return nil
}

func (a *app) busEvent(ctx context.Context, h *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	switch ev.Topic {
	case contract.TopicUIOpened:
		var p contract.UIPresence
		if decode(ev, &p) {
			a.setPresent(p.ProjectID, true)
			a.loadSection(ctx, h, p.ProjectID)
			a.renderLogged(ctx, h, p.ProjectID)
		}
	case contract.TopicUIIdle:
		var p contract.UIPresence
		if decode(ev, &p) {
			a.setPresent(p.ProjectID, false)
		}
	case contract.TopicUISection:
		var p contract.UISection
		if !decode(ev, &p) {
			return
		}
		a.setSection(p.ProjectID, p.Section)
		if p.Section == sectionID {
			a.renderLogged(ctx, h, p.ProjectID)
		}
	case contract.TopicProjectCreated:
		var p contract.ProjectChanged
		if decode(ev, &p) {
			a.renderLogged(ctx, h, p.Project.ID)
		}
	case contract.TopicProjectDeleted:
		var p contract.ProjectDeleted
		if decode(ev, &p) {
			a.drop(ctx, h, p.ID)
		}
	}
}

func every(ctx context.Context, interval time.Duration, run func()) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		run()
	}
}

func decode(ev pluginsdk.Event, into any) bool {
	if err := json.Unmarshal(ev.Payload, into); err != nil {
		logError(ev.Topic, err)
		return false
	}
	return true
}

func logError(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "processes: %s: %v\n", what, err)
	}
}

func memoryKiB() int64 {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		logError("hw.memsize", err)
		return 0
	}
	return int64(total / bytesPerKiB)
}
