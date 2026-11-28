package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clubmate/internal/proc"
	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/agents/acp"
)

const (
	slotsConfigKey   = "slots.config"
	slotsStateKey    = "slots.state"
	slotPollEnv      = "CLUBMATE_AGENTS_SLOT_POLL"
	slotSilentEnv    = "CLUBMATE_AGENTS_SLOT_SILENT"
	slotTickEnv      = "CLUBMATE_AGENTS_SLOT_TICK"
	slotMaxWaitEnv   = "CLUBMATE_AGENTS_SLOT_MAXWAIT"
	slotDefaultEnv   = "CLUBMATE_AGENTS_SLOTS_DEFAULT"
	slotDefaultOff   = "off"
	slotPollFor      = 20 * time.Second
	slotSilentFor    = 45 * time.Second
	slotTickEvery    = 2 * time.Second
	slotMaxWait      = 8 * time.Minute
	slotWaitMin      = time.Minute
	slotWaitMax      = time.Hour
	slotSettle       = 20 * time.Second
	slotCPU          = 80
	slotSimMem       = 4
	slotSimMax       = 2
	slotCoresPerTest = 6
	slotCommandRunes = 120
	slotCPUStep      = 5
	slotRequestKind  = "sr"
	slotStepKind     = "ss"
	slotStaleText    = "[Clubmate slots] this slot request was made before the agent restarted, so it is dropped. Run the command again."

	slotNone    = "none"
	slotWait    = "wait"
	slotGranted = "granted"
	slotDenied  = "denied"

	countProcesses  = "processes"
	countSimulators = "simulators"
	kindTest        = "test"
	kindSim         = "sim"

	slotWhyCount     = "%d of %d in use"
	slotWhyCPU       = "CPU %.0f %% > %g %%"
	slotWhyMemory    = "%.1f GB free < %g GB"
	slotWhySettle    = "settle pause, %s of %s"
	slotDeniedFormat = "[Clubmate slots] no %s slot in %s: %s. %sRun it again later, or end your turn and tell the user."
	slotByFormat     = "%d of %d in use by %s"
	slotSimFormat    = "Booted simulators: %s; shut one down that you don't use (scripts/sim.sh down <name>). "
	slotLineFormat   = "%s · %d of %d in use · CPU %.0f %%"
	slotLineWaiting  = " · %d waiting"
	slotWaitWords    = "waits for %s slot"
	slotWaitLine     = "Waits for %s slot"
	slotRowWord      = "%s slot"
	slotNextInLine   = "next in line"
	slotAhead        = "%d ahead"
	slotNotRun       = "not run · no %s slot in %s"
	slotWaitGlyph    = "hourglass"
	slotWordRunes    = 9
	slotHeldBy       = "Held by "
	slotWaited       = "waited %s of %d"
	errSlotIDLong    = "An Id has at most 9 characters: a–z, 0–9 or -. Rename it to 9 characters or fewer."
	slotNobody       = "nobody"
)

var (
	errSlotKind     = errors.New("a kind needs an id of lowercase letters, digits or -, unique")
	errSlotMax      = errors.New("max is 1 or more")
	errSlotCPU      = errors.New("cpu is from 0 to 100 (0: no check)")
	errSlotNegative = errors.New("memory floor and settle pause are 0 or more")
	errSlotCount    = errors.New("count is processes or simulators")
	errSlotPattern  = errors.New("a pattern is not a valid regular expression")
	errSlotWait     = errors.New("max wait is from 1 to 60 minutes")
	errSlotUnknown  = errors.New("no such slot holder")

	slotIDPattern  = regexp.MustCompile(`^[a-z0-9-]+$`)
	slotDeniedText = regexp.MustCompile(`\[Clubmate slots\] no (\S+) slot in ([^:]+):`)
	simUpTarget    = regexp.MustCompile(`\bsim\.sh\s+up\s+([^\s;&|]+)`)
	simBootTarget  = regexp.MustCompile(`\bsimctl\s+boot\s+([^\s;&|]+)`)
	testPatterns   = []string{`\bmake\s+(test|test-stress|lint-tests)\b`, `\bgo\s+test\b`, `\bxcodebuild\b.*\btest\b`, `\bswift\s+test\b`, `\bcargo\s+(test|nextest)\b`}
	simPatterns    = []string{`\bsim\.sh\s+up\b`, `\bsimctl\s+boot\b`, `\bmake\s+companion-ui-test\b`}
	slotCountKinds = []string{countProcesses, countSimulators}
)

type seconds time.Duration

func (s seconds) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(s).Seconds())
}

func (s *seconds) UnmarshalJSON(raw []byte) error {
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return err
	}
	*s = seconds(f * float64(time.Second))
	return nil
}

type slotKind struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Max       int      `json:"max"`
	CPU       float64  `json:"cpu"`
	MemFreeGB float64  `json:"memFreeGB"`
	Settle    seconds  `json:"settle"`
	Count     string   `json:"count"`
	Patterns  []string `json:"patterns"`
}

type slotConfig struct {
	Enabled bool       `json:"enabled"`
	MaxWait seconds    `json:"maxWait"`
	Kinds   []slotKind `json:"kinds"`
}

type slotHolder struct {
	ID       string    `json:"id"`
	Request  string    `json:"request"`
	Kind     string    `json:"kind"`
	Session  string    `json:"session,omitempty"`
	Call     string    `json:"call,omitempty"`
	Task     string    `json:"task,omitempty"`
	Step     bool      `json:"step,omitempty"`
	Label    string    `json:"label,omitempty"`
	PID      int       `json:"pid,omitempty"`
	PIDStart time.Time `json:"pidStart,omitzero"`
	Command  string    `json:"command,omitempty"`
	Since    time.Time `json:"since"`
}

