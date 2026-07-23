package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/carlosprados/shadowtrace/internal/engine"
	"github.com/spf13/cobra"
)

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Run the environment intrusion-detection loop",
	Long: `Continuously scan the BLE environment and alert on unknown devices that are
near (RSSI >= --rssi-min) and sustained (seen --confirm-hits windows in a row).

For the first --learn-seconds (default 24h) nothing alerts: every strong device is
added to the baseline (--baseline-file). After that, unknown near devices raise a
Telegram alert (throttled by --alert-cooldown) and every appear/leave is written to
the forensic log (--event-log). Runs until Ctrl+C / SIGTERM.`,
	Example: `  # Learn for 24h on hci1, alert below -70 dBm, reinforce at night
  shadowtrace watch --adapter hci1 --rssi-min -70 --alert-hours 0-7

  # Quick smoke test: learn 5s then alert on everything nearby
  shadowtrace watch --learn-seconds 5 --confirm-hits 1 --rssi-min -90 \
    --baseline-file /tmp/b.json --event-log /tmp/e.jsonl`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signalContext()
		defer cancel()
		c := currentConfig()
		sc, err := newScanner(c)
		if err != nil {
			return err
		}
		defer sc.Close()
		return engine.Watch(ctx, c, sc, newNotifier(c), ouiLookup(c))
	},
}

var presenceCmd = &cobra.Command{
	Use:   "presence",
	Short: "Run the legacy presence tracker (DETECTED/LOST)",
	Long: `Track specific devices and fire DETECTED/LOST alerts, fusing BLE with optional
Wi-Fi ICMP (--wifi-hosts), mDNS (--mdns) and ARP (--arp) discovery. Filter with
--name-whitelist / --ignore-macs. Runs until Ctrl+C / SIGTERM.`,
	Example: `  shadowtrace presence --name-whitelist "iPhone,Watch" --wifi-hosts phone@192.168.1.23
  shadowtrace presence --arp --arp-sweep`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signalContext()
		defer cancel()
		c := currentConfig()
		sc, err := newScanner(c)
		if err != nil {
			return err
		}
		defer sc.Close()
		return engine.Presence(ctx, c, sc, newNotifier(c))
	},
}

func init() {
	rootCmd.AddCommand(watchCmd, presenceCmd)
}
