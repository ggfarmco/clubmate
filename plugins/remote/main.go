package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	listenEnv    = "CLUBMATE_REMOTE_LISTEN"
	defaultPort  = 47621
	rescanEvery  = 30 * time.Second
	changedTopic = "remote.changed"
	computerKey  = "computer"
	enabledKey   = "enabled"
	portKey      = "port"
	titleBarKey  = "titleBarColour"
)

var (
	errUnknownCommand = errors.New("unknown command")
	errNoPairHost     = errors.New("pairing needs a Tailscale address")
	errBadPort        = errors.New("port must be between 1 and 65535")
)

type app struct {
	ready chan struct{}
	ctx   context.Context
	host  *pluginsdk.Host
	srv   *server
	ln    *listeners
	ident *identity

	enableMu sync.Mutex

	mu       sync.Mutex
	enabled  bool
	titleBar bool
	port     int
	dns      string
	tsIPs    []netip.Addr
	sub      string
	lastSeq  int64
}

type status struct {
	Enabled        bool       `json:"enabled"`
	Port           int        `json:"port"`
	Listening      []string   `json:"listening"`
	Hosts          []string   `json:"hosts"`
	Error          string     `json:"error"`
	Computer       computer   `json:"computer"`
	TitleBarColour bool       `json:"titleBarColour"`
	OtherMacs      []otherMac `json:"otherMacs"`
	TitleDot       bool       `json:"titleDot"`
}

type otherMac struct {
	Name    string   `json:"name"`
	Colour  string   `json:"colour"`
	Devices []string `json:"devices"`
}

type deviceView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	PairedAt  time.Time `json:"pairedAt"`
	LastSeen  time.Time `json:"lastSeen"`
	Connected bool      `json:"connected"`
	Macs      []macInfo `json:"macs"`
}

type enableArgs struct {
	Enabled bool `json:"enabled"`
}

type portArgs struct {
	Port int `json:"port"`
}

type colourArgs struct {
	Colour string `json:"colour"`
}

type revokeArgs struct {
	Device string `json:"device"`
}

func main() {
	a := &app{ready: make(chan struct{})}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed, UIUpdated: a.uiUpdated, Shutdown: a.shutdown})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.ctx = ctx
	a.host = h
	comp, err := loadComputer(ctx, h)
	if err != nil {
		return err
	}
	devices := newDeviceStore(h, time.Now)
	if err := devices.load(ctx); err != nil {
		return err
	}
	var enabled bool
	if _, err := h.StorageGet(ctx, enabledKey, &enabled); err != nil {
		return err
	}
	port := defaultPort
	if _, err := h.StorageGet(ctx, portKey, &port); err != nil {
		return err
	}
	titleBar := true
	if _, err := h.StorageGet(ctx, titleBarKey, &titleBar); err != nil {
		return err
	}
	a.enabled, a.port, a.titleBar = enabled, port, titleBar
	a.ident = &identity{kv: h, publish: a.publish, comp: comp}
	a.srv = &server{
		backend:  hostBackend{host: h},
		devices:  devices,
		pairing:  newPairing(time.Now),
		hub:      newHub(a.changed),
		identity: a.ident,
		changed:  a.changed,
		watching: a.watching,
		ping:     pingInterval,
	}
	if err := a.srv.clearWatching(ctx); err != nil {
		logf("clear watching: %v", err)
	}
	a.ln = newListeners(a.srv.handler(), os.Getenv(listenEnv), a.scanAddrs)
	if enabled {
		a.activate(ctx)
	}
	close(a.ready)
	go a.rescanLoop(ctx)
	return nil
}

func (a *app) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	select {
	case <-a.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	switch cmd.ID {
	case "remote.status":
		return a.status(), nil
	case "remote.enable":
		var args enableArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		if err := a.setEnabled(ctx, args.Enabled); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.set-port":
		var args portArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		if err := a.setPort(ctx, args.Port); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.set-colour":
		var args colourArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		if _, err := a.ident.setColour(ctx, args.Colour); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.set-title-bar-colour":
		var args enableArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		if err := a.setTitleBar(ctx, args.Enabled); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.pair":
		return a.pair(ctx)
	case "remote.cancel-pair":
		a.srv.pairing.cancel()
		return nil, nil
	case "remote.devices":
		return a.devices(), nil
	case "remote.revoke":
		var args revokeArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.srv.revoke(ctx, args.Device)
	}
	return nil, fmt.Errorf("%w: %s", errUnknownCommand, cmd.ID)
}

