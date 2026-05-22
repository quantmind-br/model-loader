package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func init() {
	var modeFlag string

	cmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Import a profile bundle from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			mode := profilestore.ConflictModeMerge
			if modeFlag != "" {
				mode = profilestore.ConflictMode(modeFlag)
			}

			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			res, err := profilestore.ImportBundle(svc.Store, path, mode)
			if err != nil {
				svc.Logger.Error("import_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "import failed: %v\n", err)
				return &ExitError{Code: 1}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Import result: added=%d skipped=%d renamed=%d replaced=%d\n",
				res.Added, res.Skipped, res.Renamed, res.Replaced)
			return nil
		},
	}
	cmd.Flags().StringVar(&modeFlag, "mode", "merge", "conflict resolution: merge|overwrite|rename")
	rootCmd.AddCommand(cmd)
}
