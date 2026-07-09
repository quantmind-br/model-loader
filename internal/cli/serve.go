package cli

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

func init() {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the headless HTTP proxy in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel, app.AsStateOwner())
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			if host != "" {
				svc.Cfg.Serve.Host = host
			}
			if port != 0 {
				svc.Cfg.Serve.Port = port
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// Health-check bound for a backend swap. Default 360s: a large
			// model (a 35B int4 TP=2 multimodal MoE whose Marlin expert repack
			// alone runs minutes) can take 3+ minutes to boot, past the former
			// fixed 180s that killed such loads mid-init. Override via the
			// [serve].health_check_timeout_sec config key.
			healthCheckTimeout := 360 * time.Second
			if s := svc.Cfg.Serve.HealthCheckTimeoutSec; s > 0 {
				healthCheckTimeout = time.Duration(s) * time.Second
			}

			srv := httpproxy.New(httpproxy.Config{
				Host:                svc.Cfg.Serve.Host,
				Port:                svc.Cfg.Serve.Port,
				HealthCheckTimeout:  healthCheckTimeout,
				MaxBodyBuffer:       8 << 20,
				ShutdownGracePeriod: 10 * time.Second,
			}, httpproxy.Deps{
				ProfileStore: svc.Store,
				ProcessMgr:   svc.Mgr,
				Logger:       svc.Logger,
			})

			if err := srv.Start(ctx); err != nil {
				svc.Logger.Error("serve_start_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "start: %v\n", err)
				return &ExitError{Code: 1}
			}
			displayHost := curlHost(svc.Cfg.Serve.Host)
			fmt.Fprintf(cmd.OutOrStdout(), "Listening on %s:%d (logs: %s)\n",
				svc.Cfg.Serve.Host, svc.Cfg.Serve.Port, svc.Cfg.Paths.LogDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Try: curl http://%s:%d/v1/models\n", displayHost, svc.Cfg.Serve.Port)
			svc.Logger.Info("serve_listening",
				"host", svc.Cfg.Serve.Host, "port", svc.Cfg.Serve.Port)

			<-ctx.Done()
			svc.Logger.Info("serve_signal_received")

			shCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := srv.Stop(shCtx); err != nil {
				svc.Logger.Error("serve_stop_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "stop: %v\n", err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "override bind host")
	cmd.Flags().IntVar(&port, "port", 0, "override bind port")
	rootCmd.AddCommand(cmd)
}

// curlHost returns the host suitable for a curl example. Wildcard bind
// addresses (0.0.0.0, ::, or empty) are shown as 127.0.0.1 since the
// listener is reachable from localhost; literal IPv6 hosts are bracketed.
func curlHost(host string) string {
	switch host {
	case "0.0.0.0", "::", "":
		return "127.0.0.1"
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}
