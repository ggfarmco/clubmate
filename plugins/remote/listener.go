package main

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const (
	errNoTailnet = "no Tailscale address"
	errWildcard  = listenEnv + " must name one host, not all interfaces"
	tunnelPrefix = "utun"
)

var tailnetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

type listeners struct {
	handler  http.Handler
	override string
	scan     func() ([]netip.Addr, error)

	mu     sync.Mutex
	srv    *http.Server
	port   int
	active map[string]net.Listener
	err    string
	wg     sync.WaitGroup
}

type listenState struct {
	listening []string
	hosts     []string
	port      int
	err       string
}

func newListeners(handler http.Handler, override string, scan func() ([]netip.Addr, error)) *listeners {
	return &listeners{handler: handler, override: override, scan: scan, active: map[string]net.Listener{}}
}

func (l *listeners) start(port int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv == nil {
		l.srv = newHTTPServer(l.handler)
		l.port = port
	}
	return l.refreshLocked()
}

func (l *listeners) restart(port int) bool {
	l.stop()
	return l.start(port)
}

func (l *listeners) refresh() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.refreshLocked()
}

func (l *listeners) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.srv == nil {
		return
	}
	_ = l.srv.Close()
	l.wg.Wait()
	l.srv = nil
	l.active = map[string]net.Listener{}
	l.err = ""
}

func (l *listeners) state() listenState {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := listenState{port: l.port, err: l.err}
	keys := make([]string, 0, len(l.active))
	for key := range l.active {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareHosts)
	for _, key := range keys {
		addr := l.active[key].Addr().String()
		st.listening = append(st.listening, addr)
		host, port, _ := net.SplitHostPort(key)
		st.hosts = append(st.hosts, host)
		if l.override != "" {
			_, actual, _ := net.SplitHostPort(addr)
			port = actual
		}
		st.port = atoiOr(port, st.port)
	}
	return st
}

func (l *listeners) refreshLocked() bool {
	if l.srv == nil {
		return false
	}
	wanted, err := l.wanted()
	l.err = ""
	if err != nil {
		l.err = err.Error()
	}
	changed := false
	for key, ln := range l.active {
		if !slices.Contains(wanted, key) {
			_ = ln.Close()
			delete(l.active, key)
			changed = true
		}
	}
	var failures []string
	for _, key := range wanted {
		if _, ok := l.active[key]; ok {
			continue
		}
		ln, err := net.Listen("tcp", key)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		l.active[key] = ln
		changed = true
		srv := l.srv
		l.wg.Go(func() { _ = srv.Serve(ln) })
	}
	if len(failures) > 0 {
		l.err = strings.Join(failures, "; ")
	}
	return changed
}

func (l *listeners) wanted() ([]string, error) {
	if l.override != "" {
		host, _, err := net.SplitHostPort(l.override)
		if err != nil {
			return nil, err
		}
		if ip, err := netip.ParseAddr(host); host == "" || (err == nil && ip.IsUnspecified()) {
			return nil, errors.New(errWildcard)
		}
		return []string{l.override}, nil
	}
	ips, err := l.scan()
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New(errNoTailnet)
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, netip.AddrPortFrom(ip, uint16(l.port)).String())
	}
	return out, nil
}

func tailnetAddrs(addrs []net.Addr) []netip.Addr {
	ips := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		if addr, ok := netip.AddrFromSlice(ip); ok {
			ips = append(ips, addr)
		}
	}
	return filterTailnet(ips)
}

func filterTailnet(ips []netip.Addr) []netip.Addr {
	var out []netip.Addr
	for _, addr := range ips {
		addr = addr.Unmap()
		if !slices.ContainsFunc(tailnetPrefixes, func(p netip.Prefix) bool { return p.Contains(addr) }) || slices.Contains(out, addr) {
			continue
		}
		out = append(out, addr)
	}
	slices.SortFunc(out, func(a, b netip.Addr) int {
		if a.Is4() != b.Is4() {
			if a.Is4() {
				return -1
			}
			return 1
		}
		return a.Compare(b)
	})
	return out
}

func utunAddrs() ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addrs []net.Addr
	for _, iface := range ifaces {
		if !strings.HasPrefix(iface.Name, tunnelPrefix) {
			continue
		}
		got, err := iface.Addrs()
		if err != nil {
			continue
		}
		addrs = append(addrs, got...)
	}
	return tailnetAddrs(addrs), nil
}

func compareHosts(a, b string) int {
	av4, bv4 := !strings.HasPrefix(a, "["), !strings.HasPrefix(b, "[")
	if av4 != bv4 {
		if av4 {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
