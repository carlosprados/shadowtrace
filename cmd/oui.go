package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/carlosprados/shadowtrace/internal/oui"
	"github.com/spf13/cobra"
)

var ouiCmd = &cobra.Command{
	Use:   "oui",
	Short: "Manage the MAC-vendor (OUI) database",
	Long: `Vendor identification for devices with a public MAC uses a locally cached OUI
database (--oui-file), downloaded from --oui-url (Wireshark's manuf list by default).
It auto-refreshes when missing or older than --oui-max-age-days (unless --oui-auto
is false). Use 'oui update' to force a refresh now.`,
}

var ouiUpdateCmd = &cobra.Command{
	Use:     "update",
	Short:   "Force-download the OUI database now",
	Example: `  shadowtrace oui update`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		fmt.Printf("downloading %s -> %s\n", c.OUIURL, c.OUIFile)
		if err := oui.Update(ctx, c.OUIURL, c.OUIFile); err != nil {
			return err
		}
		db, _ := oui.Load(c.OUIFile)
		fmt.Printf("done: %d vendor prefixes\n", db.Len())
		return nil
	},
}

var ouiInfoCmd = &cobra.Command{
	Use:     "info",
	Short:   "Show the OUI cache status",
	Example: `  shadowtrace oui info`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		fmt.Printf("file:    %s\n", c.OUIFile)
		fmt.Printf("url:     %s\n", c.OUIURL)
		fmt.Printf("max-age: %d days (auto=%v)\n", c.OUIMaxAgeDays, c.OUIAuto)
		if age, ok := oui.Age(c.OUIFile); ok {
			db, _ := oui.Load(c.OUIFile)
			fmt.Printf("cached:  %d prefixes, age %s\n", db.Len(), age.Round(time.Minute))
		} else {
			fmt.Println("cached:  (not downloaded yet)")
		}
		return nil
	},
}

var ouiLookupCmd = &cobra.Command{
	Use:     "lookup <mac>",
	Short:   "Look up the vendor for a MAC address",
	Args:    cobra.ExactArgs(1),
	Example: `  shadowtrace oui lookup F4:95:CE:1D:0E:77`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		db, err := oui.Load(c.OUIFile)
		if err != nil {
			return err
		}
		v := db.Vendor(args[0])
		if v == "" {
			v = "(unknown)"
		}
		fmt.Println(v)
		return nil
	},
}

func init() {
	ouiCmd.AddCommand(ouiUpdateCmd, ouiInfoCmd, ouiLookupCmd)
	rootCmd.AddCommand(ouiCmd)
}