type slotWaiter struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Rest     []string  `json:"rest,omitempty"`
	Session  string    `json:"session,omitempty"`
	Call     string    `json:"call,omitempty"`
	Step     bool      `json:"step,omitempty"`
	Label    string    `json:"label,omitempty"`
	Command  string    `json:"command,omitempty"`
	Target   string    `json:"target,omitempty"`
	Since    time.Time `json:"since"`
	LastPoll time.Time `json:"lastPoll"`
	Agent    time.Time `json:"agent,omitzero"`
	Why      string    `json:"why,omitempty"`
}

type slotState struct {
	Holders   []slotHolder         `json:"holders"`
	Waiters   []slotWaiter         `json:"waiters"`
	LastGrant map[string]time.Time `json:"lastGrant,omitempty"`
}

type slotBoard struct {
	mu       sync.Mutex
	cfg      slotConfig
	st       slotState
	res      map[string]*regexp.Regexp
	owned    map[string]bool
	stale    map[string]bool
	wake     chan struct{}
	gen      uint64
	saveMu   sync.Mutex
	saved    uint64
	host     hostLoad
	poll     time.Duration
	silent   time.Duration
	tick     time.Duration
	limit    time.Duration
	live     func(slotWaiter) bool
	draft    *slotDraft
	shown    float64
	minutes  string
	unread   bool
	whyMoved bool
}

type slotAcquireArgs struct {
	Session string `json:"session"`
	Call    string `json:"call"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
	Request string `json:"request"`
}

type slotAnswer struct {
	State   string `json:"state"`
	Request string `json:"request,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Place   int    `json:"place,omitempty"`
	Of      int    `json:"of,omitempty"`
	Why     string `json:"why,omitempty"`
	Text    string `json:"text,omitempty"`
}

type slotReleaseArgs struct {
	ID string `json:"id"`
}

type slotConfigArgs struct {
	Enabled *bool       `json:"enabled"`
	MaxWait *seconds    `json:"maxWait"`
	Kinds   *[]slotKind `json:"kinds"`
	Reset   string      `json:"reset"`
}

type slotWaitView struct {
	Kind    string        `json:"kind"`
	Place   int           `json:"place"`
	Of      int           `json:"of"`
	Why     string        `json:"why,omitempty"`
	Words   string        `json:"words"`
	Since   time.Time     `json:"since"`
	Calls   []string      `json:"-"`
	Held    *slotHeldView `json:"-"`
	MaxWait time.Duration `json:"-"`
}

type slotHeldView struct {
	Session   string
	Workspace string
	Title     string
	Command   string
	Since     time.Time
}

type slotHoldView struct {
	Kind  string    `json:"kind"`
	Since time.Time `json:"since"`
}

type slotsView struct {
	Enabled   bool           `json:"enabled"`
	MaxWait   seconds        `json:"maxWait"`
	CPU       float64        `json:"cpu"`
	MemFreeGB float64        `json:"memFreeGB"`
	Booted    []string       `json:"booted"`
	Kinds     []slotKindView `json:"kinds"`
}

type slotKindView struct {
	slotKind
	InUse   int              `json:"inUse"`
	Line    string           `json:"line"`
	Holders []slotHolderView `json:"holders"`
	Waiters []slotWaiterView `json:"waiters"`
}

type slotHolderView struct {
	ID      string    `json:"id"`
	Session string    `json:"session,omitempty"`
	Title   string    `json:"title"`
	Command string    `json:"command,omitempty"`
	PID     int       `json:"pid,omitempty"`
	Since   time.Time `json:"since"`
}

type slotWaiterView struct {
	Request string    `json:"request"`
	Session string    `json:"session,omitempty"`
	Title   string    `json:"title"`
	Command string    `json:"command,omitempty"`
	Place   int       `json:"place"`
	Why     string    `json:"why,omitempty"`
	Since   time.Time `json:"since"`
}

func defaultSlotKinds(cores int) []slotKind {
	return []slotKind{
		{ID: kindSim, Label: "Simulators", Max: slotSimMax, CPU: slotCPU, MemFreeGB: slotSimMem, Settle: seconds(slotSettle), Count: countSimulators, Patterns: slices.Clone(simPatterns)},
		{ID: kindTest, Label: "Tests", Max: max(1, cores/slotCoresPerTest), CPU: slotCPU, Settle: seconds(slotSettle), Count: countProcesses, Patterns: slices.Clone(testPatterns)},
	}
}

func defaultSlotConfig(cores int) slotConfig {
	return slotConfig{Enabled: true, MaxWait: seconds(slotMaxWait), Kinds: defaultSlotKinds(cores)}
}

func (c slotConfig) clone() slotConfig {
	c.Kinds = slices.Clone(c.Kinds)
	for i := range c.Kinds {
		c.Kinds[i].Patterns = slices.Clone(c.Kinds[i].Patterns)
	}
	return c
}

func (c slotConfig) kind(id string) (slotKind, bool) {
	i := slices.IndexFunc(c.Kinds, func(k slotKind) bool { return k.ID == id })
	if i < 0 {
		return slotKind{}, false
	}
	return c.Kinds[i], true
}

