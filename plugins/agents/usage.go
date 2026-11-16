package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"strconv"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/agents/api"
)

const (
	usageKey         = "usage"
	usageShareKey    = "usage-share"
	rateLimitMetaKey = "_claude/rateLimit"
	windowFiveHour   = "five_hour"
	windowSevenDay   = "seven_day"

	contextWarnPercent = 80
	costMarkTTL        = 8 * 24 * time.Hour
)

var shareLabels = map[string]string{windowFiveHour: "5h", windowSevenDay: "7d"}

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
	Cost *acp.Cost                  `json:"cost"`
}

type shareWindow struct {
	ResetsAt int64              `json:"resetsAt"`
	Base     float64            `json:"base"`
	Last     float64            `json:"last"`
	Total    float64            `json:"total"`
	Sessions map[string]float64 `json:"sessions"`
}

type costMark struct {
	Amount float64 `json:"amount"`
	At     int64   `json:"at"`
}

type shareState struct {
	Windows map[string]*shareWindow `json:"windows"`
	Costs   map[string]costMark     `json:"costs"`
}

type usageReport struct {
	usageState
	Sessions map[string]map[string]float64 `json:"sessions"`
}

type usageTracker struct {
	host  *pluginsdk.Host
	mu    sync.Mutex
	pubMu sync.Mutex
	state usageState
	share shareState
}

func (u *usageTracker) load(ctx context.Context, h *pluginsdk.Host) error {
	var saved usageState
	found, err := h.StorageGet(ctx, usageKey, &saved)
	if err != nil {
		return err
	}
	var share shareState
	shareFound, err := h.StorageGet(ctx, usageShareKey, &share)
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.host = h
	if found {
		u.state = saved
	}
	if shareFound {
		u.share = share
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

func (u *usageTracker) observe(agentSession, session string, update json.RawMessage) {
	rated := bytes.Contains(update, []byte(rateLimitMetaKey))
	costed := session != "" && bytes.Contains(update, []byte(`"cost"`))
	if !rated && !costed {
		return
	}
	var up rateLimitUpdate
	if json.Unmarshal(update, &up) != nil {
		return
	}
	var windows map[string]rateLimitWindow
	if rated {
		var info rateLimitInfo
		if json.Unmarshal(up.Meta[rateLimitMetaKey], &info) == nil {
			windows = info.windows()
		}
	}
	spent := costed && up.Cost != nil
	if len(windows) == 0 && !spent {
		return
	}
	now := time.Now()
	u.mu.Lock()
	if len(windows) > 0 {
		if u.state.Windows == nil {
			u.state.Windows = map[string]usageWindow{}
		}
		for name, w := range windows {
			u.state.Windows[name] = usageWindow{Used: percent(w.Utilization), ResetsAt: w.ResetsAt, Status: w.Status}
		}
		u.state.UpdatedAt = now.Unix()
		u.share.see(windows, now)
	}
	if spent {
		u.share.spend(agentSession, session, up.Cost.Amount, now)
	}
	host := u.host
	u.mu.Unlock()
	if host != nil {
		go u.publish(host, len(windows) > 0)
	}
}

func (u *usageTracker) publish(h *pluginsdk.Host, rated bool) {
	u.pubMu.Lock()
	defer u.pubMu.Unlock()
	state := u.snapshot()
	u.mu.Lock()
	share, err := json.Marshal(u.share)
	u.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err == nil {
		err = h.StorageSet(ctx, usageShareKey, json.RawMessage(share))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: save usage share:", err)
	}
	if !rated {
		return
	}
	if err := h.StorageSet(ctx, usageKey, state); err != nil {
		fmt.Fprintln(os.Stderr, "agents: save usage:", err)
	}
	if _, err := h.Publish(ctx, api.TopicUsage, state); err != nil {
		fmt.Fprintln(os.Stderr, "agents: publish:", err)
	}
}

func (u *usageTracker) sessionShare(session string) map[string]float64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.share.of(session, time.Now())
}

func (s *shareState) see(windows map[string]rateLimitWindow, now time.Time) {
	if s.Windows == nil {
		s.Windows = map[string]*shareWindow{}
	}
	for name, w := range windows {
		cur := s.Windows[name]
		if cur == nil || now.Unix() >= cur.ResetsAt || w.Utilization < cur.Base {
			s.Windows[name] = &shareWindow{ResetsAt: w.ResetsAt, Base: w.Utilization, Last: w.Utilization, Sessions: map[string]float64{}}
			continue
		}
		cur.ResetsAt = w.ResetsAt
		cur.Last = max(cur.Last, w.Utilization)
	}
}

func (s *shareState) spend(agentSession, session string, amount float64, now time.Time) {
	if s.Costs == nil {
		s.Costs = map[string]costMark{}
	}
	delta := amount
	if prev, ok := s.Costs[agentSession]; ok && amount >= prev.Amount {
		delta = amount - prev.Amount
	}
	s.Costs[agentSession] = costMark{Amount: amount, At: now.Unix()}
	maps.DeleteFunc(s.Costs, func(_ string, m costMark) bool { return now.Sub(time.Unix(m.At, 0)) > costMarkTTL })
	if delta <= 0 {
		return
	}
	for _, w := range s.Windows {
		if now.Unix() < w.ResetsAt {
			w.Total += delta
			w.Sessions[session] += delta
		}
	}
}

func (s *shareState) of(session string, now time.Time) map[string]float64 {
	out := map[string]float64{}
	for name, w := range s.Windows {
		if spent := w.Sessions[session]; spent > 0 && w.Total > 0 && now.Unix() < w.ResetsAt {
			out[name] = (w.Last - w.Base) * 100 * spent / w.Total
		}
	}
	return out
}

func (s *shareState) all(now time.Time) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, w := range s.Windows {
		for session := range w.Sessions {
			if _, ok := out[session]; !ok {
				if share := s.of(session, now); len(share) > 0 {
					out[session] = share
				}
			}
		}
	}
	return out
}

