package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/api"
)

const (
	usageKey         = "usage"
	rateLimitMetaKey = "_claude/rateLimit"
	windowFiveHour   = "five_hour"
	windowSevenDay   = "seven_day"
)

var usageWindows = []string{windowFiveHour, windowSevenDay}

type usageWindow struct {
	Used     int    `json:"used"`
	ResetsAt int64  `json:"resetsAt"`
	Status   string `json:"status,omitempty"`
}

type usageState struct {
	Windows   map[string]usageWindow `json:"windows"`
	UpdatedAt int64                  `json:"updatedAt,omitempty"`
}

type rateLimitWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    int64   `json:"resetsAt"`
	Status      string  `json:"status"`
}

type rateLimitInfo struct {
	Status         string                     `json:"status"`
	ResetsAt       int64                      `json:"resetsAt"`
	RateLimitType  string                     `json:"rateLimitType"`
	Utilization    *float64                   `json:"utilization"`
	UnifiedWindows map[string]rateLimitWindow `json:"unifiedWindows"`
}

type rateLimitUpdate struct {
	Meta map[string]json.RawMessage `json:"_meta"`
}

type usageTracker struct {
	host  *pluginsdk.Host
	mu    sync.Mutex
	pubMu sync.Mutex
	state usageState
}

func (u *usageTracker) load(ctx context.Context, h *pluginsdk.Host) error {
	var saved usageState
	found, err := h.StorageGet(ctx, usageKey, &saved)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.host = h
	if found {
		u.state = saved
	}
	return nil
}

func (u *usageTracker) snapshot() usageState {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := u.state
	out.Windows = maps.Clone(out.Windows)
	if out.Windows == nil {
		out.Windows = map[string]usageWindow{}
	}
	return out
}

func (u *usageTracker) observe(update json.RawMessage) {
	if !bytes.Contains(update, []byte(rateLimitMetaKey)) {
		return
	}
	var up rateLimitUpdate
	var info rateLimitInfo
	if json.Unmarshal(update, &up) != nil || json.Unmarshal(up.Meta[rateLimitMetaKey], &info) != nil {
		return
	}
	windows := info.windows()
	if len(windows) == 0 {
		return
	}
	u.mu.Lock()
	if u.state.Windows == nil {
		u.state.Windows = map[string]usageWindow{}
	}
	maps.Copy(u.state.Windows, windows)
	u.state.UpdatedAt = time.Now().Unix()
	host := u.host
	u.mu.Unlock()
	if host != nil {
		go u.publish(host)
	}
}

func (u *usageTracker) publish(h *pluginsdk.Host) {
	u.pubMu.Lock()
	defer u.pubMu.Unlock()
	state := u.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := h.StorageSet(ctx, usageKey, state); err != nil {
		fmt.Fprintln(os.Stderr, "agents: save usage:", err)
	}
	if _, err := h.Publish(ctx, api.TopicUsage, state); err != nil {
		fmt.Fprintln(os.Stderr, "agents: publish:", err)
	}
}

func (r rateLimitInfo) windows() map[string]usageWindow {
	out := map[string]usageWindow{}
	for _, name := range usageWindows {
		if w, ok := r.UnifiedWindows[name]; ok && w.ResetsAt > 0 {
			out[name] = usageWindow{Used: percent(w.Utilization), ResetsAt: w.ResetsAt, Status: w.Status}
		}
	}
	if _, ok := out[r.RateLimitType]; !ok && r.Utilization != nil && r.ResetsAt > 0 && (r.RateLimitType == windowFiveHour || r.RateLimitType == windowSevenDay) {
		out[r.RateLimitType] = usageWindow{Used: percent(*r.Utilization), ResetsAt: r.ResetsAt, Status: r.Status}
	}
	return out
}

func percent(fraction float64) int {
	return int(math.Round(max(fraction, 0) * 100))
}

func (a *app) usageCommand(context.Context, pluginsdk.CommandRun) (any, error) {
	return a.usage.snapshot(), nil
}
