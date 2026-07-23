package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/carlosprados/shadowtrace/internal/store"
	"github.com/spf13/cobra"
)

var eventsTailN int

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Inspect the forensic event log",
	Long: `The event log (--event-log) records one JSON object per line for every device
appear/leave: timestamp, fingerprint, identity, RSSI range, session duration and
whether the device was known. It is the forensic record and the training dataset
for the anomaly detector.

For a live view use your shell:  tail -f ~/.shadowtrace_events.jsonl | jq .`,
}

var eventsTailCmd = &cobra.Command{
	Use:     "tail",
	Short:   "Show the most recent events",
	Example: `  shadowtrace events tail -n 40`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		evs, err := store.LoadEvents(c.EventLog)
		if err != nil {
			return err
		}
		if len(evs) > eventsTailN {
			evs = evs[len(evs)-eventsTailN:]
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "TS\tEVENT\tNAME\tRSSI\tKNOWN\tIDENTITY")
		for _, e := range evs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				e.TS, e.Event, truncate(orDash(e.Name), 22), rssiCol(e), knownCol(e.Known),
				truncate(joinNonEmpty(e.Vendor, e.Kind, e.Model), 28))
		}
		return w.Flush()
	},
}

var eventsStatsCmd = &cobra.Command{
	Use:     "stats",
	Short:   "Summarise the event log",
	Example: `  shadowtrace events stats`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		evs, err := store.LoadEvents(c.EventLog)
		if err != nil {
			return err
		}
		if len(evs) == 0 {
			fmt.Printf("no events yet (%s)\n", c.EventLog)
			return nil
		}
		byType := map[string]int{}
		fps := map[string]bool{}
		var appears, unknownAppears int
		perName := map[string]int{}
		for _, e := range evs {
			byType[e.Event]++
			fps[e.FP] = true
			if e.Event == "appear" {
				appears++
				if !e.Known {
					unknownAppears++
				}
				perName[orDash(e.Name)]++
			}
		}
		fmt.Printf("events:        %d\n", len(evs))
		fmt.Printf("  appear:      %d (unknown: %d)\n", appears, unknownAppears)
		fmt.Printf("  leave:       %d\n", byType["leave"])
		fmt.Printf("distinct fps:  %d\n", len(fps))
		fmt.Printf("span:          %s  ->  %s\n", evs[0].TS, evs[len(evs)-1].TS)

		type kv struct {
			name string
			n    int
		}
		top := make([]kv, 0, len(perName))
		for k, n := range perName {
			top = append(top, kv{k, n})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
		fmt.Println("top devices (by appearances):")
		for i, t := range top {
			if i >= 10 {
				break
			}
			fmt.Printf("  %3d  %s\n", t.n, t.name)
		}
		return nil
	},
}

func rssiCol(e store.Event) string {
	if e.RSSI != nil {
		return fmt.Sprintf("%d", *e.RSSI)
	}
	if e.RSSIMax != nil && e.RSSIMin != nil {
		return fmt.Sprintf("%d..%d", *e.RSSIMin, *e.RSSIMax)
	}
	return "-"
}

func knownCol(known bool) string {
	if known {
		return "known"
	}
	return "UNKNOWN"
}

func init() {
	eventsTailCmd.Flags().IntVarP(&eventsTailN, "n", "n", 20, "number of events to show")
	eventsCmd.AddCommand(eventsTailCmd, eventsStatsCmd)
	rootCmd.AddCommand(eventsCmd)
}
