package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/carlosprados/shadowtrace/internal/store"
	"github.com/spf13/cobra"
)

var baselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Inspect and manage the learned baseline of known devices",
	Long: `The baseline (--baseline-file) is the hand-editable allowlist of habitual
devices, keyed by fingerprint. It is filled during watch-mode learning. You can
list it, inspect one entry, or forget an entry that shouldn't be treated as known
(e.g. a recurring neighbour).`,
}

var baselineListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List known devices in the baseline",
	Example: `  shadowtrace baseline list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		b := store.LoadBaseline(c.BaselineFile)
		if len(b.Fingerprints) == 0 {
			fmt.Printf("baseline empty (%s)\n", c.BaselineFile)
			return nil
		}
		fps := make([]string, 0, len(b.Fingerprints))
		for fp := range b.Fingerprints {
			fps = append(fps, fp)
		}
		sort.Slice(fps, func(i, j int) bool {
			return b.Fingerprints[fps[i]].Count > b.Fingerprints[fps[j]].Count
		})
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "COUNT\tNAME\tVENDOR\tKIND\tLAST_SEEN\tFINGERPRINT")
		for _, fp := range fps {
			e := b.Fingerprints[fp]
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
				e.Count, orDash(e.Name), orDash(e.Vendor), orDash(e.Kind), orDash(e.LastSeen), truncate(fp, 46))
		}
		fmt.Fprintf(w, "\n%d known devices | learn_until=%s\n", len(fps), b.Meta["learn_until"])
		return w.Flush()
	},
}

var baselineShowCmd = &cobra.Command{
	Use:     "show <fingerprint>",
	Short:   "Show one baseline entry as JSON",
	Args:    cobra.ExactArgs(1),
	Example: `  shadowtrace baseline show 'fp:n=fenix 3 HR|c=135'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		b := store.LoadBaseline(c.BaselineFile)
		e, ok := b.Fingerprints[args[0]]
		if !ok {
			return fmt.Errorf("fingerprint not found: %s", args[0])
		}
		out, _ := json.MarshalIndent(e, "", "  ")
		fmt.Println(string(out))
		return nil
	},
}

var baselineForgetCmd = &cobra.Command{
	Use:     "forget <fingerprint>",
	Short:   "Remove an entry so it is treated as unknown again",
	Args:    cobra.ExactArgs(1),
	Example: `  shadowtrace baseline forget 'fp:c=6'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		b := store.LoadBaseline(c.BaselineFile)
		if _, ok := b.Fingerprints[args[0]]; !ok {
			return fmt.Errorf("fingerprint not found: %s", args[0])
		}
		delete(b.Fingerprints, args[0])
		if err := store.SaveBaseline(c.BaselineFile, b); err != nil {
			return err
		}
		fmt.Println("forgotten:", args[0])
		return nil
	},
}

func init() {
	baselineCmd.AddCommand(baselineListCmd, baselineShowCmd, baselineForgetCmd)
	rootCmd.AddCommand(baselineCmd)
}
