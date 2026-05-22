package cli

import (
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/spf13/cobra"
)

func init() {
	var foreground bool
	startCmd := &cobra.Command{
		Use:   "start <profile>",
		Short: "Launch an instance from a profile",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(out io.Writer, svc *app.Services, args []string) error {
			return startInstance(out, svc.Mgr, svc.Store, args[0], foreground)
		}),
	}
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "run in the foreground (default: background)")
	instanceCmd.AddCommand(startCmd)

	instanceCmd.AddCommand(&cobra.Command{
		Use:   "stop <pid|id>",
		Short: "Stop a running instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(out io.Writer, svc *app.Services, args []string) error {
			return stopInstance(out, svc.Mgr, args[0])
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "restart <pid|id>",
		Short: "Restart a running instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(out io.Writer, svc *app.Services, args []string) error {
			return restartInstance(out, svc.Mgr, svc.Store, args[0])
		}),
	})
}

// instanceLifecycleRunE acquires the single-instance flock before bootstrap and
// fails fast if the TUI/serve holds it, then runs fn. Mirrors benchmark.go.
func instanceLifecycleRunE(fn func(out io.Writer, svc *app.Services, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
			return &ExitError{Code: 1}
		}
		release, acquired, lErr := app.AcquireSingleInstanceLock(cfg.Paths.StateDir)
		if lErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "single-instance lock: %v\n", lErr)
		}
		if release != nil {
			defer release()
		}
		if !acquired {
			fmt.Fprintln(cmd.ErrOrStderr(), "another model-loader instance is running (TUI/serve) — close it first, or run on a headless host.")
			return &ExitError{Code: 1}
		}
		svc, err := app.Bootstrap(logLevel)
		if err != nil {
			return &ExitError{Code: 1}
		}
		defer svc.Close()
		if err := fn(cmd.OutOrStdout(), svc, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

func launchMode(foreground bool) processmgr.LaunchMode {
	if foreground {
		return processmgr.LaunchForeground
	}
	return processmgr.LaunchBackground
}

func startInstance(out io.Writer, mgr processmgr.Manager, store profilestore.Store, ref string, foreground bool) error {
	prof, err := resolveProfileRef(store, ref)
	if err != nil {
		return err
	}
	ri, err := mgr.Launch(prof, launchMode(foreground), log.NewAttemptID())
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	if jsonOut {
		return emitJSON(out, ri)
	}
	fmt.Fprintf(out, "started %s — pid %d port %d\n", prof.ID, ri.PID, ri.Port)
	return nil
}

func stopInstance(out io.Writer, mgr processmgr.Manager, ref string) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	if err := mgr.Kill(ri.PID); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	fmt.Fprintf(out, "stopped pid %d (%s)\n", ri.PID, ri.ProfileID)
	return nil
}

func restartInstance(out io.Writer, mgr processmgr.Manager, store profilestore.Store, ref string) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	prof, err := store.Get(ri.ProfileID)
	if err != nil {
		return fmt.Errorf("load profile %s: %w", ri.ProfileID, err)
	}
	if err := mgr.Kill(ri.PID); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	mode := processmgr.LaunchBackground
	if !ri.Background {
		mode = processmgr.LaunchForeground
	}
	newRI, err := mgr.Launch(prof, mode, log.NewAttemptID())
	if err != nil {
		return fmt.Errorf("relaunch: %w", err)
	}
	fmt.Fprintf(out, "restarted %s — old pid %d, new pid %d\n", prof.ID, ri.PID, newRI.PID)
	return nil
}
