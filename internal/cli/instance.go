package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/spf13/cobra"
)

func init() {
	instanceCmd := &cobra.Command{
		Use:   "instance",
		Short: "Manage running llama-server instances",
	}
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List running instances",
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, _ []string) error {
			return listInstances(out, mgr, jsonOut)
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "show <pid|id>",
		Short: "Show a single instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, args []string) error {
			return showInstance(out, mgr, args[0], jsonOut)
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "history",
		Short: "List exited instances",
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, _ []string) error {
			return listHistory(out, mgr, jsonOut)
		}),
	})
	rootCmd.AddCommand(instanceCmd)
}

// instanceReadRunE wires Bootstrap (no lock — read-only) + error→ExitError.
// The string passed to fn is cfg.Paths.StateDir (for the metrics dir, etc.).
func instanceReadRunE(fn func(out io.Writer, mgr processmgr.Manager, stateDir string, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		svc, err := app.Bootstrap(logLevel)
		if err != nil {
			return &ExitError{Code: 1}
		}
		defer svc.Close()
		if err := fn(cmd.OutOrStdout(), svc.Mgr, svc.Cfg.Paths.StateDir, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

// resolveInstance matches ref against PID (numeric) then ProfileID (exact, then unique prefix).
func resolveInstance(mgr processmgr.Manager, ref string) (domain.RunningInstance, error) {
	insts := mgr.List()
	if pid, err := strconv.Atoi(ref); err == nil {
		for _, ri := range insts {
			if ri.PID == pid {
				return ri, nil
			}
		}
	}
	var byPrefix []domain.RunningInstance
	for _, ri := range insts {
		if ri.ProfileID == ref {
			return ri, nil
		}
		if strings.HasPrefix(ri.ProfileID, ref) {
			byPrefix = append(byPrefix, ri)
		}
	}
	switch len(byPrefix) {
	case 1:
		return byPrefix[0], nil
	case 0:
		return domain.RunningInstance{}, fmt.Errorf("instance not found: %s", ref)
	default:
		return domain.RunningInstance{}, fmt.Errorf("ambiguous instance ref %q matches %d instances; use the pid", ref, len(byPrefix))
	}
}

func instanceStatus(ri domain.RunningInstance) string {
	switch {
	case ri.Crashed:
		return "crashed"
	case ri.ExitedAt != nil:
		return "exited"
	default:
		return "running"
	}
}

func listInstances(w io.Writer, mgr processmgr.Manager, asJSON bool) error {
	insts := mgr.List()
	if asJSON {
		if insts == nil {
			insts = []domain.RunningInstance{}
		}
		return emitJSON(w, insts)
	}
	rows := make([][]string, 0, len(insts))
	for _, ri := range insts {
		rows = append(rows, []string{
			strconv.Itoa(ri.PID),
			clip(ri.ProfileID, 24),
			strconv.Itoa(ri.Port),
			instanceStatus(ri),
			ri.StartedAt.Format(time.RFC3339),
		})
	}
	printTable(w, []string{"PID", "PROFILE", "PORT", "STATUS", "STARTED"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no running instances)")
	}
	return nil
}

func showInstance(w io.Writer, mgr processmgr.Manager, ref string, asJSON bool) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(w, ri)
	}
	fmt.Fprintf(w, "PID:      %d\n", ri.PID)
	fmt.Fprintf(w, "Profile:  %s\n", ri.ProfileID)
	fmt.Fprintf(w, "Port:     %d\n", ri.Port)
	fmt.Fprintf(w, "Status:   %s\n", instanceStatus(ri))
	fmt.Fprintf(w, "Started:  %s\n", ri.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Log:      %s\n", dashOr(ri.LogPath))
	if ri.BinaryPath != "" {
		fmt.Fprintf(w, "Binary:   %s\n", ri.BinaryPath)
	}
	return nil
}

func listHistory(w io.Writer, mgr processmgr.Manager, asJSON bool) error {
	hist := mgr.History()
	if asJSON {
		if hist == nil {
			hist = []domain.ExitedInstance{}
		}
		return emitJSON(w, hist)
	}
	rows := make([][]string, 0, len(hist))
	for _, ei := range hist {
		rows = append(rows, []string{
			strconv.Itoa(ei.PID),
			clip(ei.ProfileID, 24),
			ei.ExitedAt.Format(time.RFC3339),
			strconv.FormatInt(ei.DurationSeconds, 10) + "s",
		})
	}
	printTable(w, []string{"PID", "PROFILE", "EXITED", "DURATION"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no exited instances)")
	}
	return nil
}