func (a *app) busEvent(_ context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	a.mu.Lock()
	a.lastSeq = max(a.lastSeq, ev.Seq)
	a.mu.Unlock()
	if ev.Topic == pluginChangedTopic {
		a.srv.forgetCommands()
	}
	a.srv.hub.publish(ev)
}

func (a *app) busClosed(ctx context.Context, h *pluginsdk.Host, sub string, cause error) {
	a.mu.Lock()
	mine := sub == a.sub
	after := a.lastSeq
	if mine {
		a.sub = ""
	}
	a.mu.Unlock()
	if !mine {
		return
	}
	logf("bus subscription closed: %v", cause)
	a.subscribe(ctx, h, resumeAfter(after))
}

func (a *app) uiUpdated(_ context.Context, _ *pluginsdk.Host, ev pluginsdk.UIUpdate) {
	select {
	case <-a.ready:
	default:
		return
	}
	a.srv.uiUpdated(ev.View)
}

func (a *app) shutdown(context.Context, *pluginsdk.Host) {
	if a.ln != nil {
		a.ln.stop()
	}
}

func (a *app) pair(ctx context.Context) (pairResult, error) {
	a.enableMu.Lock()
	defer a.enableMu.Unlock()
	if err := a.setEnabledLocked(ctx, true); err != nil {
		return pairResult{}, err
	}
	st := a.status()
	if err := pairHostsError(st); err != nil {
		return pairResult{}, err
	}
	code, expires := a.srv.pairing.issue()
	return pairResult{Link: pairLink(code, st.Hosts, st.Port, a.ident.get(), expires), Code: code, ExpiresAt: expires.UTC().Format(time.RFC3339)}, nil
}

func (a *app) setEnabled(ctx context.Context, enabled bool) error {
	a.enableMu.Lock()
	defer a.enableMu.Unlock()
	return a.setEnabledLocked(ctx, enabled)
}

func (a *app) setEnabledLocked(ctx context.Context, enabled bool) error {
	a.mu.Lock()
	was := a.enabled
	a.mu.Unlock()
	if was == enabled {
		if enabled && a.ln.refresh() {
			a.changed()
		}
		return nil
	}
	if err := a.host.StorageSet(ctx, enabledKey, enabled); err != nil {
		return err
	}
	if enabled {
		a.activate(ctx)
	} else {
		a.ln.stop()
		a.srv.hub.closeAll()
		a.srv.pairing.cancel()
	}
	a.mu.Lock()
	a.enabled = enabled
	a.mu.Unlock()
	a.changed()
	return nil
}

func (a *app) setPort(ctx context.Context, port int) error {
	if port < 1 || port > math.MaxUint16 {
		return errBadPort
	}
	a.enableMu.Lock()
	defer a.enableMu.Unlock()
	a.mu.Lock()
	was, enabled := a.port, a.enabled
	a.mu.Unlock()
	if was == port {
		return nil
	}
	if err := a.host.StorageSet(ctx, portKey, port); err != nil {
		return err
	}
	a.mu.Lock()
	a.port = port
	a.mu.Unlock()
	if enabled {
		a.srv.pairing.cancel()
		a.srv.hub.closeAll()
		a.ln.restart(port)
	}
	a.changed()
	return nil
}

func (a *app) setTitleBar(ctx context.Context, enabled bool) error {
	if err := a.host.StorageSet(ctx, titleBarKey, enabled); err != nil {
		return err
	}
	a.mu.Lock()
	a.titleBar = enabled
	a.mu.Unlock()
	a.changed()
	return nil
}

func (a *app) activate(ctx context.Context) {
	a.mu.Lock()
	subscribed := a.sub != ""
	port := a.port
	a.mu.Unlock()
	if !subscribed {
		a.subscribe(ctx, a.host, math.MaxInt64)
	}
	a.refreshTailscale(ctx)
	a.ln.start(port)
	if err := a.ln.state().err; err != "" {
		logf("not listening on every address: %s", err)
	}
}

func (a *app) subscribe(ctx context.Context, h *pluginsdk.Host, after int64) {
	sid, err := h.Subscribe(ctx, busPattern, after)
	if err != nil {
		logf("subscribe to bus: %v", err)
		return
	}
	a.mu.Lock()
	a.sub = sid
	a.mu.Unlock()
	a.srv.forgetCommands()
}

