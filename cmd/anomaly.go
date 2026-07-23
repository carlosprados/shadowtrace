package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/carlosprados/shadowtrace/internal/anomaly"
	"github.com/carlosprados/shadowtrace/internal/store"
	"github.com/spf13/cobra"
)

var (
	anomalyTop     int
	anomalyTrainer string
	anomalyPython  string
)

var anomalyCmd = &cobra.Command{
	Use:   "anomaly",
	Short: "Offline anomaly detection over the event log",
	Long: `Hybrid design: the model is TRAINED in Python (scikit-learn IsolationForest,
exported to JSON) and scored — INFERRED — here in Go. Features per sighting:
hour-of-day and weekday (cyclic), RSSI and known-flag. It flags the unusual, e.g.
a known device appearing at an odd hour, that the plain rules miss.`,
}

var anomalyScoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Score events with the trained model (Go inference), most anomalous first",
	Long: `Load the trained model (--model-file) and score every 'appear' event in the log.
Lower score = more anomalous; a negative score is an outlier under the trained
contamination. Train first with 'shadowtrace anomaly train'.`,
	Example: `  shadowtrace anomaly score --top 20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		m, err := anomaly.LoadModel(c.ModelFile)
		if err != nil {
			return fmt.Errorf("load model %s: %w (run 'shadowtrace anomaly train' first)", c.ModelFile, err)
		}
		evs, err := store.LoadEvents(c.EventLog)
		if err != nil {
			return err
		}

		type scored struct {
			ev  store.Event
			dec float64
		}
		var rows []scored
		for _, e := range evs {
			if e.Event != "appear" || e.RSSI == nil {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, e.TS)
			if err != nil {
				continue
			}
			x := anomaly.Features(ts, float64(*e.RSSI), e.Known)
			rows = append(rows, scored{e, m.Decision(x)})
		}
		if len(rows) == 0 {
			fmt.Println("no scorable 'appear' events yet.")
			return nil
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].dec < rows[j].dec })
		fmt.Printf("Model trained %s on %d events. Most anomalous of %d (lower = weirder):\n\n",
			m.TrainedAt, m.NTrain, len(rows))
		for i, r := range rows {
			if i >= anomalyTop {
				break
			}
			ts, _ := time.Parse(time.RFC3339Nano, r.ev.TS)
			fmt.Printf("  %+.3f  %s  %-22s RSSI=%ddBm  %s\n",
				r.dec, ts.Local().Format("Mon 15:04"), orDash(r.ev.Name), *r.ev.RSSI, knownCol(r.ev.Known))
		}
		return nil
	},
}

var anomalyTrainCmd = &cobra.Command{
	Use:   "train",
	Short: "Train the model in Python and export it to JSON",
	Long: `Run the Python trainer (--trainer, default tools/train.py) which fits a
scikit-learn IsolationForest over the event log and writes the JSON model that
'anomaly score' consumes. Requires python3 with scikit-learn + numpy
(the project's 'ml' extra).`,
	Example: `  shadowtrace anomaly train
  shadowtrace anomaly train --trainer ./tools/train.py --python python3`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := currentConfig()
		if _, err := os.Stat(anomalyTrainer); err != nil {
			return fmt.Errorf("trainer not found at %s (pass --trainer)", anomalyTrainer)
		}
		p := exec.Command(anomalyPython, anomalyTrainer,
			"--events", c.EventLog, "--model", c.ModelFile)
		p.Stdout, p.Stderr, p.Stdin = os.Stdout, os.Stderr, os.Stdin
		return p.Run()
	},
}

func init() {
	anomalyScoreCmd.Flags().IntVar(&anomalyTop, "top", 20, "how many anomalous events to show")
	anomalyTrainCmd.Flags().StringVar(&anomalyTrainer, "trainer", "tools/train.py", "path to the Python trainer")
	anomalyTrainCmd.Flags().StringVar(&anomalyPython, "python", "python3", "python interpreter to use")
	anomalyCmd.AddCommand(anomalyScoreCmd, anomalyTrainCmd)
	rootCmd.AddCommand(anomalyCmd)
}
