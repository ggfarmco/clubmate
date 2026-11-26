package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

	limitWarning  = "allowed_warning"
	limitRejected = "rejected"

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

type usageAlert struct {
	Window   string `json:"window"`
	Status   string `json:"status"`
	Used     int    `json:"used"`
	ResetsAt int64  `json:"resetsAt"`
}

type usageState struct {
	Account   string                 `json:"account,omitempty"`
	Email     string                 `json:"accountEmail,omitempty"`
	Windows   map[string]usageWindow `json:"windows"`
	Alert     *usageAlert            `json:"alert,omitempty"`
	UpdatedAt int64                  `json:"updatedAt,omitempty"`
}

type claudeConfig struct {
	OAuthAccount struct {
		AccountUUID      string `json:"accountUuid"`
		OrganizationUUID string `json:"organizationUuid"`
		EmailAddress     string `json:"emailAddress"`
	} `json:"oauthAccount"`
}

type accountCache struct {
	path  string
	mod   time.Time
	size  int64
	id    string
	email string
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
	host    *pluginsdk.Host
	mu      sync.Mutex
	pubMu   sync.Mutex
	state   usageState
	share   shareState
	shown   map[string]string
	account accountCache
	onShare func(sessions []string)
	tree    func() spendTree
}

type spendTree map[string][]string

func (t spendTree) of(id string) []string {
	if ids, ok := t[id]; ok {
		return ids
	}
	return []string{id}
}

func spendParent(rec record) string {
	if rec.ParentSession != "" {
		return rec.ParentSession
	}
	if rec.GateFor != nil {
		return rec.GateFor.Lander
	}
	return ""
}

func spendTreeOf(recs []record) spendTree {
	kids := map[string][]string{}
	for _, rec := range recs {
		if p := spendParent(rec); p != "" && p != rec.ID {
			kids[p] = append(kids[p], rec.ID)
		}
	}
	return spendTreeFrom(kids)
}

func spendTreeFrom(kids map[string][]string) spendTree {
	out := spendTree{}
	for id := range kids {
		ids, seen := []string{id}, map[string]bool{id: true}
		for i := 0; i < len(ids); i++ {
			for _, kid := range kids[ids[i]] {
				if !seen[kid] {
					seen[kid] = true
					ids = append(ids, kid)
				}
			}
		}
		out[id] = ids
	}
	return out
}

func (a *app) spendTree() spendTree {
	return a.roster.spendTree(a)
}

func (u *usageTracker) lineage() spendTree {
	if u.tree == nil {
		return nil
	}
	return u.tree()
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
	u.shown = u.share.keys(time.Now(), nil)
	return nil
}

func (u *usageTracker) snapshot() usageState {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := u.state
	now := time.Now().Unix()
	out.Windows = map[string]usageWindow{}
	for name, w := range u.state.Windows {
		if w.ResetsAt > now {
			out.Windows[name] = w
		}
	}
	out.Alert = alertOf(out.Windows)
	return out
}

func alertOf(windows map[string]usageWindow) *usageAlert {
	for _, status := range []string{limitRejected, limitWarning} {
		for _, name := range usageWindows {
			if w, ok := windows[name]; ok && w.Status == status {
				return &usageAlert{Window: name, Status: status, Used: w.Used, ResetsAt: w.ResetsAt}
			}
		}
	}
	return nil
}

func (u *usageTracker) accountLocked() (string, string) {
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), ".claude.json")
	if os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		path = filepath.Join(home, ".claude.json")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", ""
	}
	if c := u.account; c.path == path && c.mod.Equal(info.ModTime()) && c.size == info.Size() {
		return c.id, c.email
	}
	u.account = accountCache{path: path, mod: info.ModTime(), size: info.Size()}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var cfg claudeConfig
	if json.Unmarshal(raw, &cfg) != nil || cfg.OAuthAccount.AccountUUID == "" {
		return "", ""
	}
	sum := sha256.Sum256([]byte(cfg.OAuthAccount.AccountUUID + ":" + cfg.OAuthAccount.OrganizationUUID))
	u.account.id, u.account.email = hex.EncodeToString(sum[:8]), cfg.OAuthAccount.EmailAddress
	return u.account.id, u.account.email
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
	tree := u.lineage()
	u.mu.Lock()
	if len(windows) > 0 {
		if account, email := u.accountLocked(); account != "" {
			if account != u.state.Account && u.state.Account != "" {
				u.state.Windows = nil
				u.share.Windows = nil
			}
			u.state.Account, u.state.Email = account, email
		}
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
	changed := u.shareChangesLocked(now, tree)
	host, onShare := u.host, u.onShare
	u.mu.Unlock()
	if host != nil {
		go u.publish(host, len(windows) > 0)
	}
	if onShare != nil && len(changed) > 0 {
		onShare(changed)
	}
}