func (a *app) rescanLoop(ctx context.Context) {
	tick := time.NewTicker(rescanEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		a.mu.Lock()
		enabled, subscribed := a.enabled, a.sub != ""
		after := a.lastSeq
		a.mu.Unlock()
		if !enabled {
			continue
		}
		if !subscribed {
			a.subscribe(ctx, a.host, resumeAfter(after))
		}
		before := a.dnsName()
		a.refreshTailscale(ctx)
		if a.ln.refresh() || before != a.dnsName() {
			a.changed()
		}
	}
}

func (a *app) refreshTailscale(ctx context.Context) {
	if a.ln.override != "" {
		return
	}
	self := lookupTailscale(ctx)
	a.mu.Lock()
	a.dns, a.tsIPs = self.dns, self.ips
	a.mu.Unlock()
}

func (a *app) scanAddrs() ([]netip.Addr, error) {
	a.mu.Lock()
	ips := slices.Clone(a.tsIPs)
	a.mu.Unlock()
	if len(ips) > 0 {
		return ips, nil
	}
	return utunAddrs()
}

func (a *app) status() status {
	others := otherMacs(a.srv.devices.list())
	a.mu.Lock()
	st := status{
		Enabled:        a.enabled,
		Port:           a.port,
		Listening:      []string{},
		Hosts:          []string{},
		Computer:       a.ident.get(),
		TitleBarColour: a.titleBar,
		OtherMacs:      others,
		TitleDot:       a.titleBar && len(others) > 0,
	}
	dns := a.dns
	a.mu.Unlock()
	if !st.Enabled {
		return st
	}
	ls := a.ln.state()
	st.Port, st.Error = ls.port, ls.err
	if len(ls.listening) == 0 {
		return st
	}
	st.Listening = ls.listening
	if dns != "" {
		st.Hosts = append(st.Hosts, dns)
	}
	st.Hosts = append(st.Hosts, ls.hosts...)
	return st
}

func (a *app) devices() []deviceView {
	list := a.srv.devices.list()
	out := make([]deviceView, 0, len(list))
	for _, d := range list {
		out = append(out, deviceView{ID: d.ID, Name: d.Name, Model: d.Model, PairedAt: wireTime(d.PairedAt), LastSeen: wireTime(d.LastSeen), Connected: a.srv.hub.connected(d.ID), Macs: nonNil(d.Macs)})
	}
	return out
}

func (a *app) dnsName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dns
}

func (a *app) changed() {
	a.publish(changedTopic, struct{}{})
}

func (a *app) watching(w contract.RemoteWatching) {
	a.publish(contract.TopicRemoteWatching, w)
}

func (a *app) publish(topic string, payload any) {
	if _, err := a.host.Publish(a.ctx, topic, payload); err != nil {
		logf("publish %s: %v", topic, err)
	}
}

func loadComputer(ctx context.Context, h *pluginsdk.Host) (computer, error) {
	var c computer
	if _, err := h.StorageGet(ctx, computerKey, &c); err != nil {
		return computer{}, err
	}
	if c.ID == "" {
		c.ID = rand.Text()
	}
	c.Name = computerName(ctx)
	return c, h.StorageSet(ctx, computerKey, c)
}

func otherMacs(devices []device) []otherMac {
	slices.SortStableFunc(devices, func(a, b device) int { return b.LastSeen.Compare(a.LastSeen) })
	out := []otherMac{}
	for _, d := range devices {
		for _, m := range d.Macs {
			i := slices.IndexFunc(out, func(o otherMac) bool { return o.Name == m.Name })
			if i < 0 {
				out = append(out, otherMac{Name: m.Name, Colour: m.Colour})
				i = len(out) - 1
			}
			if !slices.Contains(out[i].Devices, d.Name) {
				out[i].Devices = append(out[i].Devices, d.Name)
			}
		}
	}
	slices.SortFunc(out, func(a, b otherMac) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func pairHostsError(st status) error {
	if len(st.Hosts) > 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", errNoPairHost, cmp.Or(st.Error, errNoTailnet))
}

func resumeAfter(lastSeq int64) int64 {
	if lastSeq <= 0 {
		return math.MaxInt64
	}
	return lastSeq
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func wireTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "remote: "+format+"\n", args...)
}
