package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/carlosprados/shadowtrace/internal/identify"
	"github.com/spf13/cobra"
)

var scanJSON bool

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Run one scan window and print what's around right now",
	Long: `Perform a single discovery window and print every device seen, with its
fingerprint and best-effort identification (vendor / kind / model). This does not
touch the baseline or the event log — it is a read-only look at the environment,
handy for discovery, calibration and debugging.`,
	Example: `  shadowtrace scan
  shadowtrace scan --adapter hci0 --window 12
  shadowtrace scan --json | jq 'sort_by(.rssi)'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		sc, err := newScanner(c)
		if err != nil {
			return err
		}
		defer sc.Close()

		obs, err := sc.ScanOnce(context.Background())
		if err != nil {
			return err
		}
		oui := ouiLookup(c)
		sort.Slice(obs, func(i, j int) bool {
			ri, rj := -999, -999
			if obs[i].RSSI != nil {
				ri = *obs[i].RSSI
			}
			if obs[j].RSSI != nil {
				rj = *obs[j].RSSI
			}
			return ri > rj
		})

		if scanJSON {
			type row struct {
				MAC         string `json:"mac"`
				Name        string `json:"name"`
				Type        string `json:"type"`
				RSSI        *int   `json:"rssi"`
				Fingerprint string `json:"fingerprint"`
				Vendor      string `json:"vendor,omitempty"`
				Kind        string `json:"kind,omitempty"`
				Model       string `json:"model,omitempty"`
				Note        string `json:"note,omitempty"`
			}
			out := make([]row, 0, len(obs))
			for _, o := range obs {
				id := identify.Identify(o, oui)
				out = append(out, row{o.MAC, o.Name, o.Type, o.RSSI, identify.Fingerprint(o),
					id.Vendor, id.Kind, id.Model, id.Note})
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "RSSI\tMAC\tNAME\tTYPE\tIDENTITY\tFINGERPRINT")
		for _, o := range obs {
			rssi := "-"
			if o.RSSI != nil {
				rssi = fmt.Sprintf("%d", *o.RSSI)
			}
			id := identify.Identify(o, oui)
			ident := joinNonEmpty(id.Vendor, id.Kind, id.Model, id.Note)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				rssi, o.MAC, truncate(orDash(o.Name), 22), o.Type, truncate(ident, 30), identify.Fingerprint(o))
		}
		fmt.Fprintf(w, "\n%d devices\n", len(obs))
		return w.Flush()
	},
}

func init() {
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "output JSON instead of a table")
	rootCmd.AddCommand(scanCmd)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func joinNonEmpty(parts ...string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += " / "
		}
		out += p
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
