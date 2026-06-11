package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/proxysupervisor"
	"github.com/spf13/cobra"
)

// proxyLoadTimeout bounds a single /_admin/load: the proxy answers only after
// the backend is healthy, and big models can take minutes to load.
const proxyLoadTimeout = 5 * time.Minute

// proxyClient is the subset of *proxysupervisor.Supervisor the lifecycle
// commands depend on; tests substitute a fake.
type proxyClient interface {
	EnsureRunning(ctx context.Context) error
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Unload(ctx context.Context, force bool) (httpproxy.Status, error)
	Status() httpproxy.Status
	BaseURL() string
}

func init() {
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "start <profile>",
		Short: "Load a profile's backend through the HTTP proxy",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(ctx context.Context, out io.Writer, errw io.Writer, svc *app.Services, proxy proxyClient, args []string) error {
			return startInstance(ctx, out, errw, proxy, svc.Store, args[0])
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "stop <pid|id>",
		Short: "Stop a running instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(ctx context.Context, out io.Writer, errw io.Writer, svc *app.Services, proxy proxyClient, args []string) error {
			return stopInstance(ctx, out, proxy, svc.Mgr, args[0])
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "restart <pid|id>",
		Short: "Restart a running instance through the HTTP proxy",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(ctx context.Context, out io.Writer, errw io.Writer, svc *app.Services, proxy proxyClient, args []string) error {
			return restartInstance(ctx, out, errw, proxy, svc.Mgr, svc.Store, args[0])
		}),
	})
}

