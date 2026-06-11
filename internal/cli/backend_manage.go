package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/spf13/cobra"
)

func init() {
	var (
		addExecutable string
		addKind       string
	)
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new backend to the catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				addBackend(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0], addExecutable, addKind))
		},
	}
	addCmd.Flags().StringVar(&addExecutable, "executable", "", "backend executable / launch command (required)")
	addCmd.Flags().StringVar(&addKind, "kind", "", "backend kind (e.g. llama-server, vllm, sglang) (required)")
	_ = addCmd.MarkFlagRequired("executable")
	_ = addCmd.MarkFlagRequired("kind")
	backendCmd.AddCommand(addCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a backend from the catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				deleteBackend(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0]))
		},
	}
	backendCmd.AddCommand(deleteCmd)

	setDefaultCmd := &cobra.Command{
		Use:   "set-default <id>",
		Short: "Set the catalog's default backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				setDefaultBackend(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0]))
		},
	}
	backendCmd.AddCommand(setDefaultCmd)
}

// addBackend validates the kind against registered generators, creates the
// backend, and reports the next step (schema generation).
func addBackend(out io.Writer, mgr backendManager, name, executable, kind string) error {
	bk := domain.BackendKind(kind)
	gens := mgr.Generators()
	if _, ok := gens[bk]; !ok {
		return fmt.Errorf("unknown backend kind %q (valid: %s)", kind, knownKinds(gens))
	}
	b, err := mgr.AddBackend(context.Background(), name, executable, bk)
	if err != nil {
		return fmt.Errorf("add backend: %w", err)
	}
	fmt.Fprintf(out, "added backend %s (kind %s); run 'backend schema refresh %s' to generate its schema\n", b.ID, b.Kind, b.ID)
	return nil
}

// deleteBackend resolves the id (exact or unique prefix) and removes it.
func deleteBackend(out io.Writer, mgr backendManager, idOrPrefix string) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	if err := mgr.DeleteBackend(b.ID); err != nil {
		return fmt.Errorf("delete backend: %w", err)
	}
	fmt.Fprintf(out, "deleted backend %s\n", b.ID)
	return nil
}

// setDefaultBackend resolves the id and marks it as the catalog default.
func setDefaultBackend(out io.Writer, mgr backendManager, idOrPrefix string) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	if err := mgr.SetDefaultBackend(b.ID); err != nil {
		return fmt.Errorf("set default backend: %w", err)
	}
	fmt.Fprintf(out, "default backend set to %s\n", b.ID)
	return nil
}

// knownKinds returns the registered backend kinds as a sorted, comma-separated
// list for error messages.
func knownKinds(gens map[domain.BackendKind]backendschema.Generator) string {
	kinds := make([]string, 0, len(gens))
	for k := range gens {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	return strings.Join(kinds, ", ")
}