func (r rateLimitInfo) windows() map[string]rateLimitWindow {
	out := map[string]rateLimitWindow{}
	for _, name := range usageWindows {
		if w, ok := r.UnifiedWindows[name]; ok && w.ResetsAt > 0 {
			out[name] = w
		}
	}
	if _, ok := out[r.RateLimitType]; !ok && r.Utilization != nil && r.ResetsAt > 0 && (r.RateLimitType == windowFiveHour || r.RateLimitType == windowSevenDay) {
		out[r.RateLimitType] = rateLimitWindow{Utilization: *r.Utilization, ResetsAt: r.ResetsAt, Status: r.Status}
	}
	return out
}

func percent(fraction float64) int {
	return int(math.Round(max(fraction, 0) * 100))
}

func (a *app) usageCommand(context.Context, pluginsdk.CommandRun) (any, error) {
	state := a.usage.snapshot()
	a.usage.mu.Lock()
	sessions := a.usage.share.all(time.Now())
	a.usage.mu.Unlock()
	return usageReport{usageState: state, Sessions: sessions}, nil
}

func shareParts(share map[string]float64) []string {
	var out []string
	for _, name := range usageWindows {
		v, ok := share[name]
		if !ok {
			continue
		}
		amount := "<1%"
		if v >= 0.5 {
			amount = "≈" + strconv.Itoa(int(math.Round(v))) + "%"
		}
		out = append(out, amount+" of "+shareLabels[name])
	}
	return out
}

func usageParts(u *acp.UsageUpdate) (string, bool) {
	if u == nil || u.Size <= 0 {
		return "", false
	}
	fill := int(math.Round(float64(u.Used) * 100 / float64(u.Size)))
	return strconv.Itoa(fill) + "% context", fill >= contextWarnPercent
}
