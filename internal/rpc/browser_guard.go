package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const browserResolveTimeout = 5 * time.Second

var (
	errBrowserScheme    = errors.New("browser: only http and https pages can be opened")
	errBrowserNoHost    = errors.New("browser: the url has no host")
	errBrowserLocalHost = errors.New("browser: local and private network addresses are refused")

	browserBlockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("255.255.255.255/32"),
	}
)

type hostResolver func(ctx context.Context, host string) ([]netip.Addr, error)

func systemResolver(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func (s *Server) guardBrowserURL(ctx context.Context, in BrowserGuardInput) error {
	if in.URL == "" || in.URL == "about:blank" {
		return nil
	}
	return checkBrowserURL(ctx, s.browserResolve, in.URL)
}

func checkBrowserURL(ctx context.Context, resolve hostResolver, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("browser: bad url: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return errBrowserScheme
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return errBrowserNoHost
	}
	if localHostName(host) {
		return fmt.Errorf("%w: %s", errBrowserLocalHost, host)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return checkBrowserAddr(host, addr)
	}
	if addr, numeric, ok := parseLooseIPv4(host); numeric {
		if !ok {
			return fmt.Errorf("%w: %s", errBrowserLocalHost, host)
		}
		return checkBrowserAddr(host, addr)
	}
	ctx, cancel := context.WithTimeout(ctx, browserResolveTimeout)
	defer cancel()
	addrs, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("browser: cannot resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("browser: cannot resolve %s", host)
	}
	for _, addr := range addrs {
		if err := checkBrowserAddr(host, addr); err != nil {
			return err
		}
	}
	return nil
}

func checkBrowserAddr(host string, addr netip.Addr) error {
	if blockedBrowserAddr(addr) {
		return fmt.Errorf("%w: %s", errBrowserLocalHost, host)
	}
	return nil
}

func blockedBrowserAddr(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	for _, p := range browserBlockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func localHostName(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "local" || strings.HasSuffix(host, ".local")
}

func parseLooseIPv4(host string) (netip.Addr, bool, bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if !looseIPv4Number(parts[len(parts)-1]) {
		return netip.Addr{}, false, false
	}
	if len(parts) > 4 {
		return netip.Addr{}, true, false
	}
	var value uint64
	for i, part := range parts {
		n, err := parseLooseIPv4Part(part)
		if err != nil {
			return netip.Addr{}, true, false
		}
		if i < len(parts)-1 {
			if n > 255 {
				return netip.Addr{}, true, false
			}
			value |= n << (8 * (3 - i))
			continue
		}
		if n >= 1<<(8*(5-len(parts))) {
			return netip.Addr{}, true, false
		}
		value |= n
	}
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}), true, true
}

func looseIPv4Number(part string) bool {
	if part == "" {
		return false
	}
	if rest, ok := strings.CutPrefix(part, "0x"); ok {
		return strings.Trim(rest, "0123456789abcdef") == ""
	}
	return strings.Trim(part, "0123456789") == ""
}

func parseLooseIPv4Part(part string) (uint64, error) {
	switch {
	case part == "":
		return 0, strconv.ErrSyntax
	case strings.HasPrefix(part, "0x"):
		if part == "0x" {
			return 0, nil
		}
		return strconv.ParseUint(part[2:], 16, 64)
	case len(part) > 1 && part[0] == '0':
		return strconv.ParseUint(part[1:], 8, 64)
	}
	return strconv.ParseUint(part, 10, 64)
}