// instanceLifecycleRunE acquires the single-instance flock before bootstrap and
// fails fast if the TUI/serve holds it, then constructs the proxy supervisor
// (the only client channel to backends) and runs fn. Mirrors benchmark.go.
func instanceLifecycleRunE(fn func(ctx context.Context, out io.Writer, errw io.Writer, svc *app.Services, proxy proxyClient, args []string) error) func(*cobra.Command, []string) error {
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

		supervisor := proxysupervisor.New(proxysupervisor.Config{
			StatePath: filepath.Join(svc.Cfg.Paths.StateDir, "proxy-state.json"),
			LogDir:    svc.Cfg.Paths.LogDir,
			Host:      svc.Cfg.Serve.Host,
			Port:      svc.Cfg.Serve.Port,
			Logger:    svc.Logger,
		})
		if err := supervisor.Reconcile(); err != nil {
			svc.Logger.Error("proxy_reconcile_failed", "err", err)
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		if err := fn(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), svc, supervisor, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

func startInstance(ctx context.Context, out io.Writer, errw io.Writer, proxy proxyClient, store profilestore.Store, ref string) error {
	prof, err := resolveProfileRef(store, ref)
	if err != nil {
		return err
	}
	if err := proxy.EnsureRunning(ctx); err != nil {
		return fmt.Errorf("start http proxy: %w", err)
	}
	fmt.Fprintf(errw, "loading %s via proxy — waiting for backend health (up to %s)…\n", prof.ID, proxyLoadTimeout)
	loadCtx, cancel := context.WithTimeout(ctx, proxyLoadTimeout)
	defer cancel()
	st, err := proxy.Load(loadCtx, prof.ID)
	if err != nil {
		return fmt.Errorf("load profile %s: %w", prof.ID, err)
	}
	if jsonOut {
		return emitJSON(out, st)
	}
	fmt.Fprintf(out, "loaded %s — pid %d — serving at %s\n", st.LoadedProfileID, st.LoadedPID, proxy.BaseURL())
	return nil
}

// proxyOwnsRef reports whether ref names the backend currently loaded behind
// the proxy, by pid or by exact profile id.
func proxyOwnsRef(st httpproxy.Status, ref string) bool {
	if st.LoadedPID == 0 {
		return false
	}
	if pid, err := strconv.Atoi(ref); err == nil && pid == st.LoadedPID {
		return true
	}
	return st.LoadedProfileID != "" && ref == st.LoadedProfileID
}

func stopInstance(ctx context.Context, out io.Writer, proxy proxyClient, mgr processmgr.Manager, ref string) error {
	st := proxy.Status()
	if proxyOwnsRef(st, ref) {
		unloaded, err := proxy.Unload(ctx, false)
		if err != nil {
			return fmt.Errorf("unload: %w", err)
		}
		if jsonOut {
			return emitJSON(out, unloaded)
		}
		fmt.Fprintf(out, "unloaded %s (pid %d)\n", st.LoadedProfileID, st.LoadedPID)
		return nil
	}
	// Orphan path: a process not owned by the proxy is killed directly.
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	if st.LoadedPID != 0 && ri.PID == st.LoadedPID {
		// A profile-prefix ref resolved to the proxy-loaded backend.
		unloaded, err := proxy.Unload(ctx, false)
		if err != nil {
			return fmt.Errorf("unload: %w", err)
		}
		if jsonOut {
			return emitJSON(out, unloaded)
		}
		fmt.Fprintf(out, "unloaded %s (pid %d)\n", st.LoadedProfileID, st.LoadedPID)
		return nil
	}
	if err := refuseKillOnDegradedProxy(proxy, st, ri.PID); err != nil {
		return err
	}
	if err := mgr.Kill(ri.PID); err != nil {
		if errors.Is(err, processmgr.ErrUnknownPID) {
			// The process is already gone — stopping it is a no-op success.
			fmt.Fprintf(out, "pid %d (%s) already exited\n", ri.PID, ri.ProfileID)
			return nil
		}
		return fmt.Errorf("kill: %w", err)
	}
	fmt.Fprintf(out, "stopped pid %d (%s)\n", ri.PID, ri.ProfileID)
	return nil
}

// refuseKillOnDegradedProxy guards the orphan-kill path: never kill a pid
// directly while the proxy might be routing to it. A running proxy whose
// /_status probe failed reports empty loaded fields exactly like a healthy
// proxy with nothing loaded, so we key off the explicit probe-failure marker
// set by proxysupervisor.Status. The probe has a short timeout and can fail
// transiently, so we re-fetch once before refusing.
func refuseKillOnDegradedProxy(proxy proxyClient, st httpproxy.Status, pid int) error {
	if !st.Running || !strings.HasPrefix(st.LastError, "status_probe_failed") {
		return nil
	}
	st = proxy.Status()
	if st.Running && strings.HasPrefix(st.LastError, "status_probe_failed") {
		return fmt.Errorf("proxy is running but its status is unavailable; refusing to kill pid %d directly — retry or stop the proxy first", pid)
	}
	if st.LoadedPID == pid {
		// The refreshed status reveals the pid is the proxy-loaded backend.
		return fmt.Errorf("pid %d is loaded behind the proxy; refusing to kill it directly — retry the command so it goes through the proxy", pid)
	}
	return nil
}

func restartInstance(ctx context.Context, out io.Writer, errw io.Writer, proxy proxyClient, mgr processmgr.Manager, store profilestore.Store, ref string) error {
	st := proxy.Status()
	var profileID string
	proxyOwned := proxyOwnsRef(st, ref)
	if proxyOwned {
		profileID = st.LoadedProfileID
	} else {
		ri, err := resolveInstance(mgr, ref)
		if err != nil {
			return err
		}
		profileID = ri.ProfileID
		if err := refuseKillOnDegradedProxy(proxy, st, ri.PID); err != nil {
			return err
		}
		// Kill the orphan OS process before loading so VRAM is freed first.
		if err := mgr.Kill(ri.PID); err != nil && !errors.Is(err, processmgr.ErrUnknownPID) {
			return fmt.Errorf("kill orphan before restart: %w", err)
		}
	}
	if _, err := store.Get(profileID); err != nil {
		return fmt.Errorf("load profile %s: %w", profileID, err)
	}
	if err := proxy.EnsureRunning(ctx); err != nil {
		return fmt.Errorf("start http proxy: %w", err)
	}
	// For proxy-owned instances, Unload terminates the backend through the proxy.
	// For orphans, we already killed the process above; no Unload needed.
	if proxyOwned {
		fmt.Fprintf(errw, "unloading %s (pid %d)…\n", st.LoadedProfileID, st.LoadedPID)
		if _, err := proxy.Unload(ctx, false); err != nil {
			return fmt.Errorf("unload: %w", err)
		}
	}
	fmt.Fprintf(errw, "loading %s via proxy — waiting for backend health (up to %s)…\n", profileID, proxyLoadTimeout)
	loadCtx, cancel := context.WithTimeout(ctx, proxyLoadTimeout)
	defer cancel()
	newSt, err := proxy.Load(loadCtx, profileID)
	if err != nil {
		return fmt.Errorf("load profile %s: %w", profileID, err)
	}
	if jsonOut {
		return emitJSON(out, newSt)
	}
	fmt.Fprintf(out, "restarted %s — pid %d — serving at %s\n", newSt.LoadedProfileID, newSt.LoadedPID, proxy.BaseURL())
	return nil
}