func (c *slotConfig) validate() (map[string]*regexp.Regexp, error) {
	if d := time.Duration(c.MaxWait); d < slotWaitMin || d > slotWaitMax {
		return nil, errSlotWait
	}
	res := map[string]*regexp.Regexp{}
	seen := map[string]bool{}
	for i := range c.Kinds {
		k := &c.Kinds[i]
		k.ID, k.Label = strings.TrimSpace(k.ID), strings.TrimSpace(k.Label)
		switch {
		case !slotIDPattern.MatchString(k.ID) || seen[k.ID]:
			return nil, fmt.Errorf("%w: %q", errSlotKind, k.ID)
		case k.Max < 1:
			return nil, fmt.Errorf("%s: %w", k.ID, errSlotMax)
		case k.CPU < 0 || k.CPU > 100:
			return nil, fmt.Errorf("%s: %w", k.ID, errSlotCPU)
		case k.MemFreeGB < 0 || k.Settle < 0:
			return nil, fmt.Errorf("%s: %w", k.ID, errSlotNegative)
		case !slices.Contains(slotCountKinds, k.Count):
			return nil, fmt.Errorf("%s: %w", k.ID, errSlotCount)
		}
		seen[k.ID] = true
		k.Label = cmp.Or(k.Label, k.ID)
		k.Patterns = slices.DeleteFunc(k.Patterns, func(p string) bool { return strings.TrimSpace(p) == "" })
		for _, p := range k.Patterns {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, fmt.Errorf("%s: %w: %w", k.ID, errSlotPattern, err)
			}
			res[p] = re
		}
	}
	slices.SortFunc(c.Kinds, func(x, y slotKind) int { return strings.Compare(x.ID, y.ID) })
	return res, nil
}

func grantable(k slotKind, inUse int, load hostLoad, sinceLast time.Duration) (bool, string) {
	switch {
	case inUse >= k.Max:
		return false, fmt.Sprintf(slotWhyCount, inUse, k.Max)
	case inUse == 0:
		return true, ""
	}
	if cpu := load.CPU(); k.CPU > 0 && cpu > k.CPU {
		return false, fmt.Sprintf(slotWhyCPU, cpu, k.CPU)
	}
	if free := load.MemFreeGB(); k.MemFreeGB > 0 && free < k.MemFreeGB {
		return false, fmt.Sprintf(slotWhyMemory, free, k.MemFreeGB)
	}
	if settle := time.Duration(k.Settle); sinceLast < settle {
		return false, fmt.Sprintf(slotWhySettle, sinceLast.Round(time.Second), settle)
	}
	return true, ""
}

func (a *app) loadSlots(ctx context.Context) {
	b := &a.resources
	b.host = newHostLoad()
	b.poll = envDelay(slotPollEnv, slotPollFor)
	b.silent = envDelay(slotSilentEnv, slotSilentFor)
	b.tick = envDelay(slotTickEnv, slotTickEvery)
	b.limit = envDelay(slotMaxWaitEnv, 0)
	cfg := defaultSlotConfig(runtime.NumCPU())
	cfg.Enabled = os.Getenv(slotDefaultEnv) != slotDefaultOff
	var stored slotConfig
	if found, err := a.host.StorageGet(ctx, slotsConfigKey, &stored); err != nil {
		logError("read the slot config", err)
	} else if found {
		if _, err := stored.validate(); err == nil {
			cfg = stored
		} else {
			logError("the stored slot config", err)
		}
	}
	res, _ := cfg.validate()
	b.mu.Lock()
	b.cfg, b.res, b.owned, b.wake, b.live = cfg, res, map[string]bool{}, make(chan struct{}), a.waiterLive
	b.mu.Unlock()
}

func (a *app) restoreSlots(ctx context.Context) {
	var st slotState
	found, err := a.host.StorageGet(ctx, slotsStateKey, &st)
	a.adoptSlotState(ctx, st, found, err)
}

func (a *app) adoptSlotState(ctx context.Context, st slotState, found bool, err error) {
	b := &a.resources
	if err != nil {
		if !found {
			b.mu.Lock()
			first := !b.unread
			b.unread = true
			b.mu.Unlock()
			if first {
				logError("read the slot state", err)
			}
			return
		}
		logError("decode the slot state", err)
		st = slotState{}
	}
	b.mu.Lock()
	b.stale = map[string]bool{}
	for _, w := range st.Waiters {
		b.stale[w.ID] = true
	}
	st.Waiters = b.st.Waiters
	b.st = st
	b.unread = false
	a.settleSlotsLocked(time.Now())
	b.touchLocked()
	b.mu.Unlock()
	a.saveSlots(ctx)
}

func (a *app) slotsOn() bool {
	b := &a.resources
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.Enabled
}

func (a *app) slotWaitLimit() time.Duration {
	b := &a.resources
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Duration(b.cfg.MaxWait)
}

func (a *app) slotConfig() slotConfig {
	b := &a.resources
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg.clone()
}

func (a *app) setSlotConfig(ctx context.Context, cfg slotConfig) error {
	cfg = cfg.clone()
	res, err := cfg.validate()
	if err != nil {
		return err
	}
	if err := a.host.StorageSet(ctx, slotsConfigKey, cfg); err != nil {
		return err
	}
	b := &a.resources
	b.mu.Lock()
	before := b.cfg
	b.cfg, b.res = cfg, res
	if !cfg.Enabled {
		b.st.Waiters = nil
	}
	a.settleSlotsLocked(time.Now())
	b.touchLocked()
	b.mu.Unlock()
	a.saveSlots(ctx)
	if before.Enabled != cfg.Enabled || before.MaxWait != cfg.MaxWait {
		logError("write the tool hooks", a.syncToolHooks(ctx))
	}
	if cfg.Enabled && !before.Enabled {
		go a.rehookAll()
	}
	a.slotsChanged()
	return nil
}

func (a *app) rehookAll() {
	for _, s := range a.allSessions() {
		a.rehookSession(s)
	}
}

func (b *slotBoard) maxWaitLocked() time.Duration {
	return cmp.Or(b.limit, time.Duration(b.cfg.MaxWait))
}

func (b *slotBoard) touchLocked() {
	b.gen++
	close(b.wake)
	b.wake = make(chan struct{})
}

