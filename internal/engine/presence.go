package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/carlosprados/shadowtrace/internal/config"
	"github.com/carlosprados/shadowtrace/internal/notify"
	"github.com/carlosprados/shadowtrace/internal/scan"
	"github.com/carlosprados/shadowtrace/internal/store"
)

type seenDev struct {
	name string
	typ  string
	rssi *int
}

func matchWhitelist(list []string, name string) bool {
	if len(list) == 0 {
		return true
	}
	if name == "" {
		return false
	}
	low := strings.ToLower(name)
	for _, w := range list {
		if strings.Contains(low, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

func rssiText(r *int) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf(" RSSI=%ddBm", *r)
}

// Presence runs the legacy presence tracker (DETECTED/LOST) until ctx is cancelled.
func Presence(ctx context.Context, cfg config.Config, sc *scan.Scanner, n *notify.Notifier) error {
	st := store.LoadState(cfg.StateFile)
	ignore := map[string]bool{}
	for _, m := range cfg.IgnoreMACs {
		ignore[m] = true
	}

	n.Send(fmt.Sprintf("▶️ %s started. interval=%ds, window=%ds, lost_after=%ds",
		cfg.TagPrefix(), cfg.IntervalSeconds, cfg.WindowSeconds, cfg.PresenceGoneAfter))

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		start := time.Now()
		obs, err := sc.ScanOnce(ctx)
		if err != nil {
			fmt.Println("[ERROR] Scan failed:", err)
			sleepCtx(ctx, 5*time.Second)
			continue
		}

		seen := map[string]seenDev{}
		for _, o := range obs {
			if ignore[o.MAC] {
				continue
			}
			if !matchWhitelist(cfg.NameWhitelist, o.Name) {
				continue
			}
			seen[o.MAC] = seenDev{name: o.Name, typ: o.Type, rssi: o.RSSI}
		}
		for _, s := range scan.WifiScan(ctx, cfg) {
			seen[s.Key] = seenDev{name: s.Name, typ: s.Type}
		}
		for _, s := range scan.ArpScan(ctx, cfg) {
			seen[s.Key] = seenDev{name: s.Name, typ: s.Type}
		}

		now := time.Now()
		changed := false

		for key, info := range seen {
			prev := st[key]
			if prev == nil || prev.Status == "gone" {
				st[key] = &store.StateEntry{
					Name: info.name, Type: info.typ, RSSI: info.rssi,
					LastSeen: nowISO(), Status: "present",
				}
				changed = true
				n.Send(fmt.Sprintf("🟢 %s — DETECTED %s [%s] (%s)%s",
					cfg.TagPrefix(), orUnknown(info.name), key, info.typ, rssiText(info.rssi)))
			} else {
				prev.LastSeen = nowISO()
				prev.RSSI = info.rssi
				if info.name != "" {
					prev.Name = info.name
				}
				if prev.Type == "" {
					prev.Type = info.typ
				}
			}
		}

		for key, prev := range st {
			if prev.Status == "gone" {
				continue
			}
			last, err := time.Parse(time.RFC3339Nano, prev.LastSeen)
			if err != nil {
				last = now
			}
			if now.Sub(last) > time.Duration(cfg.PresenceGoneAfter)*time.Second {
				prev.Status = "gone"
				changed = true
				n.Send(fmt.Sprintf("🔴 %s — LOST %s [%s] (%s)%s",
					cfg.TagPrefix(), orUnknown(prev.Name), key, prev.Type, rssiText(prev.RSSI)))
			}
		}

		if changed {
			_ = store.SaveState(cfg.StateFile, st)
		}

		elapsed := time.Since(start)
		if d := time.Duration(cfg.IntervalSeconds)*time.Second - elapsed; d > 0 {
			sleepCtx(ctx, d)
		}
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
