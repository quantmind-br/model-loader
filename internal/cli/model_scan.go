package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/spf13/cobra"
)

// modelListItem is the JSON/table view of a discovered local model.
type modelListItem struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	SizeBytes    int64  `json:"sizeBytes"`
	Quant        string `json:"quant,omitempty"`
	Params       string `json:"params,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

func init() {
	var paths []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List local GGUF models found under the configured search paths",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			search := cfg.Models.SearchPaths
			if len(paths) > 0 {
				search = paths
			}
			return exitOnErr(cmd.ErrOrStderr(),
				listLocalModels(cmd.OutOrStdout(), cmd.ErrOrStderr(), buildScanner(), search, jsonOut))
		},
	}
	cmd.Flags().StringArrayVar(&paths, "path", nil, "override search path(s) to scan (repeatable)")
	modelCmd.AddCommand(cmd)
}

// listLocalModels scans paths and prints the discovered models. Per-root scan
// errors are warned to errw but do not fail the command.
func listLocalModels(out, errw io.Writer, scanner modelscanner.Scanner, paths []string, asJSON bool) error {
	if len(paths) == 0 {
		return errors.New("no model search paths configured (set models.search_paths or pass --path)")
	}
	ch, err := scanner.Scan(context.Background(), paths)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	var items []modelListItem
	// Drain until ch is closed; ScanEventProgress and ScanEventDone are intentionally ignored.
	for ev := range ch {
		switch ev.Type {
		case domain.ScanEventFile:
			if ev.File != nil {
				items = append(items, modelListItem{
					Path:         ev.File.Path,
					Name:         ev.File.Name,
					SizeBytes:    ev.File.SizeBytes,
					Quant:        ev.File.Quant,
					Params:       ev.File.Params,
					Architecture: ev.File.Architecture,
				})
			}
		case domain.ScanEventError:
			if ev.Error != nil {
				fmt.Fprintf(errw, "warning: scan %s: %v\n", ev.Root, ev.Error)
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Path < items[j].Path })

	if asJSON {
		if items == nil {
			items = []modelListItem{}
		}
		return emitJSON(out, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "no models found")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, m := range items {
		rows = append(rows, []string{
			dashOr(m.Name), dashOr(m.Params), dashOr(m.Quant), humanBytes(m.SizeBytes), m.Path,
		})
	}
	printTable(out, []string{"NAME", "PARAMS", "QUANT", "SIZE", "PATH"}, rows)
	return nil
}