func (a *app) saveSlots(ctx context.Context) {
	b := &a.resources
	b.mu.Lock()
	if b.unread {
		b.mu.Unlock()
		return
	}
	gen := b.gen
	st := slotState{Holders: slices.Clone(b.st.Holders), Waiters: slices.Clone(b.st.Waiters), LastGrant: b.st.LastGrant}
	b.mu.Unlock()
	b.saveMu.Lock()
	defer b.saveMu.Unlock()
	if gen < b.saved {
		return
	}
	if err := a.host.StorageSet(context.WithoutCancel(ctx), slotsStateKey, st); err != nil {
		logError("store the slot state", err)
		return
	}
	b.saved = gen + 1
}

func (b *slotBoard) match(command string) []string {
	var kinds []string
	for _, k := range b.cfg.Kinds {
		if slices.ContainsFunc(k.Patterns, func(p string) bool { re := b.res[p]; return re != nil && re.MatchString(command) }) {
			kinds = append(kinds, k.ID)
		}
	}
	return kinds
}

func simTarget(command string) string {
	for _, re := range []*regexp.Regexp{simUpTarget, simBootTarget} {
		if m := re.FindStringSubmatch(command); m != nil {
			return strings.Trim(m[1], `'"`)
		}
	}
	return ""
}

func (b *slotBoard) booted(target string) bool {
	return target != "" && slices.ContainsFunc(b.host.BootedSims(), func(s simDevice) bool { return s.Name == target || s.UDID == target })
}

func (b *slotBoard) needsSimsLocked(w slotWaiter) bool {
	sim := func(kind string) bool {
		k, ok := b.cfg.kind(kind)
		return ok && k.Count == countSimulators
	}
	if slices.ContainsFunc(b.st.Waiters, func(x slotWaiter) bool { return sim(x.Kind) }) || slices.ContainsFunc(b.st.Holders, func(h slotHolder) bool { return sim(h.Kind) }) {
		return true
	}
	return w.Command != "" && slices.ContainsFunc(b.match(w.Command), sim)
}

func (a *app) refreshSims(w slotWaiter) {
	b := &a.resources
	m, ok := b.host.(*machineLoad)
	if !ok {
		return
	}
	b.mu.Lock()
	need := b.cfg.Enabled && b.needsSimsLocked(w)
	idle := b.cfg.Enabled && slices.ContainsFunc(b.cfg.Kinds, func(k slotKind) bool { return k.Count == countSimulators })
	b.mu.Unlock()
	switch {
	case need:
		m.refreshSims(time.Now(), simsCacheFor)
	case idle:
		m.refreshSims(time.Now(), simsIdleFor)
	}
}

func (b *slotBoard) inUseLocked(k slotKind) int {
	n := 0
	for _, h := range b.st.Holders {
		if h.Kind == k.ID {
			n++
		}
	}
	if k.Count == countSimulators {
		n = max(n, len(agentSims(b.host.BootedSims())))
	}
	return n
}

func (b *slotBoard) waiterIndex(request string) int {
	return slices.IndexFunc(b.st.Waiters, func(w slotWaiter) bool { return w.ID == request })
}

func (b *slotBoard) placeLocked(request string) (int, int) {
	i := b.waiterIndex(request)
	if i < 0 {
		return 0, 0
	}
	kind := b.st.Waiters[i].Kind
	place, of := 0, 0
	for j, w := range b.st.Waiters {
		if w.Kind == kind {
			of++
			if j <= i {
				place++
			}
		}
	}
	return place, of
}

func (b *slotBoard) advanceLocked(i int, now time.Time) {
	w := b.st.Waiters[i]
	b.st.Waiters = slices.Delete(b.st.Waiters, i, i+1)
	if len(w.Rest) == 0 {
		return
	}
	w.Kind, w.Rest, w.Why = w.Rest[0], w.Rest[1:], ""
	b.st.Waiters = append(b.st.Waiters, w)
}

func (b *slotBoard) grantLocked(now time.Time) bool {
	if b.unread {
		return false
	}
	changed := false
	for i := 0; i < len(b.st.Waiters); {
		w := b.st.Waiters[i]
		if k, ok := b.cfg.kind(w.Kind); !ok || k.Count == countSimulators && b.booted(w.Target) {
			b.advanceLocked(i, now)
			changed = true
			continue
		}
		i++
	}
	for _, k := range b.cfg.Kinds {
		for {
			ok, why := grantable(k, b.inUseLocked(k), b.host, now.Sub(b.st.LastGrant[k.ID]))
			i := slices.IndexFunc(b.st.Waiters, func(w slotWaiter) bool { return w.Kind == k.ID && (b.live == nil || b.live(w)) })
			if !ok || i < 0 {
				for j := range b.st.Waiters {
					if b.st.Waiters[j].Kind == k.ID && b.st.Waiters[j].Why != why {
						b.st.Waiters[j].Why = why
						b.whyMoved = true
					}
				}
				break
			}
			w := b.st.Waiters[i]
			h := slotHolder{ID: w.ID + ":" + k.ID, Request: w.ID, Kind: k.ID, Session: w.Session, Call: w.Call, Step: w.Step, Label: w.Label, Command: w.Command, Since: now}
			b.st.Holders = append(b.st.Holders, h)
			if w.Step {
				b.owned[h.ID] = true
			}
			if b.st.LastGrant == nil {
				b.st.LastGrant = map[string]time.Time{}
			}
			b.st.LastGrant[k.ID] = now
			b.advanceLocked(i, now)
			changed = true
		}
	}
	return changed
}

func (b *slotBoard) dropRequestLocked(request string) {
	b.st.Waiters = slices.DeleteFunc(b.st.Waiters, func(w slotWaiter) bool { return w.ID == request })
	b.st.Holders = slices.DeleteFunc(b.st.Holders, func(h slotHolder) bool {
		if h.Request == request {
			delete(b.owned, h.ID)
			return true
		}
		return false
	})
}

