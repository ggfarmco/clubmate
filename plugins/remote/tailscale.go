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
	dns       string
	ips       []netip.Addr
	installed bool
	failed    bool
}

func lookupTailscale(ctx context.Context) tailscaleSelf {
	bin := tailscaleApp
	if _, err := os.Stat(bin); err != nil {
		if bin, err = exec.LookPath("tailscale"); err != nil {
			return tailscaleSelf{}
		}
	}
	return runTailscale(ctx, bin)
}

func runTailscale(ctx context.Context, bin string) tailscaleSelf {
	ctx, cancel := context.WithTimeout(ctx, tailscaleTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "status", "--json")
	cmd.Env = append(os.Environ(), "TAILSCALE_BE_CLI=1")
	out, err := cmd.Output()
	if err != nil && ctx.Err() != nil {
		return tailscaleSelf{installed: true, failed: true}
	}
	self, ok := parseTailscale(out)
	if !ok {
		line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		logf("tailscale status --json: %s", line)
		return tailscaleSelf{installed: true, failed: true}
	}
	self.installed = true
	return self
}

func parseTailscale(raw []byte) (tailscaleSelf, bool) {
	var st tailscaleStatus
	if json.Unmarshal(raw, &st) != nil {
		return tailscaleSelf{}, false
	}
	ips := make([]netip.Addr, 0, len(st.Self.TailscaleIPs))
	for _, s := range st.Self.TailscaleIPs {
		if ip, err := netip.ParseAddr(s); err == nil {
			ips = append(ips, ip)
		}
	}
	return tailscaleSelf{dns: strings.TrimSuffix(st.Self.DNSName, "."), ips: filterTailnet(ips)}, true
}

func computerName(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "scutil", "--get", "ComputerName").Output()
	if name := strings.TrimSpace(string(out)); err == nil && name != "" {
		return name
	}
	name, _ := os.Hostname()
	return name
}
