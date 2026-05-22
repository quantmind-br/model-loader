package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/spf13/cobra"
)

// backendSchemaCmd groups schema inspection and editing under `backend schema`.
var backendSchemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Show, refresh, and apply backend validation schemas",
}

func init() {
	backendCmd.AddCommand(backendSchemaCmd)

	showCmd := &cobra.Command{
		Use:   "show <backend-id>",
		Short: "Show a backend's validation schema (summary, or full JSON with --json)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				showSchema(cmd.OutOrStdout(), buildSchemaManager(cfg), buildSchemaStore(cfg), args[0], jsonOut))
		},
	}
	backendSchemaCmd.AddCommand(showCmd)
}

// showSchema resolves the backend, loads its schema, and prints a summary (or
// the full schema as JSON when asJSON is set).
func showSchema(out io.Writer, mgr backendManager, store backendcatalog.SchemaStore, idOrPrefix string, asJSON bool) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
	schema, err := store.Load(ref)
	if err != nil {
		return fmt.Errorf("load schema for %s: %w", b.ID, err)
	}
	if asJSON {
		return emitJSON(out, schema)
	}
	fmt.Fprintf(out, "Backend:   %s\n", b.ID)
	fmt.Fprintf(out, "Kind:      %s\n", schema.BackendKind)
	fmt.Fprintf(out, "Version:   %d\n", schema.SchemaVersion)
	fmt.Fprintf(out, "Editable:  %t\n", schema.Source.Editable)
	if schema.Source.SourceVersion != "" {
		fmt.Fprintf(out, "Source:    %s\n", schema.Source.SourceVersion)
	}
	fmt.Fprintf(out, "Flags:     %d\n", len(schema.Flags))
	if schema.Presentation != nil && len(schema.Presentation.Groups) > 0 {
		groups := append([]domain.PresentationGroup(nil), schema.Presentation.Groups...)
		sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
		names := make([]string, 0, len(groups))
		for _, g := range groups {
			label := fmt.Sprintf("%s(%d)", g.Name, len(g.Flags))
			if g.Highlighted {
				label += "*"
			}
			names = append(names, label)
		}
		fmt.Fprintf(out, "Groups:    %s\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(out, "Rules:     %d\n", len(schema.Rules))
	return nil
}