func (a *app) settleSlotsLocked(now time.Time) bool {
	b := &a.resources
	changed := false
	for _, w := range slices.Clone(b.st.Waiters) {
		if now.Sub(w.LastPoll) > b.silent {
			b.dropRequestLocked(w.ID)
			changed = true
		}
	}
	held := len(b.st.Holders)
	b.st.Holders = slices.DeleteFunc(b.st.Holders, func(h slotHolder) bool {
		if a.holderGone(b, h) {
			delete(b.owned, h.ID)
			return true
		}
		return false
	})
	changed = changed || held != len(b.st.Holders)
	if b.cfg.Enabled && b.grantLocked(now) {
		changed = true
	}
	return changed
}

func (a *app) waiterLive(w slotWaiter) bool {
	if w.Step {
		return true
	}
	s := a.session(w.Session)
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proc != nil && !s.rec.closed() && (w.Agent.IsZero() || w.Agent.Equal(s.attached))
}

func (a *app) attachedAt(id string) time.Time {
	s := a.session(id)
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attached
}

func (a *app) holderGone(b *slotBoard, h slotHolder) bool {
	if h.Step {
		if h.PID == 0 {
			return !b.owned[h.ID]
		}
		start, err := proc.StartTime(h.PID)
		return err != nil || !start.Equal(h.PIDStart)
	}
	s := a.session(h.Session)
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch {
	case s.rec.closed() || s.deleted || s.proc == nil || h.Since.Before(s.attached):
		return true
	case h.Task != "":
		return !s.background[h.Task]
	}
	return !s.workingLocked()
}

func (a *app) slotLoop() {
	t := time.NewTicker(a.resources.tick)
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case now := <-t.C:
			b := &a.resources
			if m, ok := b.host.(*machineLoad); ok {
				m.sample(now)
			}
			a.refreshSims(slotWaiter{})
			b.mu.Lock()
			unread := b.unread
			b.mu.Unlock()
			if unread {
				a.restoreSlots(context.Background())
			}
			b.mu.Lock()
			cpuMoved := b.cfg.Enabled && math.Abs(b.host.CPU()-b.shown) >= slotCPUStep
			changed := a.settleSlotsLocked(now)
			if changed {
				b.touchLocked()
			}
			minuteMoved := b.minuteMovedLocked(now)
			whyMoved := b.whyMoved || minuteMoved
			b.whyMoved = false
			b.mu.Unlock()
			if changed {
				a.saveSlots(context.Background())
				a.slotsChanged()
			} else if whyMoved {
				a.slotsChanged()
			} else if cpuMoved {
				a.refreshSettings(context.Background())
			}
		}
	}
}

func (a *app) slotsChanged() {
	a.refreshSettings(context.Background())
	seen := map[string]bool{}
	for _, s := range a.allSessions() {
		if ws := s.snapshot().Workspace; !seen[ws] {
			seen[ws] = true
			a.kick(ws)
		}
	}
}

func (a *app) slotAcquireCommand(ctx context.Context, cmd pluginsdk.CommandRun, args slotAcquireArgs) (any, error) {
	session := cmp.Or(cmd.Caller, args.Session)
	if session == "" || a.session(session) == nil {
		return slotAnswer{State: slotNone}, nil
	}
	w := slotWaiter{ID: args.Request, Session: session, Call: args.Call, Command: clipRunes(oneLine(args.Command), slotCommandRunes), Target: simTarget(args.Command)}
	return a.acquire(ctx, w, nil), nil
}

