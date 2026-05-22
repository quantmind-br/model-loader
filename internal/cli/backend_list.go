package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/spf13/cobra"
)

type backendListItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Executable string `json:"executable"`
	Default    bool   `json:"default"`
	SchemaRef  string `json:"schemaRef,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured backends from the catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				listBackends(cmd.OutOrStdout(), buildSchemaManager(cfg), jsonOut))
		},
	}
	backendCmd.AddCommand(cmd)
}

// listBackends prints all catalog backends, flagging the default one.
func listBackends(out io.Writer, mgr backendManager, asJSON bool) error {
	backends, err := mgr.ListBackends()
	if err != nil {
		return fmt.Errorf("list backends: %w", err)
	}
	defaultID, err := mgr.DefaultBackendID()
	if err != nil {
		return fmt.Errorf("default backend: %w", err)
	}

	items := make([]backendListItem, 0, len(backends))
	for _, b := range backends {
		items = append(items, backendListItem{
			ID:         b.ID,
			Name:       b.Name,
			Kind:       string(b.Kind),
			Executable: b.Executable,
			Default:    b.ID == defaultID,
			SchemaRef:  b.SchemaRef,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	if asJSON {
		return emitJSON(out, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "no backends configured")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		def := ""
		if it.Default {
			def = "yes"
		}
		rows = append(rows, []string{it.ID, dashOr(it.Name), it.Kind, dashOr(it.Executable), def})
	}
	printTable(out, []string{"ID", "NAME", "KIND", "EXECUTABLE", "DEFAULT"}, rows)
	return nil
}
