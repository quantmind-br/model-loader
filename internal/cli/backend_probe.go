package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/spf13/cobra"
)

type probeItem struct {
	BackendID string `json:"backendId"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latencyMs"`
	Detail    string `json:"detail,omitempty"`
	Error     string `json:"error,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "probe [backend-id]",
		Short: "Health-check backend binaries (--version/--help); probes all when no id is given",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			filter := ""
			if len(args) == 1 {
				b, rerr := resolveBackend(buildSchemaManager(cfg), args[0])
				if rerr != nil {
					return exitOnErr(cmd.ErrOrStderr(), rerr)
				}
				filter = b.ID
			}
			return exitOnErr(cmd.ErrOrStderr(),
				probeBackends(cmd.OutOrStdout(), buildProber(cfg), filter, jsonOut))
		},
	}
	backendCmd.AddCommand(cmd)
}

// probeBackends drains the prober channel, skips the terminal Done event, and
// (when filterID != "") keeps only the matching backend.
func probeBackends(out io.Writer, prober backendProber, filterID string, asJSON bool) error {
	ch, err := prober.Probe(context.Background())
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	items := make([]probeItem, 0)
	for ev := range ch {
		if ev.Done {
			continue
		}
		if filterID != "" && ev.BackendID != filterID {
			continue
		}
		it := probeItem{
			BackendID: ev.BackendID,
			Status:    string(ev.Status),
			LatencyMS: ev.Latency.Milliseconds(),
			Detail:    ev.Detail,
		}
		if ev.Err != nil {
			it.Error = ev.Err.Error()
		}
		items = append(items, it)
	}

	if asJSON {
		return emitJSON(out, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "no backends probed")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		detail := it.Detail
		if it.Error != "" {
			detail = it.Error
		}
		rows = append(rows, []string{it.BackendID, it.Status, fmt.Sprintf("%dms", it.LatencyMS), clip(dashOr(detail), 60)})
	}
	printTable(out, []string{"BACKEND", "STATUS", "LATENCY", "DETAIL"}, rows)
	return nil
}
