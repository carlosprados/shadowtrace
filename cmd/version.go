package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X .../cmd.Version=...".
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the version and build info",
	Example: `  shadowtrace version`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("shadowtrace %s (%s %s/%s)\n", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	},
}

func init() { rootCmd.AddCommand(versionCmd) }
