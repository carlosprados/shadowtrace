package scan

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/carlosprados/shadowtrace/internal/config"
)

// Sighting is a non-BLE presence hit (Wi-Fi/mDNS/ARP), keyed for the presence map.
type Sighting struct {
	Key  string
	Name string
	Type string
}

func pingHost(ctx context.Context, host string) bool {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(c, "ping", "-c", "1", "-W", "1", host).Run() == nil
}

// WifiScan pings configured hosts and, if enabled, folds in mDNS discovery.
func WifiScan(ctx context.Context, cfg config.Config) []Sighting {
	var out []Sighting
	for _, entry := range cfg.WifiHosts {
		name, host := entry, entry
		if i := strings.Index(entry, "@"); i >= 0 {
			name, host = entry[:i], entry[i+1:]
		}
		if pingHost(ctx, host) {
			out = append(out, Sighting{Key: "wifi:" + host, Name: name, Type: "WiFi"})
		}
	}
	if cfg.MDNS {
		out = append(out, mdnsScan(ctx)...)
	}
	return out
}

func mdnsScan(ctx context.Context) []Sighting {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "avahi-browse", "-artp", "-t").Output()
	if err != nil {
		return nil
	}
	var res []Sighting
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || (line[0] != '+' && line[0] != '=') {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) < 9 {
			continue
		}
		name, host := parts[3], parts[6]
		key := host
		if key == "" {
			key = name
		}
		res = append(res, Sighting{Key: "mdns:" + key, Name: firstNonEmpty(name, host), Type: "mDNS"})
	}
	return res
}

type neigh struct {
	Dst    string `json:"dst"`
	Lladdr string `json:"lladdr"`
	State  string `json:"state"`
	Dev    string `json:"dev"`
}

func ipJSON(ctx context.Context, args ...string) []byte {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, args[0], args[1:]...).Output()
	if err != nil {
		return nil
	}
	return out
}

// ArpScan reads the neighbour table (optionally sweeping first) and reports MACs.
func ArpScan(ctx context.Context, cfg config.Config) []Sighting {
	if !cfg.ARP {
		return nil
	}
	subnets := cfg.ARPSubnets
	if len(subnets) == 0 {
		subnets = autoSubnets(ctx)
	}
	if cfg.ARPSweep && len(subnets) > 0 {
		arpSweep(ctx, subnets, cfg.ARPSweepLimit, cfg.ARPTimeoutMS)
	}
	raw := ipJSON(ctx, "ip", "-j", "neigh")
	var table []neigh
	if json.Unmarshal(raw, &table) != nil {
		return nil
	}
	var out []Sighting
	for _, n := range table {
		mac := strings.ToUpper(n.Lladdr)
		if n.Dst == "" || mac == "" {
			continue
		}
		if n.State == "FAILED" || n.State == "INCOMPLETE" {
			continue
		}
		out = append(out, Sighting{Key: "arp:" + mac, Name: n.Dst, Type: "ARP"})
	}
	return out
}

func autoSubnets(ctx context.Context) []string {
	raw := ipJSON(ctx, "ip", "-j", "route", "show", "scope", "link")
	var routes []struct {
		Dst string `json:"dst"`
	}
	if json.Unmarshal(raw, &routes) != nil {
		return nil
	}
	var out []string
	for _, r := range routes {
		if !strings.Contains(r.Dst, "/") || r.Dst == "default" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(r.Dst); err == nil {
			ones, _ := ipnet.Mask.Size()
			if ones >= 16 && ones <= 30 {
				out = append(out, r.Dst)
			}
		}
	}
	return out
}

func arpSweep(ctx context.Context, subnets []string, limit, timeoutMS int) {
	var ips []string
	for _, cidr := range subnets {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		for cur := ip.Mask(ipnet.Mask); ipnet.Contains(cur) && len(ips) < limit; cur = nextIP(cur) {
			ips = append(ips, cur.String())
		}
		if len(ips) >= limit {
			break
		}
	}
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			pingHost(ctx, h)
		}(ip)
	}
	wg.Wait()
}

func nextIP(ip net.IP) net.IP {
	out := make(net.IP, len(ip))
	copy(out, ip)
	for i := len(out) - 1; i >= 0; i-- {
		out[i]++
		if out[i] != 0 {
			break
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
