// Command shadowtrace is a self-contained BLE/Wi-Fi environment intrusion
// detector and presence watcher. Run `shadowtrace help` to discover everything.
package main

import (
	"fmt"
	"os"

	"github.com/carlosprados/shadowtrace/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
