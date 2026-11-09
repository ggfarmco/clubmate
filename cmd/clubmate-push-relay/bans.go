package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

var errBadBanRow = errors.New("want key <hex prefix> or ip <address or cidr>")

type bans struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	keys    []string
	ips     []netip.Prefix
}

func loadBans(path string) (*bans, error) {
	b := &bans{path: path}
	if err := b.refresh(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *bans) refresh() error {
	if b.path == "" {
		return nil
	}
	info, err := os.Stat(b.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if info == nil {
		b.modTime, b.size, b.keys, b.ips = time.Time{}, 0, nil, nil
		return nil
	}
	if info.ModTime().Equal(b.modTime) && info.Size() == b.size {
		return nil
	}
	keys, ips, err := readBans(b.path)
	if err != nil {
		return err
	}
	b.modTime, b.size, b.keys, b.ips = info.ModTime(), info.Size(), keys, ips
	return nil
}

func (b *bans) key(hexKey string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.ContainsFunc(b.keys, func(prefix string) bool { return strings.HasPrefix(hexKey, prefix) })
}

func (b *bans) ip(addr netip.Addr) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.ContainsFunc(b.ips, func(p netip.Prefix) bool { return p.Contains(addr) })
}

func readBans(path string) ([]string, []netip.Prefix, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var (
		keys []string
		ips  []netip.Prefix
	)
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, nil, fmt.Errorf("%s:%d: %w", path, n, errBadBanRow)
		}
		switch fields[0] {
		case "key":
			prefix := strings.ToLower(fields[1])
			if _, err := hex.DecodeString(prefix + strings.Repeat("0", len(prefix)%2)); err != nil {
				return nil, nil, fmt.Errorf("%s:%d: %w", path, n, errBadBanRow)
			}
			keys = append(keys, prefix)
		case "ip":
			prefix, err := parseBanIP(fields[1])
			if err != nil {
				return nil, nil, fmt.Errorf("%s:%d: %w", path, n, errBadBanRow)
			}
			ips = append(ips, prefix)
		default:
			return nil, nil, fmt.Errorf("%s:%d: %w", path, n, errBadBanRow)
		}
	}
	return keys, ips, sc.Err()
}

func parseBanIP(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}