func (a *app) acquire(ctx context.Context, w slotWaiter, kinds []string) slotAnswer {
	b := &a.resources
	a.refreshSims(w)
	deadline := time.Now().Add(b.poll)
	b.mu.Lock()
	if !b.cfg.Enabled {
		b.mu.Unlock()
		if w.ID == "" {
			return slotAnswer{State: slotNone}
		}
		return slotAnswer{State: slotGranted, Request: w.ID}
	}
	now := time.Now()
	joined := false
	if b.waiterIndex(w.ID) < 0 {
		if w.ID != "" && slices.ContainsFunc(b.st.Holders, func(h slotHolder) bool { return h.Request == w.ID }) {
			b.mu.Unlock()
			return slotAnswer{State: slotGranted, Request: w.ID}
		}
		if b.stale[w.ID] {
			delete(b.stale, w.ID)
			b.mu.Unlock()
			return slotAnswer{State: slotDenied, Request: w.ID, Text: slotStaleText}
		}
		if kinds == nil {
			kinds = b.match(w.Command)
		}
		if len(kinds) == 0 {
			b.mu.Unlock()
			return slotAnswer{State: slotNone}
		}
		w.ID = cmp.Or(w.ID, id.New(slotRequestKind))
		w.Kind, w.Rest, w.Since, w.LastPoll, w.Agent = kinds[0], kinds[1:], now, now, a.attachedAt(w.Session)
		b.st.Waiters = append(b.st.Waiters, w)
		b.touchLocked()
		joined = true
	}
	request := w.ID
	for {
		now = time.Now()
		if !b.cfg.Enabled {
			b.dropRequestLocked(request)
			b.touchLocked()
			b.mu.Unlock()
			a.saveSlots(ctx)
			return slotAnswer{State: slotGranted, Request: request}
		}
		i := b.waiterIndex(request)
		if i >= 0 {
			if old := b.st.Waiters[i]; !old.Agent.IsZero() && !old.Agent.Equal(a.attachedAt(old.Session)) {
				b.dropRequestLocked(request)
				b.touchLocked()
				b.mu.Unlock()
				a.saveSlots(ctx)
				a.slotsChanged()
				return slotAnswer{State: slotDenied, Request: request, Text: slotStaleText}
			}
			b.st.Waiters[i].LastPoll = now
		}
		changed := b.grantLocked(now)
		if changed {
			b.touchLocked()
		}
		changed, joined = changed || joined, false
		i = b.waiterIndex(request)
		if i < 0 {
			b.gen++
			b.mu.Unlock()
			a.saveSlots(ctx)
			a.slotsChanged()
			return slotAnswer{State: slotGranted, Request: request}
		}
		cur := b.st.Waiters[i]
		limit := b.maxWaitLocked()
		if !cur.Step && now.Sub(cur.Since) >= limit {
			text := a.deniedTextLocked(cur)
			b.dropRequestLocked(request)
			b.touchLocked()
			b.mu.Unlock()
			a.saveSlots(ctx)
			a.slotsChanged()
			return slotAnswer{State: slotDenied, Request: request, Kind: cur.Kind, Why: cur.Why, Text: text}
		}
		if !now.Before(deadline) || ctx.Err() != nil {
			place, of := b.placeLocked(request)
			b.gen++
			b.mu.Unlock()
			a.saveSlots(ctx)
			if changed {
				a.slotsChanged()
			}
			return slotAnswer{State: slotWait, Request: request, Kind: cur.Kind, Place: place, Of: of, Why: cur.Why}
		}
		wake := b.wake
		b.mu.Unlock()
		if changed {
			a.saveSlots(ctx)
			a.slotsChanged()
		}
		until := deadline
		if !cur.Step {
			until = minTime(until, cur.Since.Add(limit))
		}
		timer := time.NewTimer(min(time.Until(until), b.tick))
		select {
		case <-wake:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		b.mu.Lock()
	}
}

func minTime(x, y time.Time) time.Time {
	if y.Before(x) {
		return y
	}
	return x
}

func (a *app) holderTitle(h slotHolder) string {
	if h.Step {
		return cmp.Or(h.Label, h.Command)
	}
	if s := a.session(h.Session); s != nil {
		return cmp.Or(s.snapshot().Title, h.Session)
	}
	return h.Session
}

func (a *app) deniedTextLocked(w slotWaiter) string {
	b := &a.resources
	k, _ := b.cfg.kind(w.Kind)
	var titles []string
	for _, h := range b.st.Holders {
		if h.Kind == k.ID {
			titles = append(titles, `"`+a.holderTitle(h)+`"`)
		}
	}
	by := strings.Join(titles, ", ")
	if by == "" {
		by = slotNobody
	}
	why := fmt.Sprintf(slotByFormat, b.inUseLocked(k), k.Max, by)
	if w.Why != "" && !strings.HasSuffix(w.Why, fmt.Sprintf(" of %d in use", k.Max)) {
		why += "; " + w.Why
	}
	sims := ""
	if k.Count == countSimulators {
		if names := agentSims(b.host.BootedSims()); len(names) > 0 {
			sims = fmt.Sprintf(slotSimFormat, strings.Join(names, ", "))
		}
	}
	return fmt.Sprintf(slotDeniedFormat, k.ID, waitText(b.maxWaitLocked()), why, sims)
}

func waitText(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	return d.String()
}

type slotCall struct {
	call  string
	task  string
	ended bool
}

func slotCallOf(kind string, payload json.RawMessage) *slotCall {
	if kind != acp.UpdateToolCall && kind != acp.UpdateToolCallUpdate {
		return nil
	}
	var u struct {
		backgroundUpdate
		Status string `json:"status"`
	}
	if json.Unmarshal(payload, &u) != nil || u.ToolCallID == "" {
		return nil
	}
	var res struct {
		BackgroundTaskID string `json:"backgroundTaskId"`
	}
	_ = json.Unmarshal(u.Meta.ClaudeCode.ToolResponse, &res)
	c := &slotCall{call: u.ToolCallID, task: res.BackgroundTaskID, ended: u.Status == toolCompleted || u.Status == toolFailed}
	if c.task == "" && !c.ended {
		return nil
	}
	return c
}

func (a *app) slotCallEnded(session string, c slotCall) {
	call, task := c.call, c.task
	b := &a.resources
	b.mu.Lock()
	changed := false
	for i := 0; i < len(b.st.Holders); {
		h := &b.st.Holders[i]
		if h.Session != session || h.Call != call || h.Task != "" {
			i++
			continue
		}
		changed = true
		if task != "" {
			h.Task = task
			i++
			continue
		}
		b.st.Holders = slices.Delete(b.st.Holders, i, i+1)
	}
	if changed {
		b.grantLocked(time.Now())
		b.touchLocked()
	}
	b.mu.Unlock()
	if changed {
		a.saveSlots(context.Background())
		a.slotsChanged()
	}
}

func (a *app) acquireStepSlot(ctx context.Context, kind, label string, note func(string)) (func(int), func(), error) {
	request := id.New(slotStepKind)
	w := slotWaiter{ID: request, Step: true, Label: label, Command: label}
	shown := ""
	for {
		ans := a.acquire(ctx, w, []string{kind})
		switch ans.State {
		case slotNone, slotGranted:
			return a.stepStarted(request), a.stepReleased(request), nil
		}
		if err := ctx.Err(); err != nil {
			a.stepReleased(request)()
			return nil, nil, err
		}
		if text := cmp.Or(ans.Why, fmt.Sprintf("place %d of %d", ans.Place, ans.Of)); text != shown {
			shown = text
			note(text)
		}
	}
}

func (a *app) stepStarted(request string) func(int) {
	return func(pid int) {
		start, err := proc.StartTime(pid)
		if err != nil {
			return
		}
		b := &a.resources
		b.mu.Lock()
		for i := range b.st.Holders {
			if b.st.Holders[i].Request == request {
				b.st.Holders[i].PID, b.st.Holders[i].PIDStart = pid, start
			}
		}
		b.gen++
		b.mu.Unlock()
		a.saveSlots(context.Background())
	}
}

func (a *app) stepReleased(request string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			b := &a.resources
			b.mu.Lock()
			b.dropRequestLocked(request)
			b.grantLocked(time.Now())
			b.touchLocked()
			b.mu.Unlock()
			a.saveSlots(context.Background())
			a.slotsChanged()
		})
	}
}