func (u *usageTracker) shareChangesLocked(now time.Time, tree spendTree) []string {
	cur := u.share.keys(now, tree)
	var out []string
	for id, key := range cur {
		if u.shown[id] != key {
			out = append(out, id)
		}
	}
	for id := range u.shown {
		if _, ok := cur[id]; !ok {
			out = append(out, id)
		}
	}
	u.shown = cur
	slices.Sort(out)
	return out
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

func (u *usageTracker) sessionShare(ids []string) map[string]float64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.share.of(ids, time.Now())
}

func (u *usageTracker) fold(child, parent string) {
	if parent == "" {
		return
	}
	u.mu.Lock()
	moved := u.share.fold(child, parent)
	host := u.host
	u.mu.Unlock()
	if moved && host != nil {
		go u.publish(host, false)
	}
}

func (s *shareState) fold(child, parent string) bool {
	moved := false
	for _, w := range s.Windows {
		if spent, ok := w.Sessions[child]; ok {
			w.Sessions[parent] += spent
			delete(w.Sessions, child)
			moved = true
		}
	}
	return moved
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

func (s *shareState) of(ids []string, now time.Time) map[string]float64 {
	out := map[string]float64{}
	for name, w := range s.Windows {
		spent := 0.0
		for _, id := range ids {
			spent += w.Sessions[id]
		}
		if spent > 0 && w.Total > 0 && w.Last > w.Base && now.Unix() < w.ResetsAt {
			if v := math.Round((w.Last-w.Base)*1000*spent/w.Total) / 10; v > 0 {
				out[name] = v
			}
		}
	}
	return out
}

func (s *shareState) keys(now time.Time, tree spendTree) map[string]string {
	out := map[string]string{}
	for id, share := range s.all(now, tree) {
		key := ""
		for _, name := range usageWindows {
			key += name + "=" + shareAmount(share[name]) + ";"
		}
		out[id] = key
	}
	return out
}

func (s *shareState) all(now time.Time, tree spendTree) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	seen := map[string]bool{}
	add := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if share := s.of(tree.of(id), now); len(share) > 0 {
			out[id] = share
		}
	}
	for _, w := range s.Windows {
		for session := range w.Sessions {
			add(session)
		}
	}
	for id := range tree {
		add(id)
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

func (a *app) sharesChanged(ids []string) {
	for _, id := range ids {
		if s := a.session(id); s != nil {
			s.changed(changeUsage, false)
		}
	}
}

func (a *app) usageCommand(context.Context, pluginsdk.CommandRun) (any, error) {
	state := a.usage.snapshot()
	tree := a.spendTree()
	a.usage.mu.Lock()
	sessions := a.usage.share.all(time.Now(), tree)
	a.usage.mu.Unlock()
	return usageReport{usageState: state, Sessions: sessions}, nil
}

func shareParts(share map[string]float64) []string {
	var out []string
	for _, name := range usageWindows {
		if v, ok := share[name]; ok && v > 0 {
			out = append(out, shareAmount(v)+" of "+shareLabels[name])
		}
	}
	return out
}

func shareAmount(v float64) string {
	switch {
	case v <= 0:
		return ""
	case v < 0.5:
		return "<1%"
	}
	return "≈" + strconv.Itoa(int(math.Round(v))) + "%"
}

func shareWords(share map[string]float64) map[string]string {
	out := map[string]string{}
	for _, name := range usageWindows {
		if word := shareAmount(share[name]); word != "" {
			out[name] = word
		}
	}
	return out
}

func shareDetail(share map[string]float64) string {
	return strings.Join(shareParts(share), " · ")
}

func shareLabel(share map[string]float64) string {
	if v := share[windowFiveHour]; v >= 0.5 {
		return shareAmount(v) + " of " + shareLabels[windowFiveHour]
	}
	return ""
}

func usageParts(u *acp.UsageUpdate) (string, bool) {
	fill, ok := contextFill(u)
	if !ok {
		return "", false
	}
	return strconv.Itoa(fill) + "% context", fill >= contextWarnPercent
}

func contextFill(u *acp.UsageUpdate) (int, bool) {
	if u == nil || u.Size <= 0 {
		return 0, false
	}
	return int(math.Round(float64(u.Used) * 100 / float64(u.Size))), true
}
