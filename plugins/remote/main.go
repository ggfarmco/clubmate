package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	listenEnv    = "CLUBMATE_REMOTE_LISTEN"
	logIDLen     = 8
	maxLogValue  = 64
	slowRPC      = 2 * time.Second
	testModeEnv  = "CLUBMATE_TEST_MODE"
	defaultPort  = 47621
	rescanEvery  = 30 * time.Second
	pushTimeout  = time.Minute
	changedTopic = "remote.changed"
	computerKey  = "computer"
	enabledKey   = "enabled"
	portKey      = "port"
	titleBarKey  = "titleBarColour"
)

var (
	logMu   sync.Mutex
	logSink = func(line string) { fmt.Fprintln(os.Stderr, "remote: "+line) }

	errUnknownCommand = errors.New("unknown command")
	errNoPairHost     = errors.New("pairing needs a Tailscale address")
	errBadPort        = errors.New("port must be between 1 and 65535")

	userOnly = map[string]bool{
		"remote.enable":           true,
		"remote.set-port":         true,
		"remote.pair":             true,
		"remote.cancel-pair":      true,
		"remote.revoke":           true,
		"remote.set-push-relay":   true,
		"remote.report-upload":    true,
		"remote.set-report-inbox": true,
		"remote.set-tunnel":       true,
		"remote.tunnel-rotate":    true,
	}
)

type app struct {
	ready chan struct{}
	ctx   context.Context
	host  *pluginsdk.Host
	srv   *server
	ln    *listeners
	ident *identity
	rep   *reports
	tun   *tunnelClient

	enableMu sync.Mutex
	seedMu   sync.Mutex

	store     kvStore
	seedEvery time.Duration
	seedFor   time.Duration

	mu       sync.Mutex
	enabled  bool
	titleBar bool
	port     int
	dns      string
	tsIPs    []netip.Addr
	tsReason string
	tsCLI    bool
	upAddrs  func() ([]netip.Addr, error)
	sub      string
	lastSeq  int64
}

type status struct {
	Enabled        bool         `json:"enabled"`
	Port           int          `json:"port"`
	Listening      []string     `json:"listening"`
	Hosts          []string     `json:"hosts"`
	Error          string       `json:"error"`
	Computer       computer     `json:"computer"`
	TitleBarColour bool         `json:"titleBarColour"`
	OtherMacs      []otherMac   `json:"otherMacs"`
	TitleDot       bool         `json:"titleDot"`
	Push           bool         `json:"push"`
	ReportRelay    string       `json:"reportRelay"`
	Relay          relayView    `json:"relay"`
	Tunnel         tunnelStatus `json:"tunnel"`
	Tailnet        tailnetView  `json:"tailnet"`
	PairReady      bool         `json:"pairReady"`
	PairHint       string       `json:"pairHint"`
}

const (
	tailnetOK    = "ok"
	tailnetOff   = "off"
	tailnetError = "error"

	severityOK    = "ok"
	severityNote  = "note"
	severityWarn  = "warn"
	severityError = "error"
)

