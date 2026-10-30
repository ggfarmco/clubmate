package main

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	tailscaleApp     = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	tailscaleTimeout = 3 * time.Second
)

type tailscaleStatus struct {
	Self struct {
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
	} `json:"Self"`
}

type tailscaleSelf struct {
	dns string
	ips []netip.Addr
}

func lookupTailscale(ctx context.Context) tailscaleSelf {
	bin := tailscaleApp
	if _, err := os.Stat(bin); err != nil {
		if bin, err = exec.LookPath("tailscale"); err != nil {
			return tailscaleSelf{}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, tailscaleTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return tailscaleSelf{}
	}
	return parseTailscale(out)
}

func parseTailscale(raw []byte) tailscaleSelf {
	var st tailscaleStatus
	if json.Unmarshal(raw, &st) != nil {
		return tailscaleSelf{}
	}
	ips := make([]netip.Addr, 0, len(st.Self.TailscaleIPs))
	for _, s := range st.Self.TailscaleIPs {
		if ip, err := netip.ParseAddr(s); err == nil {
			ips = append(ips, ip)
		}
	}
	return tailscaleSelf{dns: strings.TrimSuffix(st.Self.DNSName, "."), ips: filterTailnet(ips)}
}

func computerName(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "scutil", "--get", "ComputerName").Output()
	if name := strings.TrimSpace(string(out)); err == nil && name != "" {
		return name
	}
	name, _ := os.Hostname()
	return name
}
