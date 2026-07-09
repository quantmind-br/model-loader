package cli

import (
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/service/configweb"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// newBenchWebCmd builds `benchmark web`: a read-only browser for saved runs.
// A separate process cannot observe the TUI's in-flight run, so the live
// monitor shows "no run in progress"; live monitoring from the CLI is the job
// of `benchmark run`'s own live renderer. Blocks until the browser done page is
// served or SIGINT/SIGTERM, then exits 0.
func newBenchWebCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "web",
		Short: "Open a read-only web viewer for saved benchmark runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			profiles, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "profile store: %v\n", err)
				return &ExitError{Code: 1}
			}
			viewer := configweb.NewBenchViewer(configweb.BenchViewerDeps{
				Runs:     store,
				Profiles: profiles,
				Live:     nil, // a separate process can't see the TUI's live run
				// UIUX-027: tear down immediately on Ctrl-C — there is no browser
				// /closed round-trip to wait for from a headless CLI.
				ImmediateShutdown: true,
			})
			url, err := viewer.Start()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "start viewer: %v\n", err)
				return &ExitError{Code: 1}
			}
			fmt.Fprintln(cmd.OutOrStdout(), url)
			fmt.Fprintln(cmd.ErrOrStderr(), "serving benchmark viewer — press Ctrl-C to exit")

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			select {
			case <-ctx.Done():
				viewer.Cancel()
				<-viewer.Done()
			case <-viewer.Done():
			}
			return nil
		},
	}
}