type tailnetView struct {
	State    string `json:"state"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
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
	Path      string    `json:"path"`
	Push      bool      `json:"push"`
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

type relayArgs struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type pushRelayArgs struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	Auto  bool   `json:"auto"`
}

func main() {
	a := &app{ready: make(chan struct{}), seedEvery: seedEvery, seedFor: seedFor}
	pluginsdk.Main(pluginsdk.Plugin{Init: a.init, Command: a.command, BusEvent: a.busEvent, BusClosed: a.busClosed, UIUpdated: a.uiUpdated, Shutdown: a.shutdown})
}

func (a *app) init(ctx context.Context, h *pluginsdk.Host) error {
	a.ctx = ctx
	a.host = h
	a.store = h
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
	relay := newRelayClient(h, &http.Client{Timeout: relayTimeout})
	relay.dir = pluginsdk.StateDir()
	relay.changed = a.changed
	relay.onSigned = a.relaySigned
	relay.bind(ctx)
	if err := relay.load(ctx); err != nil {
		logf("load push relay: %v", err)
	}
	a.rep = newReports(relay, h, &http.Client{Timeout: reportTimeout}, pluginsdk.StateDir(), time.Now)
	a.srv = &server{
		backend:  hostBackend{host: h},
		devices:  devices,
		pairing:  newPairing(time.Now),
		hub:      newHub(a.changed),
		identity: a.ident,
		relay:    relay,
		requests: newRequestLog(h, time.Now),
		changed:  a.changed,
		watching: a.watching,
		ping:     pingInterval,
		now:      time.Now,
	}
	if err := a.srv.clearWatching(ctx); err != nil {
		logf("clear watching: %v", err)
	}
	a.ln = newListeners(a.srv.handler(), os.Getenv(listenEnv), a.scanAddrs)
	a.tun = newTunnelClient(ctx, relay, h, newKeyStore(comp.ID), a.srv.handler(), a.changed)
	a.tun.allowed = enabled
	a.tun.tickets = a.srv.ticketSet
	a.tun.sess.classify = a.srv.ticketKind
	a.srv.tun = a.tun
	if err := a.tun.load(ctx); err != nil {
		logf("load tunnel: %v", err)
	}
	a.subscribe(ctx, h, math.MaxInt64)
	relay.setAccess(enabled)
	if enabled {
		a.activate(ctx)
		relay.ensure()
	}
	close(a.ready)
	go a.rescanLoop(ctx)
	go a.srv.requests.pruneLoop(ctx)
	go a.seedPushTriggers(ctx)
	return nil
}

func (a *app) command(ctx context.Context, _ *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	if userOnly[cmd.ID] && (cmd.Agent || cmd.Caller != "") {
		return nil, fmt.Errorf("%w: %s", errUnknownCommand, cmd.ID)
	}
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
		_ = a.srv.syncTickets(ctx)
		return nil, nil
	case "remote.devices":
		return a.devices(), nil
	case "remote.revoke":
		var args revokeArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.srv.revoke(ctx, args.Device)
	case "remote.push":
		var args pushArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		a.mu.Lock()
		enabled := a.enabled
		a.mu.Unlock()
		ctx, cancel := context.WithTimeout(ctx, pushTimeout)
		defer cancel()
		return a.srv.pushCommand(ctx, cmd.WorkspaceID, enabled, args)
	case "remote.set-push-relay":
		var args pushRelayArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		if err := a.setPushRelay(ctx, args); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.set-tunnel":
		var args tunnelArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		if err := a.tun.configure(ctx, args.On); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.tunnel-rotate":
		if err := a.tun.rotate(ctx); err != nil {
			return nil, err
		}
		return a.status(), nil
	case "remote.report-upload":
		var args reportUploadArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		return a.rep.upload(ctx, args.Path)
	case "remote.set-report-inbox":
		var args relayArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.rep.setInbox(ctx, args.URL, args.Token)
	case "remote.reports-fetch":
		return a.rep.fetch(ctx)
	case "remote.reports-done":
		var args reportsDoneArgs
		if err := decodeArgs(cmd.Args, &args); err != nil {
			return nil, err
		}
		return nil, a.rep.done(args.IDs)
	}
	return nil, fmt.Errorf("%w: %s", errUnknownCommand, cmd.ID)
}

func (a *app) busEvent(ctx context.Context, _ *pluginsdk.Host, _ string, ev pluginsdk.Event) {
	a.mu.Lock()
	a.lastSeq = max(a.lastSeq, ev.Seq)
	enabled := a.enabled
	a.mu.Unlock()
	if ev.Topic == workspaceCreatedTopic || ev.Topic == workspaceDeletedTopic {
		a.workspaceEvent(ctx, ev)
		return
	}
	if ev.Topic == pluginChangedTopic {
		a.srv.forgetCommands()
	}
	a.srv.hub.publish(ev)
	if enabled && ev.Topic == agentsChangedTopic && a.srv.relay.configured() {
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, pushTimeout)
			defer cancel()
			a.srv.push(ctx, ev)
		}()
	}
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
	if a.tun != nil {
		a.tun.stop()
	}
}

func (a *app) pair(ctx context.Context) (pairResult, error) {
	a.enableMu.Lock()
	defer a.enableMu.Unlock()
	if err := a.setEnabledLocked(ctx, true); err != nil {
		return pairResult{}, err
	}
	st := a.status()
	route, err := a.tun.pairInfo(ctx)
	if err != nil {
		logf("pair link without the relay path: %v", err)
	}
	if route == nil {
		if err := pairHostsError(st); err != nil {
			return pairResult{}, err
		}
	}
	code, ticket, expires := a.srv.pairing.issueWithTicket()
	var relay *linkRelay
	if route != nil {
		relay = &linkRelay{URL: route.URL, Key: route.Key, KID: route.KID, Ticket: ticket}
		if err := a.srv.syncTickets(ctx); err != nil && len(st.Hosts) == 0 {
			a.srv.pairing.cancel()
			return pairResult{}, fmt.Errorf("the relay did not take the pairing ticket: %w", err)
		}
	}
	return pairResult{Link: pairLink(code, st.Hosts, st.Port, a.ident.get(), expires, relay), Code: code, ExpiresAt: expires.UTC().Format(time.RFC3339), Relay: relay != nil}, nil
}

func (a *app) setPushRelay(ctx context.Context, args pushRelayArgs) error {
	relay := a.srv.relay
	if args.Auto {
		if args.URL != "" || args.Token != "" {
			return errSignupAutoArgs
		}
		if err := relay.setAuto(ctx); err != nil {
			return err
		}
		relay.ensure()
	} else if err := relay.configure(ctx, args.URL, args.Token); err != nil {
		return err
	}
	a.tun.restart()
	return nil
}

func (a *app) relaySigned(ctx context.Context) {
	a.tun.defaultOn(ctx)
	a.tun.restart()
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
	if a.tun != nil {
		a.tun.allow(enabled)
	}
	a.srv.relay.setAccess(enabled)
	if enabled {
		a.srv.relay.resume(ctx)
		a.srv.relay.ensure()
	}
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
		_ = a.srv.syncTickets(ctx)
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
	sid, err := h.Subscribe(ctx, subscribePattern, after)
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
		if !subscribed {
			a.subscribe(ctx, a.host, resumeAfter(after))
		}
		if !enabled {
			continue
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
	a.setTailscale(lookupTailscale(ctx))
}

func (a *app) setTailscale(self tailscaleSelf) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tsCLI = self.installed
	if !self.failed {
		a.dns, a.tsIPs, a.tsReason = self.dns, self.ips, self.reason
	}
}

func (a *app) scanAddrs() ([]netip.Addr, error) {
	a.mu.Lock()
	ips, reason, cli, up := slices.Clone(a.tsIPs), a.tsReason, a.tsCLI, a.upAddrs
	a.mu.Unlock()
	if up == nil {
		up = utunAddrs
	}
	if len(ips) == 0 && !cli {
		return utunAddrs()
	}
	if reason != "" {
		return nil, noTailnetError{reason}
	}
	if len(ips) == 0 {
		return nil, noTailnetError{errNoTailnet}
	}
	present, err := up()
	if err != nil {
		return nil, err
	}
	ips = slices.DeleteFunc(ips, func(ip netip.Addr) bool { return !slices.Contains(present, ip) })
	if len(ips) == 0 {
		return nil, noTailnetError{errTailscaleNotUp}
	}
	return ips, nil
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
		Push:           a.srv.relay.configured(),
		ReportRelay:    a.srv.relay.reportHost(),
	}
	dns := a.dns
	a.mu.Unlock()
	st.Relay = a.srv.relay.view(st.Enabled)
	if a.tun != nil {
		st.Tunnel = a.tun.status()
	}
	offline := false
	if st.Enabled {
		ls := a.ln.state()
		st.Port, st.Error, offline = ls.port, ls.err, ls.offline
		if len(ls.listening) > 0 {
			st.Listening = ls.listening
			if dns != "" {
				st.Hosts = append(st.Hosts, dns)
			}
			st.Hosts = append(st.Hosts, ls.hosts...)
		}
	}
	st.PairReady = len(st.Hosts) > 0 || st.Tunnel.State == tunnelConnected
	if st.Enabled && !st.PairReady {
		st.PairHint = pairHint(st)
	}
	st.Tailnet = tailnetState(st, offline)
	return st
}

func pairHint(st status) string {
	reason := cmp.Or(st.Error, errNoTailnet)
	switch st.Tunnel.State {
	case tunnelConnecting:
		return "No Tailscale address (" + reason + "); the relay path is still connecting."
	case tunnelError:
		return "No Tailscale address (" + reason + ") and the relay path is down: " + st.Tunnel.Detail
	}
	return "No Tailscale address (" + reason + "). Turn on the relay path to pair without Tailscale."
}

func tailnetState(st status, offline bool) tailnetView {
	if !st.Enabled {
		return tailnetView{State: tailnetOff, Severity: severityNote, Detail: "Off"}
	}
	if len(st.Listening) > 0 {
		if st.Error != "" {
			return tailnetView{State: tailnetError, Severity: severityWarn, Detail: st.Error}
		}
		return tailnetView{State: tailnetOK, Severity: severityOK, Detail: "Listening on " + listenAddress(st)}
	}
	if !offline && st.Error != "" {
		severity := severityError
		if st.Tunnel.State == tunnelConnected {
			severity = severityWarn
		}
		return tailnetView{State: tailnetError, Severity: severity, Detail: st.Error}
	}
	reason := sentence(cmp.Or(st.Error, errNoTailnet))
	switch st.Tunnel.State {
	case tunnelConnected:
		return tailnetView{State: tailnetOff, Severity: severityNote, Detail: reason + ". Phones connect through the relay."}
	case tunnelConnecting:
		return tailnetView{State: tailnetOff, Severity: severityWarn, Detail: reason + ". The relay path is still connecting."}
	}
	return tailnetView{State: tailnetOff, Severity: severityError, Detail: reason + "."}
}

func listenAddress(st status) string {
	if len(st.Hosts) == 0 {
		return st.Listening[0]
	}
	return net.JoinHostPort(st.Hosts[0], strconv.Itoa(st.Port))
}

func sentence(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

func (a *app) devices() []deviceView {
	list := a.srv.devices.list()
	out := make([]deviceView, 0, len(list))
	for _, d := range list {
		out = append(out, deviceView{ID: d.ID, Name: d.Name, Model: d.Model, PairedAt: wireTime(d.PairedAt), LastSeen: wireTime(d.LastSeen), Connected: a.srv.hub.connected(d.ID), Path: a.srv.hub.path(d.ID), Push: d.PushToken != "", Macs: nonNil(d.Macs)})
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
	if st.PairReady || len(st.Hosts) > 0 {
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
	logLine(fmt.Sprintf(format, args...))
}

func logEvent(event string, kv ...string) {
	var b strings.Builder
	b.WriteString(event)
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteString(" " + kv[i] + "=" + logValue(kv[i+1]))
	}
	logLine(b.String())
}

func logLine(line string) {
	logMu.Lock()
	sink := logSink
	logMu.Unlock()
	sink(line)
}

func setLogSink(sink func(string)) func(string) {
	logMu.Lock()
	defer logMu.Unlock()
	prev := logSink
	logSink = sink
	return prev
}

func logValue(v string) string {
	if v == "" {
		return "-"
	}
	v = strings.Map(func(r rune) rune {
		if r <= ' ' || r == '=' || r == 0x7f || !unicode.IsPrint(r) {
			return '_'
		}
		return r
	}, v)
	if runes := []rune(v); len(runes) > maxLogValue {
		return string(runes[:maxLogValue])
	}
	return v
}

func logID(id string) string {
	if runes := []rune(id); len(runes) > logIDLen {
		return string(runes[:logIDLen])
	}
	return id
}

func logMillis(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}