func (a *app) slotView(session string) (*slotWaitView, *slotHoldView) {
	b := &a.resources
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.cfg.Enabled {
		return nil, nil
	}
	var wait *slotWaitView
	for _, w := range b.st.Waiters {
		if w.Session != session {
			continue
		}
		if wait == nil {
			place, of := b.placeLocked(w.ID)
			wait = &slotWaitView{Kind: w.Kind, Place: place, Of: of, Why: w.Why, Words: fmt.Sprintf(slotWaitWords, w.Kind), Since: w.Since, MaxWait: b.maxWaitLocked(), Held: a.slotHeldLocked(w.Kind)}
		}
		if w.Call != "" {
			wait.Calls = append(wait.Calls, w.Call)
		}
	}
	var hold *slotHoldView
	for _, h := range b.st.Holders {
		if h.Session == session && (hold == nil || h.Since.Before(hold.Since)) {
			hold = &slotHoldView{Kind: h.Kind, Since: h.Since}
		}
	}
	return wait, hold
}

func (a *app) slotHeldLocked(kind string) *slotHeldView {
	var first *slotHolder
	for i, h := range a.resources.st.Holders {
		if h.Kind == kind && (first == nil || h.Since.Before(first.Since)) {
			first = &a.resources.st.Holders[i]
		}
	}
	if first == nil {
		return nil
	}
	v := &slotHeldView{Title: a.holderTitle(*first), Command: first.Command, Since: first.Since}
	if s := a.session(first.Session); s != nil && !first.Step {
		v.Session, v.Workspace = first.Session, s.snapshot().Workspace
	}
	return v
}

func (a *app) slotLanding(rec record) *landingView {
	w, _ := a.slotView(rec.ID)
	if w == nil {
		return nil
	}
	rest := nonEmpty(slotPlace(w.Place), w.Why)
	segments := append([]string{fmt.Sprintf(slotWaitLine, w.Kind)}, rest...)
	mac := append(append([]string{w.Words}, rest...), elapsed(time.Since(w.Since)))
	since := w.Since
	return &landingView{Words: slotRowWordOf(w.Kind), Badge: w.Words, Line: strings.Join(segments, " · "), Segments: segments, Since: &since, Glyph: slotWaitGlyph, MacLine: strings.Join(mac, " · "), MacSegments: mac}
}

func slotRowWordOf(kind string) string {
	if word := fmt.Sprintf(slotRowWord, kind); utf8.RuneCountInString(word) <= slotWordRunes {
		return word
	}
	return kind
}

func slotPlace(place int) string {
	switch {
	case place < 1:
		return ""
	case place == 1:
		return slotNextInLine
	}
	return fmt.Sprintf(slotAhead, place-1)
}

func (b *slotBoard) minuteMovedLocked(now time.Time) bool {
	var shown []string
	for _, w := range b.st.Waiters {
		shown = append(shown, w.ID+"="+elapsed(now.Sub(w.Since)))
	}
	if len(shown) > 0 {
		for _, h := range b.st.Holders {
			shown = append(shown, h.ID+"="+elapsed(now.Sub(h.Since)))
		}
	}
	key := strings.Join(shown, ",")
	moved := key != b.minutes && len(shown) > 0
	b.minutes = key
	return moved
}

func (a *app) slotWaits(session string, w *slotWaitView) bool {
	s := a.session(session)
	if w == nil || len(w.Calls) == 0 || s == nil {
		return false
	}
	open := s.openCalls()
	return len(open) > 0 && !slices.ContainsFunc(open, func(id string) bool { return !slices.Contains(w.Calls, id) })
}

func slotKey(w *slotWaitView, call string, now time.Time) string {
	if w == nil || !slices.Contains(w.Calls, call) {
		return ""
	}
	key := w.Kind + "/" + strconv.Itoa(w.Place) + "/" + elapsed(now.Sub(w.Since))
	if h := w.Held; h != nil {
		key += "/" + h.Session + "/" + h.Title + "/" + elapsed(now.Sub(h.Since))
	}
	return key
}

func slotStamp(w *slotWaitView, now time.Time) []string {
	if w == nil {
		return nil
	}
	var out []string
	for _, call := range slices.Sorted(slices.Values(w.Calls)) {
		out = append(out, call, slotKey(w, call, now))
	}
	return out
}

func slotOnly(w *slotWaitView, _ *slotHoldView) *slotWaitView {
	return w
}

func slotBlock(en *entry, w *slotWaitView, workspace string, now time.Time) *callBlock {
	if en.Kind != entryTool {
		return nil
	}
	if w != nil && slices.Contains(w.Calls, en.ID) {
		place := slotPlace(w.Place)
		b := &callBlock{State: callStateSlot, Word: w.Words, Icon: slotWaitGlyph, Line: strings.Join(nonEmpty(fmt.Sprintf(slotWaitLine, w.Kind), place), blockSep)}
		if place != "" {
			place = strings.ToUpper(place[:1]) + place[1:]
		}
		b.Wait = strings.Join(nonEmpty(place, fmt.Sprintf(slotWaited, elapsed(now.Sub(w.Since)), int(w.MaxWait/time.Minute))), blockSep)
		if h := w.Held; h != nil {
			b.Held = &callBlockHeld{Title: h.Title, Rest: strings.Join(nonEmpty(h.Command, elapsed(now.Sub(h.Since))), blockSep)}
			if h.Session != "" && h.Workspace == workspace {
				b.Held.Session = h.Session
			}
		}
		return b
	}
	var status string
	if json.Unmarshal(en.Tool["status"], &status) != nil || status != "failed" {
		return nil
	}
	f, _, _ := parseTool(en.Tool)
	for _, c := range f.Content {
		if m := slotDeniedText.FindStringSubmatch(c.Content.Text); m != nil {
			word := fmt.Sprintf(slotNotRun, m[1], m[2])
			return &callBlock{State: callStateNotRun, Word: word, Line: strings.ToUpper(word[:1]) + word[1:]}
		}
	}
	return nil
}

