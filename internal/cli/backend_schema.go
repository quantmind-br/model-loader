package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
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

	refreshCmd := &cobra.Command{
		Use:   "refresh <backend-id>",
		Short: "Re-generate a backend's schema from its --help output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			if err := refreshSchema(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0]); err != nil {
				return exitOnErr(cmd.ErrOrStderr(), err)
			}
			// Seed a default presentation so `backend schema show` reflects groups
			// immediately, matching what app.Bootstrap does (idempotent).
			_, _ = backendschema.EnsurePresentations(buildCatalogStore(cfg), buildSchemaStore(cfg))
			return nil
		},
	}
	backendSchemaCmd.AddCommand(refreshCmd)

	var applyFile string
	applyCmd := &cobra.Command{
		Use:   "apply <backend-id> -f <file>",
		Short: "Apply a hand-edited schema JSON, marking it editable so refresh preserves it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				applySchema(cmd.OutOrStdout(), buildSchemaManager(cfg), buildSchemaStore(cfg), args[0], applyFile))
		},
	}
	applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "path to the schema JSON file to apply (required)")
	_ = applyCmd.MarkFlagRequired("file")
	backendSchemaCmd.AddCommand(applyCmd)
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

// refreshSchema resolves the backend and triggers manager-driven regeneration
// from the backend's --help output. Editable schemas are deleted first by the
// manager so the regeneration is fresh.
func refreshSchema(out io.Writer, mgr backendManager, idOrPrefix string) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	if err := mgr.RefreshSchema(b.ID); err != nil {
		return fmt.Errorf("refresh schema: %w", err)
	}
	fmt.Fprintf(out, "refreshed schema for %s\n", b.ID)
	return nil
}

// applySchema reads a hand-edited schema from file, reconciles its identity with
// the target backend, marks it editable (so RefreshSchema preserves it), and
// persists it via the schema store.
func applySchema(out io.Writer, mgr backendManager, store backendcatalog.SchemaStore, idOrPrefix, file string) error {
	if file == "" {
		return fmt.Errorf("a schema file is required (-f/--file)")
	}
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var schema domain.BackendValidationSchema
	if err := dec.Decode(&schema); err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	if schema.BackendID != "" && schema.BackendID != b.ID {
		return fmt.Errorf("schema backendId %q does not match backend %q", schema.BackendID, b.ID)
	}
	if schema.BackendKind != "" && schema.BackendKind != b.Kind {
		return fmt.Errorf("schema backendKind %q does not match backend kind %q", schema.BackendKind, b.Kind)
	}
	schema.BackendID = b.ID
	schema.BackendKind = b.Kind
	schema.Source.Editable = true

	ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
	if err := store.Save(ref, schema); err != nil {
		return fmt.Errorf("save schema: %w", err)
	}
	fmt.Fprintf(out, "applied schema for %s (editable=true, %d flags)\n", b.ID, len(schema.Flags))
	return nil
}
