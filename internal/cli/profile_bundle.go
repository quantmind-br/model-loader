package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

func init() {
	var out string
	exportCmd := &cobra.Command{
		Use:   "export [id...]",
		Short: "Export profiles as a JSON bundle (all profiles if none given)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			dest := out
			if dest == "" {
				dest = "-"
			}
			if err := exportProfiles(cmd.OutOrStdout(), svc.Store, args, dest); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	exportCmd.Flags().StringVarP(&out, "output", "o", "", "write bundle to file (default: stdout)")
	profileCmd.AddCommand(exportCmd)

	var mode string
	importCmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Import a profile bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			cm := profilestore.ConflictMode(mode)
			if err := importProfiles(cmd.OutOrStdout(), svc.Store, args[0], cm); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	importCmd.Flags().StringVar(&mode, "mode", "merge", "conflict resolution: merge|overwrite|rename")
	profileCmd.AddCommand(importCmd)

	profileCmd.AddCommand(&cobra.Command{
		Use:   "validate <id|name>",
		Short: "Validate a profile against its backend schema",
		Long: `Validate a profile against its backend's flag schema.

Exit codes:
  0  Profile is valid (warnings may still be printed to stderr).
  1  Profile not found or lookup error.
  2  Profile has blocking validation errors.

With --json the result is printed as a JSON object instead of human-readable
lines. Warnings are always printed to stderr regardless of --json.`,
		Example: `  model-loader profile validate my-profile
  model-loader profile validate my-profile --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			code := validateProfile(cmd.OutOrStdout(), cmd.ErrOrStderr(), svc.Store, svc.Val, svc.Resolver, args[0], jsonOut)
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	})
}

func exportProfiles(w io.Writer, store profilestore.Store, ids []string, dest string) error {
	all, err := store.List()
	if err != nil {
		return err
	}
	var selected []domain.Profile
	if len(ids) == 0 {
		selected = all
	} else {
		want := map[string]bool{}
		for _, id := range ids {
			p, rerr := resolveProfileRef(store, id)
			if rerr != nil {
				return rerr
			}
			want[p.ID] = true
		}
		for _, p := range all {
			if want[p.ID] {
				selected = append(selected, p)
			}
		}
	}
	if selected == nil {
		selected = []domain.Profile{}
	}
	bundle := profilestore.ExportBundle{
		SchemaVersion: profilestore.ExportBundleSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		Profiles:      selected,
	}
	if dest == "-" || dest == "" {
		return emitJSON(w, bundle)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := emitJSON(f, bundle); err != nil {
		return err
	}
	fmt.Fprintf(w, "exported %d profile(s) to %s\n", len(selected), dest)
	return nil
}

func importProfiles(w io.Writer, store profilestore.Store, path string, mode profilestore.ConflictMode) error {
	res, err := profilestore.ImportBundle(store, path, mode)
	if err != nil {
		return err
	}
	if jsonOut {
		return emitJSON(w, res)
	}
	fmt.Fprintf(w, "import result: added=%d skipped=%d renamed=%d replaced=%d\n",
		res.Added, res.Skipped, res.Renamed, res.Replaced)
	return nil
}

// validationResult is the JSON-serialisable result of a profile validation.
type validationResult struct {
	ID       string                 `json:"id"`
	Valid    bool                   `json:"valid"`
	Errors   []validator.FieldIssue `json:"errors,omitempty"`
	Warnings []validator.FieldIssue `json:"warnings,omitempty"`
}

// validateProfile returns exit code: 0 ok, 1 lookup error, 2 blocking errors.
// When asJSON is true the result is emitted as JSON instead of human-readable lines.
func validateProfile(out, errw io.Writer, store profilestore.Store, val validator.Validator, resolver backendcatalog.Resolver, ref string, asJSON bool) int {
	p, err := resolveProfileRef(store, ref)
	if err != nil {
		fmt.Fprintln(errw, err)
		return 1
	}
	schema, kind := resolveSchema(resolver, p)
	if val == nil {
		if asJSON {
			_ = emitJSON(out, validationResult{ID: p.ID, Valid: true})
		} else {
			fmt.Fprintln(out, "ok (no validator)")
		}
		return 0
	}
	report := val.Validate(p, schema, kind)
	if asJSON {
		res := validationResult{
			ID:       p.ID,
			Valid:    !report.HasBlockingErrors(),
			Errors:   report.Errors,
			Warnings: report.Warnings,
		}
		_ = emitJSON(out, res)
		if report.HasBlockingErrors() {
			return 2
		}
		return 0
	}
	for _, wm := range report.Warnings {
		fmt.Fprintf(errw, "warning: %s: %s\n", wm.Field, wm.Message)
	}
	if report.HasBlockingErrors() {
		for _, e := range report.Errors {
			fmt.Fprintf(errw, "error: %s: %s\n", e.Field, e.Message)
		}
		return 2
	}
	fmt.Fprintf(out, "ok: %s is valid\n", p.ID)
	return 0
}
