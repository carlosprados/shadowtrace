// Package cmd wires the Cobra command tree. Every command carries a Long
// description and an Example so a human — or an AI — can learn the whole tool
// from `shadowtrace help` and `--help` alone.
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/carlosprados/shadowtrace/internal/config"
	"github.com/carlosprados/shadowtrace/internal/identify"
	"github.com/carlosprados/shadowtrace/internal/notify"
	"github.com/carlosprados/shadowtrace/internal/oui"
	"github.com/carlosprados/shadowtrace/internal/scan"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var v = viper.New()

var rootCmd = &cobra.Command{
	Use:   "shadowtrace",
	Short: "BLE/Wi-Fi environment intrusion detection and presence watcher",
	Long: `ShadowTrace watches the radio environment around a machine.

In watch mode (the default) it scans the surrounding Bluetooth LE environment,
learns a baseline of habitual devices, and alerts on unknown devices with strong,
sustained signal (i.e. physically near/inside), while writing a forensic event log
that doubles as the training dataset for the anomaly detector.

Detects devices that emit radio, not people: a phone that is off or in airplane
mode is invisible. Treat it as a complementary/forensic layer, not a replacement
for a camera or door sensor.

Configuration precedence (highest first): command-line flag, environment variable
(legacy SHADOWTRACE names, e.g. WATCH_RSSI_MIN), built-in default. The systemd unit
loads them from ~/.config/shadowtrace.env.

Start here:
  shadowtrace scan            # one-shot look at what's around right now
  shadowtrace watch           # run the IDS loop
  shadowtrace events stats    # summarise the forensic log
  shadowtrace anomaly score   # flag unusual sightings (needs a trained model)`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error { return rootCmd.Execute() }

func init() {
	pf := rootCmd.PersistentFlags()

	strFlags := []struct{ key, env, def, usage string }{
		{config.KeyAppName, "APP_NAME", "ShadowTrace", "display name used in alerts"},
		{config.KeyLocationTag, "LOCATION_TAG", "", "optional location tag shown in alerts, e.g. [Office]"},
		{config.KeyMode, "MODE", "watch", "operating mode: watch | presence"},
		{config.KeyTgToken, "TELEGRAM_BOT_TOKEN", "", "Telegram bot token (empty = print alerts to stdout)"},
		{config.KeyTgChat, "TELEGRAM_CHAT_ID", "", "Telegram chat id"},
		{config.KeyAdapter, "BT_ADAPTER", "", "preferred Bluetooth adapter, e.g. hci1 (empty = first)"},
		{config.KeyTransport, "SCAN_TRANSPORT", "auto", "discovery transport: auto | le | bredr"},
		{config.KeyAlertHours, "ALERT_HOURS", "", "reinforced hours start-end (24h local), e.g. 0-7"},
		{config.KeyHomeMACs, "HOME_MACS", "", "comma-separated MACs always treated as known"},
		{config.KeyBaselineFile, "BASELINE_FILE", "~/.shadowtrace_baseline.json", "learned, hand-editable baseline"},
		{config.KeyEventLog, "EVENT_LOG", "~/.shadowtrace_events.jsonl", "forensic event log (JSONL)"},
		{config.KeyModelFile, "ANOMALY_MODEL", "~/.shadowtrace_anomaly.json", "trained anomaly model (JSON)"},
		{config.KeyOUIFile, "OUI_FILE", "~/.shadowtrace_oui.tsv", "vendor OUI database cache"},
		{config.KeyOUIURL, "OUI_URL", oui.DefaultURL, "URL to download the OUI database from"},
		{config.KeyStateFile, "STATE_FILE", "~/.shadowtrace_state.json", "presence-mode state file"},
		{config.KeyNameAllowlist, "NAME_WHITELIST", "", "presence: only track names containing these substrings"},
		{config.KeyIgnoreMACs, "IGNORE_MACS", "", "presence: comma-separated MACs to ignore"},
		{config.KeyWifiHosts, "WIFI_HOSTS", "", "presence: hosts to ping (name@host or host)"},
		{config.KeyARPSubnets, "ARP_SUBNETS", "", "presence: CIDRs to sweep (auto-detected if empty)"},
	}
	for _, f := range strFlags {
		pf.String(f.key, f.def, f.usage)
		_ = v.BindPFlag(f.key, pf.Lookup(f.key))
		_ = v.BindEnv(f.key, f.env)
	}

	intFlags := []struct {
		key, env string
		def      int
		usage    string
	}{
		{config.KeyWindow, "SCAN_WINDOW_SECONDS", 8, "discovery window per cycle (seconds)"},
		{config.KeyInterval, "SCAN_INTERVAL_SECONDS", 20, "full cycle duration (seconds)"},
		{config.KeyRSSIMin, "WATCH_RSSI_MIN", -70, "watch: min RSSI to count as near (weaker = farther)"},
		{config.KeyRSSIMinNight, "WATCH_RSSI_MIN_NIGHT", 0, "watch: RSSI threshold during alert-hours (0 = same)"},
		{config.KeyConfirmHits, "WATCH_CONFIRM_HITS", 2, "watch: consecutive strong windows before present"},
		{config.KeyGoneAfter, "WATCH_GONE_AFTER_SECONDS", 120, "watch: grace before a present device is gone"},
		{config.KeyLearnSeconds, "WATCH_LEARN_SECONDS", 86400, "watch: learning window that fills the baseline"},
		{config.KeyOUIMaxAge, "OUI_MAX_AGE_DAYS", 30, "auto-refresh OUI db when older than N days (0 = never)"},
		{config.KeyAlertCooldown, "ALERT_COOLDOWN_SECONDS", 600, "watch: min seconds between repeat alerts per device"},
		{config.KeyPresGoneAfter, "GONE_AFTER_SECONDS", 60, "presence: seconds unseen before LOST"},
		{config.KeyARPSweepLimit, "ARP_SWEEP_LIMIT", 256, "presence: max hosts to ping per sweep"},
		{config.KeyARPTimeoutMS, "ARP_TIMEOUT_MS", 500, "presence: per-ping timeout (ms)"},
	}
	for _, f := range intFlags {
		pf.Int(f.key, f.def, f.usage)
		_ = v.BindPFlag(f.key, pf.Lookup(f.key))
		_ = v.BindEnv(f.key, f.env)
	}

	boolFlags := []struct {
		key, env string
		def      bool
		usage    string
	}{
		{config.KeyContinuous, "CONTINUOUS_DISCOVERY", true, "keep discovery on between cycles"},
		{config.KeyOUIAuto, "OUI_AUTO_UPDATE", true, "auto-download OUI db when missing/stale"},
		{config.KeyMDNS, "MDNS_DISCOVERY", true, "presence: mDNS discovery via avahi-browse"},
		{config.KeyARP, "ARP_DISCOVERY", false, "presence: ARP/neighbour discovery"},
		{config.KeyARPSweep, "ARP_SWEEP", false, "presence: ping-sweep subnets before reading neighbours"},
		{config.KeyDebug, "DEBUG", false, "verbose debug logging"},
	}
	for _, f := range boolFlags {
		pf.Bool(f.key, f.def, f.usage)
		_ = v.BindPFlag(f.key, pf.Lookup(f.key))
		_ = v.BindEnv(f.key, f.env)
	}
}

func currentConfig() config.Config { return config.Load(v) }

func newNotifier(c config.Config) *notify.Notifier {
	return notify.New(c.TelegramToken, c.TelegramChatID)
}

func newScanner(c config.Config) (*scan.Scanner, error) {
	sc, err := scan.NewScanner(c)
	if err != nil {
		return nil, fmt.Errorf("bluetooth init failed (need BlueZ + adapter + system D-Bus): %w", err)
	}
	return sc, nil
}

// ouiLookup loads the OUI vendor database (auto-refreshing it when enabled and
// stale) and returns a nil-safe lookup for the identify layer.
func ouiLookup(c config.Config) identify.VendorFunc {
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	var db *oui.DB
	if c.OUIAuto {
		maxAge := time.Duration(c.OUIMaxAgeDays) * 24 * time.Hour
		db, _ = oui.EnsureFresh(ctx, c.OUIFile, c.OUIURL, maxAge, false)
	} else {
		db, _ = oui.Load(c.OUIFile)
	}
	return db.Vendor
}
