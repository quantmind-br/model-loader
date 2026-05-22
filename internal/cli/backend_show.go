package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/spf13/cobra"
)

type backendDetail struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Executable    string     `json:"executable"`
	SchemaRef     string     `json:"schemaRef,omitempty"`
	Description   string     `json:"description,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	Default       bool       `json:"default"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	GeneratedAt   *time.Time `json:"generatedAt,omitempty"`
	SourceVersion string     `json:"sourceVersion,omitempty"`
}

func init() {
	cmd := &cobra.Command{
		Use:   "show <backend-id>",
		Short: "Show full metadata for a single backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			return exitOnErr(cmd.ErrOrStderr(),
				showBackend(cmd.OutOrStdout(), buildSchemaManager(cfg), args[0], jsonOut))
		},
	}
	backendCmd.AddCommand(cmd)
}

// showBackend resolves idOrPrefix and prints the backend's full metadata.
func showBackend(out io.Writer, mgr backendManager, idOrPrefix string, asJSON bool) error {
	b, err := resolveBackend(mgr, idOrPrefix)
	if err != nil {
		return err
	}
	defaultID, err := mgr.DefaultBackendID()
	if err != nil {
		return fmt.Errorf("default backend: %w", err)
	}
	d := backendDetail{
		ID:            b.ID,
		Name:          b.Name,
		Kind:          string(b.Kind),
		Executable:    b.Executable,
		SchemaRef:     b.SchemaRef,
		Description:   b.Description,
		Tags:          b.Tags,
		Default:       b.ID == defaultID,
		CreatedAt:     b.Meta.CreatedAt,
		UpdatedAt:     b.Meta.UpdatedAt,
		GeneratedAt:   b.Meta.GeneratedAt,
		SourceVersion: b.Meta.SourceVersion,
	}
	if asJSON {
		return emitJSON(out, d)
	}
	fmt.Fprintf(out, "ID:          %s\n", d.ID)
	fmt.Fprintf(out, "Name:        %s\n", dashOr(d.Name))
	fmt.Fprintf(out, "Kind:        %s\n", d.Kind)
	fmt.Fprintf(out, "Executable:  %s\n", dashOr(d.Executable))
	fmt.Fprintf(out, "Schema ref:  %s\n", dashOr(d.SchemaRef))
	fmt.Fprintf(out, "Default:     %t\n", d.Default)
	if d.Description != "" {
		fmt.Fprintf(out, "Description: %s\n", d.Description)
	}
	if len(d.Tags) > 0 {
		fmt.Fprintf(out, "Tags:        %s\n", strings.Join(d.Tags, ", "))
	}
	fmt.Fprintf(out, "Created:     %s\n", d.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "Updated:     %s\n", d.UpdatedAt.Format(time.RFC3339))
	if d.GeneratedAt != nil {
		fmt.Fprintf(out, "Generated:   %s\n", d.GeneratedAt.Format(time.RFC3339))
	}
	if d.SourceVersion != "" {
		fmt.Fprintf(out, "Source ver:  %s\n", d.SourceVersion)
	}
	return nil
}
