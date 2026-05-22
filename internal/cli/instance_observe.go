package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/spf13/cobra"
)

func init() {
	var follow bool
	logsCmd := &cobra.Command{
		Use:   "logs <pid|id>",
		Short: "Print (or follow) an instance's log",
		Args:  cobra.ExactArgs(1),
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, args []string) error {
			if follow {
				return followLogs(out, mgr, args[0], 1*time.Second)
			}
			return printLogsOnce(out, mgr, args[0])
		}),
	}
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log (like tail -f)")
	instanceCmd.AddCommand(logsCmd)

	var watch bool
	var interval time.Duration
	metricsCmd := &cobra.Command{
		Use:   "metrics <pid|id>",
		Short: "Show recent metrics for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, stateDir string, args []string) error {
			metricsDir := filepath.Join(stateDir, "metrics")
			if watch {
				return watchMetrics(out, mgr, metricsDir, args[0], interval)
			}
			return printMetricsOnce(out, mgr, metricsDir, args[0], jsonOut)
		}),
	}
	metricsCmd.Flags().BoolVarP(&watch, "watch", "w", false, "continuously re-poll metrics (like watch)")
	metricsCmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "polling interval for --watch")
	instanceCmd.AddCommand(metricsCmd)
}

// printLogsOnce opens the instance's log via TailLogs and copies it to out.
func printLogsOnce(out io.Writer, mgr processmgr.Manager, ref string) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	rc, err := mgr.TailLogs(ri.PID)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(out, rc)
	return err
}

// followLogs copies the log then re-opens on interval and copies any growth.
// It runs until the process is interrupted (Ctrl-C terminates the command).
func followLogs(out io.Writer, mgr processmgr.Manager, ref string, interval time.Duration) error {
	if err := printLogsOnce(out, mgr, ref); err != nil {
		return err
	}
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	// Best-effort tail: re-open and skip already-emitted bytes.
	var offset int64
	if rc, e := mgr.TailLogs(ri.PID); e == nil {
		n, _ := io.Copy(io.Discard, rc)
		offset = n
		rc.Close()
	}
	for {
		time.Sleep(interval)
		rc, e := mgr.TailLogs(ri.PID)
		if e != nil {
			return e
		}
		skipped, _ := io.CopyN(io.Discard, rc, offset)
		n, _ := io.Copy(out, rc)
		rc.Close()
		offset = skipped + n
	}
}

// printMetricsOnce reads the last 5 minutes of metrics for the resolved
// instance and prints the latest record (table or JSON).
func printMetricsOnce(out io.Writer, mgr processmgr.Manager, metricsDir, ref string, asJSON bool) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	recs, err := metricsstore.Read(metricsDir, ri.ProfileID, time.Now().Add(-5*time.Minute))
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		fmt.Fprintln(out, "(no recent metrics)")
		return nil
	}
	last := recs[len(recs)-1]
	if asJSON {
		return emitJSON(out, last)
	}
	fmt.Fprintf(out, "Profile: %s  pid %d\n", ri.ProfileID, ri.PID)
	fmt.Fprintf(out, "tok/s:   %.1f\n", last.TokensPerSec)
	fmt.Fprintf(out, "TTFT:    %dms\n", last.TTFTMs)
	fmt.Fprintf(out, "RPS:     %.2f\n", last.RPS)
	fmt.Fprintf(out, "Slot util: %.0f%%\n", last.SlotUtilization*100)
	return nil
}

// watchMetrics continuously re-polls metrics on interval until interrupted.
func watchMetrics(out io.Writer, mgr processmgr.Manager, metricsDir, ref string, interval time.Duration) error {
	for {
		fmt.Fprint(out, "\033[H\033[2J") // clear screen
		if err := printMetricsOnce(out, mgr, metricsDir, ref, false); err != nil {
			return err
		}
		time.Sleep(interval)
	}
}