func slotCard(n sdui.Node, en *entry, w *slotWaitView, workspace string, now time.Time) sdui.Node {
	b := slotBlock(en, w, workspace, now)
	if b == nil {
		return n
	}
	n.Props["statusWord"], n.Props["statusLine"] = b.Word, b.Line
	if b.Icon != "" {
		n.Props["statusIcon"] = b.Icon
	}
	if b.Wait != "" {
		n.Props["slotWait"] = b.Wait
	}
	if h := b.Held; h != nil {
		held := map[string]any{"title": h.Title, "rest": h.Rest}
		if h.Session != "" {
			held["session"] = h.Session
		}
		n.Props["slotHeld"] = held
	}
	return n
}

func (a *app) slotsSnapshot() slotsView {
	b := &a.resources
	b.mu.Lock()
	defer b.mu.Unlock()
	cpu := b.host.CPU()
	b.shown = cpu
	booted := agentSims(b.host.BootedSims())
	out := slotsView{Enabled: b.cfg.Enabled, MaxWait: b.cfg.MaxWait, CPU: cpu, MemFreeGB: b.host.MemFreeGB(), Booted: booted, Kinds: []slotKindView{}}
	if out.Booted == nil {
		out.Booted = []string{}
	}
	for _, k := range b.cfg.Kinds {
		v := slotKindView{slotKind: k, InUse: b.inUseLocked(k), Holders: []slotHolderView{}, Waiters: []slotWaiterView{}}
		for _, h := range b.st.Holders {
			if h.Kind == k.ID {
				v.Holders = append(v.Holders, slotHolderView{ID: h.ID, Session: h.Session, Title: a.holderTitle(h), Command: h.Command, PID: h.PID, Since: h.Since})
			}
		}
		for _, w := range b.st.Waiters {
			if w.Kind == k.ID {
				place, _ := b.placeLocked(w.ID)
				v.Waiters = append(v.Waiters, slotWaiterView{Request: w.ID, Session: w.Session, Title: a.holderTitle(slotHolder{Session: w.Session, Step: w.Step, Label: w.Label, Command: w.Command}), Command: w.Command, Place: place, Why: w.Why, Since: w.Since})
			}
		}
		v.Line = fmt.Sprintf(slotLineFormat, k.ID, v.InUse, k.Max, cpu)
		if len(v.Waiters) > 0 {
			v.Line += fmt.Sprintf(slotLineWaiting, len(v.Waiters))
		}
		out.Kinds = append(out.Kinds, v)
	}
	return out
}

func (a *app) slotsCommand(_ context.Context, _ pluginsdk.CommandRun, _ struct{}) (any, error) {
	return a.slotsSnapshot(), nil
}

func (a *app) slotReleaseCommand(ctx context.Context, cmd pluginsdk.CommandRun, args slotReleaseArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errUserOnly
	}
	return nil, a.releaseSlot(ctx, args.ID)
}

func (a *app) releaseSlot(ctx context.Context, holder string) error {
	b := &a.resources
	b.mu.Lock()
	i := slices.IndexFunc(b.st.Holders, func(h slotHolder) bool { return h.ID == holder })
	if i < 0 {
		b.mu.Unlock()
		return errSlotUnknown
	}
	delete(b.owned, holder)
	b.st.Holders = slices.Delete(b.st.Holders, i, i+1)
	b.grantLocked(time.Now())
	b.touchLocked()
	b.mu.Unlock()
	a.saveSlots(ctx)
	a.slotsChanged()
	return nil
}

func (a *app) slotConfigCommand(ctx context.Context, cmd pluginsdk.CommandRun, args slotConfigArgs) (any, error) {
	if args.Enabled == nil && args.MaxWait == nil && args.Kinds == nil && args.Reset == "" {
		return a.slotConfig(), nil
	}
	if !a.userWord(cmd) {
		return nil, errUserOnly
	}
	cfg := a.slotConfig()
	if args.Enabled != nil {
		cfg.Enabled = *args.Enabled
	}
	if args.MaxWait != nil {
		cfg.MaxWait = *args.MaxWait
	}
	if args.Kinds != nil {
		cfg.Kinds = *args.Kinds
	}
	if args.Reset != "" {
		reset, err := resetKind(cfg, args.Reset)
		if err != nil {
			return nil, err
		}
		cfg = reset
	}
	if err := a.setSlotConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return a.slotConfig(), nil
}

func resetKind(cfg slotConfig, kind string) (slotConfig, error) {
	cfg = cfg.clone()
	i := slices.IndexFunc(cfg.Kinds, func(k slotKind) bool { return k.ID == kind })
	j := slices.IndexFunc(defaultSlotKinds(runtime.NumCPU()), func(k slotKind) bool { return k.ID == kind })
	switch {
	case j < 0:
		return cfg, fmt.Errorf("%w: %q has no defaults", errSlotKind, kind)
	case i < 0:
		cfg.Kinds = append(cfg.Kinds, defaultSlotKinds(runtime.NumCPU())[j])
	default:
		cfg.Kinds[i] = defaultSlotKinds(runtime.NumCPU())[j]
	}
	return cfg, nil
}
