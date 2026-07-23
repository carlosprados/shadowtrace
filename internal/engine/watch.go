// Package engine runs the watch (environment IDS) and presence scan loops.
package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/carlosprados/shadowtrace/internal/config"
	"github.com/carlosprados/shadowtrace/internal/identify"
	"github.com/carlosprados/shadowtrace/internal/model"
	"github.com/carlosprados/shadowtrace/internal/notify"
	"github.com/carlosprados/shadowtrace/internal/scan"
	"github.com/carlosprados/shadowtrace/internal/store"
)

type track struct {
	hits             int
	firstSeen        time.Time
	lastSeen         time.Time
	maxRSSI, minRSSI int
	alertedAt        time.Time
	hasAlerted       bool
	inSession        bool
	name, mac, typ   string
	known            bool
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func inAlertHours(spec string, t time.Time) bool {
	if !strings.Contains(spec, "-") {
		return false
	}
	fs := strings.SplitN(spec, "-", 2)
	start, err1 := strconv.Atoi(strings.TrimSpace(fs[0]))
	end, err2 := strconv.Atoi(strings.TrimSpace(fs[1]))
	if err1 != nil || err2 != nil || start == end {
		return false
	}
	h := t.Local().Hour()
	if start < end {
		return h >= start && h < end
	}
	return h >= start || h < end
}

func idSuffix(id model.Identity) string {
	var b []string
	for _, s := range []string{id.Vendor, id.Kind, id.Model, id.Note} {
		if s != "" {
			b = append(b, s)
		}
	}
	if len(b) == 0 {
		return ""
	}
	return " {" + strings.Join(b, " / ") + "}"
}

func deviceLine(name, mac, typ string, rssi int) string {
	n := name
	if n == "" {
		n = "unknown"
	}
	return fmt.Sprintf("%s [%s] (%s) RSSI=%ddBm", n, mac, typ, rssi)
}

// Watch runs the environment intrusion-detection loop until ctx is cancelled.
// oui may be nil (vendor identification then relies on SIG company ids only).
func Watch(ctx context.Context, cfg config.Config, sc *scan.Scanner, n *notify.Notifier, oui identify.VendorFunc) error {
	b := store.LoadBaseline(cfg.BaselineFile)

	var learnUntil time.Time
	if s, ok := b.Meta["learn_until"]; ok {
		learnUntil, _ = time.Parse(time.RFC3339Nano, s)
	}
	if learnUntil.IsZero() {
		learnUntil = time.Now().Add(time.Duration(cfg.LearnSeconds) * time.Second)
		b.Meta["learn_until"] = learnUntil.UTC().Format(time.RFC3339Nano)
		b.Meta["created"] = nowISO()
		_ = store.SaveBaseline(cfg.BaselineFile, b)
	}

	homeMACs := map[string]bool{}
	for _, m := range cfg.HomeMACs {
		homeMACs[m] = true
	}

	tracks := map[string]*track{}
	// macFP anchors a device's fingerprint for the life of its session: BlueZ
	// enriches a device's properties across cycles, so the same MAC can otherwise
	// drift to a new (richer) fingerprint and be treated as a second device. We
	// keep the first fingerprint seen for a MAC until its session closes; MAC
	// rotation (a new MAC) still yields a fresh fingerprint for cross-rotation grouping.
	macFP := map[string]string{}

	n.Send(fmt.Sprintf("👁️ %s WATCH started on %s. rssi_min=%ddBm, confirm=%d, known=%d, learning_until=%s",
		cfg.TagPrefix(), sc.AdapterName(), cfg.RSSIMin, cfg.ConfirmHits, len(b.Fingerprints),
		learnUntil.Local().Format("2006-01-02 15:04")))

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

		now := time.Now()
		learning := now.Before(learnUntil)
		threshold := cfg.RSSIMin
		if inAlertHours(cfg.AlertHours, now) {
			threshold = cfg.RSSIMinNight
		}
		changed := false
		seen := map[string]bool{}

		for _, o := range obs {
			if o.RSSI == nil || *o.RSSI < threshold {
				continue
			}
			rssi := *o.RSSI
			fp := identify.Fingerprint(o)
			if anchored, ok := macFP[o.MAC]; ok {
				fp = anchored // keep this MAC's first fingerprint for the session
			} else {
				macFP[o.MAC] = fp
			}
			id := identify.Identify(o, oui)
			seen[fp] = true
			known := homeMACs[o.MAC]
			if _, ok := b.Fingerprints[fp]; ok {
				known = true
			}

			if learning || known {
				if learnEntry(b, fp, o, id) {
					changed = true
				}
			}

			tr := tracks[fp]
			if tr == nil {
				tr = &track{firstSeen: now, maxRSSI: rssi, minRSSI: rssi, name: o.Name, mac: o.MAC, typ: o.Type}
				tracks[fp] = tr
			}
			tr.hits++
			tr.lastSeen = now
			if rssi > tr.maxRSSI {
				tr.maxRSSI = rssi
			}
			if rssi < tr.minRSSI {
				tr.minRSSI = rssi
			}
			if o.Name != "" && !identify.LooksLikeMAC(o.Name) {
				tr.name = o.Name
			}
			tr.known = known

			if tr.hits >= cfg.ConfirmHits && !tr.inSession {
				tr.inSession = true
				lb := learning
				_ = store.AppendEvent(cfg.EventLog, store.Event{
					TS: nowISO(), Event: "appear", FP: fp, Name: tr.name, MAC: o.MAC,
					Type: o.Type, Vendor: id.Vendor, Kind: id.Kind, Model: id.Model,
					RSSI: &rssi, Known: known, Learning: &lb,
				})
				if !known && !learning {
					if !tr.hasAlerted || now.Sub(tr.alertedAt) >= time.Duration(cfg.AlertCooldown)*time.Second {
						tr.alertedAt = now
						tr.hasAlerted = true
						night := ""
						if inAlertHours(cfg.AlertHours, now) {
							night = " 🌙"
						}
						n.Send(fmt.Sprintf("🚨 %s — UNKNOWN nearby%s %s%s",
							cfg.TagPrefix(), night, deviceLine(tr.name, o.MAC, o.Type, rssi), idSuffix(id)))
					}
				}
			}
		}

		// Close sessions no longer seen strongly.
		for fp, tr := range tracks {
			if seen[fp] {
				continue
			}
			if now.Sub(tr.lastSeen) > time.Duration(cfg.GoneAfter)*time.Second {
				if tr.inSession {
					dur := int(tr.lastSeen.Sub(tr.firstSeen).Seconds())
					mx, mn := tr.maxRSSI, tr.minRSSI
					_ = store.AppendEvent(cfg.EventLog, store.Event{
						TS: nowISO(), Event: "leave", FP: fp, Name: tr.name, MAC: tr.mac,
						Type: tr.typ, RSSIMax: &mx, RSSIMin: &mn, Duration: &dur, Known: tr.known,
					})
				}
				delete(tracks, fp)
				for m, f := range macFP {
					if f == fp {
						delete(macFP, m)
					}
				}
			}
		}

		if learning && !now.Before(learnUntil) {
			b.Meta["learn_done"] = nowISO()
			changed = true
			n.Send(fmt.Sprintf("✅ %s — learning finished. %d known devices. Now alerting on unknowns near (RSSI ≥ %ddBm).",
				cfg.TagPrefix(), len(b.Fingerprints), cfg.RSSIMin))
		}

		if changed {
			_ = store.SaveBaseline(cfg.BaselineFile, b)
		}

		elapsed := time.Since(start)
		if d := time.Duration(cfg.IntervalSeconds)*time.Second - elapsed; d > 0 {
			sleepCtx(ctx, d)
		}
	}
}

// learnEntry updates/creates a baseline entry; returns true on structural change.
func learnEntry(b *store.Baseline, fp string, o model.Observation, id model.Identity) bool {
	entry := b.Fingerprints[fp]
	isNew := entry == nil
	if isNew {
		entry = &store.BaselineEntry{Type: o.Type, FirstSeen: nowISO(), MACs: []string{}}
	}
	newMAC := !contains(entry.MACs, o.MAC)
	if newMAC {
		entry.MACs = append(entry.MACs, o.MAC)
	}
	enriched := false
	if o.Name != "" && !identify.LooksLikeMAC(o.Name) && entry.Name == "" {
		entry.Name, enriched = o.Name, true
	}
	if entry.Vendor == "" && id.Vendor != "" {
		entry.Vendor, enriched = id.Vendor, true
	}
	if entry.Kind == "" && id.Kind != "" {
		entry.Kind, enriched = id.Kind, true
	}
	if entry.Model == "" && id.Model != "" {
		entry.Model, enriched = id.Model, true
	}
	entry.LastSeen = nowISO()
	entry.Count++
	if isNew {
		b.Fingerprints[fp] = entry
	}
	return isNew || newMAC || enriched
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
